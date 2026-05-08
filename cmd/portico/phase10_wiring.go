// Phase 10 wiring — bridges the api PlaygroundController interface with
// the concrete internal/playground.Service + Playback. Lives in
// cmd/portico per CLAUDE.md §4.4.

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/playground"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	skillruntime "github.com/hurtener/Portico_gateway/internal/skills/runtime"
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

	// Dispatcher seam — playground IssueCall routes through this so a
	// playground tool call exercises the same code path an external MCP
	// client does. mcpSessions backs the dispatcher: each playground
	// session lazily acquires a sibling mcpgw.Session keyed by the
	// playground sid so dispatcher caches stick across calls.
	dispatcher  *mcpgw.Dispatcher
	mcpSessions *mcpgw.SessionRegistry
	skillsMgr   *skillruntime.Manager

	mcpMu       sync.Mutex
	mcpSessByPG map[string]*mcpgw.Session // playground.session.id -> mcpgw.Session

	// Pending-call buffer. IssueCall registers (sid, params); StreamCall
	// pops the entry and runs the dispatcher inline. Buffered so a
	// disconnected stream consumer doesn't leak goroutines.
	pendingMu sync.Mutex
	pending   map[string]pendingCall
}

// pendingCall is one in-flight call awaiting its SSE consumer.
type pendingCall struct {
	playgroundSID string
	req           api.PlaygroundCallRequest
	addedAt       time.Time
}

