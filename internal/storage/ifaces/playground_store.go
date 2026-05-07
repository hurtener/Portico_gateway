package ifaces

import (
	"context"
	"encoding/json"
	"time"
)

// PlaygroundCaseRecord is the persistence shape for a saved playground
// case. Payload is canonical JSON of the call shape; Tags is a canonical
// JSON array.
type PlaygroundCaseRecord struct {
	TenantID    string          `json:"tenant_id"`
	CaseID      string          `json:"case_id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Kind        string          `json:"kind"`
	Target      string          `json:"target"`
	Payload     json.RawMessage `json:"payload"`
	SnapshotID  string          `json:"snapshot_id,omitempty"`
	Tags        []string        `json:"tags"`
	CreatedAt   time.Time       `json:"created_at"`
	CreatedBy   string          `json:"created_by,omitempty"`
}

// PlaygroundRunRecord is the persistence shape for a run row. Status is
// one of "running" | "ok" | "error" | "denied".
type PlaygroundRunRecord struct {
	TenantID      string    `json:"tenant_id"`
	RunID         string    `json:"run_id"`
	CaseID        string    `json:"case_id,omitempty"`
	SessionID     string    `json:"session_id"`
	SnapshotID    string    `json:"snapshot_id"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at,omitempty"`
	Status        string    `json:"status"`
	DriftDetected bool      `json:"drift_detected"`
	Summary       string    `json:"summary,omitempty"`
}

// PlaygroundCasesQuery filters PlaygroundStore.ListCases.
type PlaygroundCasesQuery struct {
	Limit  int
	Cursor string
	Tag    string
	Kind   string
}

// PlaygroundRunsQuery filters PlaygroundStore.ListRuns.
type PlaygroundRunsQuery struct {
	CaseID string
	Limit  int
	Cursor string
}

// PlaygroundStore is the persistence seam for Phase 10 saved cases + runs.
// All methods are tenant-scoped; ErrNotFound flags missing rows.
type PlaygroundStore interface {
	// Cases.
	UpsertCase(ctx context.Context, c *PlaygroundCaseRecord) error
	GetCase(ctx context.Context, tenantID, caseID string) (*PlaygroundCaseRecord, error)
	ListCases(ctx context.Context, tenantID string, q PlaygroundCasesQuery) ([]*PlaygroundCaseRecord, string, error)
	DeleteCase(ctx context.Context, tenantID, caseID string) error

	// Runs.
	InsertRun(ctx context.Context, r *PlaygroundRunRecord) error
	UpdateRun(ctx context.Context, r *PlaygroundRunRecord) error
	GetRun(ctx context.Context, tenantID, runID string) (*PlaygroundRunRecord, error)
	ListRuns(ctx context.Context, tenantID string, q PlaygroundRunsQuery) ([]*PlaygroundRunRecord, string, error)
}
