package mcpgw

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/namespace"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	httpclient "github.com/hurtener/Portico_gateway/internal/mcp/southbound/http"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Dispatcher routes MCP requests from the northbound transport to the
// right southbound client. Holds a per-session aggregated tools cache
// (60s TTL) and a 5s fan-out timeout for tools/list aggregation.
type Dispatcher struct {
	manager *southboundmgr.Manager
	log     *slog.Logger

	resources *ResourceAggregator
	prompts   *PromptAggregator
	mux       *ListChangedMux

	policy   *PolicyPipeline
	emitter  audit.Emitter

	cacheMu          sync.Mutex
	toolsCache       map[string]toolsCacheEntry // sessionID -> tools
	cacheTTL         time.Duration
	listToolsTimeout time.Duration
}

type toolsCacheEntry struct {
	tools     []protocol.Tool
	expiresAt time.Time
}

// NewDispatcher constructs a Dispatcher. Defaults: 60s tool cache, 5s
// fan-out timeout for tools/list aggregation.
func NewDispatcher(m *southboundmgr.Manager, log *slog.Logger) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{
		manager:          m,
		log:              log,
		toolsCache:       make(map[string]toolsCacheEntry),
		cacheTTL:         60 * time.Second,
		listToolsTimeout: 5 * time.Second,
	}
}

// SetAggregators installs the Phase 3 resource/prompt aggregators and
// the list-changed mux. Optional — when nil, dispatcher routes for those
// surfaces return MethodNotFound.
func (d *Dispatcher) SetAggregators(r *ResourceAggregator, p *PromptAggregator, mux *ListChangedMux) {
	d.resources = r
	d.prompts = p
	d.mux = mux
}

// SetPolicyPipeline installs the policy → approval → credentials chain
// run before every tools/call. nil disables the chain (dev mode default).
func (d *Dispatcher) SetPolicyPipeline(p *PolicyPipeline) { d.policy = p }

// SetAuditEmitter installs the emitter used for tool_call.start / .complete /
// .failed events. nil falls back to a NopEmitter.
func (d *Dispatcher) SetAuditEmitter(e audit.Emitter) {
	if e == nil {
		e = audit.NopEmitter{}
	}
	d.emitter = e
}

// Resources returns the resource aggregator (nil if not configured).
// REST handlers reach into the dispatcher to drive synthetic in-process
// sessions for /v1/resources.
func (d *Dispatcher) Resources() *ResourceAggregator { return d.resources }

// Prompts returns the prompt aggregator (nil if not configured).
func (d *Dispatcher) Prompts() *PromptAggregator { return d.prompts }

// InvalidateSession drops every per-session cache the dispatcher and
// its aggregators hold. The caller (typically a SessionRegistry.OnClose
// hook) must arrange this so cache entries don't leak past session
// shutdown — particularly relevant for long-lived gateways with high
// session churn.
func (d *Dispatcher) InvalidateSession(sessionID string) {
	if sessionID == "" {
		return
	}
	d.cacheMu.Lock()
	delete(d.toolsCache, sessionID)
	d.cacheMu.Unlock()
	if d.resources != nil {
		d.resources.InvalidateSession(sessionID)
	}
}

// HandleRequest is the main entry point used by the northbound transport.
// It returns either a Result body to encode or an *Error.
func (d *Dispatcher) HandleRequest(ctx context.Context, sess *Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	switch req.Method {
	case protocol.MethodInitialize:
		return d.handleInitialize(ctx, sess, req)
	case protocol.MethodPing:
		return json.RawMessage(`{}`), nil
	case protocol.MethodToolsList:
		return d.handleToolsList(ctx, sess, req)
	case protocol.MethodToolsCall:
		return d.handleToolsCall(ctx, sess, req)
	case protocol.MethodResourcesList:
		return d.handleResourcesList(ctx, sess, req)
	case protocol.MethodResourcesRead:
		return d.handleResourcesRead(ctx, sess, req)
	case protocol.MethodResourcesTemplatesList:
		return d.handleResourceTemplatesList(ctx, sess, req)
	case protocol.MethodPromptsList:
		return d.handlePromptsList(ctx, sess, req)
	case protocol.MethodPromptsGet:
		return d.handlePromptsGet(ctx, sess, req)
	default:
		return nil, protocol.NewError(protocol.ErrMethodNotFound, "method not supported in this build", map[string]string{"method": req.Method})
	}
}