// newPlaygroundAdapter constructs the adapter. emitter / store may be nil
// in highly degraded boots; the adapter checks before use. dispatcher and
// mcpSessions are required for IssueCall to dispatch; without them the
// adapter falls back to an audit-only stub frame.
func newPlaygroundAdapter(
	sessions *playground.Service,
	binder *playground.SnapshotBinder,
	playback *playground.Playback,
	correlator *playground.Correlator,
	emitter audit.Emitter,
	snapSvc *snapshots.Service,
	store ifaces.PlaygroundStore,
	dispatcher *mcpgw.Dispatcher,
	mcpSessions *mcpgw.SessionRegistry,
	skillsMgr *skillruntime.Manager,
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
		dispatcher:  dispatcher,
		mcpSessions: mcpSessions,
		skillsMgr:   skillsMgr,
		mcpSessByPG: make(map[string]*mcpgw.Session),
		pending:     make(map[string]pendingCall),
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
	// Snapshot is bound lazily on first Catalog/IssueCall — pure ping
	// sessions never pay the cost of materialising one. Only honour an
	// explicit pin here so the operator's choice is reflected immediately.
	if a.binder != nil && req.SnapshotID != "" {
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
	a.releaseMCPSession(sid)
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
	snap, err := a.ensureSnapshot(ctx, sess)
	if err != nil {
		return nil, fmt.Errorf("catalog snapshot: %w", err)
	}
	if snap == nil {
		return &api.PlaygroundCatalogDTO{SnapshotID: sess.SnapshotID, Catalog: map[string]any{}}, nil
	}
	// Convert snapshot → frontend-ready catalog payload.
	body, _ := json.Marshal(snap)
	var generic map[string]any
	_ = json.Unmarshal(body, &generic)
	return &api.PlaygroundCatalogDTO{SnapshotID: snap.ID, Catalog: generic}, nil
}

// ensureSnapshot resolves (or creates) the catalog snapshot bound to sess.
// Called from Catalog and IssueCall so the snapshot lifetime is "first use",
// not "session start" — pure ping sessions never pay the cost.
func (a *playgroundAdapter) ensureSnapshot(ctx context.Context, sess *playground.Session) (*snapshots.Snapshot, error) {
	if a.snapshotSvc == nil {
		return nil, nil
	}
	if sess.SnapshotID != "" {
		if snap, err := a.snapshotSvc.Get(ctx, sess.SnapshotID); err == nil && snap != nil {
			return snap, nil
		}
	}
	snap, err := a.snapshotSvc.Create(ctx, sess.TenantID, sess.ID)
	if err != nil {
		return nil, err
	}
	if sess.SnapshotID == "" {
		sess.SnapshotID = snap.ID
	}
	return snap, nil
}

// IssueCall registers a pending call keyed by the returned cid. The
// caller is expected to follow up with GET /stream/{cid} which actually
// dispatches against the southbound. Splitting the two halves preserves
// the original SSE handshake without forcing the operator to wait for
// dispatch on the POST side.
func (a *playgroundAdapter) IssueCall(_ context.Context, sid string, req api.PlaygroundCallRequest) (*api.PlaygroundCallEnvelope, error) {
	if a == nil {
		return nil, errors.New("playground service not configured")
	}
	sess := a.sessions.Get(sid)
	if sess == nil {
		return nil, errors.New("session not found")
	}
	if req.Target == "" {
		return nil, errors.New("target required")
	}
	cid := newPlaygroundCallID()
	a.pendingMu.Lock()
	a.pending[cid] = pendingCall{playgroundSID: sid, req: req, addedAt: time.Now().UTC()}
	a.gcPendingLocked(time.Now().UTC().Add(-2 * time.Minute))
	a.pendingMu.Unlock()
	return &api.PlaygroundCallEnvelope{
		CallID:    cid,
		SessionID: sess.ID,
		Status:    "enqueued",
	}, nil
}

// StreamCall pops the pending call for cid and dispatches it through the
// real mcpgw.Dispatcher, streaming the result as a single chunk frame
// followed by an end frame. Errors surface as an "error" frame whose data
// is the JSON-RPC error envelope. When the dispatcher is not configured
// (unlikely in a healthy boot) the adapter degrades to a stub frame so
// the SSE contract is preserved.
func (a *playgroundAdapter) StreamCall(ctx context.Context, sid, cid string) (<-chan api.PlaygroundStreamFrame, error) {
	if a == nil {
		return nil, errors.New("playground service not configured")
	}
	sess := a.sessions.Get(sid)
	if sess == nil {
		return nil, errors.New("session not found")
	}
	a.pendingMu.Lock()
	call, ok := a.pending[cid]
	if ok {
		delete(a.pending, cid)
	}
	a.pendingMu.Unlock()
	if !ok || call.playgroundSID != sid {
		return nil, errors.New("call not found or expired")
	}

	out := make(chan api.PlaygroundStreamFrame, 4)
	go a.runDispatch(ctx, sess, cid, call.req, out)
	return out, nil
}

// runDispatch is the IssueCall+StreamCall fused dispatch loop. It pushes
// at most three frames: a status comment, the chunk, and an end (or
// error) frame, then closes the channel.
func (a *playgroundAdapter) runDispatch(ctx context.Context, sess *playground.Session, cid string, req api.PlaygroundCallRequest, out chan<- api.PlaygroundStreamFrame) {
	defer close(out)

	// Snapshot bind on first call so drift detection is meaningful for
	// this session going forward.
	if _, err := a.ensureSnapshot(ctx, sess); err != nil {
		a.log.Warn("playground: ensure snapshot before call", "err", err, "session_id", sess.ID)
	}

	if a.dispatcher == nil || a.mcpSessions == nil {
		a.emitStub(ctx, sess, cid, out)
		return
	}

	method, params, err := translatePlaygroundCall(req)
	if err != nil {
		a.emitErrorFrame(out, cid, err.Error())
		return
	}

	mcpSess, err := a.acquireMCPSession(sess)
	if err != nil {
		a.emitErrorFrame(out, cid, err.Error())
		return
	}

	jsonReq := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		ID:      json.RawMessage(`"` + cid + `"`),
		Params:  params,
	}
	body, perr := a.dispatcher.HandleRequest(ctx, mcpSess, jsonReq)
	if perr != nil {
		errBody, _ := json.Marshal(map[string]any{
			"call_id": cid,
			"error":   perr,
		})
		select {
		case out <- api.PlaygroundStreamFrame{Type: "error", Data: errBody}:
		case <-ctx.Done():
		}
		return
	}

	chunkData, _ := json.Marshal(map[string]any{
		"call_id": cid,
		"result":  json.RawMessage(body),
	})
	select {
	case out <- api.PlaygroundStreamFrame{Type: "chunk", Data: chunkData}:
	case <-ctx.Done():
		return
	}
	select {
	case out <- api.PlaygroundStreamFrame{Type: "end", Data: json.RawMessage(`{"call_id":"` + cid + `"}`)}:
	case <-ctx.Done():
	}
}

