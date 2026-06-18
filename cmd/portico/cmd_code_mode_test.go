package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/storage"
	sqlitestorage "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// seedCodeModeDB opens a migrated temp DB and inserts one catalog snapshot for a
// session, returning the DSN. The CLI opens its own connection to the same file.
func seedCodeModeDB(t *testing.T) string {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/cm.db"
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	be, err := storage.Open(context.Background(), config.StorageConfig{Driver: "sqlite", DSN: dsn}, logger)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db := be.(*sqlitestorage.DB).SQL()

	// catalog_snapshots.tenant_id has a FK to tenants(id).
	if _, err := db.Exec(`INSERT INTO tenants(id, display_name, plan) VALUES ('acme','Acme','enterprise')`); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	snap := snapshots.Snapshot{
		ID:       "snap-cli",
		TenantID: "acme",
		Tools: []snapshots.ToolInfo{{
			NamespacedName: "github.list_issues",
			ServerID:       "github",
			Description:    "List issues",
			InputSchema:    json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"}},"required":["repo"]}`),
		}},
	}
	payload, _ := json.Marshal(snap)
	if _, err := db.Exec(`INSERT INTO catalog_snapshots(id, tenant_id, session_id, payload_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		"snap-cli", "acme", "sess-cli", string(payload), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	_ = be.Close()
	return dsn
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it wrote.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := fn()
	_ = w.Close()
	os.Stdout = orig
	out, _ := io.ReadAll(r)
	return string(out), err
}

func TestCodeModeRender_DumpsStubs(t *testing.T) {
	dsn := seedCodeModeDB(t)
	out, err := captureStdout(t, func() error {
		return runCodeModeRender(context.Background(), []string{"--session", "sess-cli", "--tenant", "acme", "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "servers/github.pyi") || !strings.Contains(out, "def list_issues") {
		t.Errorf("render output missing stub: %s", out)
	}
}

func TestCodeModeRender_RequiresSession(t *testing.T) {
	if err := runCodeModeRender(context.Background(), []string{"--dsn", "file:/tmp/none.db"}); err == nil {
		t.Fatal("expected error without --session")
	}
}

func TestCodeModeExec_PureCompute(t *testing.T) {
	dsn := seedCodeModeDB(t)
	out, err := captureStdout(t, func() error {
		return runCodeModeExec(context.Background(), []string{"--session", "sess-cli", "--dsn", dsn, "--code", "result = 6 * 7"})
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(out, `"result": 42`) && !strings.Contains(out, `"result":42`) {
		t.Errorf("exec output missing result: %s", out)
	}
}

func TestCodeModeExec_UnsafeRejected(t *testing.T) {
	dsn := seedCodeModeDB(t)
	_, err := captureStdout(t, func() error {
		return runCodeModeExec(context.Background(), []string{"--session", "sess-cli", "--dsn", dsn, "--code", `load("os","x")` + "\nresult = 1"})
	})
	if err == nil || !strings.Contains(err.Error(), "code_mode.unsafe_call") {
		t.Fatalf("want unsafe_call error, got %v", err)
	}
}

func TestCodeModeExec_ToolCallOfflineError(t *testing.T) {
	dsn := seedCodeModeDB(t)
	_, err := captureStdout(t, func() error {
		return runCodeModeExec(context.Background(), []string{"--session", "sess-cli", "--dsn", dsn, "--code", `result = github.list_issues(repo="x")`})
	})
	if err == nil || !strings.Contains(err.Error(), "code_mode.tool_error") {
		t.Fatalf("want tool_error (offline dispatcher), got %v", err)
	}
}

func TestOfflineDispatcher_FailsClosed(t *testing.T) {
	_, perr := offlineDispatcher{}.DispatchToolCall(context.Background(), "github.x", nil)
	if perr == nil || perr.Code != protocol.ErrUpstreamUnavailable {
		t.Fatalf("offline dispatcher must fail closed, got %v", perr)
	}
}
