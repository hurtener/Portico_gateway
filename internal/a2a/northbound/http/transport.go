// Package http is the northbound A2A transport: Portico's inbound A2A surface,
// mounted under /a2a on the same listener as /mcp, /v1, and /api. It serves
// Portico's agent card for discovery and a JSON-RPC 2.0 endpoint that routes
// unary A2A calls to a registered peer.
//
// V1 routing model — Portico is a governed single-endpoint A2A proxy: an
// inbound message/send (or tasks/get|cancel) names its target registered peer
// via params.metadata.portico_peer; the transport hands off to the governed
// dispatch path, which enforces the caller's Agent Profile, attaches egress
// credentials, dispatches, and audits. (The richer "Portico exposes its own
// skills routed by bridges" model lands with the bridge unit.)
//
// The transport is mounted INSIDE the auth group, so tenant identity + the
// resolved Agent Profile are already in the request context — A2A never bypasses
// the V1 envelope (tenant → auth → profile → audit).
package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
)

// maxBody caps an inbound JSON-RPC body (defensive; A2A payloads are small).
const maxBody = 8 << 20 // 8 MiB

// metaPeerKey is the params.metadata key naming the target registered peer for
// a governed proxy call.
const metaPeerKey = "portico_peer"

// Dispatcher is the governed outbound A2A path the transport routes to. The
// concrete implementation is *dispatch.Dispatcher; it is an interface here so
// the transport stays decoupled and unit-testable.
type Dispatcher interface {
	SendMessage(ctx context.Context, tenantID, peerID string, params a2a.MessageSendParams) (json.RawMessage, *a2a.Error)
	GetTask(ctx context.Context, tenantID, peerID string, params a2a.TaskQueryParams) (*a2a.Task, *a2a.Error)
	CancelTask(ctx context.Context, tenantID, peerID string, params a2a.TaskIDParams) (*a2a.Task, *a2a.Error)
}

// CardProvider returns Portico's agent card for the given tenant. Skills are
// aggregated from the tenant's registered peers once agent-card ingestion lands;
// until then the card advertises Portico's identity + capabilities.
type CardProvider func(ctx context.Context, tenantID string) a2a.AgentCard

// Handler serves the northbound A2A surface.
type Handler struct {
	disp Dispatcher
	card CardProvider
	log  *slog.Logger
}

// NewHandler builds the transport. log defaults to slog.Default().
func NewHandler(disp Dispatcher, card CardProvider, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{disp: disp, card: card, log: log}
}

// AgentCard serves GET /a2a/.well-known/agent.json — Portico's discovery
// document for the request's tenant.
func (h *Handler) AgentCard(w http.ResponseWriter, r *http.Request) {
	id, ok := tenant.From(r.Context())
	if !ok {
		http.Error(w, "no tenant", http.StatusUnauthorized)
		return
	}
	card := h.card(r.Context(), id.TenantID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(card)
}

// RPC serves POST /a2a — the JSON-RPC 2.0 endpoint. It parses the envelope,
// resolves the tenant from context, and routes unary methods to the governed
// dispatch path. Every response (incl. errors) echoes the request id.
func (h *Handler) RPC(w http.ResponseWriter, r *http.Request) {
	id, ok := tenant.From(r.Context())
	if !ok {
		writeErr(w, nil, a2a.NewError(a2a.ErrInternalError, "no tenant in context"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeErr(w, nil, a2a.NewError(a2a.ErrInternalError, "read body"))
		return
	}
	var req a2a.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, nil, a2a.NewError(a2a.ErrParseError, "parse error"))
		return
	}

	ctx := r.Context()
	switch req.Method {
	case a2a.MethodMessageSend:
		h.handleSendMessage(ctx, w, id.TenantID, req)
	case a2a.MethodTasksGet:
		h.handleGetTask(ctx, w, id.TenantID, req)
	case a2a.MethodTasksCancel:
		h.handleCancelTask(ctx, w, id.TenantID, req)
	default:
		writeErr(w, req.ID, a2a.NewError(a2a.ErrMethodNotFound, "method not found"))
	}
}

func (h *Handler) handleSendMessage(ctx context.Context, w http.ResponseWriter, tenantID string, req a2a.Request) {
	var params a2a.MessageSendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeErr(w, req.ID, a2a.NewError(a2a.ErrInvalidParams, "invalid params"))
		return
	}
	peerID, ok := metaPeer(params.Metadata)
	if !ok {
		writeErr(w, req.ID, a2a.NewError(a2a.ErrInvalidParams, "missing params.metadata."+metaPeerKey))
		return
	}
	res, aerr := h.disp.SendMessage(ctx, tenantID, peerID, params)
	if aerr != nil {
		writeErr(w, req.ID, aerr)
		return
	}
	writeResult(w, req.ID, res)
}

func (h *Handler) handleGetTask(ctx context.Context, w http.ResponseWriter, tenantID string, req a2a.Request) {
	var params a2a.TaskQueryParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeErr(w, req.ID, a2a.NewError(a2a.ErrInvalidParams, "invalid params"))
		return
	}
	peerID, ok := metaPeer(params.Metadata)
	if !ok {
		writeErr(w, req.ID, a2a.NewError(a2a.ErrInvalidParams, "missing params.metadata."+metaPeerKey))
		return
	}
	task, aerr := h.disp.GetTask(ctx, tenantID, peerID, params)
	if aerr != nil {
		writeErr(w, req.ID, aerr)
		return
	}
	writeTask(w, req.ID, task)
}

func (h *Handler) handleCancelTask(ctx context.Context, w http.ResponseWriter, tenantID string, req a2a.Request) {
	var params a2a.TaskIDParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeErr(w, req.ID, a2a.NewError(a2a.ErrInvalidParams, "invalid params"))
		return
	}
	peerID, ok := metaPeer(params.Metadata)
	if !ok {
		writeErr(w, req.ID, a2a.NewError(a2a.ErrInvalidParams, "missing params.metadata."+metaPeerKey))
		return
	}
	task, aerr := h.disp.CancelTask(ctx, tenantID, peerID, params)
	if aerr != nil {
		writeErr(w, req.ID, aerr)
		return
	}
	writeTask(w, req.ID, task)
}

// metaPeer extracts the target peer id from a params.metadata map.
func metaPeer(m map[string]any) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[metaPeerKey]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

func writeResult(w http.ResponseWriter, id, result json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a2a.Response{JSONRPC: a2a.JSONRPCVersion, ID: id, Result: result})
}

func writeTask(w http.ResponseWriter, id json.RawMessage, task *a2a.Task) {
	b, err := json.Marshal(task)
	if err != nil {
		writeErr(w, id, a2a.NewError(a2a.ErrInternalError, "marshal task"))
		return
	}
	writeResult(w, id, b)
}

func writeErr(w http.ResponseWriter, id json.RawMessage, e *a2a.Error) {
	w.Header().Set("Content-Type", "application/json")
	// JSON-RPC errors are carried in the 200 body (the transport succeeded).
	_ = json.NewEncoder(w).Encode(a2a.Response{JSONRPC: a2a.JSONRPCVersion, ID: id, Error: e})
}
