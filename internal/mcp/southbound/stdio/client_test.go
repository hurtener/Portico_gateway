package stdio_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound/stdio"
)

// buildMock builds the mockmcp binary into a temp dir and returns its path.
// Cached per-process so multiple tests don't re-link.
var (
	mockOnce sync.Once
	mockBin  string
	mockErr  error
)

func mockmcpBinary(t *testing.T) string {
	t.Helper()
	mockOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mockmcp-build-")
		if err != nil {
			mockErr = err
			return
		}
		bin := filepath.Join(dir, "mockmcp")
		// Find module root by walking up from this test's dir until we hit go.mod.
		root, err := moduleRoot()
		if err != nil {
			mockErr = err
			return
		}
		cmd := exec.Command("go", "build", "-o", bin, "./examples/servers/mock/cmd/mockmcp")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			mockErr = errors.New(string(out))
			return
		}
		mockBin = bin
	})
	if mockErr != nil {
		t.Fatalf("build mockmcp: %v", mockErr)
	}
	return mockBin
}

func moduleRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

func newStdioClient(t *testing.T, args ...string) *stdio.Client {
	t.Helper()
	bin := mockmcpBinary(t)
	c := stdio.New(stdio.Config{
		ServerID:     "mock",
		Command:      bin,
		Args:         args,
		StartTimeout: 5 * time.Second,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() {
		_ = c.Close(context.Background())
	})
	return c
}

func TestStdioClient_InitializeAndListTools(t *testing.T) {
	c := newStdioClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !c.Initialized() {
		t.Fatal("not initialized after Start")
	}
	if c.ServerInfo().Name != "mockmcp" {
		t.Errorf("server name = %q", c.ServerInfo().Name)
	}
	if c.Capabilities().Tools == nil {
		t.Error("expected tools capability")
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) < 4 {
		t.Errorf("expected >=4 tools, got %d", len(tools))
	}
	want := map[string]bool{"echo": false, "add": false, "slow": false, "broken": false}
	for _, tt := range tools {
		want[tt.Name] = true
	}
	for n, ok := range want {
		if !ok {
			t.Errorf("missing tool %q", n)
		}
	}
}

func TestStdioClient_CallTool(t *testing.T) {
	c := newStdioClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"message": "hello"})
	res, err := c.CallTool(ctx, "echo", args, nil, nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "hello" {
		t.Errorf("content = %+v", res.Content)
	}
}

func TestStdioClient_CallToolProgress(t *testing.T) {
	c := newStdioClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]int{"duration_ms": 200})
	var (
		progressMu sync.Mutex
		progress   []protocol.ProgressParams
	)
	cb := southbound.ProgressCallback(func(p protocol.ProgressParams) {
		progressMu.Lock()
		progress = append(progress, p)
		progressMu.Unlock()
	})
	token := json.RawMessage(`"tok-1"`)
	res, err := c.CallTool(ctx, "slow", args, token, cb)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("missing result content")
	}
	progressMu.Lock()
	defer progressMu.Unlock()
	if len(progress) < 2 {
		t.Errorf("expected >=2 progress events, got %d", len(progress))
	}
}

func TestStdioClient_Cancellation(t *testing.T) {
	c := newStdioClient(t)
	startCtx, cancelStart := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStart()
	if err := c.Start(startCtx); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]int{"duration_ms": 5000})
	callCtx, cancelCall := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.CallTool(callCtx, "slow", args, nil, nil)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancelCall()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CallTool did not return after cancel within 2s")
	}
}
