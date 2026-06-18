package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestGovernanceStore_Customer_RoundTrip(t *testing.T) {
	db := open(t)
	store := db.Governance()
	ctx := context.Background()

	c := &ifaces.Customer{
		TenantID:    "tenant-a",
		ID:          "cust-1",
		Name:        "Acme",
		Description: "big customer",
		WebhookURL:  "https://hooks.example/acme",
	}
	if err := store.PutCustomer(ctx, c); err != nil {
		t.Fatalf("put customer: %v", err)
	}
	got, err := store.GetCustomer(ctx, "tenant-a", "cust-1")
	if err != nil {
		t.Fatalf("get customer: %v", err)
	}
	if got.Name != "Acme" || got.Description != "big customer" || got.WebhookURL != "https://hooks.example/acme" {
		t.Errorf("customer mismatch: %+v", got)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Errorf("timestamps not set: %+v", got)
	}

	if _, err := store.GetCustomer(ctx, "tenant-a", "nope"); !errors.Is(err, ifaces.ErrGovernanceNotFound) {
		t.Fatalf("want ErrGovernanceNotFound, got %v", err)
	}
}

func TestGovernanceStore_Team_CustomerFK_SetNullOnDelete(t *testing.T) {
	db := open(t)
	store := db.Governance()
	ctx := context.Background()

	if err := store.PutCustomer(ctx, &ifaces.Customer{TenantID: "t", ID: "c1", Name: "C1"}); err != nil {
		t.Fatalf("put customer: %v", err)
	}
	if err := store.PutTeam(ctx, &ifaces.Team{TenantID: "t", ID: "tm1", CustomerID: "c1", Name: "Marketing"}); err != nil {
		t.Fatalf("put team: %v", err)
	}
	if err := store.DeleteCustomer(ctx, "t", "c1"); err != nil {
		t.Fatalf("delete customer: %v", err)
	}
	got, err := store.GetTeam(ctx, "t", "tm1")
	if err != nil {
		t.Fatalf("get team after customer delete: %v", err)
	}
	if got.CustomerID != "" {
		t.Errorf("customer_id should be NULL (empty) after FK SET NULL, got %q", got.CustomerID)
	}
}

func TestGovernanceStore_VK_RoundTrip_AllowlistsAndSecret(t *testing.T) {
	db := open(t)
	store := db.Governance()
	ctx := context.Background()

	vk := &ifaces.VirtualKey{
		TenantID:           "tenant-a",
		ID:                 "vk-1",
		Name:               "prod-app",
		Salt:               []byte{1, 2, 3, 4},
		HMAC:               []byte{9, 8, 7, 6, 5},
		ParentKind:         "team",
		ParentID:           "tm-1",
		ProfileID:          "",
		Scopes:             []string{"llm:invoke", "mcp:call"},
		ProviderAllowlist:  []string{"anthropic", "openai"},
		ModelAllowlist:     []string{"claude-3-5-sonnet"},
		MCPServerAllowlist: []string{"github", "jira"},
		Enabled:            true,
	}
	if err := store.PutVirtualKey(ctx, vk); err != nil {
		t.Fatalf("put vk: %v", err)
	}
	got, err := store.GetVirtualKey(ctx, "tenant-a", "vk-1")
	if err != nil {
		t.Fatalf("get vk: %v", err)
	}
	if got.Name != "prod-app" || got.ParentKind != "team" || got.ParentID != "tm-1" || !got.Enabled {
		t.Errorf("vk scalar mismatch: %+v", got)
	}
	if string(got.Salt) != string(vk.Salt) || string(got.HMAC) != string(vk.HMAC) {
		t.Errorf("salt/hmac not round-tripped: salt=%v hmac=%v", got.Salt, got.HMAC)
	}
	assertSliceEqual(t, "Scopes", got.Scopes, vk.Scopes)
	assertSliceEqual(t, "ProviderAllowlist", got.ProviderAllowlist, vk.ProviderAllowlist)
	assertSliceEqual(t, "ModelAllowlist", got.ModelAllowlist, vk.ModelAllowlist)
	assertSliceEqual(t, "MCPServerAllowlist", got.MCPServerAllowlist, vk.MCPServerAllowlist)

	// PutVirtualKey replaces allowlists on a second Put.
	vk.ProviderAllowlist = []string{"anthropic"}
	vk.ModelAllowlist = nil
	vk.MCPServerAllowlist = []string{"slack"}
	if err := store.PutVirtualKey(ctx, vk); err != nil {
		t.Fatalf("second put vk: %v", err)
	}
	got, err = store.GetVirtualKey(ctx, "tenant-a", "vk-1")
	if err != nil {
		t.Fatalf("get vk after update: %v", err)
	}
	assertSliceEqual(t, "ProviderAllowlist (replaced)", got.ProviderAllowlist, []string{"anthropic"})
	assertSliceEqual(t, "ModelAllowlist (cleared)", got.ModelAllowlist, []string{})
	assertSliceEqual(t, "MCPServerAllowlist (replaced)", got.MCPServerAllowlist, []string{"slack"})
}

