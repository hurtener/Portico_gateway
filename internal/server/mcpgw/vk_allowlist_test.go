package mcpgw

import (
	"context"
	"testing"

	virtualkeys "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

func TestCheckServerAllowedByVK(t *testing.T) {
	d := &Dispatcher{} // nil emitter → audit is a no-op
	sess := &Session{ID: "s1", TenantID: "t1", UserID: "u1"}

	// No VK on the context (JWT caller) → allowed.
	if perr := d.checkServerAllowedByVK(context.Background(), sess, "github"); perr != nil {
		t.Fatalf("non-VK request must be allowed, got %+v", perr)
	}

	// VK with an empty allowlist → all servers allowed.
	ctxAll := virtualkeys.WithResolved(context.Background(), &virtualkeys.Resolved{VKID: "vk1", TenantID: "t1"})
	if perr := d.checkServerAllowedByVK(ctxAll, sess, "anything"); perr != nil {
		t.Fatalf("empty allowlist must allow all, got %+v", perr)
	}

	// VK scoped to [github]: github allowed, jira rejected with the VK code.
	ctxGH := virtualkeys.WithResolved(context.Background(), &virtualkeys.Resolved{
		VKID: "vk1", TenantID: "t1", MCPServerAllowlist: []string{"github"},
	})
	if perr := d.checkServerAllowedByVK(ctxGH, sess, "github"); perr != nil {
		t.Fatalf("github must be allowed, got %+v", perr)
	}
	perr := d.checkServerAllowedByVK(ctxGH, sess, "jira")
	if perr == nil {
		t.Fatal("jira must be rejected by a github-only VK")
	}
	if perr.Code != protocol.ErrVKScopeViolation {
		t.Fatalf("want ErrVKScopeViolation (%d), got %d", protocol.ErrVKScopeViolation, perr.Code)
	}
}
