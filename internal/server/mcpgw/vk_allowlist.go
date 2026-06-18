package mcpgw

import (
	"context"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	virtualkeys "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// checkServerAllowedByVK rejects an MCP tool call whose downstream server falls
// outside the caller's Virtual Key MCP-server allowlist (Phase 15.5, acceptance
// #16). When the caller is not a VK (JWT auth) or the VK has no server allowlist
// (empty = all), it is a no-op. This is reached by direct tools/call AND by
// in-sandbox Code Mode calls (they share dispatchToolCall), so a VK's allowlist
// also constrains Code Mode — a github-only VK cannot reach jira via the sandbox.
func (d *Dispatcher) checkServerAllowedByVK(ctx context.Context, sess *Session, serverID string) *protocol.Error {
	vk, ok := virtualkeys.FromContext(ctx)
	if !ok || vk == nil || vk.AllowsServer(serverID) {
		return nil
	}
	d.emitVKScopeViolation(ctx, sess, vk.VKID, serverID)
	return protocol.NewError(protocol.ErrVKScopeViolation, "virtual key scope violation", map[string]any{
		"vk_id":     vk.VKID,
		"server_id": serverID,
		"reason":    "server_outside_vk_allowlist",
	})
}

// emitVKScopeViolation records the rejection (nil-safe emitter).
func (d *Dispatcher) emitVKScopeViolation(ctx context.Context, sess *Session, vkID, serverID string) {
	if d.emitter == nil {
		return
	}
	d.emitter.Emit(ctx, audit.Event{
		Type:       audit.EventVKScopeViolation,
		TenantID:   sess.TenantID,
		SessionID:  sess.ID,
		UserID:     sess.UserID,
		OccurredAt: time.Now().UTC(),
		Payload:    map[string]any{"vk_id": vkID, "server_id": serverID, "reason": "server_outside_vk_allowlist"},
	})
}
