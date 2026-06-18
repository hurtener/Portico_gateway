package virtualkeys_test

import (
	"context"
	"errors"
	"testing"

	vk "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestLooksLikeVK(t *testing.T) {
	if !vk.LooksLikeVK("pk-portico-vk_abc.secret") {
		t.Fatal("VK token should be recognised")
	}
	for _, s := range []string{"", "Bearer eyJ...", "sk-openai", "pkportico"} {
		if vk.LooksLikeVK(s) {
			t.Errorf("%q should not look like a VK", s)
		}
	}
}

func TestResolved_Context(t *testing.T) {
	ctx := context.Background()
	if _, ok := vk.FromContext(ctx); ok {
		t.Fatal("empty context should have no resolved VK")
	}
	r := &vk.Resolved{VKID: "vk_1", TenantID: "t"}
	ctx = vk.WithResolved(ctx, r)
	got, ok := vk.FromContext(ctx)
	if !ok || got.VKID != "vk_1" {
		t.Fatalf("context round-trip failed: ok=%v got=%+v", ok, got)
	}
}

func TestResolved_Allowlists(t *testing.T) {
	r := &vk.Resolved{
		ProviderAllowlist: []string{"anthropic"},
		ModelAllowlist:    []string{"claude-3-5-sonnet"},
	}
	if !r.AllowsProvider("anthropic") || r.AllowsProvider("openai") {
		t.Fatal("provider allowlist match/deny wrong")
	}
	if !r.AllowsModel("claude-3-5-sonnet") || r.AllowsModel("gpt-4") {
		t.Fatal("model allowlist match/deny wrong")
	}
	if r.HasScope("nope") {
		t.Fatal("HasScope false positive")
	}
}

func TestService_Create_Validation(t *testing.T) {
	svc := vk.NewService(newStore(t))
	ctx := context.Background()
	if _, err := svc.Create(ctx, vk.CreateParams{Name: "n"}); err == nil {
		t.Fatal("missing tenant_id should error")
	}
	if _, err := svc.Create(ctx, vk.CreateParams{TenantID: "t"}); err == nil {
		t.Fatal("missing name should error")
	}
	if _, err := svc.Create(ctx, vk.CreateParams{TenantID: "t", Name: "n", ParentKind: "team"}); err == nil {
		t.Fatal("parent_kind without parent_id should error")
	}
}

func TestService_RotateRevoke_MissingVK(t *testing.T) {
	svc := vk.NewService(newStore(t))
	ctx := context.Background()
	if _, err := svc.Rotate(ctx, "t", "vk_missing"); !errors.Is(err, ifaces.ErrGovernanceNotFound) {
		t.Fatalf("rotate missing: want ErrGovernanceNotFound, got %v", err)
	}
	if err := svc.Revoke(ctx, "t", "vk_missing"); !errors.Is(err, ifaces.ErrGovernanceNotFound) {
		t.Fatalf("revoke missing: want ErrGovernanceNotFound, got %v", err)
	}
}

func TestService_Create_WithParent(t *testing.T) {
	svc := vk.NewService(newStore(t))
	ctx := context.Background()
	created, err := svc.Create(ctx, vk.CreateParams{
		TenantID: "t", Name: "n", ParentKind: "team", ParentID: "tm-1",
	})
	if err != nil {
		t.Fatalf("create with parent: %v", err)
	}
	if created.VK.ParentKind != "team" || created.VK.ParentID != "tm-1" {
		t.Fatalf("parent not set: %+v", created.VK)
	}
}