// HandleNotification processes inbound notifications from the client.
// Phase 1 handles cancellations + the initialized notification; everything
// else is debug-logged.
func (d *Dispatcher) HandleNotification(_ context.Context, sess *Session, n *protocol.Notification) {
	switch n.Method {
	case protocol.NotifInitialized:
		d.log.Debug("client initialized", "session_id", sess.ID)
	case protocol.NotifCancelled:
		var p protocol.CancelledParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			d.log.Warn("malformed cancelled notification", "err", err)
			return
		}
		sess.Cancel(string(p.RequestID))
	default:
		d.log.Debug("notification ignored", "method", n.Method, "session_id", sess.ID)
	}
}

// ----- handlers ------------------------------------------------------------

func (d *Dispatcher) handleInitialize(ctx context.Context, sess *Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	var params protocol.InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, protocol.NewError(protocol.ErrInvalidParams, err.Error(), nil)
		}
	}
	sess.InitParams = params
	sess.ClientCaps = protocol.RecordClientCaps(params.Capabilities)

	// Aggregate downstream caps. Phase 1 surfaces tools-only; resources +
	// prompts come in Phase 3 once the dispatcher routes them.
	servers, err := d.serversFor(ctx, sess)
	if err != nil {
		d.log.Warn("dispatcher: list servers for initialize", "err", err)
	}
	caps := make([]protocol.ServerCapabilities, 0, len(servers))
	for _, s := range servers {
		c, err := d.acquireFor(ctx, sess, s)
		if err != nil {
			d.log.Warn("server unavailable during initialize",
				"server_id", s.Spec.ID, "err", err)
			continue
		}
		caps = append(caps, c.Capabilities())
	}
	srv := protocol.AggregateServerCaps(caps)
	// Always advertise the tools capability — Phase 1 always exposes tools
	// even when no downstream is configured. ListChanged is NOT advertised
	// because the dispatcher does not yet emit
	// notifications/tools/list_changed; Phase 2's registry change-event
	// fan-out will wire it.
	if srv.Tools == nil {
		srv.Tools = &protocol.ToolsCapability{}
	}

	// Phase 3: advertise resources + prompts when the aggregators are
	// configured. listChanged is advertised so clients can subscribe; the
	// mux applies stable/live mode per session.
	if d.resources != nil {
		if srv.Resources == nil {
			srv.Resources = &protocol.ResourcesCapability{}
		}
		srv.Resources.ListChanged = true
	}
	if d.prompts != nil {
		if srv.Prompts == nil {
			srv.Prompts = &protocol.PromptsCapability{}
		}
		srv.Prompts.ListChanged = true
	}
	if d.mux != nil {
		mode := extractListChangedMode(params.Capabilities.Experimental)
		d.mux.SetMode(sess.ID, mode)
	}

	res := protocol.InitializeResult{
		ProtocolVersion: protocol.ProtocolVersion,
		Capabilities:    srv,
		ServerInfo: protocol.Implementation{
			Name:        "portico-gateway",
			Version:     "phase-3.5",
			Description: "Portico — multi-tenant MCP gateway and Skill runtime",
		},
	}
	body, err := json.Marshal(res)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
	}
	return body, nil
}

