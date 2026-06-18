package virtualkeys_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	vk "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

func newStore(t *testing.T) ifaces.GovernanceStore {
	t.Helper()
	db, err := sqlite.Open(context.Background(), ":memory:", slog.Default())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db.Governance()
}

func TestParseToken(t *testing.T) {
	id, err := vk.NewID()
	if err != nil {
		t.Fatal(err)
	}
	token := vk.ComposeToken(id, "SECRET123")
	gotID, gotSecret, err := vk.ParseToken(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if gotID != id || gotSecret != "SECRET123" {
		t.Fatalf("parse mismatch: id=%q secret=%q", gotID, gotSecret)
	}
	for _, bad := range []string{
		"", "Bearer x", "sk-not-a-vk", "pk-portico-", "pk-portico-noseparator",
		"pk-portico-.secretonly", "pk-portico-vk_abc.", "pk-portico-wrongprefix.secret",
	} {
		if _, _, err := vk.ParseToken(bad); !errors.Is(err, vk.ErrMalformedToken) {
			t.Errorf("ParseToken(%q): want ErrMalformedToken, got %v", bad, err)
		}
	}
}

func TestHMAC_VerifyRejectsWrongSecret(t *testing.T) {
	salt, _ := vk.NewSalt()
	mac := vk.ComputeHMAC(salt, "right")
	if !vk.VerifyHMAC(salt, mac, "right") {
		t.Fatal("correct secret must verify")
	}
	if vk.VerifyHMAC(salt, mac, "wrong") {
		t.Fatal("wrong secret must NOT verify")
	}
	// A different salt with the same secret must not verify against the old mac.
	salt2, _ := vk.NewSalt()
	if vk.VerifyHMAC(salt2, mac, "right") {
		t.Fatal("different salt must not verify")
	}
}

func TestService_Create_ReturnsSecretOnce_NoPlaintextStored(t *testing.T) {
	store := newStore(t)
	svc := vk.NewService(store)
	ctx := context.Background()

	created, err := svc.Create(ctx, vk.CreateParams{
		TenantID: "t", Name: "prod", Scopes: []string{"llm:invoke"},
		ProviderAllowlist: []string{"anthropic"}, ModelAllowlist: []string{"claude-3-5-sonnet"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(created.Token, vk.TokenPrefix) {
		t.Fatalf("token missing prefix: %q", created.Token)
	}
	// The stored row must carry salt+hmac and NOT the secret.
	stored, err := store.GetVirtualKey(ctx, "t", created.VK.ID)
	if err != nil {
		t.Fatalf("get stored: %v", err)
	}
	if len(stored.Salt) == 0 || len(stored.HMAC) == 0 {
		t.Fatal("stored VK missing salt/hmac")
	}
	_, secret, _ := vk.ParseToken(created.Token)
	if strings.Contains(string(stored.HMAC), secret) || strings.Contains(string(stored.Salt), secret) {
		t.Fatal("secret leaked into stored salt/hmac")
	}
	// The stored HMAC must verify against the issued secret.
	if !vk.VerifyHMAC(stored.Salt, stored.HMAC, secret) {
		t.Fatal("stored hmac does not verify the issued secret")
	}
}

func TestResolver_HappyPath(t *testing.T) {
	store := newStore(t)
	svc := vk.NewService(store)
	res := vk.NewResolver(store, 0)
	ctx := context.Background()

	created, err := svc.Create(ctx, vk.CreateParams{
		TenantID: "tenant-a", Name: "app", Scopes: []string{"llm:invoke", "mcp:call"},
		MCPServerAllowlist: []string{"github"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	r, err := res.Resolve(ctx, created.Token)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r.TenantID != "tenant-a" || r.VKID != created.VK.ID {
		t.Fatalf("resolved wrong VK: %+v", r)
	}
	if !r.HasScope("llm:invoke") || !r.HasScope("mcp:call") {
		t.Fatalf("scopes not hydrated: %+v", r.Scopes)
	}
	if !r.AllowsServer("github") || r.AllowsServer("jira") {
		t.Fatalf("MCP allowlist wrong: %+v", r.MCPServerAllowlist)
	}
	// Empty allowlists mean "all".
	if !r.AllowsProvider("anything") || !r.AllowsModel("anything") {
		t.Fatal("empty provider/model allowlist should allow all")
	}
	// Second resolve hits the cache and returns the same pointer-equivalent state.
	if _, err := res.Resolve(ctx, created.Token); err != nil {
		t.Fatalf("cached resolve: %v", err)
	}
}

func TestResolver_WrongSecret_Unknown(t *testing.T) {
	store := newStore(t)
	svc := vk.NewService(store)
	res := vk.NewResolver(store, 0)
	ctx := context.Background()

	created, _ := svc.Create(ctx, vk.CreateParams{TenantID: "t", Name: "n"})
	// Tamper the secret.
	forged := vk.ComposeToken(created.VK.ID, "totally-wrong-secret-value-000000000000")
	if _, err := res.Resolve(ctx, forged); !errors.Is(err, vk.ErrUnknown) {
		t.Fatalf("forged secret: want ErrUnknown, got %v", err)
	}
	// Malformed token: ErrUnknown, no DB dependency.
	if _, err := res.Resolve(ctx, "pk-portico-garbage"); !errors.Is(err, vk.ErrUnknown) {
		t.Fatalf("malformed: want ErrUnknown, got %v", err)
	}
	// Unknown id.
	if _, err := res.Resolve(ctx, vk.ComposeToken("vk_deadbeefdeadbeefdeadbeef", "x")); !errors.Is(err, vk.ErrUnknown) {
		t.Fatalf("unknown id: want ErrUnknown, got %v", err)
	}
}

func TestResolver_RevocationImmediate(t *testing.T) {
	store := newStore(t)
	svc := vk.NewService(store)
	res := vk.NewResolver(store, 0)
	ctx := context.Background()

	created, _ := svc.Create(ctx, vk.CreateParams{TenantID: "t", Name: "n"})
	if _, err := res.Resolve(ctx, created.Token); err != nil {
		t.Fatalf("pre-revoke resolve: %v", err)
	}
	if err := svc.Revoke(ctx, "t", created.VK.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	res.InvalidateVK(created.VK.ID) // what the REST handler does
	if _, err := res.Resolve(ctx, created.Token); !errors.Is(err, vk.ErrRevoked) {
		t.Fatalf("after revoke: want ErrRevoked, got %v", err)
	}
}

func TestResolver_RotationPreservesIdentity(t *testing.T) {
	store := newStore(t)
	svc := vk.NewService(store)
	res := vk.NewResolver(store, 0)
	ctx := context.Background()

	created, _ := svc.Create(ctx, vk.CreateParams{TenantID: "t", Name: "n", Scopes: []string{"llm:invoke"}})
	oldToken := created.Token
	if _, err := res.Resolve(ctx, oldToken); err != nil {
		t.Fatalf("pre-rotate resolve: %v", err)
	}

	rotated, err := svc.Rotate(ctx, "t", created.VK.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	res.InvalidateVK(created.VK.ID)
	if rotated.VK.ID != created.VK.ID {
		t.Fatal("rotation must preserve the VK id")
	}
	// Old token stops working.
	if _, err := res.Resolve(ctx, oldToken); !errors.Is(err, vk.ErrUnknown) {
		t.Fatalf("old token after rotate: want ErrUnknown, got %v", err)
	}
	// New token works and keeps scopes.
	r, err := res.Resolve(ctx, rotated.Token)
	if err != nil {
		t.Fatalf("new token resolve: %v", err)
	}
	if !r.HasScope("llm:invoke") {
		t.Fatal("rotated VK lost its scopes")
	}
}

func TestResolver_CrossTenantImpossible(t *testing.T) {
	store := newStore(t)
	svc := vk.NewService(store)
	res := vk.NewResolver(store, 0)
	ctx := context.Background()

	a, _ := svc.Create(ctx, vk.CreateParams{TenantID: "tenant-a", Name: "a"})
	// A's token ALWAYS resolves to tenant-a — there is no way to present it "as"
	// tenant-b; the tenant is derived from the VK, not from any caller input.
	r, err := res.Resolve(ctx, a.Token)
	if err != nil || r.TenantID != "tenant-a" {
		t.Fatalf("VK must resolve to its own tenant: tenant=%q err=%v", r.TenantID, err)
	}
}
