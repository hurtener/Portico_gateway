package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func seedExecution(t *testing.T, store ifaces.CodeModeStore, tenant, execID, sessID string) {
	t.Helper()
	if err := store.PutExecution(context.Background(), &ifaces.CodeModeExecution{
		TenantID:    tenant,
		ExecutionID: execID,
		SessionID:   sessID,
		StartedAt:   "2026-06-17T10:00:00Z",
		Status:      ifaces.CodeModeStatusAwaitingApproval,
		SnippetSHA:  "abc123",
		SpanID:      "span-1",
	}); err != nil {
		t.Fatalf("seed execution: %v", err)
	}
}

func newContinuation(tenant, token, execID, sessID string, expires time.Time) *ifaces.CodeModeContinuation {
	return &ifaces.CodeModeContinuation{
		TenantID:           tenant,
		ContinuationToken:  token,
		ExecutionID:        execID,
		SessionID:          sessID,
		SnapshotID:         "snap-1",
		Code:               "result = github.create_issue(repo='r')",
		CachedResultsJSON:  `[{"ok":true}]`,
		AwaitingCallIndex:  1,
		AwaitingApprovalID: "appr-1",
		ClockUnix:          1750156800,
		ExpiresAt:          expires.UTC().Format(time.RFC3339),
	}
}

func TestCodeModeStore_ExecutionRoundTrip(t *testing.T) {
	db := open(t)
	store := db.CodeMode()
	ctx := context.Background()

	seedExecution(t, store, "tenant-a", "exec-1", "sess-1")

	// Update to completed.
	if err := store.UpdateExecutionStatus(ctx, &ifaces.CodeModeExecution{
		TenantID:       "tenant-a",
		ExecutionID:    "exec-1",
		Status:         ifaces.CodeModeStatusCompleted,
		FinishedAt:     "2026-06-17T10:01:00Z",
		ToolCalls:      3,
		TokensSavedEst: 1280,
		OutputRedacted: "ok",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := store.ListExecutions(ctx, "tenant-a", "sess-1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 execution, got %d", len(got))
	}
	e := got[0]
	if e.Status != ifaces.CodeModeStatusCompleted || e.ToolCalls != 3 || e.TokensSavedEst != 1280 || e.FinishedAt != "2026-06-17T10:01:00Z" || e.OutputRedacted != "ok" {
		t.Errorf("unexpected execution: %+v", e)
	}
}

func TestCodeModeStore_ListExecutions_TenantIsolated(t *testing.T) {
	db := open(t)
	store := db.CodeMode()
	ctx := context.Background()

	seedExecution(t, store, "tenant-a", "exec-a", "sess-a")
	seedExecution(t, store, "tenant-b", "exec-b", "sess-b")

	a, err := store.ListExecutions(ctx, "tenant-a", "", 10)
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	if len(a) != 1 || a[0].ExecutionID != "exec-a" {
		t.Fatalf("tenant-a leak: %+v", a)
	}
}

func TestCodeModeStore_ConsumeContinuation_SingleUse(t *testing.T) {
	db := open(t)
	store := db.CodeMode()
	ctx := context.Background()
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)

	seedExecution(t, store, "tenant-a", "exec-1", "sess-1")
	if err := store.PutContinuation(ctx, newContinuation("tenant-a", "tok-1", "exec-1", "sess-1", now.Add(24*time.Hour))); err != nil {
		t.Fatalf("put continuation: %v", err)
	}

	got, err := store.ConsumeContinuation(ctx, "tenant-a", "tok-1", now)
	if err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if got.Code == "" || got.AwaitingApprovalID != "appr-1" || got.AwaitingCallIndex != 1 || got.CachedResultsJSON != `[{"ok":true}]` {
		t.Errorf("consume returned wrong row: %+v", got)
	}

	// Second consume must fail closed: double_resume guard.
	if _, err := store.ConsumeContinuation(ctx, "tenant-a", "tok-1", now); !errors.Is(err, ifaces.ErrContinuationConsumed) {
		t.Fatalf("want ErrContinuationConsumed on second consume, got %v", err)
	}
}

