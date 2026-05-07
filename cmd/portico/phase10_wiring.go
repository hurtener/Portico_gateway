// Phase 10 wiring — bridges the api PlaygroundController interface with
// the concrete internal/playground.Service + Playback. Lives in
// cmd/portico per CLAUDE.md §4.4.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/playground"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// playgroundAdapter implements api.PlaygroundController.
type playgroundAdapter struct {
	sessions    *playground.Service
	binder      *playground.SnapshotBinder
	playback    *playground.Playback
	correlator  *playground.Correlator
	auditEm     audit.Emitter
	snapshotSvc *snapshots.Service
	store       ifaces.PlaygroundStore
	log         *slog.Logger
}

// newPlaygroundAdapter constructs the adapter. emitter / store may be nil
// in highly degraded boots; the adapter checks before use.
func newPlaygroundAdapter(
	sessions *playground.Service,
	binder *playground.SnapshotBinder,
	playback *playground.Playback,
	correlator *playground.Correlator,
	emitter audit.Emitter,
	snapSvc *snapshots.Service,
	store ifaces.PlaygroundStore,
	log *slog.Logger,
) *playgroundAdapter {
	return &playgroundAdapter{
		sessions:    sessions,
		binder:      binder,
		playback:    playback,
		correlator:  correlator,
		auditEm:     emitter,
		snapshotSvc: snapSvc,
		store:       store,
		log:         log,
	}
}

func (a *playgroundAdapter) StartSession(ctx context.Context, req api.PlaygroundStartSessionRequest) (*api.PlaygroundSessionDTO, error) {
	if a == nil || a.sessions == nil {
		return nil, errors.New("playground service not configured")
	}
	sess, err := a.sessions.StartSession(ctx, playground.SessionRequest{
		TenantID:        req.TenantID,
		SnapshotID:      req.SnapshotID,
		RuntimeOverride: req.RuntimeOverride,
		Scopes:          req.Scopes,
	})
	if err != nil {
		return nil, err
	}
	// Best-effort snapshot bind so the session's snapshot_id is populated.
	if a.binder != nil {
		if bind, err := a.binder.Bind(ctx, sess.TenantID, sess.ID, sess.SnapshotID); err == nil {
			sess.SnapshotID = bind.SnapshotID
		}
	}
	if a.auditEm != nil {
		a.auditEm.Emit(ctx, audit.Event{
			Type:       "playground.session.started",
			TenantID:   sess.TenantID,
			SessionID:  sess.ID,
			OccurredAt: time.Now().UTC(),
			Payload: map[string]any{
				"playground_session": sess.ID,
				"snapshot_id":        sess.SnapshotID,
				"actor":              sess.ActorID,
			},
		})
	}
	return &api.PlaygroundSessionDTO{
		ID:         sess.ID,
		TenantID:   sess.TenantID,
		ActorID:    sess.ActorID,
		SnapshotID: sess.SnapshotID,
		Token:      sess.Token,
		ExpiresAt:  sess.ExpiresAt,
		CreatedAt:  sess.CreatedAt,
	}, nil
}

func (a *playgroundAdapter) EndSession(ctx context.Context, sid string) error {
	if a == nil || a.sessions == nil {
		return errors.New("playground service not configured")
	}
	if a.sessions.Get(sid) == nil {
		return errors.New("session not found")
	}
	a.sessions.End(sid)
	if a.auditEm != nil {
		a.auditEm.Emit(ctx, audit.Event{
			Type:       "playground.session.ended",
			SessionID:  sid,
			OccurredAt: time.Now().UTC(),
			Payload:    map[string]any{"playground_session": sid},
		})
	}
	return nil
}

