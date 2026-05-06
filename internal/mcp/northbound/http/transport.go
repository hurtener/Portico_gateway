// Package http is the northbound MCP transport (Streamable HTTP). Phase 1
// supports POST /mcp returning application/json, GET /mcp opening a long-
// lived SSE channel for server-to-client notifications, and DELETE /mcp
// terminating a session.
//
// SSE on the POST response (intermediate progress + final result in one
// stream) is intentionally deferred: progress notifications go to the GET
// SSE channel instead. Per spec both placements are valid.
//
// Spec version: 2025-11-25. The Origin guard, SSE event ids, and stricter
// Accept negotiation enforce the clarifications made in that revision.
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"strings"
	"sync/atomic"

	"log/slog"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	"github.com/hurtener/Portico_gateway/internal/telemetry"
)

const (
	headerSessionID = "Mcp-Session-Id"
)

// Dispatcher is the dispatcher contract Handler depends on. Defined here to
// keep the transport package free of upward imports beyond protocol.
type Dispatcher interface {
	HandleRequest(ctx context.Context, sess *mcpgw.Session, req *protocol.Request) (json.RawMessage, *protocol.Error)
	HandleNotification(ctx context.Context, sess *mcpgw.Session, n *protocol.Notification)
}

// HandlerConfig tunes optional transport behavior. Zero value is fine
// for tests; production callers pass an allow-list of Origins.
type HandlerConfig struct {
	// AllowedOrigins is the set of acceptable Origin header values for
	// browser clients. Requests with an Origin not in the list are
	// rejected with 403 (per spec 2025-11-25). An empty list rejects
	// every Origin-bearing request — operators must opt in explicitly.
	AllowedOrigins []string
	// AllowLocalhostOrigins, when true, additionally permits Origins
	// served from localhost / 127.0.0.1 / [::1] on any port. Set this
	// in dev mode so the SvelteKit dev server can talk to the gateway.
	AllowLocalhostOrigins bool
}

// Handler is the http.Handler that implements MCP-over-HTTP.
type Handler struct {
	sessions   *mcpgw.SessionRegistry
	dispatcher Dispatcher
	log        *slog.Logger
	cfg        HandlerConfig
	// serverInit, when non-nil, intercepts inbound JSON-RPC responses
	// whose id matches a pending server-initiated request (e.g. an
	// elicitation/create reply) and routes them away from the normal
	// dispatcher path.
	serverInit *ServerInitiatedRequester
}

// SetServerInitiated installs the server-initiated request requester. The
// gateway calls this after constructing the Handler so the requester can
// reference the same SessionRegistry.
func (h *Handler) SetServerInitiated(r *ServerInitiatedRequester) {
	h.serverInit = r
}

// NewHandler constructs a Handler with the default (empty) HandlerConfig.
// Use NewHandlerWithConfig when you need to populate AllowedOrigins.
func NewHandler(sessions *mcpgw.SessionRegistry, d Dispatcher, log *slog.Logger) *Handler {
	return NewHandlerWithConfig(sessions, d, log, HandlerConfig{})
}

// NewHandlerWithConfig constructs a Handler with a populated config.
func NewHandlerWithConfig(sessions *mcpgw.SessionRegistry, d Dispatcher, log *slog.Logger, cfg HandlerConfig) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{sessions: sessions, dispatcher: d, log: log, cfg: cfg}
}

func (h *Handler) ServeHTTP(w nethttp.ResponseWriter, r *nethttp.Request) {
	// Spec 2025-11-25: invalid Origin headers MUST 403. The check runs
	// before any session work so a misbehaving browser never leaks
	// session state.
	if !h.originAllowed(r) {
		writeJSONError(w, nethttp.StatusForbidden, protocol.ErrInvalidRequest, "origin not allowed", map[string]string{"origin": r.Header.Get("Origin")})
		return
	}
	switch r.Method {
	case nethttp.MethodPost:
		h.handlePost(w, r)
	case nethttp.MethodGet:
		h.handleGet(w, r)
	case nethttp.MethodDelete:
		h.handleDelete(w, r)
	default:
		w.Header().Set("Allow", "POST, GET, DELETE")
		writeJSONError(w, nethttp.StatusMethodNotAllowed, protocol.ErrInvalidRequest, "method not allowed", nil)
	}
}

