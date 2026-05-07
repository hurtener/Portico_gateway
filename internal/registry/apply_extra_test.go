package registry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Phase 9 follow-up: cover the new Phase-9 paths in apply.go more
// thoroughly (Apply error branches, Restart, Logs, MutOp.String).

func TestMutOp_String(t *testing.T) {
	cases := map[registry.MutOp]string{
		registry.MutOpCreate:  "create",
		registry.MutOpUpdate:  "update",
		registry.MutOpDelete:  "delete",
		registry.MutOpRestart: "restart",
		registry.MutOp(99):    "unknown",
	}
	for op, want := range cases {
		if got := op.String(); got != want {
			t.Errorf("MutOp(%d).String() = %q, want %q", op, got, want)
		}
	}
}

func TestApply_RejectsZeroTenant(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	if _, err := r.Apply(context.Background(), "", registry.Mutation{
		Op: registry.MutOpCreate, Server: &registry.ServerSpec{ID: "x"},
	}); err == nil {
		t.Errorf("expected error on empty tenant id")
	}
}

func TestApply_NilRegistry(t *testing.T) {
	var r *registry.Registry
	if _, err := r.Apply(context.Background(), "t", registry.Mutation{}); err == nil {
		t.Errorf("expected error from nil registry")
	}
}

func TestApply_CreateRequiresSpec(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	if _, err := r.Apply(context.Background(), "t", registry.Mutation{Op: registry.MutOpCreate}); err == nil {
		t.Errorf("expected error when create has no spec")
	}
}

func TestApply_UpdateRequiresSpec(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	if _, err := r.Apply(context.Background(), "t", registry.Mutation{Op: registry.MutOpUpdate}); err == nil {
		t.Errorf("expected error when update has no spec")
	}
}

func TestApply_DeleteRequiresID(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	if _, err := r.Apply(context.Background(), "t", registry.Mutation{Op: registry.MutOpDelete}); err == nil {
		t.Errorf("expected error when delete has no id")
	}
}

func TestApply_DeleteByServerSpec(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	enabled := true
	spec := &registry.ServerSpec{ID: "s", Transport: "stdio", Stdio: &registry.StdioSpec{Command: "/bin/true"}, Enabled: &enabled}
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpCreate, Server: spec}); err != nil {
		t.Fatal(err)
	}
	// Pass the spec (not ServerID) to exercise the m.Server.ID fallback.
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpDelete, Server: spec}); err != nil {
		t.Fatal(err)
	}
}

func TestApply_RestartRequiresID(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	if _, err := r.Apply(context.Background(), "t", registry.Mutation{Op: registry.MutOpRestart}); err == nil {
		t.Errorf("expected error when restart has no id")
	}
}

func TestApply_RestartByServerSpec(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	enabled := true
	spec := &registry.ServerSpec{ID: "s", Transport: "stdio", Stdio: &registry.StdioSpec{Command: "/bin/true"}, Enabled: &enabled}
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpCreate, Server: spec}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpRestart, Server: spec, Reason: "ad-hoc"}); err != nil {
		t.Fatal(err)
	}
}

func TestApply_UnknownOp(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	if _, err := r.Apply(context.Background(), "t", registry.Mutation{Op: registry.MutOp(99)}); err == nil {
		t.Errorf("expected error from unknown op")
	}
}

func TestRestart_NilRegistry(t *testing.T) {
	var r *registry.Registry
	if _, err := r.Restart(context.Background(), "t", "s", "r"); err == nil {
		t.Errorf("expected error from nil registry restart")
	}
}

func TestRestart_UnknownServer(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	if _, err := r.Restart(context.Background(), "t", "missing", "r"); !errors.Is(err, ifaces.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLogs_NilRegistry(t *testing.T) {
	var r *registry.Registry
	if _, err := r.Logs(context.Background(), "t", "s", time.Time{}); err == nil {
		t.Errorf("expected error from nil registry logs")
	}
}

func TestLogs_UnknownServer(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	if _, err := r.Logs(context.Background(), "t", "missing", time.Time{}); !errors.Is(err, ifaces.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLogs_ReturnsClosedChannel(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	enabled := true
	spec := &registry.ServerSpec{ID: "s", Transport: "stdio", Stdio: &registry.StdioSpec{Command: "/bin/true"}, Enabled: &enabled}
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpCreate, Server: spec}); err != nil {
		t.Fatal(err)
	}
	ch, err := r.Logs(context.Background(), "acme", "s", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	// Channel should be drained immediately (closed).
	select {
	case _, open := <-ch:
		if open {
			t.Errorf("expected closed channel, got open")
		}
	default:
		t.Errorf("channel should yield zero value (closed), got default")
	}
}