func (d *Dispatcher) handleToolsList(ctx context.Context, sess *Session, _ *protocol.Request) (json.RawMessage, *protocol.Error) {
	// Cache check (per-session — tenant scoping via session uniqueness).
	d.cacheMu.Lock()
	if e, ok := d.toolsCache[sess.ID]; ok && time.Now().Before(e.expiresAt) {
		d.cacheMu.Unlock()
		body, _ := json.Marshal(protocol.ListToolsResult{Tools: e.tools})
		return body, nil
	}
	d.cacheMu.Unlock()

	listCtx, cancel := context.WithTimeout(ctx, d.listToolsTimeout)
	defer cancel()

	servers, err := d.serversFor(listCtx, sess)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
	}

	type result struct {
		serverID string
		tools    []protocol.Tool
		err      error
	}
	results := make(chan result, len(servers))
	for _, s := range servers {
		s := s
		go func() {
			c, err := d.acquireFor(listCtx, sess, s)
			if err != nil {
				results <- result{serverID: s.Spec.ID, err: err}
				return
			}
			tools, err := c.ListTools(listCtx)
			results <- result{serverID: s.Spec.ID, tools: tools, err: err}
		}()
	}

	combined := make([]protocol.Tool, 0)
	for i := 0; i < len(servers); i++ {
		r := <-results
		if r.err != nil {
			d.log.Warn("tools/list partial failure",
				"server_id", r.serverID, "err", r.err)
			continue
		}
		for _, t := range r.tools {
			t.Name = namespace.JoinTool(r.serverID, t.Name)
			combined = append(combined, t)
		}
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].Name < combined[j].Name })

	d.cacheMu.Lock()
	d.toolsCache[sess.ID] = toolsCacheEntry{tools: combined, expiresAt: time.Now().Add(d.cacheTTL)}
	d.cacheMu.Unlock()

	body, err := json.Marshal(protocol.ListToolsResult{Tools: combined})
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
	}
	return body, nil
}

func (d *Dispatcher) handleToolsCall(ctx context.Context, sess *Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	var params protocol.CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewError(protocol.ErrInvalidParams, err.Error(), nil)
	}
	serverID, toolName, ok := namespace.SplitTool(params.Name)
	if !ok {
		return nil, protocol.NewError(protocol.ErrToolNotEnabled, "tool name must be qualified as <server>.<tool>", map[string]string{"name": params.Name})
	}
	if d.manager == nil {
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, "manager not configured", nil)
	}

	// Phase 5: policy → approval → credentials.
	var prep *PipelineResult
	if d.policy != nil {
		var err error
		prep, err = d.policy.Evaluate(ctx, sess, params)
		if err != nil {
			return nil, protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
		}
		if prep.StructuredError != nil {
			return nil, prep.StructuredError
		}
	}

	if _, err := d.manager.Get(ctx, sess.TenantID, serverID); err != nil {
		if errors.Is(err, ifaces.ErrNotFound) {
			return nil, protocol.NewError(protocol.ErrToolNotEnabled, "unknown server", map[string]string{"server_id": serverID})
		}
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), map[string]string{"server_id": serverID})
	}
	acquireReq := southboundmgr.AcquireRequest{
		TenantID:  sess.TenantID,
		UserID:    sess.UserID,
		SessionID: sess.ID,
		ServerID:  serverID,
	}
	if prep != nil && prep.PrepTarget != nil {
		acquireReq.AuthHeaders = prep.PrepTarget.Headers
		acquireReq.AuthEnv = prep.PrepTarget.Env
	}
	client, err := d.manager.Acquire(ctx, acquireReq)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), map[string]string{"server_id": serverID})
	}

	// Extract the optional progress token from _meta.
	progressToken := extractProgressToken(params.Meta)
	progressCB := southbound.ProgressCallback(nil)
	if len(progressToken) > 0 {
		progressCB = func(p protocol.ProgressParams) {
			body, _ := json.Marshal(p)
			dropped := sess.EmitNotification(protocol.Notification{
				JSONRPC: protocol.JSONRPCVersion,
				Method:  protocol.NotifProgress,
				Params:  body,
			})
			if dropped {
				d.log.Warn("progress notification dropped (sse backpressure or session closed)",
					"session_id", sess.ID,
					"server_id", serverID,
					"tool", toolName)
			}
		}
	}

	// Per-call cancellable context registered on the session for client-driven cancel.
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if len(req.ID) > 0 {
		sess.RegisterCancel(string(req.ID), cancel)
		defer sess.UnregisterCancel(string(req.ID))
	}
	// Attach Phase 5 per-call headers (OAuth Bearer, header_inject, secret_ref)
	// so the southbound HTTP client picks them up via DefaultHeaderProvider.
	if prep != nil && prep.PrepTarget != nil && len(prep.PrepTarget.Headers) > 0 {
		callCtx = httpclient.WithHeaders(callCtx, prep.PrepTarget.Headers)
	}

	startedAt := time.Now()
	d.emitToolCall(ctx, sess, audit.EventToolCallStart, params, serverID, prep, 0, nil)
	res, err := client.CallTool(callCtx, toolName, params.Arguments, progressToken, progressCB)
	if err != nil {
		d.emitToolCall(ctx, sess, audit.EventToolCallFailed, params, serverID, prep, time.Since(startedAt), err)
		var pe *protocol.Error
		if errors.As(err, &pe) {
			return nil, pe
		}
		// transport / context error
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), map[string]string{"server_id": serverID})
	}
	d.emitToolCall(ctx, sess, audit.EventToolCallComplete, params, serverID, prep, time.Since(startedAt), nil)

	// Tick supervisor bookkeeping (last-call-at, idle reset).
	d.manager.Tick(ctx, southboundmgr.AcquireRequest{
		TenantID:  sess.TenantID,
		UserID:    sess.UserID,
		SessionID: sess.ID,
		ServerID:  serverID,
	})

	body, mErr := json.Marshal(res)
	if mErr != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, mErr.Error(), nil)
	}
	return body, nil
}