func TestGovernanceStore_VK_NeverStoresPlaintextSecret(t *testing.T) {
	db := open(t)
	store := db.Governance()
	ctx := context.Background()

	// A token-shaped secret value — must NEVER appear anywhere in the DB.
	const secret = "pk-portico-THISSHOULDNEVERBEPERSISTEDABCDEF1234"
	vk := &ifaces.VirtualKey{
		TenantID: "t", ID: "vk-1", Name: "n",
		Salt: []byte{1, 2, 3}, HMAC: []byte{4, 5, 6}, Enabled: true,
	}
	if err := store.PutVirtualKey(ctx, vk); err != nil {
		t.Fatalf("put vk: %v", err)
	}
	// Scan the whole VK row textually; the secret must not be present.
	var name, parentKind string
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT name, parent_kind FROM governance_virtual_keys WHERE tenant_id=? AND id=?`,
		"t", "vk-1").Scan(&name, &parentKind); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if name == secret || parentKind == secret {
		t.Fatalf("plaintext secret leaked into a VK column")
	}
}

func TestGovernanceStore_VK_List_TenantIsolated(t *testing.T) {
	db := open(t)
	store := db.Governance()
	ctx := context.Background()

	for _, vk := range []*ifaces.VirtualKey{
		{TenantID: "tenant-a", ID: "vk-1", Name: "a1", Salt: []byte{1}, HMAC: []byte{1}, Enabled: true},
		{TenantID: "tenant-a", ID: "vk-2", Name: "a2", Salt: []byte{1}, HMAC: []byte{1}, Enabled: true},
		{TenantID: "tenant-b", ID: "vk-1", Name: "b1", Salt: []byte{1}, HMAC: []byte{1}, Enabled: true},
	} {
		if err := store.PutVirtualKey(ctx, vk); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	a, err := store.ListVirtualKeys(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list tenant-a: %v", err)
	}
	if len(a) != 2 {
		t.Fatalf("tenant-a: want 2, got %d", len(a))
	}
	for _, vk := range a {
		if vk.TenantID != "tenant-a" {
			t.Errorf("cross-tenant leak: %+v", vk)
		}
	}
}

func TestGovernanceStore_VK_LookupByID_CrossTenant(t *testing.T) {
	db := open(t)
	store := db.Governance()
	ctx := context.Background()

	// Two tenants, same VK *name* but globally-unique ids.
	if err := store.PutVirtualKey(ctx, &ifaces.VirtualKey{
		TenantID: "tenant-a", ID: "vk-aaa", Name: "app", Salt: []byte{1}, HMAC: []byte{2}, Enabled: true,
	}); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := store.PutVirtualKey(ctx, &ifaces.VirtualKey{
		TenantID: "tenant-b", ID: "vk-bbb", Name: "app", Salt: []byte{3}, HMAC: []byte{4}, Enabled: true,
	}); err != nil {
		t.Fatalf("seed b: %v", err)
	}

	got, err := store.LookupVirtualKeyByID(ctx, "vk-bbb")
	if err != nil {
		t.Fatalf("lookup by id: %v", err)
	}
	if got.TenantID != "tenant-b" || got.ID != "vk-bbb" {
		t.Errorf("resolver returned wrong row: %+v", got)
	}
	if _, err := store.LookupVirtualKeyByID(ctx, "vk-unknown"); !errors.Is(err, ifaces.ErrGovernanceNotFound) {
		t.Fatalf("want ErrGovernanceNotFound, got %v", err)
	}
}

func TestGovernanceStore_VK_Delete_CascadesAllowlists(t *testing.T) {
	db := open(t)
	store := db.Governance()
	ctx := context.Background()

	if err := store.PutVirtualKey(ctx, &ifaces.VirtualKey{
		TenantID: "t", ID: "vk-1", Name: "n", Salt: []byte{1}, HMAC: []byte{2},
		ProviderAllowlist: []string{"anthropic"}, ModelAllowlist: []string{"x"},
		MCPServerAllowlist: []string{"github"}, Enabled: true,
	}); err != nil {
		t.Fatalf("put vk: %v", err)
	}
	if err := store.DeleteVirtualKey(ctx, "t", "vk-1"); err != nil {
		t.Fatalf("delete vk: %v", err)
	}
	if _, err := store.GetVirtualKey(ctx, "t", "vk-1"); !errors.Is(err, ifaces.ErrGovernanceNotFound) {
		t.Fatalf("want NotFound after delete, got %v", err)
	}
	for _, table := range []string{"vk_provider_allowlist", "vk_model_allowlist", "vk_mcp_server_allowlist"} {
		var n int
		// nolint:gosec // table is a test constant
		if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE tenant_id=? AND vk_id=?`, "t", "vk-1").Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s not cascaded: count=%d", table, n)
		}
	}
}