// translatePlaygroundCall maps the playground call shape to the
// JSON-RPC method + params the dispatcher expects.
func translatePlaygroundCall(req api.PlaygroundCallRequest) (string, json.RawMessage, error) {
	args := req.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	switch req.Kind {
	case "tool_call", "":
		var argObj json.RawMessage = args
		// CallToolParams.Arguments is already raw JSON; embed as-is.
		params, err := json.Marshal(protocol.CallToolParams{
			Name:      req.Target,
			Arguments: argObj,
		})
		if err != nil {
			return "", nil, err
		}
		return protocol.MethodToolsCall, params, nil
	case "resource_read":
		params, err := json.Marshal(protocol.ReadResourceParams{URI: req.Target})
		if err != nil {
			return "", nil, err
		}
		return protocol.MethodResourcesRead, params, nil
	case "prompt_get":
		var argMap map[string]string
		if len(args) > 0 {
			// Best-effort decode; non-string values are dropped.
			var raw map[string]any
			if err := json.Unmarshal(args, &raw); err == nil {
				argMap = make(map[string]string, len(raw))
				for k, v := range raw {
					if s, ok := v.(string); ok {
						argMap[k] = s
					}
				}
			}
		}
		params, err := json.Marshal(protocol.GetPromptParams{Name: req.Target, Arguments: argMap})
		if err != nil {
			return "", nil, err
		}
		return protocol.MethodPromptsGet, params, nil
	default:
		return "", nil, fmt.Errorf("unknown playground call kind %q", req.Kind)
	}
}

// acquireMCPSession returns the mcpgw.Session for this playground
// session, creating it on first use. The session is registered with the
// session registry so dispatcher caches stick across calls.
func (a *playgroundAdapter) acquireMCPSession(sess *playground.Session) (*mcpgw.Session, error) {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	if existing, ok := a.mcpSessByPG[sess.ID]; ok {
		return existing, nil
	}
	mcpSess := a.mcpSessions.Create(sess.TenantID, sess.ActorID, sess.Token)
	a.mcpSessByPG[sess.ID] = mcpSess
	return mcpSess, nil
}

// releaseMCPSession closes and forgets the sibling mcpgw session.
func (a *playgroundAdapter) releaseMCPSession(playgroundSID string) {
	a.mcpMu.Lock()
	mcpSess, ok := a.mcpSessByPG[playgroundSID]
	delete(a.mcpSessByPG, playgroundSID)
	a.mcpMu.Unlock()
	if ok && a.mcpSessions != nil {
		a.mcpSessions.Close(mcpSess.ID)
	}
}

// emitStub keeps backward compatibility with the previous stub for the
// off-nominal case where the dispatcher seam is unavailable.
func (a *playgroundAdapter) emitStub(ctx context.Context, sess *playground.Session, cid string, out chan<- api.PlaygroundStreamFrame) {
	if a.auditEm != nil {
		a.auditEm.Emit(ctx, audit.Event{
			Type:       audit.EventToolCallComplete,
			TenantID:   sess.TenantID,
			SessionID:  sess.ID,
			OccurredAt: time.Now().UTC(),
			Payload: map[string]any{
				"call_id":            cid,
				"playground_session": sess.ID,
				"stub":               true,
			},
		})
	}
	select {
	case out <- api.PlaygroundStreamFrame{
		Type: "chunk",
		Data: json.RawMessage(`{"call_id":"` + cid + `","stub":true}`),
	}:
	case <-ctx.Done():
		return
	}
	select {
	case out <- api.PlaygroundStreamFrame{Type: "end", Data: json.RawMessage(`{"call_id":"` + cid + `"}`)}:
	case <-ctx.Done():
	}
}

