package mcpgw

import (
	"context"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/profiles"
)

// Agent Profile enforcement on the MCP path (Phase 14). The profile is resolved
// into the request context by the profile middleware; the dispatcher reads it
// via profiles.FromContext and gates tools/list (filtering) and tools/call
// (rejection). A nil or default profile allows everything, so existing clients
// with no profile bound are unaffected (back-compat).

// filterToolsByProfile drops tools the request's agent profile does not allow.
// A nil/default profile returns the slice unchanged (Allows* short-circuits to
// true), so the common path allocates nothing extra.
func filterToolsByProfile(ctx context.Context, tools []protocol.Tool) []protocol.Tool {
	prof := profiles.FromContext(ctx)
	if prof == nil || prof.IsDefault {
		return tools
	}
	out := make([]protocol.Tool, 0, len(tools))
	for _, t := range tools {
		if prof.AllowsTool(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// checkToolAllowedByProfile rejects a tools/call whose tool is outside the
// caller's agent profile surface, with a typed agent_profile_violation error and
// an audit event. Returns nil when allowed (incl. the nil/default profile).
func (d *Dispatcher) checkToolAllowedByProfile(ctx context.Context, sess *Session, tool string) *protocol.Error {
	prof := profiles.FromContext(ctx)
	if prof == nil || prof.IsDefault || prof.AllowsTool(tool) {
		return nil
	}
	d.emitAgentProfileViolation(ctx, sess, prof, tool, "tool_outside_profile")
	return protocol.NewError(protocol.ErrAgentProfileViolation, "agent profile violation", map[string]any{
		"profile_id": prof.ID,
		"tool":       tool,
		"reason":     "tool_outside_profile",
	})
}

// emitAgentProfileViolation records the rejection (nil-safe emitter).
func (d *Dispatcher) emitAgentProfileViolation(ctx context.Context, sess *Session, prof *profiles.Profile, tool, reason string) {
	if d.emitter == nil {
		return
	}
	d.emitter.Emit(ctx, audit.Event{
		Type:       audit.EventAgentProfileViolation,
		TenantID:   sess.TenantID,
		SessionID:  sess.ID,
		UserID:     sess.UserID,
		OccurredAt: time.Now().UTC(),
		Payload:    map[string]any{"profile_id": prof.ID, "tool": tool, "reason": reason},
	})
}
