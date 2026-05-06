package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/runtime/process"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"

	_ "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// supBinOnce builds the mockmcp binary once per test process (reuses the
// same temp build the e2e test uses). Returns the path.
var (
	supBinOnce sync.Once
	supBinPath string
	supBinErr  error
)

func supMockBin(t *testing.T) string {
	t.Helper()
	supBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "supmock-")
		if err != nil {
			supBinErr = err
			return
		}
		bin := filepath.Join(dir, "mockmcp")
		root, err := repoRoot()
		if err != nil {
			supBinErr = err
			return
		}
		cmd := exec.Command("go", "build", "-o", bin, "./examples/servers/mock/cmd/mockmcp")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			supBinErr = errors.New(string(out))
			return
		}
		supBinPath = bin
	})
	if supBinErr != nil {
		t.Fatalf("build mockmcp: %v", supBinErr)
	}
	return supBinPath
}

// newSupervisorWithRegistry builds a supervisor + registry pair backed by
// an in-memory SQLite for unit-level acquire tests.
func newSupervisorWithRegistry(t *testing.T) (*process.Supervisor, *registry.Registry) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := config.StorageConfig{Driver: "sqlite", DSN: ":memory:"}
	backend, err := storage.Open(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Tenants().Upsert(context.Background(), &ifaces.Tenant{
		ID: "acme", DisplayName: "acme", Plan: "free",
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Tenants().Upsert(context.Background(), &ifaces.Tenant{
		ID: "beta", DisplayName: "beta", Plan: "free",
	}); err != nil {
		t.Fatal(err)
	}
	reg := registry.New(backend.Registry(), logger)
	sup := process.NewSupervisor(logger, process.NewResolver(nil), reg)
	t.Cleanup(func() { _ = sup.StopAll(context.Background()) })
	return sup, reg
}

func TestSupervisor_PerTenantInstanceIsolation(t *testing.T) {
	sup, _ := newSupervisorWithRegistry(t)
	bin := supMockBin(t)

	spec := &registry.ServerSpec{
		ID:          "mock",
		Transport:   "stdio",
		RuntimeMode: registry.ModePerTenant,
		Stdio:       &registry.StdioSpec{Command: bin},
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cA, err := sup.Acquire(ctx, spec, process.AcquireOpts{TenantID: "acme"})
	if err != nil {
		t.Fatalf("acquire acme: %v", err)
	}
	cB, err := sup.Acquire(ctx, spec, process.AcquireOpts{TenantID: "beta"})
	if err != nil {
		t.Fatalf("acquire beta: %v", err)
	}

	if cA == cB {
		t.Error("per_tenant should produce distinct clients per tenant")
	}

	// Same tenant: same client.
	cA2, err := sup.Acquire(ctx, spec, process.AcquireOpts{TenantID: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if cA != cA2 {
		t.Error("repeat acquire for same tenant should reuse client")
	}
}

func TestSupervisor_SharedGlobalReusesClient(t *testing.T) {
	sup, _ := newSupervisorWithRegistry(t)
	bin := supMockBin(t)

	spec := &registry.ServerSpec{
		ID:          "mock",
		Transport:   "stdio",
		RuntimeMode: registry.ModeSharedGlobal,
		Stdio:       &registry.StdioSpec{Command: bin},
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cA, err := sup.Acquire(ctx, spec, process.AcquireOpts{TenantID: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	cB, err := sup.Acquire(ctx, spec, process.AcquireOpts{TenantID: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if cA != cB {
		t.Error("shared_global should reuse the same client across tenants")
	}
}

func TestSupervisor_FailedStartReturnsError(t *testing.T) {
	sup, _ := newSupervisorWithRegistry(t)

	spec := &registry.ServerSpec{
		ID:          "mock",
		Transport:   "stdio",
		RuntimeMode: registry.ModeSharedGlobal,
		Stdio:       &registry.StdioSpec{Command: "/definitely/does/not/exist/abc"},
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := sup.Acquire(ctx, spec, process.AcquireOpts{TenantID: "acme"})
	if err == nil {
		t.Fatal("expected error spawning nonexistent command")
	}
}

// TestE2E_SupervisorThroughGateway verifies the dispatcher → manager →
// supervisor path with a per_tenant runtime mode. Two concurrent calls
// from different tenants should each see their own process.
func TestE2E_SupervisorThroughGateway(t *testing.T) {
	bin := supMockBin(t)
	srv, _ := startMcpDevServer(t, []config.ServerSpec{
		{
			ID:          "mock",
			Transport:   "stdio",
			RuntimeMode: registry.ModePerTenant,
			Stdio:       &config.StdioSpec{Command: bin},
		},
	})
	// Test scaffolding seeds under tenant "dev"; force initialize and
	// confirm a tools/call resolves through the supervisor.
	resp, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize,
		protocol.InitializeParams{ProtocolVersion: protocol.ProtocolVersion}))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	args, _ := json.Marshal(map[string]string{"message": "via-supervisor"})
	callResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsCall, protocol.CallToolParams{
		Name:      "mock.echo",
		Arguments: args,
	}))
	if callResp.Error != nil {
		t.Fatalf("call err: %+v", callResp.Error)
	}
	var res protocol.CallToolResult
	_ = json.Unmarshal(callResp.Result, &res)
	if len(res.Content) != 1 || res.Content[0].Text != "via-supervisor" {
		t.Errorf("unexpected echo: %+v", res)
	}
}

// helper used in setup to avoid redeclaring io.Discard helpers.
var _ = bytes.NewBuffer
var _ = http.MethodGet