// serversFor returns the snapshots visible to sess. Empty tenant id (e.g.
// dev mode without upstream identity) returns an empty list silently.
func (d *Dispatcher) serversFor(ctx context.Context, sess *Session) ([]*registry.Snapshot, error) {
	if d.manager == nil {
		return nil, nil
	}
	tenantID := sess.TenantID
	if tenantID == "" {
		return []*registry.Snapshot{}, nil
	}
	return d.manager.Servers(ctx, tenantID)
}

// emitToolCall is the audit helper for the start/complete/failed events.
// d.emitter is nil-safe via SetAuditEmitter; callers may also pass nil
// emitter for tests.
func (d *Dispatcher) emitToolCall(ctx context.Context, sess *Session, evType string, params protocol.CallToolParams, serverID string, prep *PipelineResult, dur time.Duration, err error) {
	if d.emitter == nil {
		return
	}
	payload := map[string]any{
		"tool":      params.Name,
		"server_id": serverID,
	}
	if prep != nil {
		if prep.Decision.SkillID != "" {
			payload["skill_id"] = prep.Decision.SkillID
		}
		if prep.Decision.RiskClass != "" {
			payload["risk_class"] = prep.Decision.RiskClass
		}
	}
	if dur > 0 {
		payload["duration_ms"] = dur.Milliseconds()
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	d.emitter.Emit(ctx, audit.Event{
		Type:      evType,
		TenantID:  sess.TenantID,
		SessionID: sess.ID,
		UserID:    sess.UserID,
		Payload:   payload,
	})
}

func (d *Dispatcher) acquireFor(ctx context.Context, sess *Session, snap *registry.Snapshot) (southbound.Client, error) {
	if !snap.Record.Enabled {
		return nil, errors.New("server disabled")
	}
	return d.manager.Acquire(ctx, southboundmgr.AcquireRequest{
		TenantID:  sess.TenantID,
		UserID:    sess.UserID,
		SessionID: sess.ID,
		ServerID:  snap.Spec.ID,
	})
}

func extractProgressToken(meta json.RawMessage) json.RawMessage {
	if len(meta) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(meta, &m); err != nil {
		return nil
	}
	return m["progressToken"]
}

// extractListChangedMode reads the client's experimental opt-in for live
// list-changed forwarding. The protocol carries the hint as
// `experimental.portico.listChanged: "live" | "stable"`. Anything else
// returns the project default (stable).
func extractListChangedMode(exp map[string]json.RawMessage) ListChangedMode {
	raw, ok := exp["portico"]
	if !ok || len(raw) == 0 {
		return ModeStable
	}
	var p struct {
		ListChanged string `json:"listChanged"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ModeStable
	}
	if p.ListChanged == string(ModeLive) {
		return ModeLive
	}
	return ModeStable
}

// ----- Phase 3 handlers --------------------------------------------------

func (d *Dispatcher) handleResourcesList(ctx context.Context, sess *Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	if d.resources == nil {
		return nil, protocol.NewError(protocol.ErrMethodNotFound, "resources not configured", nil)
	}
	cursor := cursorOf(req)
	d.subscribeAllForSession(ctx, sess)
	res, err := d.resources.ListAll(ctx, sess, cursor)
	if err != nil {
		return nil, asProtocolError(err)
	}
	return mustMarshal(res)
}

func (d *Dispatcher) handleResourcesRead(ctx context.Context, sess *Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	if d.resources == nil {
		return nil, protocol.NewError(protocol.ErrMethodNotFound, "resources not configured", nil)
	}
	var params protocol.ReadResourceParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewError(protocol.ErrInvalidParams, err.Error(), nil)
	}
	res, err := d.resources.Read(ctx, sess, params.URI)
	if err != nil {
		return nil, asProtocolError(err)
	}
	return mustMarshal(res)
}

func (d *Dispatcher) handleResourceTemplatesList(ctx context.Context, sess *Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	if d.resources == nil {
		return nil, protocol.NewError(protocol.ErrMethodNotFound, "resources not configured", nil)
	}
	cursor := cursorOf(req)
	d.subscribeAllForSession(ctx, sess)
	res, err := d.resources.ListTemplates(ctx, sess, cursor)
	if err != nil {
		return nil, asProtocolError(err)
	}
	return mustMarshal(res)
}

func (d *Dispatcher) handlePromptsList(ctx context.Context, sess *Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	if d.prompts == nil {
		return nil, protocol.NewError(protocol.ErrMethodNotFound, "prompts not configured", nil)
	}
	cursor := cursorOf(req)
	d.subscribeAllForSession(ctx, sess)
	res, err := d.prompts.ListAll(ctx, sess, cursor)
	if err != nil {
		return nil, asProtocolError(err)
	}
	return mustMarshal(res)
}

func (d *Dispatcher) handlePromptsGet(ctx context.Context, sess *Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	if d.prompts == nil {
		return nil, protocol.NewError(protocol.ErrMethodNotFound, "prompts not configured", nil)
	}
	var params protocol.GetPromptParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewError(protocol.ErrInvalidParams, err.Error(), nil)
	}
	res, err := d.prompts.Get(ctx, sess, params.Name, params.Arguments)
	if err != nil {
		return nil, asProtocolError(err)
	}
	return mustMarshal(res)
}

// subscribeAllForSession registers the session as a subscriber for every
// server visible to its tenant. Idempotent. Called from list handlers
// so the list-changed mux knows which sessions to notify.
func (d *Dispatcher) subscribeAllForSession(ctx context.Context, sess *Session) {
	if d.mux == nil {
		return
	}
	servers, err := d.serversFor(ctx, sess)
	if err != nil {
		return
	}
	for _, s := range servers {
		d.mux.Subscribe(sess.ID, s.Spec.ID)
	}
}

func cursorOf(req *protocol.Request) string {
	if len(req.Params) == 0 {
		return ""
	}
	var p struct {
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return ""
	}
	return p.Cursor
}

func mustMarshal(v any) (json.RawMessage, *protocol.Error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
	}
	return body, nil
}

func asProtocolError(err error) *protocol.Error {
	if err == nil {
		return nil
	}
	var pe *protocol.Error
	if errors.As(err, &pe) {
		return pe
	}
	return protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
}
