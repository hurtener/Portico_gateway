package playground

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

type stubExecutor struct {
	status, summary string
	err             error
}

func (s *stubExecutor) Execute(_ context.Context, _ *Session, _ Case) (string, string, error) {
	return s.status, s.summary, s.err
}

func openTestStore(t *testing.T) *sqlite.DB {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "playback.db") + "?cache=shared"
	db, err := sqlite.Open(context.Background(), dsn, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPlayback_HappyPath(t *testing.T) {
	db := openTestStore(t)
	svc := newTestService(t)
	binder := NewSnapshotBinder(&fakeSnapshotResolver{
		live: &snapshots.Snapshot{ID: "live-1", TenantID: "t", OverallHash: "h"},
	})
	pb := NewPlayback(svc, binder, db.Playground(), audit.NopEmitter{}, &stubExecutor{status: "ok", summary: "done"})

	c := Case{ID: "case-1", Kind: "tool_call", Target: "x.y"}
	run, err := pb.Replay(context.Background(), "t", "alice", c)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if run.Status != "ok" || run.Summary != "done" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if run.DriftDetected {
		t.Fatalf("no drift expected")
	}
}

func TestPlayback_DriftDetected_StillExecutes(t *testing.T) {
	db := openTestStore(t)
	svc := newTestService(t)
	binder := NewSnapshotBinder(&fakeSnapshotResolver{
		live: &snapshots.Snapshot{ID: "live-1", TenantID: "t", OverallHash: "live-hash"},
		historical: map[string]*snapshots.Snapshot{
			"hist-1": {ID: "hist-1", TenantID: "t", OverallHash: "old-hash"},
		},
	})
	emitter := &audit.SliceEmitter{}
	pb := NewPlayback(svc, binder, db.Playground(), emitter, &stubExecutor{status: "ok", summary: "done"})

	c := Case{ID: "case-1", Kind: "tool_call", Target: "x.y", SnapshotID: "hist-1"}
	run, err := pb.Replay(context.Background(), "t", "alice", c)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !run.DriftDetected {
		t.Fatalf("expected drift detected")
	}
	if run.Status != "ok" {
		t.Fatalf("expected execution to still succeed, status=%s", run.Status)
	}
	// Drift event emitted.
	saw := false
	for _, ev := range emitter.Events() {
		if ev.Type == "schema.drift" {
			saw = true
			break
		}
	}
	if !saw {
		t.Fatalf("expected schema.drift audit event")
	}
}

func TestPlayback_PolicyDeniesCallsAreRecorded(t *testing.T) {
	db := openTestStore(t)
	svc := newTestService(t)
	pb := NewPlayback(svc, nil, db.Playground(), audit.NopEmitter{}, &stubExecutor{status: "denied", summary: "policy denied"})

	c := Case{ID: "case-1", Kind: "tool_call", Target: "x.y"}
	run, err := pb.Replay(context.Background(), "t", "alice", c)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if run.Status != "denied" {
		t.Fatalf("expected denied status, got %s", run.Status)
	}
}

func TestPlayback_ExecutorErrorRecorded(t *testing.T) {
	db := openTestStore(t)
	svc := newTestService(t)
	pb := NewPlayback(svc, nil, db.Playground(), audit.NopEmitter{}, &stubExecutor{err: errors.New("server_unavailable")})
	c := Case{ID: "c", Kind: "tool_call", Target: "x.y"}
	run, err := pb.Replay(context.Background(), "t", "alice", c)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if run.Status != "error" {
		t.Fatalf("expected error status, got %s", run.Status)
	}
	if run.Summary == "" {
		t.Fatalf("expected error summary")
	}
}

func TestPlayback_NilGuards(t *testing.T) {
	var p *Playback
	if _, err := p.Replay(context.Background(), "t", "u", Case{}); err == nil {
		t.Fatalf("expected error from nil playback")
	}
	pb := NewPlayback(nil, nil, nil, nil, nil)
	if _, err := pb.Replay(context.Background(), "t", "u", Case{}); err == nil {
		t.Fatalf("expected error when deps missing")
	}
}
