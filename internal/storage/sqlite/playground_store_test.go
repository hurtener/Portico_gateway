package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "playground.db") + "?cache=shared"
	db, err := Open(context.Background(), dsn, nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPlaygroundStore_CaseRoundTrip(t *testing.T) {
	db := openTestDB(t)
	store := db.Playground()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	rec := &ifaces.PlaygroundCaseRecord{
		TenantID:    "tenant-a",
		CaseID:      "case-001",
		Name:        "happy path",
		Description: "first case",
		Kind:        "tool_call",
		Target:      "github.list_repos",
		Payload:     json.RawMessage(`{"name":"github.list_repos","arguments":{"owner":"foo"}}`),
		SnapshotID:  "snap-1",
		Tags:        []string{"smoke", "happy"},
		CreatedAt:   now,
		CreatedBy:   "alice",
	}
	if err := store.UpsertCase(ctx, rec); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.GetCase(ctx, rec.TenantID, rec.CaseID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != rec.Name || got.Kind != rec.Kind || got.Target != rec.Target {
		t.Fatalf("mismatch: got %+v", got)
	}
	if got.SnapshotID != rec.SnapshotID || got.CreatedBy != rec.CreatedBy {
		t.Fatalf("mismatch snapshot/createdby: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "smoke" || got.Tags[1] != "happy" {
		t.Fatalf("tags mismatch: %v", got.Tags)
	}
	// Payload canonical-JSON round-trip.
	var pIn, pOut map[string]any
	_ = json.Unmarshal(rec.Payload, &pIn)
	_ = json.Unmarshal(got.Payload, &pOut)
	if pIn["name"] != pOut["name"] {
		t.Fatalf("payload mismatch")
	}
}

func TestPlaygroundStore_ListAndDelete(t *testing.T) {
	db := openTestDB(t)
	store := db.Playground()
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		rec := &ifaces.PlaygroundCaseRecord{
			TenantID:  "tenant-a",
			CaseID:    "case-" + string(rune('a'+i)),
			Name:      "n",
			Kind:      "tool_call",
			Target:    "x.y",
			Payload:   json.RawMessage(`{}`),
			Tags:      []string{"smoke"},
			CreatedAt: now,
		}
		if err := store.UpsertCase(ctx, rec); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	cases, _, err := store.ListCases(ctx, "tenant-a", ifaces.PlaygroundCasesQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(cases) != 3 {
		t.Fatalf("got %d cases, want 3", len(cases))
	}

	// Tag filter.
	got, _, err := store.ListCases(ctx, "tenant-a", ifaces.PlaygroundCasesQuery{Tag: "missing"})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected zero matches for tag 'missing', got %d", len(got))
	}

	// Tenant isolation.
	other, _, err := store.ListCases(ctx, "tenant-b", ifaces.PlaygroundCasesQuery{})
	if err != nil {
		t.Fatalf("list other tenant: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("tenant-b should see zero cases, got %d", len(other))
	}

	// Delete one.
	if err := store.DeleteCase(ctx, "tenant-a", "case-a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetCase(ctx, "tenant-a", "case-a"); !errors.Is(err, ifaces.ErrNotFound) {
		t.Fatalf("expected not-found after delete, got %v", err)
	}
}

func TestPlaygroundStore_RunLifecycle(t *testing.T) {
	db := openTestDB(t)
	store := db.Playground()
	ctx := context.Background()

	rec := &ifaces.PlaygroundRunRecord{
		TenantID:   "tenant-a",
		RunID:      "run-001",
		CaseID:     "case-1",
		SessionID:  "sess-1",
		SnapshotID: "snap-1",
		StartedAt:  time.Now().UTC(),
		Status:     "running",
	}
	if err := store.InsertRun(ctx, rec); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	rec.Status = "ok"
	rec.EndedAt = time.Now().UTC()
	rec.DriftDetected = true
	rec.Summary = "completed"
	if err := store.UpdateRun(ctx, rec); err != nil {
		t.Fatalf("update run: %v", err)
	}

	got, err := store.GetRun(ctx, "tenant-a", "run-001")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "ok" || !got.DriftDetected || got.Summary != "completed" {
		t.Fatalf("update mismatch: %+v", got)
	}

	runs, _, err := store.ListRuns(ctx, "tenant-a", ifaces.PlaygroundRunsQuery{CaseID: "case-1"})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
}

func TestPlaygroundStore_RejectsBadInput(t *testing.T) {
	db := openTestDB(t)
	store := db.Playground()
	ctx := context.Background()

	if err := store.UpsertCase(ctx, &ifaces.PlaygroundCaseRecord{}); err == nil {
		t.Fatalf("expected error for empty record")
	}
	if err := store.InsertRun(ctx, &ifaces.PlaygroundRunRecord{TenantID: "t"}); err == nil {
		t.Fatalf("expected error for missing fields")
	}
	if _, err := store.GetCase(ctx, "", ""); !errors.Is(err, ifaces.ErrNotFound) {
		t.Fatalf("expected not-found for empty input")
	}
}
