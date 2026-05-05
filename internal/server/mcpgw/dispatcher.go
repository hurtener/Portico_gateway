package mcpgw

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/namespace"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
)

// Dispatcher routes MCP requests from the northbound transport to the right
// southbound client. Holds a per-session aggregated tools cache (60s TTL).
type Dispatcher struct {
	manager *southboundmgr.Manager
	log     *slog.Logger

	cacheMu      sync.Mutex
	toolsCache   map[string]toolsCacheEntry // sessionID -> tools
	cacheTTL     time.Duration
	listToolsTimeout time.Duration
}

type toolsCacheEntry struct {
	tools     []protocol.Tool
	expiresAt time.Time
}

// NewDispatcher constructs a Dispatcher. Defaults: 60s tool cache, 5s fan-out
// timeout for tools/list aggregation.
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
	default:
		return nil, protocol.NewError(protocol.ErrMethodNotFound, "method not supported in this build", map[string]string{"method": req.Method})
	}
}

// HandleNotification processes inbound notifications from the client. Phase 1
// handles cancellations + the initialized notification; everything else is
// debug-logged.
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
	servers := d.manager.Servers()
	caps := make([]protocol.ServerCapabilities, 0, len(servers))
	for _, s := range servers {
		c, err := d.manager.AcquireClient(ctx, s.ID)
		if err != nil {
			d.log.Warn("server unavailable during initialize", "server_id", s.ID, "err", err)
			continue
		}
		caps = append(caps, c.Capabilities())
	}
	srv := protocol.AggregateServerCaps(caps)
	// Always advertise gateway-level tools cap so list_changed is honored.
	if srv.Tools == nil {
		srv.Tools = &protocol.ToolsCapability{}
	}
	srv.Tools.ListChanged = true

	res := protocol.InitializeResult{
		ProtocolVersion: protocol.ProtocolVersion,
		Capabilities:    srv,
		ServerInfo: protocol.Implementation{
			Name:    "portico-gateway",
			Version: "phase-1",
		},
	}
	body, err := json.Marshal(res)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
	}
	return body, nil
}

func (d *Dispatcher) handleToolsList(ctx context.Context, sess *Session, _ *protocol.Request) (json.RawMessage, *protocol.Error) {
	// Cache check
	d.cacheMu.Lock()
	if e, ok := d.toolsCache[sess.ID]; ok && time.Now().Before(e.expiresAt) {
		d.cacheMu.Unlock()
		body, _ := json.Marshal(protocol.ListToolsResult{Tools: e.tools})
		return body, nil
	}
	d.cacheMu.Unlock()

	listCtx, cancel := context.WithTimeout(ctx, d.listToolsTimeout)
	defer cancel()

	servers := d.manager.Servers()

	type result struct {
		serverID string
		tools    []protocol.Tool
		err      error
	}
	results := make(chan result, len(servers))
	for _, s := range servers {
		s := s
		go func() {
			c, err := d.manager.AcquireClient(listCtx, s.ID)
			if err != nil {
				results <- result{serverID: s.ID, err: err}
				return
			}
			tools, err := c.ListTools(listCtx)
			results <- result{serverID: s.ID, tools: tools, err: err}
		}()
	}

	combined := make([]protocol.Tool, 0)
	for i := 0; i < len(servers); i++ {
		r := <-results
		if r.err != nil {
			d.log.Warn("tools/list partial failure", "server_id", r.serverID, "err", r.err)
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
	if _, present := d.manager.Get(serverID); !present {
		return nil, protocol.NewError(protocol.ErrToolNotEnabled, "unknown server", map[string]string{"server_id": serverID})
	}
	client, err := d.manager.AcquireClient(ctx, serverID)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), map[string]string{"server_id": serverID})
	}

	// Extract the optional progress token from _meta.
	progressToken := extractProgressToken(params.Meta)
	progressCB := southbound.ProgressCallback(nil)
	if len(progressToken) > 0 {
		progressCB = func(p protocol.ProgressParams) {
			body, _ := json.Marshal(p)
			sess.EmitNotification(protocol.Notification{
				JSONRPC: protocol.JSONRPCVersion,
				Method:  protocol.NotifProgress,
				Params:  body,
			})
		}
	}

	// Per-call cancellable context registered on the session for client-driven cancel.
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if len(req.ID) > 0 {
		sess.RegisterCancel(string(req.ID), cancel)
		defer sess.UnregisterCancel(string(req.ID))
	}

	res, err := client.CallTool(callCtx, toolName, params.Arguments, progressToken, progressCB)
	if err != nil {
		var pe *protocol.Error
		if errors.As(err, &pe) {
			return nil, pe
		}
		// transport / context error
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), map[string]string{"server_id": serverID})
	}
	body, mErr := json.Marshal(res)
	if mErr != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, mErr.Error(), nil)
	}
	return body, nil
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
