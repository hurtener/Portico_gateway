package process

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	"github.com/hurtener/Portico_gateway/internal/registry"
)

// fakeClient is the minimal southbound surface the health checker needs.
type fakeClient struct {
	pingCount  atomic.Int32
	failAfter  int32
	pingErrMsg string
}

func (f *fakeClient) Ping(_ context.Context) error {
	n := f.pingCount.Add(1)
	if f.failAfter > 0 && n >= f.failAfter {
		return errors.New(f.pingErrMsg)
	}
	return nil
}

func newTestInstance(t *testing.T, interval, timeout time.Duration, client clientSnapshot) *instance {
	t.Helper()
	spec := &registry.ServerSpec{
		ID:          "fake",
		Transport:   "stdio",
		RuntimeMode: registry.ModeSharedGlobal,
		Stdio:       &registry.StdioSpec{Command: "/bin/true"},
		Health: registry.HealthSpec{
			PingInterval: registry.Duration(interval),
			PingTimeout:  registry.Duration(timeout),
		},
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	inst := &instance{
		id:    "inst_test",
		spec:  spec,
		state: StateRunning,
	}
	// Casting through the interface to the concrete client field is awkward;
	// the supervisor stores southbound.Client. Here we sidestep by setting
	// a type-compatible probe target via the snapshot test seam.
	inst.client = wrapFakeAsClient(client)
	return inst
}

// fakeClientWrapper exposes a fakeClient through the southbound.Client
// interface contract — only Ping + Close are exercised by the health
// checker; the rest return zero values so accidental misuse is at least
// non-panicking.
type fakeClientWrapper struct {
	probe clientSnapshot
}

func wrapFakeAsClient(c clientSnapshot) southbound.Client { return &fakeClientWrapper{probe: c} }

func (w *fakeClientWrapper) Start(_ context.Context) error { return nil }
func (w *fakeClientWrapper) Initialized() bool             { return true }
func (w *fakeClientWrapper) Capabilities() protocol.ServerCapabilities {
	return protocol.ServerCapabilities{}
}
func (w *fakeClientWrapper) ServerInfo() protocol.Implementation {
	return protocol.Implementation{}
}
func (w *fakeClientWrapper) Ping(ctx context.Context) error { return w.probe.Ping(ctx) }
func (w *fakeClientWrapper) ListTools(_ context.Context) ([]protocol.Tool, error) {
	return nil, nil
}
func (w *fakeClientWrapper) CallTool(_ context.Context, _ string, _ json.RawMessage, _ json.RawMessage, _ southbound.ProgressCallback) (*protocol.CallToolResult, error) {
	return nil, nil
}
func (w *fakeClientWrapper) ListResources(_ context.Context, _ string) ([]protocol.Resource, string, error) {
	return nil, "", nil
}
func (w *fakeClientWrapper) ListResourceTemplates(_ context.Context, _ string) ([]protocol.ResourceTemplate, string, error) {
	return nil, "", nil
}
func (w *fakeClientWrapper) ReadResource(_ context.Context, _ string) (*protocol.ReadResourceResult, error) {
	return nil, nil
}
func (w *fakeClientWrapper) SubscribeResource(_ context.Context, _ string) error   { return nil }
func (w *fakeClientWrapper) UnsubscribeResource(_ context.Context, _ string) error { return nil }
func (w *fakeClientWrapper) ListPrompts(_ context.Context, _ string) ([]protocol.Prompt, string, error) {
	return nil, "", nil
}
func (w *fakeClientWrapper) GetPrompt(_ context.Context, _ string, _ map[string]string) (*protocol.GetPromptResult, error) {
	return nil, nil
}
func (w *fakeClientWrapper) Notifications() <-chan protocol.Notification {
	ch := make(chan protocol.Notification)
	return ch
}
func (w *fakeClientWrapper) Close(_ context.Context) error { return nil }

func TestHealthChecker_HealthyClientNeverMarksCrashed(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	sup := NewSupervisor(logger, NewResolver(nil), nil)
	t.Cleanup(func() { _ = sup.StopAll(context.Background()) })

	inst := newTestInstance(t, 30*time.Millisecond, 100*time.Millisecond, &fakeClient{})
	sup.health.Track(inst)

	time.Sleep(150 * time.Millisecond)
	inst.mu.Lock()
	state := inst.state
	inst.mu.Unlock()
	if state != StateRunning {
		t.Errorf("healthy client transitioned to %s", state)
	}
}

func TestHealthChecker_FailingClientMarksCrashed(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	sup := NewSupervisor(logger, NewResolver(nil), nil)
	t.Cleanup(func() { _ = sup.StopAll(context.Background()) })

	inst := newTestInstance(t, 30*time.Millisecond, 100*time.Millisecond,
		&fakeClient{failAfter: 1, pingErrMsg: "down"})
	sup.health.Track(inst)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inst.mu.Lock()
		state := inst.state
		inst.mu.Unlock()
		if state == StateCrashed {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("failing client did not transition to crashed within 2s")
}

func TestHealthChecker_DisabledIntervalIsNoop(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	sup := NewSupervisor(logger, NewResolver(nil), nil)
	t.Cleanup(func() { _ = sup.StopAll(context.Background()) })

	fc := &fakeClient{}
	inst := newTestInstance(t, 0, 100*time.Millisecond, fc)
	sup.health.Track(inst)
	time.Sleep(80 * time.Millisecond)
	if fc.pingCount.Load() != 0 {
		t.Errorf("disabled interval should not probe; got %d pings", fc.pingCount.Load())
	}
}