func (a *playgroundAdapter) emitErrorFrame(out chan<- api.PlaygroundStreamFrame, cid, msg string) {
	body, _ := json.Marshal(map[string]any{"call_id": cid, "error": map[string]any{"message": msg}})
	select {
	case out <- api.PlaygroundStreamFrame{Type: "error", Data: body}:
	default:
	}
}

// gcPendingLocked drops pending entries older than cutoff so a
// disconnected SSE consumer doesn't leak memory. Caller holds pendingMu.
func (a *playgroundAdapter) gcPendingLocked(cutoff time.Time) {
	for cid, p := range a.pending {
		if p.addedAt.Before(cutoff) {
			delete(a.pending, cid)
		}
	}
}

func newPlaygroundCallID() string {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based; the id is for correlation, not security.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (i * 8))
		}
	}
	return "call_" + base64.RawURLEncoding.EncodeToString(b)
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

// SetSkillEnabled toggles a skill for the playground session's underlying
// mcpgw session. Acquires the mcpgw session if it doesn't exist yet (rare
// — operators usually pick a tool first, but the catalog rail also lets
// them toggle skills before any tool call). After the toggle, invalidates
// the session's catalog snapshot so the next refreshCatalog reflects the
// new virtual-directory filter.
func (a *playgroundAdapter) SetSkillEnabled(ctx context.Context, sid, skillID string, enabled bool) error {
	if a == nil {
		return errors.New("playground service not configured")
	}
	if a.skillsMgr == nil {
		return errors.New("skills runtime not configured")
	}
	sess := a.sessions.Get(sid)
	if sess == nil {
		return errors.New("session not found")
	}
	if _, ok := a.skillsMgr.Catalog().Get(skillID); !ok {
		return fmt.Errorf("unknown skill %q", skillID)
	}
	mcpSess, err := a.acquireMCPSession(sess)
	if err != nil {
		return err
	}
	if err := a.skillsMgr.Enablement().Set(ctx, sess.TenantID, mcpSess.ID, skillID, enabled); err != nil {
		return fmt.Errorf("set enablement: %w", err)
	}
	a.skillsMgr.IndexGenerator().Invalidate(sess.TenantID, mcpSess.ID)
	// Drop the session's bound snapshot so the next Catalog call rebuilds
	// it with the post-toggle skill set. The snapshot service caches by
	// session id; without invalidation the rail would show stale state.
	if a.dispatcher != nil {
		a.dispatcher.InvalidateSession(mcpSess.ID)
	}
	if binder := a.snapshotBinder(); binder != nil {
		binder.Forget(mcpSess.ID)
	}
	sess.SnapshotID = ""
	if a.auditEm != nil {
		evType := "playground.skill.enabled"
		if !enabled {
			evType = "playground.skill.disabled"
		}
		a.auditEm.Emit(ctx, audit.Event{
			Type:       evType,
			TenantID:   sess.TenantID,
			SessionID:  sess.ID,
			OccurredAt: time.Now().UTC(),
			Payload: map[string]any{
				"skill_id":           skillID,
				"playground_session": sess.ID,
				"mcpgw_session":      mcpSess.ID,
			},
		})
	}
	return nil
}

// snapshotBinder returns the dispatcher's snapshot binder, if any.
// Returns nil when the dispatcher isn't configured (highly degraded
// boot) so callers can skip the invalidation step.
func (a *playgroundAdapter) snapshotBinder() *mcpgw.SnapshotBinder {
	if a == nil || a.dispatcher == nil {
		return nil
	}
	return a.dispatcher.SnapshotBinder()
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
