package mcpgw

import (
	"context"
	"encoding/json"
	"time"

	a2aproto "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/profiles"
)

// A2ABridge is the governed outbound A2A path an MCP→A2A bridge dispatches to.
// Satisfied by *dispatch.Dispatcher; declared as an interface so mcpgw does not
// import the dispatch package (the binary injects it via SetA2ABridge).
type A2ABridge interface {
	SendMessageByPeerName(ctx context.Context, tenantID, peerName string, params a2aproto.MessageSendParams) (json.RawMessage, *a2aproto.Error)
}

// SetA2ABridge wires the outbound A2A dispatcher used by MCP→A2A bridge routes.
// Optional — when unset, no MCP tool is bridged and all calls route over MCP.
func (d *Dispatcher) SetA2ABridge(b A2ABridge) { d.a2aBridge = b }

// tryMCPToA2ABridge checks whether the caller's Agent Profile bridges
// params.Name to an A2A peer task. When it does, the call dispatches over A2A
// (governed by AllowsA2APeer in the dispatcher + AllowsA2ATask here, since the
// task is explicit) and the result is translated back to an MCP CallToolResult;
// it returns (result, err, true). When no bridge applies it returns
// (_, _, false) and normal MCP routing continues.
//
// A bridged tool is governed by the bridge + the A2A entitlement gates, NOT the
// MCP tool surface (it never reaches an MCP server) — so this runs before the
// AllowsTool surface check.
func (d *Dispatcher) tryMCPToA2ABridge(ctx context.Context, sess *Session, params protocol.CallToolParams, reqID string) (json.RawMessage, *protocol.Error, bool) {
	if d.a2aBridge == nil {
		return nil, nil, false
	}
	prof := profiles.FromContext(ctx)
	bridge, ok := prof.BridgeForMCPTool(params.Name)
	if !ok {
		return nil, nil, false
	}

	// The bridged A2A task is explicit → enforce per-task entitlement (the peer
	// gate is applied inside the A2A dispatcher).
	namespacedTask := bridge.A2APeer + "." + bridge.A2ATask
	if prof != nil && !prof.IsDefault && !prof.AllowsA2ATask(namespacedTask) {
		d.emitAgentProfileViolation(ctx, sess, prof, namespacedTask, "a2a_task_outside_profile")
		return nil, protocol.NewError(protocol.ErrAgentProfileViolation, "agent profile violation", map[string]any{
			"profile_id": prof.ID,
			"a2a_task":   namespacedTask,
			"reason":     "a2a_task_outside_profile",
		}), true
	}

	msgID := "mcp-bridge"
	if reqID != "" {
		msgID = "mcp-bridge-" + reqID
	}
	msgParams := a2aproto.MessageSendParams{
		Message: a2aproto.Message{
			Role:      a2aproto.RoleUser,
			MessageID: msgID,
			Kind:      a2aproto.KindMessage,
			Parts:     []a2aproto.Part{mcpArgsToA2APart(params.Arguments)},
		},
		Metadata: map[string]any{"a2a_task": bridge.A2ATask, "bridged_from_mcp_tool": params.Name},
	}

	startedAt := time.Now()
	raw, aerr := d.a2aBridge.SendMessageByPeerName(ctx, sess.TenantID, bridge.A2APeer, msgParams)
	if aerr != nil {
		d.emitBridge(ctx, sess, params.Name, bridge.A2APeer, bridge.A2ATask, time.Since(startedAt), aerr)
		return nil, protocol.NewError(protocol.ErrUpstreamUnavailable, aerr.Message, map[string]any{
			"a2a_code": aerr.Code,
			"peer":     bridge.A2APeer,
			"a2a_task": bridge.A2ATask,
		}), true
	}
	d.emitBridge(ctx, sess, params.Name, bridge.A2APeer, bridge.A2ATask, time.Since(startedAt), nil)

	// Translate the verbatim A2A result (a Task or Message) into an MCP
	// CallToolResult: the JSON is carried as a text content block and as
	// structuredContent so MCP clients can consume either shape.
	result := protocol.CallToolResult{
		Content:           []protocol.ContentBlock{{Type: "text", Text: string(raw)}},
		StructuredContent: raw,
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, "marshal bridged result", nil), true
	}
	return body, nil, true
}

// mcpArgsToA2APart maps MCP tool arguments onto an A2A message Part. Object args
// become a DataPart (lossless); a non-object payload (or empty) falls back to a
// text part / empty data so the message is always well-formed.
func mcpArgsToA2APart(args json.RawMessage) a2aproto.Part {
	if len(args) == 0 {
		return a2aproto.Part{Kind: a2aproto.PartKindData, Data: map[string]any{}}
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err == nil {
		return a2aproto.Part{Kind: a2aproto.PartKindData, Data: m}
	}
	return a2aproto.Part{Kind: a2aproto.PartKindText, Text: string(args)}
}

// emitBridge records a bridged MCP→A2A dispatch (nil-safe emitter). The A2A
// dispatcher also emits a2a.dispatch on success; this marks the MCP side.
func (d *Dispatcher) emitBridge(ctx context.Context, sess *Session, mcpTool, peer, task string, dur time.Duration, aerr *a2aproto.Error) {
	if d.emitter == nil {
		return
	}
	payload := map[string]any{
		"mcp_tool":    mcpTool,
		"a2a_peer":    peer,
		"a2a_task":    task,
		"duration_ms": dur.Milliseconds(),
	}
	evType := audit.EventA2ADispatch
	if aerr != nil {
		payload["error_code"] = aerr.Code
		evType = audit.EventToolCallFailed
	}
	d.emitter.Emit(ctx, audit.Event{
		Type:       evType,
		TenantID:   sess.TenantID,
		SessionID:  sess.ID,
		UserID:     sess.UserID,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
}