// originAllowed enforces the Streamable HTTP Origin guard (spec 2025-11-25).
// Requests without an Origin header (programmatic clients, curl, server-
// to-server) are always allowed. Requests carrying an Origin must match
// the configured allow-list, with an optional bypass for localhost in
// dev mode.
func (h *Handler) originAllowed(r *nethttp.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range h.cfg.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	if h.cfg.AllowLocalhostOrigins && isLocalhostOrigin(origin) {
		return true
	}
	return false
}

func isLocalhostOrigin(origin string) bool {
	// Origin format is scheme://host[:port]. Strip the scheme and
	// extract the host.
	host := origin
	if i := strings.Index(host, "://"); i > 0 {
		host = host[i+3:]
	}
	if i := strings.Index(host, "/"); i > 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i+1:], "]") {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// ----- POST /mcp ----------------------------------------------------------

func (h *Handler) handlePost(w nethttp.ResponseWriter, r *nethttp.Request) {
	body := readBody(r)

	// Server-initiated request response: a JSON-RPC reply whose id
	// matches a pending elicitation. The requester consumes it directly
	// and answers 202 Accepted; the dispatcher never sees it.
	if h.serverInit != nil && h.serverInit.TryDeliver(body) {
		w.WriteHeader(nethttp.StatusAccepted)
		return
	}

	var req protocol.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, nethttp.StatusBadRequest, protocol.ErrParseError, err.Error(), nil)
		return
	}
	if req.JSONRPC == "" {
		req.JSONRPC = protocol.JSONRPCVersion
	}

	// Resolve / create the session.
	sess, isNew := h.resolveSession(r, req)
	if sess == nil {
		writeJSONError(w, nethttp.StatusNotFound, protocol.ErrInvalidRequest, "session not found", map[string]string{"hint": "include Mcp-Session-Id from initialize response"})
		return
	}
	sess.Touch()

	// Phase 6: extract any traceparent the client supplied at the HTTP
	// layer so dispatcher spans link to the upstream trace. Per-method
	// `_meta.traceparent` (when carried in params) is read by the
	// dispatcher itself once it has the typed params.
	r = r.WithContext(telemetry.ExtractFromHTTP(r.Context(), r.Header))

	// Notifications never produce a body.
	if req.IsNotification() {
		var n protocol.Notification
		_ = json.Unmarshal(body, &n)
		h.dispatcher.HandleNotification(r.Context(), sess, &n) //nolint:contextcheck // r.Context after WithContext is the new ctx
		w.WriteHeader(nethttp.StatusAccepted)
		return
	}

	result, errBody := h.dispatcher.HandleRequest(r.Context(), sess, &req) //nolint:contextcheck // r.Context after WithContext is the new ctx

	if isNew {
		w.Header().Set(headerSessionID, sess.ID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(nethttp.StatusOK)

	resp := protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      req.ID,
		Result:  result,
		Error:   errBody,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// resolveSession returns the active session for the request. For initialize
// requests it always creates a fresh session; for any other method, it
// requires a Mcp-Session-Id header pointing at a known session. Sessions
// cannot be implicitly created by non-initialize requests.
func (h *Handler) resolveSession(r *nethttp.Request, req protocol.Request) (sess *mcpgw.Session, isNew bool) {
	sid := r.Header.Get(headerSessionID)
	if sid != "" {
		if existing, ok := h.sessions.Get(sid); ok {
			return existing, false
		}
		// Header was supplied but the session is unknown. Only initialize
		// is allowed to bootstrap a fresh session in that state.
		if req.Method != protocol.MethodInitialize {
			return nil, false
		}
	} else if req.Method != protocol.MethodInitialize {
		// No session id and not an initialize: reject. Per MCP spec the
		// client must complete the handshake before any other method.
		return nil, false
	}
	tenantID, userID, raw := identityFrom(r)
	return h.sessions.Create(tenantID, userID, raw), true
}

// ----- GET /mcp (SSE) -----------------------------------------------------

func (h *Handler) handleGet(w nethttp.ResponseWriter, r *nethttp.Request) {
	sid := r.Header.Get(headerSessionID)
	sess, ok := h.sessions.Get(sid)
	if !ok {
		writeJSONError(w, nethttp.StatusNotFound, protocol.ErrInvalidRequest, "session not found", nil)
		return
	}

	// Spec 2025-11-25: clients MUST send Accept: text/event-stream for
	// the SSE channel. Reject with 406 Not Acceptable when missing so
	// misconfigured clients fail loudly instead of pinning a hung GET.
	if !acceptsSSE(r.Header.Get("Accept")) {
		writeJSONError(w, nethttp.StatusNotAcceptable, protocol.ErrInvalidRequest, "Accept must include text/event-stream", nil)
		return
	}

	flusher, ok := w.(nethttp.Flusher)
	if !ok {
		writeJSONError(w, nethttp.StatusInternalServerError, protocol.ErrInternalError, "responsewriter is not a flusher", nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
	w.WriteHeader(nethttp.StatusOK)
	flusher.Flush()

	// Per-stream event-id counter. Spec 2025-11-25 says event ids should
	// encode stream identity; using sessionID-<n> satisfies that without
	// introducing a replay buffer (resumption is best-effort: clients
	// reconnect, server starts a fresh sequence).
	var seq atomic.Int64
	streamID := sess.ID

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case n, ok := <-sess.Notifications():
			if !ok {
				return // session closed
			}
			id := fmt.Sprintf("%s-%d", streamID, seq.Add(1))

			// Phase 5 server-initiated requests piggyback on the
			// notifications channel with a special method marker. Emit
			// them as `event: server_request` carrying the embedded
			// JSON-RPC envelope; the client POSTs back a normal response.
			if n.Method == serverRequestNotifMethod && len(n.Params) > 0 {
				fmt.Fprintf(w, "event: server_request\nid: %s\n", id)
				fmt.Fprintf(w, "data: %s\n\n", n.Params)
				flusher.Flush()
				continue
			}

			body, err := json.Marshal(n)
			if err != nil {
				h.log.Warn("sse marshal failed", "err", err)
				continue
			}
			fmt.Fprintf(w, "id: %s\n", id)
			fmt.Fprintf(w, "data: %s\n\n", body)
			flusher.Flush()
		}
	}
}

// acceptsSSE returns true when the Accept header (RFC 7231) lists
// text/event-stream or */*. Empty Accept is permissive — most clients
// forget to send it for GETs.
func acceptsSSE(accept string) bool {
	if accept == "" {
		return true
	}
	for _, part := range strings.Split(accept, ",") {
		mt := strings.TrimSpace(part)
		if i := strings.Index(mt, ";"); i >= 0 {
			mt = strings.TrimSpace(mt[:i])
		}
		switch mt {
		case "text/event-stream", "*/*", "text/*":
			return true
		}
	}
	return false
}

// ----- DELETE /mcp --------------------------------------------------------

func (h *Handler) handleDelete(w nethttp.ResponseWriter, r *nethttp.Request) {
	sid := r.Header.Get(headerSessionID)
	if sid == "" {
		writeJSONError(w, nethttp.StatusBadRequest, protocol.ErrInvalidRequest, "missing session id", nil)
		return
	}
	h.sessions.Close(sid)
	w.WriteHeader(nethttp.StatusNoContent)
}

// ----- helpers ------------------------------------------------------------

func identityFrom(r *nethttp.Request) (tenantID, userID, rawToken string) {
	if id, ok := tenant.From(r.Context()); ok {
		return id.TenantID, id.UserID, id.RawToken
	}
	return "", "", ""
}

func readBody(r *nethttp.Request) []byte {
	const max = 8 << 20 // 8 MiB
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		return body
	}
	if int64(len(body)) > max {
		return body[:max]
	}
	return body
}

func writeJSONError(w nethttp.ResponseWriter, status, code int, msg string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`null`),
		Error:   protocol.NewError(code, msg, data),
	}
	_ = json.NewEncoder(w).Encode(resp)
}
