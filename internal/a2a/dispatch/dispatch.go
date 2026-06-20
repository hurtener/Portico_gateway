// Package dispatch is the governed outbound A2A call path: it enforces the
// caller's Agent Profile, acquires a pooled southbound client for the target
// peer, dispatches the unary A2A method, and audits the result. The northbound
// transport and the MCP↔A2A bridges both call through here so every A2A call
// — however it enters Portico — traverses the identical governed envelope
// (tenant → profile → pool → audit).
//
// Profile enforcement here is peer-level (Profile.AllowsA2APeer): A2A
// `message/send` carries no explicit task id (the agent decides), so per-task
// gating (Profile.AllowsA2ATask) lives in the bridge layer, where a named task
// is explicit. The Agent Profile remains the single source of truth for
// entitlement (AGENTS.md §13).
package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	"github.com/hurtener/Portico_gateway/internal/a2a/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/profiles"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Dispatcher governs outbound A2A calls to registered peers.
type Dispatcher struct {
	store   ifaces.A2APeerStore
	pool    *manager.Pool
	emitter audit.Emitter
	log     *slog.Logger
}

// New builds a Dispatcher. emitter may be nil (auditing becomes a no-op);
// log defaults to slog.Default().
func New(store ifaces.A2APeerStore, pool *manager.Pool, emitter audit.Emitter, log *slog.Logger) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{store: store, pool: pool, emitter: emitter, log: log}
}

// SendMessage dispatches a unary message/send to peer peerID on behalf of the
// caller in ctx. Returns the raw JSON result (Task or Message) or a JSON-RPC
// *a2a.Error (profile violation, unknown/disabled peer, or transport failure).
func (d *Dispatcher) SendMessage(ctx context.Context, tenantID, peerID string, params a2a.MessageSendParams) (json.RawMessage, *a2a.Error) {
	peer, aerr := d.resolveAndEnforce(ctx, tenantID, peerID)
	if aerr != nil {
		return nil, aerr
	}
	client, err := d.pool.Acquire(ctx, tenantID, peerID)
	if err != nil {
		return nil, mapErr(err)
	}
	res, err := client.SendMessage(ctx, params)
	if err != nil {
		return nil, mapErr(err)
	}
	d.auditDispatch(ctx, tenantID, peer, a2a.MethodMessageSend, "")
	return res, nil
}

// GetTask dispatches tasks/get to peer peerID, governed identically.
func (d *Dispatcher) GetTask(ctx context.Context, tenantID, peerID string, params a2a.TaskQueryParams) (*a2a.Task, *a2a.Error) {
	peer, aerr := d.resolveAndEnforce(ctx, tenantID, peerID)
	if aerr != nil {
		return nil, aerr
	}
	client, err := d.pool.Acquire(ctx, tenantID, peerID)
	if err != nil {
		return nil, mapErr(err)
	}
	task, err := client.GetTask(ctx, params)
	if err != nil {
		return nil, mapErr(err)
	}
	d.auditDispatch(ctx, tenantID, peer, a2a.MethodTasksGet, params.ID)
	return task, nil
}

// CancelTask dispatches tasks/cancel to peer peerID, governed identically.
func (d *Dispatcher) CancelTask(ctx context.Context, tenantID, peerID string, params a2a.TaskIDParams) (*a2a.Task, *a2a.Error) {
	peer, aerr := d.resolveAndEnforce(ctx, tenantID, peerID)
	if aerr != nil {
		return nil, aerr
	}
	client, err := d.pool.Acquire(ctx, tenantID, peerID)
	if err != nil {
		return nil, mapErr(err)
	}
	task, err := client.CancelTask(ctx, params)
	if err != nil {
		return nil, mapErr(err)
	}
	d.auditDispatch(ctx, tenantID, peer, a2a.MethodTasksCancel, params.ID)
	return task, nil
}

// resolveAndEnforce loads the peer (tenant-scoped) and enforces the caller's
// Agent Profile AllowsA2APeer. Returns the peer, or a JSON-RPC error: unknown
// peer → ErrInvalidParams; profile denies the peer → ErrProfileViolation (with
// an audited agent_profile.violation event).
func (d *Dispatcher) resolveAndEnforce(ctx context.Context, tenantID, peerID string) (*ifaces.A2APeer, *a2a.Error) {
	peer, err := d.store.GetPeer(ctx, tenantID, peerID)
	if err != nil {
		if errors.Is(err, ifaces.ErrA2APeerNotFound) {
			return nil, a2a.NewError(a2a.ErrInvalidParams, "unknown a2a peer")
		}
		return nil, a2a.NewError(a2a.ErrInternalError, "a2a peer lookup failed")
	}
	prof := profiles.FromContext(ctx)
	if prof != nil && !prof.IsDefault && !prof.AllowsA2APeer(peer.Name) {
		d.auditProfileViolation(ctx, tenantID, prof.ID, peer.Name)
		return nil, &a2a.Error{
			Code:    a2a.ErrProfileViolation,
			Message: "agent profile violation",
			Data:    mustJSON(map[string]any{"profile_id": prof.ID, "peer": peer.Name, "reason": "peer_outside_profile"}),
		}
	}
	return peer, nil
}

// mapErr converts a southbound/pool error into a JSON-RPC *a2a.Error. An
// existing *a2a.Error (a peer's JSON-RPC error response) passes through; a
// disabled peer → ErrUnsupportedOperation; an unknown peer (race) →
// ErrInvalidParams; anything else → ErrInternalError.
func mapErr(err error) *a2a.Error {
	if err == nil {
		return nil
	}
	var pe *a2a.Error
	if errors.As(err, &pe) {
		return pe
	}
	if errors.Is(err, manager.ErrPeerDisabled) {
		return a2a.NewError(a2a.ErrUnsupportedOperation, "a2a peer is disabled")
	}
	if errors.Is(err, ifaces.ErrA2APeerNotFound) {
		return a2a.NewError(a2a.ErrInvalidParams, "unknown a2a peer")
	}
	return a2a.NewError(a2a.ErrInternalError, err.Error())
}

func (d *Dispatcher) auditDispatch(ctx context.Context, tenantID string, peer *ifaces.A2APeer, method, taskID string) {
	if d.emitter == nil {
		return
	}
	payload := map[string]any{"peer_id": peer.ID, "peer": peer.Name, "method": method}
	if taskID != "" {
		payload["task_id"] = taskID
	}
	d.emitter.Emit(ctx, audit.Event{
		Type:       audit.EventA2ADispatch,
		TenantID:   tenantID,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
}

func (d *Dispatcher) auditProfileViolation(ctx context.Context, tenantID, profileID, peerName string) {
	if d.emitter == nil {
		return
	}
	d.emitter.Emit(ctx, audit.Event{
		Type:       audit.EventAgentProfileViolation,
		TenantID:   tenantID,
		OccurredAt: time.Now().UTC(),
		Payload:    map[string]any{"profile_id": profileID, "peer": peerName, "reason": "peer_outside_profile"},
	})
}

// mustJSON marshals v for an error Data field; on the impossible marshal error
// it returns null rather than panicking in a request path.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}
