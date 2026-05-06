// Package http is the northbound MCP transport (Streamable HTTP). Phase 1
// supports POST /mcp returning application/json, GET /mcp opening a long-
// lived SSE channel for server-to-client notifications, and DELETE /mcp
// terminating a session.
//
// SSE on the POST response (intermediate progress + final result in one
// stream) is intentionally deferred: progress notifications go to the GET
// SSE channel instead. Per spec both placements are valid.
package http

import (
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"

	"log/slog"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
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

// Handler is the http.Handler that implements MCP-over-HTTP.
type Handler struct {
	sessions   *mcpgw.SessionRegistry
	dispatcher Dispatcher
	log        *slog.Logger
}

func NewHandler(sessions *mcpgw.SessionRegistry, d Dispatcher, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{sessions: sessions, dispatcher: d, log: log}
}

func (h *Handler) ServeHTTP(w nethttp.ResponseWriter, r *nethttp.Request) {
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

// ----- POST /mcp ----------------------------------------------------------

func (h *Handler) handlePost(w nethttp.ResponseWriter, r *nethttp.Request) {
	body := readBody(r)
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

	// Notifications never produce a body.
	if req.IsNotification() {
		var n protocol.Notification
		_ = json.Unmarshal(body, &n)
		h.dispatcher.HandleNotification(r.Context(), sess, &n)
		w.WriteHeader(nethttp.StatusAccepted)
		return
	}

	result, errBody := h.dispatcher.HandleRequest(r.Context(), sess, &req)

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
	tenantID, userID := identityFrom(r)
	return h.sessions.Create(tenantID, userID), true
}

// ----- GET /mcp (SSE) -----------------------------------------------------

func (h *Handler) handleGet(w nethttp.ResponseWriter, r *nethttp.Request) {
	sid := r.Header.Get(headerSessionID)
	sess, ok := h.sessions.Get(sid)
	if !ok {
		writeJSONError(w, nethttp.StatusNotFound, protocol.ErrInvalidRequest, "session not found", nil)
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
	w.WriteHeader(nethttp.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case n, ok := <-sess.Notifications():
			if !ok {
				return // session closed
			}
			body, err := json.Marshal(n)
			if err != nil {
				h.log.Warn("sse marshal failed", "err", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", body)
			flusher.Flush()
		}
	}
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

func identityFrom(r *nethttp.Request) (tenantID, userID string) {
	if id, ok := tenant.From(r.Context()); ok {
		return id.TenantID, id.UserID
	}
	return "", ""
}

func readBody(r *nethttp.Request) []byte {
	const max = 8 << 20 // 8 MiB
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if len(buf) > max {
				return buf[:max]
			}
		}
		if err != nil {
			break
		}
	}
	return buf
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
