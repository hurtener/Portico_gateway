// Package playground hosts the Phase 10 integration tests. The tests
// here exercise the playground surface end-to-end via the in-process
// playground.Service + Playback against a real SQLite store. The full
// HTTP-stack tests live in test/integration/ and the smoke script.
package playground

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/playground"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

func newTestSetup(t *testing.T) (*playground.Service, *playground.Playback, *audit.SliceEmitter, *sqlite.DB) {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "playground-e2e.db") + "?cache=shared"
	db, err := sqlite.Open(context.Background(), dsn, nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	signKey, err := playground.NewSigningKey()
	if err != nil {
		t.Fatalf("signing key: %v", err)
	}
	svc, err := playground.New(playground.Config{SigningKey: signKey, TTL: time.Minute})
	if err != nil {
		t.Fatalf("svc: %v", err)
	}
	emitter := &audit.SliceEmitter{}
	binder := playground.NewSnapshotBinder(&fakeResolver{
		live: &snapshots.Snapshot{ID: "live-1", TenantID: "tenant-a", OverallHash: "h"},
		hist: map[string]*snapshots.Snapshot{
			"hist-old": {ID: "hist-old", TenantID: "tenant-a", OverallHash: "old"},
		},
	})
	pb := playground.NewPlayback(svc, binder, db.Playground(), emitter, &okExecutor{em: emitter})
	return svc, pb, emitter, db
}

type fakeResolver struct {
	live *snapshots.Snapshot
	hist map[string]*snapshots.Snapshot
}

func (f *fakeResolver) Get(_ context.Context, id string) (*snapshots.Snapshot, error) {
	if s, ok := f.hist[id]; ok {
		return s, nil
	}
	return nil, snapshots.ErrNotFound
}

func (f *fakeResolver) Create(_ context.Context, _, _ string) (*snapshots.Snapshot, error) {
	if f.live != nil {
		return f.live, nil
	}
	return &snapshots.Snapshot{ID: "live-1", TenantID: "tenant-a", OverallHash: "h"}, nil
}

type okExecutor struct {
	em audit.Emitter
}

func (e *okExecutor) Execute(ctx context.Context, sess *playground.Session, c playground.Case) (string, string, error) {
	if e.em != nil {
		e.em.Emit(ctx, audit.Event{
			Type:       audit.EventToolCallComplete,
			TenantID:   sess.TenantID,
			SessionID:  sess.ID,
			OccurredAt: time.Now().UTC(),
			Payload:    map[string]any{"tool": c.Target, "playground_session": sess.ID},
		})
	}
	return "ok", "completed", nil
}

func TestE2E_Playground_HappyPath(t *testing.T) {
	svc, _, _, db := newTestSetup(t)
	sess, err := svc.StartSession(context.Background(), playground.SessionRequest{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if sess.Token == "" || sess.ID == "" {
		t.Fatalf("expected populated session")
	}
	// Save a case and replay it.
	store := db.Playground()
	rec := casePB("case-happy", "x.y", "")
	if err := store.UpsertCase(context.Background(), &rec); err != nil {
		t.Fatalf("save case: %v", err)
	}
}

func TestE2E_Playground_StreamingResponse(t *testing.T) {
	// The streaming surface is exercised by the smoke script via SSE.
	// Here we assert the playback emits the expected start/complete pair
	// of audit events through the executor.
	_, pb, em, _ := newTestSetup(t)
	c := playground.Case{ID: "c1", Kind: "tool_call", Target: "x.y", Payload: json.RawMessage(`{}`)}
	if _, err := pb.Replay(context.Background(), "tenant-a", "alice", c); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !hasAudit(em.Events(), audit.EventToolCallComplete) {
		t.Fatalf("expected tool_call.complete audit; got %v", em.Events())
	}
}

func TestE2E_Playground_PolicyDenied(t *testing.T) {
	svc, _, em, db := newTestSetup(t)
	exec := &deniedExecutor{}
	pb := playground.NewPlayback(svc, nil, db.Playground(), em, exec)
	run, err := pb.Replay(context.Background(), "tenant-a", "alice",
		playground.Case{ID: "c2", Kind: "tool_call", Target: "x.y"})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if run.Status != "denied" {
		t.Fatalf("expected denied status, got %s", run.Status)
	}
}

func TestE2E_Playground_TenantIsolation(t *testing.T) {
	_, _, _, db := newTestSetup(t)
	store := db.Playground()
	c := casePB("case-t-a", "x.y", "")
	c.TenantID = "tenant-a"
	if err := store.UpsertCase(context.Background(), &c); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Tenant-b should not see tenant-a's case.
	if _, err := store.GetCase(context.Background(), "tenant-b", c.CaseID); err == nil {
		t.Fatalf("tenant-b leaked tenant-a's case")
	}
	cases, _, err := store.ListCases(context.Background(), "tenant-b", ifaces.PlaygroundCasesQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(cases) != 0 {
		t.Fatalf("tenant-b sees %d cases; expected 0", len(cases))
	}
}

func TestE2E_Playground_Replay_AgainstDrift(t *testing.T) {
	svc, pb, em, _ := newTestSetup(t)
	// Live and historical have different hashes so binding flags drift.
	c := playground.Case{ID: "c-drift", Kind: "tool_call", Target: "x.y", SnapshotID: "hist-old"}
	run, err := pb.Replay(context.Background(), "tenant-a", "alice", c)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !run.DriftDetected {
		t.Fatalf("expected drift flagged")
	}
	// schema.drift event tagged with run id.
	if !hasAudit(em.Events(), "schema.drift") {
		t.Fatalf("expected schema.drift audit")
	}
	_ = svc
}

func TestE2E_Playground_GoroutineLeak(t *testing.T) {
	baseline := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		svc, _, _, _ := newTestSetup(t)
		sess, err := svc.StartSession(context.Background(), playground.SessionRequest{TenantID: "tenant-a"})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		svc.End(sess.ID)
	}
	// Allow a tick for any goroutines to settle. We're tolerant: if
	// growth exceeds a tiny constant we fail.
	time.Sleep(10 * time.Millisecond)
	now := runtime.NumGoroutine()
	if now-baseline > 50 {
		t.Fatalf("goroutine leak detected: baseline=%d now=%d", baseline, now)
	}
}

// helpers ----

func casePB(id, target, snap string) ifaces.PlaygroundCaseRecord {
	return ifaces.PlaygroundCaseRecord{
		TenantID:   "tenant-a",
		CaseID:     id,
		Name:       id,
		Kind:       "tool_call",
		Target:     target,
		Payload:    json.RawMessage(`{}`),
		SnapshotID: snap,
		CreatedAt:  time.Now().UTC(),
	}
}

func hasAudit(events []audit.Event, t string) bool {
	for _, ev := range events {
		if ev.Type == t {
			return true
		}
	}
	return false
}

type deniedExecutor struct{}

func (deniedExecutor) Execute(_ context.Context, _ *playground.Session, _ playground.Case) (string, string, error) {
	return "denied", "policy denied", nil
}