func TestCodeModeStore_ConsumeContinuation_Expired(t *testing.T) {
	db := open(t)
	store := db.CodeMode()
	ctx := context.Background()
	created := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)

	seedExecution(t, store, "tenant-a", "exec-1", "sess-1")
	if err := store.PutContinuation(ctx, newContinuation("tenant-a", "tok-exp", "exec-1", "sess-1", created.Add(time.Hour))); err != nil {
		t.Fatalf("put continuation: %v", err)
	}

	// Resume two hours later — past the expiry.
	later := created.Add(2 * time.Hour)
	if _, err := store.ConsumeContinuation(ctx, "tenant-a", "tok-exp", later); !errors.Is(err, ifaces.ErrContinuationExpired) {
		t.Fatalf("want ErrContinuationExpired, got %v", err)
	}
	// The expired row is deleted, so a retry sees not-found.
	if _, err := store.ConsumeContinuation(ctx, "tenant-a", "tok-exp", later); !errors.Is(err, ifaces.ErrContinuationNotFound) {
		t.Fatalf("want ErrContinuationNotFound after expiry delete, got %v", err)
	}
}

func TestCodeModeStore_ConsumeContinuation_CrossTenantInvisible(t *testing.T) {
	db := open(t)
	store := db.CodeMode()
	ctx := context.Background()
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)

	seedExecution(t, store, "tenant-a", "exec-1", "sess-1")
	if err := store.PutContinuation(ctx, newContinuation("tenant-a", "tok-secret", "exec-1", "sess-1", now.Add(24*time.Hour))); err != nil {
		t.Fatalf("put continuation: %v", err)
	}

	// Tenant B cannot see (or consume) tenant A's token — class C5.
	if _, err := store.ConsumeContinuation(ctx, "tenant-b", "tok-secret", now); !errors.Is(err, ifaces.ErrContinuationNotFound) {
		t.Fatalf("want ErrContinuationNotFound for cross-tenant, got %v", err)
	}
	// And tenant A can still consume it (proving B's probe did not mutate it).
	if _, err := store.ConsumeContinuation(ctx, "tenant-a", "tok-secret", now); err != nil {
		t.Fatalf("tenant-a consume after cross-tenant probe: %v", err)
	}
}

func TestCodeModeStore_ConsumeContinuation_NotFound(t *testing.T) {
	db := open(t)
	store := db.CodeMode()
	ctx := context.Background()
	if _, err := store.ConsumeContinuation(ctx, "tenant-a", "nope", time.Now()); !errors.Is(err, ifaces.ErrContinuationNotFound) {
		t.Fatalf("want ErrContinuationNotFound, got %v", err)
	}
}

func TestCodeModeStore_DeleteExpiredContinuations(t *testing.T) {
	db := open(t)
	store := db.CodeMode()
	ctx := context.Background()
	base := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)

	seedExecution(t, store, "tenant-a", "exec-1", "sess-1")
	// One expired, one live.
	if err := store.PutContinuation(ctx, newContinuation("tenant-a", "tok-old", "exec-1", "sess-1", base.Add(-time.Hour))); err != nil {
		t.Fatalf("put old: %v", err)
	}
	if err := store.PutContinuation(ctx, newContinuation("tenant-a", "tok-new", "exec-1", "sess-1", base.Add(24*time.Hour))); err != nil {
		t.Fatalf("put new: %v", err)
	}

	n, err := store.DeleteExpiredContinuations(ctx, base)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 swept, got %d", n)
	}
	// The live one survives and is still consumable.
	if _, err := store.ConsumeContinuation(ctx, "tenant-a", "tok-new", base); err != nil {
		t.Fatalf("live continuation consumed away: %v", err)
	}
}

func TestCodeModeStore_PutContinuation_RequiresFields(t *testing.T) {
	db := open(t)
	store := db.CodeMode()
	ctx := context.Background()
	// Missing snapshot_id.
	err := store.PutContinuation(ctx, &ifaces.CodeModeContinuation{
		TenantID:           "tenant-a",
		ContinuationToken:  "tok",
		ExecutionID:        "exec-1",
		SessionID:          "sess-1",
		AwaitingApprovalID: "appr-1",
	})
	if err == nil {
		t.Fatal("want error for missing snapshot_id")
	}
}
