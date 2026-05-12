package api

import (
	"testing"

	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Substrate derivations are pure functions; we test them in isolation
// rather than wiring a full Deps. The integration path is exercised in
// the existing handler tests once the typed client lands.

func TestDeriveAuthState_StrategyMapping(t *testing.T) {
	cases := map[string]string{
		"":                      "none",
		"env_inject":            "env",
		"http_header_inject":    "header",
		"oauth2_token_exchange": "oauth",
		"secret_reference":      "vault_ref",
		"credential_shim":       "vault_ref",
		"future_strategy":       "future_strategy", // pass-through preserves forward-compat
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			snap := &registry.Snapshot{Spec: registry.ServerSpec{Auth: &registry.AuthSpec{Strategy: in}}}
			if got := deriveAuthState(snap); got != want {
				t.Fatalf("deriveAuthState(%q) = %q, want %q", in, got, want)
			}
		})
	}

	// Nil auth → "none" — common case for stdio servers without
	// secret plumbing.
	if got := deriveAuthState(&registry.Snapshot{}); got != "none" {
		t.Fatalf("nil auth → %q, want \"none\"", got)
	}
	if got := deriveAuthState(nil); got != "none" {
		t.Fatalf("nil snapshot → %q, want \"none\"", got)
	}
}

func TestDerivePolicyState_RuleMatching(t *testing.T) {
	rules := []policy.Rule{
		{ID: "r1", Enabled: true, Conditions: policy.Conditions{Match: policy.Match{Servers: []string{"alpha"}}}, Actions: policy.Actions{Allow: true}},
		{ID: "r2", Enabled: true, Conditions: policy.Conditions{Match: policy.Match{Servers: []string{"beta"}}}, Actions: policy.Actions{RequireApproval: true}},
		{ID: "r3", Enabled: true, Conditions: policy.Conditions{Match: policy.Match{Servers: []string{"gamma"}}}, Actions: policy.Actions{Deny: true}},
		{ID: "r4", Enabled: false, Conditions: policy.Conditions{Match: policy.Match{Servers: []string{"alpha"}}}, Actions: policy.Actions{Deny: true}}, // disabled rules ignored
		{ID: "r5", Enabled: true, Conditions: policy.Conditions{Match: policy.Match{}}, Actions: policy.Actions{Allow: true}},                           // empty match = applies to all
	}
	cases := map[string]string{
		"alpha": "enforced", // allow rule + tenant-wide allow rule (r5)
		"beta":  "approval", // approval rule wins over the tenant-wide allow
		"gamma": "disabled", // deny wins over the tenant-wide allow
		"delta": "enforced", // only the tenant-wide r5 applies
		"":      "none",     // empty server id falls through
	}
	for srv, want := range cases {
		t.Run(srv, func(t *testing.T) {
			if got := derivePolicyState(srv, rules); got != want {
				t.Fatalf("derivePolicyState(%q) = %q, want %q", srv, got, want)
			}
		})
	}

	// Empty rule set → "none".
	if got := derivePolicyState("alpha", nil); got != "none" {
		t.Fatalf("empty rules → %q, want \"none\"", got)
	}
}

func TestDeriveServerSubstrate_AllFieldsSet(t *testing.T) {
	snap := &registry.Snapshot{
		Spec: registry.ServerSpec{
			Auth: &registry.AuthSpec{Strategy: "oauth2_token_exchange"},
		},
		Record: ifaces.ServerRecord{ID: "srv-1"},
	}
	agg := tenantSubstrate{
		// no policy rules → "none"
		// skills_count from the map
		skillsByServer: map[string]int{"srv-1": 3, "srv-other": 1},
	}
	got := deriveServerSubstrate(snap, agg)

	if got.AuthState != "oauth" {
		t.Errorf("auth_state = %q, want oauth", got.AuthState)
	}
	if got.PolicyState != "none" {
		t.Errorf("policy_state = %q, want none", got.PolicyState)
	}
	if got.SkillsCount != 3 {
		t.Errorf("skills_count = %d, want 3", got.SkillsCount)
	}
	// No latest snapshot in the agg → capabilities stay zeros.
	if got.Capabilities.Tools != 0 || got.Capabilities.Resources != 0 {
		t.Errorf("capabilities should be zero without a snapshot, got %+v", got.Capabilities)
	}
}