func (a *playgroundAdapter) GetSession(sid string) *api.PlaygroundSessionDTO {
	if a == nil {
		return nil
	}
	sess := a.sessions.Get(sid)
	if sess == nil {
		return nil
	}
	return &api.PlaygroundSessionDTO{
		ID:         sess.ID,
		TenantID:   sess.TenantID,
		ActorID:    sess.ActorID,
		SnapshotID: sess.SnapshotID,
		Token:      sess.Token,
		ExpiresAt:  sess.ExpiresAt,
		CreatedAt:  sess.CreatedAt,
	}
}

func (a *playgroundAdapter) Catalog(ctx context.Context, sid string) (*api.PlaygroundCatalogDTO, error) {
	if a == nil {
		return nil, errors.New("playground service not configured")
	}
	sess := a.sessions.Get(sid)
	if sess == nil {
		return nil, errors.New("session not found")
	}
	if a.snapshotSvc == nil {
		return &api.PlaygroundCatalogDTO{SnapshotID: sess.SnapshotID, Catalog: map[string]any{}}, nil
	}
	var snap *snapshots.Snapshot
	var err error
	if sess.SnapshotID != "" {
		snap, err = a.snapshotSvc.Get(ctx, sess.SnapshotID)
		if err != nil || snap == nil {
			snap, err = a.snapshotSvc.Create(ctx, sess.TenantID, sess.ID)
		}
	} else {
		snap, err = a.snapshotSvc.Create(ctx, sess.TenantID, sess.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("catalog snapshot: %w", err)
	}
	// Convert snapshot → frontend-ready catalog payload.
	body, _ := json.Marshal(snap)
	var generic map[string]any
	_ = json.Unmarshal(body, &generic)
	return &api.PlaygroundCatalogDTO{SnapshotID: snap.ID, Catalog: generic}, nil
}

// IssueCall — V1 of the playground synchronously routes through the
// dispatcher would require a SessionRegistry seam this slice doesn't yet
// expose cleanly. The adapter records the call as a Run and emits the
// canonical tool_call.* audit events so the correlation tab populates;
// the streaming endpoint surfaces a single "end" frame.
//
// A richer dispatcher hand-off lands in a follow-up; the contract here
// is intentionally narrow so the surface (REST + SSE shape + audit
// trail) is exercisable end-to-end before Phase 11 grows the seam.
func (a *playgroundAdapter) IssueCall(ctx context.Context, sid string, req api.PlaygroundCallRequest) (*api.PlaygroundCallEnvelope, error) {
	if a == nil {
		return nil, errors.New("playground service not configured")
	}
	sess := a.sessions.Get(sid)
	if sess == nil {
		return nil, errors.New("session not found")
	}
	cid := "call_" + time.Now().UTC().Format("20060102T150405.000")
	if a.auditEm != nil {
		a.auditEm.Emit(ctx, audit.Event{
			Type:       audit.EventToolCallStart,
			TenantID:   sess.TenantID,
			SessionID:  sess.ID,
			OccurredAt: time.Now().UTC(),
			Payload: map[string]any{
				"call_id":            cid,
				"tool":               req.Target,
				"kind":               req.Kind,
				"playground_session": sess.ID,
			},
		})
	}
	return &api.PlaygroundCallEnvelope{
		CallID:    cid,
		SessionID: sess.ID,
		Status:    "enqueued",
	}, nil
}

func (a *playgroundAdapter) StreamCall(ctx context.Context, sid, cid string) (<-chan api.PlaygroundStreamFrame, error) {
	if a == nil {
		return nil, errors.New("playground service not configured")
	}
	sess := a.sessions.Get(sid)
	if sess == nil {
		return nil, errors.New("session not found")
	}
	out := make(chan api.PlaygroundStreamFrame, 4)
	go func() {
		defer close(out)
		// Stream-lite: emit one synthetic chunk + end frame so the
		// browser-side parser can be exercised; richer streaming lands
		// when the dispatcher seam is wired through.
		select {
		case out <- api.PlaygroundStreamFrame{
			Type: "chunk",
			Data: json.RawMessage(`{"call_id":"` + cid + `","chunk":"hello"}`),
		}:
		case <-ctx.Done():
			return
		}
		// Emit a tool_call.complete audit so correlation populates.
		if a.auditEm != nil {
			a.auditEm.Emit(ctx, audit.Event{
				Type:       audit.EventToolCallComplete,
				TenantID:   sess.TenantID,
				SessionID:  sess.ID,
				OccurredAt: time.Now().UTC(),
				Payload: map[string]any{
					"call_id":            cid,
					"playground_session": sess.ID,
				},
			})
		}
		select {
		case out <- api.PlaygroundStreamFrame{Type: "end", Data: json.RawMessage(`{"call_id":"` + cid + `"}`)}:
		case <-ctx.Done():
		}
	}()
	return out, nil
}

func (a *playgroundAdapter) Correlation(ctx context.Context, sid string, since time.Time) (any, error) {
	if a == nil || a.correlator == nil {
		return nil, errors.New("correlator not configured")
	}
	sess := a.sessions.Get(sid)
	if sess == nil {
		return nil, errors.New("session not found")
	}
	return a.correlator.Get(ctx, sess.TenantID, playground.CorrelationFilter{
		SessionID: sid,
		Since:     since,
	})
}

func (a *playgroundAdapter) RunCorrelation(ctx context.Context, runID string) (any, error) {
	if a == nil || a.correlator == nil || a.store == nil {
		return nil, errors.New("correlator not configured")
	}
	// Locate the run row across tenants by scanning known tenants — the
	// REST handler already validated tenant scope; here we simply pull
	// the row whose run_id matches. In practice the correlator could be
	// invoked with the tenant-bound id; this is an intentional small
	// shortcut.
	// V1: fail fast with "not_found" if the caller didn't pass the tenant.
	return nil, errors.New("run correlation requires tenant context — not implemented in V1 adapter")
}

func (a *playgroundAdapter) Replay(ctx context.Context, tenantID, actorID, caseID string) (*api.PlaygroundRunDTO, error) {
	if a == nil || a.playback == nil || a.store == nil {
		return nil, errors.New("playback not configured")
	}
	rec, err := a.store.GetCase(ctx, tenantID, caseID)
	if err != nil {
		return nil, err
	}
	c := playground.Case{
		ID:         rec.CaseID,
		Name:       rec.Name,
		Kind:       rec.Kind,
		Target:     rec.Target,
		Payload:    rec.Payload,
		SnapshotID: rec.SnapshotID,
		Tags:       rec.Tags,
	}
	run, err := a.playback.Replay(ctx, tenantID, actorID, c)
	if err != nil {
		return nil, err
	}
	return &api.PlaygroundRunDTO{
		ID:            run.ID,
		CaseID:        run.CaseID,
		SessionID:     run.SessionID,
		SnapshotID:    run.SnapshotID,
		Status:        run.Status,
		DriftDetected: run.DriftDetected,
		Summary:       run.Summary,
		StartedAt:     run.StartedAt,
		EndedAt:       run.EndedAt,
	}, nil
}

// playbackExecutor is the runtime executor playback uses. Phase 10 ships
// a stub that emits a `tool_call.complete` audit event so correlation
// populates; richer dispatcher integration lands in a follow-up.
type playbackExecutor struct {
	emitter audit.Emitter
}

func newPlaybackExecutor(em audit.Emitter) *playbackExecutor {
	return &playbackExecutor{emitter: em}
}

func (p *playbackExecutor) Execute(ctx context.Context, sess *playground.Session, c playground.Case) (string, string, error) {
	if p.emitter != nil {
		p.emitter.Emit(ctx, audit.Event{
			Type:       audit.EventToolCallComplete,
			TenantID:   sess.TenantID,
			SessionID:  sess.ID,
			OccurredAt: time.Now().UTC(),
			Payload: map[string]any{
				"tool":               c.Target,
				"playground_session": sess.ID,
				"replay":             true,
			},
		})
	}
	return "ok", "replay completed", nil
}
