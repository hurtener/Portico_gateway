package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestPolicyRulesStore_RoundTrip(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	if err := db.Tenants().Upsert(ctx, &ifaces.Tenant{ID: "acme", DisplayName: "Acme", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	store := db.PolicyRules()

	r := &ifaces.PolicyRuleRecord{
		TenantID:   "acme",
		RuleID:     "deny-destructive",
		Priority:   10,
		Enabled:    true,
		RiskClass:  "destructive",
		Conditions: []byte(`{"match":{"tools":["github.delete_repo"]}}`),
		Actions:    []byte(`{"deny":true}`),
		Notes:      "block destructive github actions",
		UpdatedBy:  "ops@acme",
	}
	if err := store.Upsert(ctx, r); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.Get(ctx, "acme", "deny-destructive")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Priority != 10 || got.RiskClass != "destructive" || !got.Enabled {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if string(got.Conditions) != string(r.Conditions) {
		t.Errorf("conditions changed: %s vs %s", got.Conditions, r.Conditions)
	}

	// Update
	r.Priority = 1
	r.Notes = "updated"
	if err := store.Upsert(ctx, r); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	got, _ = store.Get(ctx, "acme", "deny-destructive")
	if got.Priority != 1 || got.Notes != "updated" {
		t.Errorf("update failed: %+v", got)
	}

	// List
	r2 := *r
	r2.RuleID = "allow-read"
	r2.Priority = 100
	r2.Enabled = false
	r2.Actions = []byte(`{"allow":true}`)
	if err := store.Upsert(ctx, &r2); err != nil {
		t.Fatal(err)
	}
	all, err := store.List(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("list len = %d, want 2", len(all))
	}
	if all[0].RuleID != "deny-destructive" || all[1].RuleID != "allow-read" {
		t.Errorf("ordering = %s, %s", all[0].RuleID, all[1].RuleID)
	}

	// Delete
	if err := store.Delete(ctx, "acme", "allow-read"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "acme", "allow-read"); !errors.Is(err, ifaces.ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestPolicyRulesStore_TenantIsolation(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	for _, id := range []string{"acme", "beta"} {
		if err := db.Tenants().Upsert(ctx, &ifaces.Tenant{ID: id, DisplayName: id, Plan: "pro"}); err != nil {
			t.Fatal(err)
		}
	}
	store := db.PolicyRules()
	if err := store.Upsert(ctx, &ifaces.PolicyRuleRecord{
		TenantID: "acme", RuleID: "r1", Priority: 1, Enabled: true,
		RiskClass: "read", Conditions: []byte(`{}`), Actions: []byte(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(ctx, &ifaces.PolicyRuleRecord{
		TenantID: "beta", RuleID: "r1", Priority: 1, Enabled: true,
		RiskClass: "read", Conditions: []byte(`{}`), Actions: []byte(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	acme, _ := store.List(ctx, "acme")
	beta, _ := store.List(ctx, "beta")
	if len(acme) != 1 || len(beta) != 1 {
		t.Errorf("isolation broken: acme=%d beta=%d", len(acme), len(beta))
	}
}

func TestPolicyRulesStore_ReplaceAll(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	if err := db.Tenants().Upsert(ctx, &ifaces.Tenant{ID: "acme", DisplayName: "Acme", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	store := db.PolicyRules()
	for i, id := range []string{"old1", "old2", "old3"} {
		if err := store.Upsert(ctx, &ifaces.PolicyRuleRecord{
			TenantID: "acme", RuleID: id, Priority: i, Enabled: true,
			RiskClass: "read", Conditions: []byte(`{}`), Actions: []byte(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReplaceAll(ctx, "acme", []*ifaces.PolicyRuleRecord{
		{TenantID: "acme", RuleID: "new1", Priority: 1, Enabled: true,
			RiskClass: "write", Conditions: []byte(`{}`), Actions: []byte(`{"allow":true}`)},
	}); err != nil {
		t.Fatal(err)
	}
	all, _ := store.List(ctx, "acme")
	if len(all) != 1 || all[0].RuleID != "new1" {
		t.Errorf("ReplaceAll left stale rules: %+v", all)
	}
}
