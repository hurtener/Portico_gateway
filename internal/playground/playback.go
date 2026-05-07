package playground

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Case is the shape Replay consumes — same as the saved-cases REST DTO.
type Case struct {
	ID         string
	Name       string
	Kind       string // tool_call | resource_read | prompt_get
	Target     string
	Payload    json.RawMessage
	SnapshotID string
	Tags       []string
}

// Run mirrors playground_runs schema.
type Run struct {
	ID            string
	CaseID        string
	SessionID     string
	SnapshotID    string
	Status        string
	DriftDetected bool
	Summary       string
	StartedAt     time.Time
	EndedAt       time.Time
}

// Executor is the seam Replay calls to actually issue the call. The real
// implementation in cmd/portico routes through the dispatcher. Tests
// pass a fake.
type Executor interface {
	Execute(ctx context.Context, sess *Session, c Case) (status, summary string, err error)
}

// Playback owns the replay machinery: bind a session, issue the call,
// detect drift, record the run row.
type Playback struct {
	sessions *Service
	binder   *SnapshotBinder
	store    ifaces.PlaygroundStore
	emitter  audit.Emitter
	executor Executor
}

// NewPlayback constructs a Playback.
func NewPlayback(sessions *Service, binder *SnapshotBinder, store ifaces.PlaygroundStore, emitter audit.Emitter, exec Executor) *Playback {
	if emitter == nil {
		emitter = audit.NopEmitter{}
	}
	return &Playback{
		sessions: sessions,
		binder:   binder,
		store:    store,
		emitter:  emitter,
		executor: exec,
	}
}

// Replay opens a fresh session bound to the case's pinned snapshot (or
// live if none), executes the call via the executor, and records a run
// row. Drift detection is reflected on the Run + a `schema.drift` audit
// event is emitted when the snapshot fingerprint moved.
func (p *Playback) Replay(ctx context.Context, tenantID, actorID string, c Case) (*Run, error) {
	if p == nil {
		return nil, errors.New("playground: playback not configured")
	}
	if p.sessions == nil || p.store == nil {
		return nil, errors.New("playground: playback missing dependencies")
	}
	sess, err := p.sessions.StartSession(ctx, SessionRequest{
		TenantID:    tenantID,
		ActorUserID: actorID,
		SnapshotID:  c.SnapshotID,
	})
	if err != nil {
		return nil, fmt.Errorf("playground: start session: %w", err)
	}
	defer p.sessions.End(sess.ID)

	var bind *Binding
	if p.binder != nil {
		bind, err = p.binder.Bind(ctx, tenantID, sess.ID, c.SnapshotID)
		if err != nil {
			// Binder fails: still record the run as error.
			runID := newRunID()
			now := time.Now().UTC()
			rec := &ifaces.PlaygroundRunRecord{
				TenantID:   tenantID,
				RunID:      runID,
				CaseID:     c.ID,
				SessionID:  sess.ID,
				SnapshotID: c.SnapshotID,
				StartedAt:  now,
				EndedAt:    now,
				Status:     "error",
				Summary:    "snapshot_bind_failed: " + err.Error(),
			}
			_ = p.store.InsertRun(ctx, rec)
			return runFromRecord(rec), nil
		}
	}

	runID := newRunID()
	startedAt := time.Now().UTC()
	snapshotID := c.SnapshotID
	if bind != nil && snapshotID == "" {
		snapshotID = bind.SnapshotID
	}
	if snapshotID == "" {
		// Synthesize a placeholder so the NOT NULL invariant holds.
		// This happens when no binder is wired (tests / dev tooling).
		snapshotID = "playground-no-snapshot"
	}
	rec := &ifaces.PlaygroundRunRecord{
		TenantID:   tenantID,
		RunID:      runID,
		CaseID:     c.ID,
		SessionID:  sess.ID,
		SnapshotID: snapshotID,
		StartedAt:  startedAt,
		Status:     "running",
	}
	if err := p.store.InsertRun(ctx, rec); err != nil {
		return nil, fmt.Errorf("playground: insert run: %w", err)
	}
	if bind != nil && bind.DriftDetected {
		rec.DriftDetected = true
		p.emitter.Emit(ctx, audit.Event{
			Type:       "schema.drift",
			TenantID:   tenantID,
			SessionID:  sess.ID,
			UserID:     actorID,
			OccurredAt: time.Now().UTC(),
			Payload: map[string]any{
				"playground_run_id":  runID,
				"playground_session": sess.ID,
				"pinned_snapshot":    bind.SnapshotID,
				"pinned_hash":        bind.OverallHash,
				"live_hash":          bind.LiveHash,
			},
		})
	}

	status, summary := "ok", "completed"
	if p.executor != nil {
		st, sm, execErr := p.executor.Execute(ctx, sess, c)
		if st != "" {
			status = st
		}
		if sm != "" {
			summary = sm
		}
		if execErr != nil {
			status = "error"
			summary = execErr.Error()
		}
	}

	rec.Status = status
	rec.Summary = summary
	rec.EndedAt = time.Now().UTC()
	if err := p.store.UpdateRun(ctx, rec); err != nil {
		return nil, fmt.Errorf("playground: update run: %w", err)
	}
	return runFromRecord(rec), nil
}

func runFromRecord(rec *ifaces.PlaygroundRunRecord) *Run {
	if rec == nil {
		return nil
	}
	return &Run{
		ID:            rec.RunID,
		CaseID:        rec.CaseID,
		SessionID:     rec.SessionID,
		SnapshotID:    rec.SnapshotID,
		Status:        rec.Status,
		DriftDetected: rec.DriftDetected,
		Summary:       rec.Summary,
		StartedAt:     rec.StartedAt,
		EndedAt:       rec.EndedAt,
	}
}

func newRunID() string {
	return "run_" + ulid.MustNew(ulid.Timestamp(time.Now().UTC()), ulid.DefaultEntropy()).String()
}
