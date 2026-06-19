package policy

import (
	"context"
	"testing"
)

func ptrBool(b bool) *bool      { return &b }
func ptrF64(f float64) *float64 { return &f }

// matched reports whether a single-rule ruleset matched the call.
func matched(m Match, call ToolCallShape) bool {
	rs := RuleSet{Rules: []Rule{
		{ID: "r", Priority: 1, Enabled: true, Conditions: Conditions{Match: m}, Actions: Actions{Deny: true}},
	}}
	return len(DryRun(context.Background(), rs, call).MatchedRules) == 1
}

func TestMatch_VKID(t *testing.T) {
	m := Match{VKs: []string{"vk-1"}}
	if !matched(m, ToolCallShape{TenantID: "t", VK: &VKView{ID: "vk-1"}}) {
		t.Error("vk-1 should match")
	}
	if matched(m, ToolCallShape{TenantID: "t", VK: &VKView{ID: "vk-2"}}) {
		t.Error("vk-2 should not match")
	}
	if matched(m, ToolCallShape{TenantID: "t"}) {
		t.Error("no VK in context should not match a VK matcher")
	}
}

func TestMatch_VKScopeTeamCustomer(t *testing.T) {
	if !matched(Match{VKScopes: []string{"llm:invoke"}},
		ToolCallShape{TenantID: "t", VK: &VKView{ID: "v", Scopes: []string{"llm:invoke", "mcp:call"}}}) {
		t.Error("vk scope should match")
	}
	if matched(Match{VKScopes: []string{"admin"}},
		ToolCallShape{TenantID: "t", VK: &VKView{ID: "v", Scopes: []string{"llm:invoke"}}}) {
		t.Error("absent scope should not match")
	}
	if !matched(Match{VKTeam: "tm-1"}, ToolCallShape{TenantID: "t", VK: &VKView{ID: "v", Team: "tm-1"}}) {
		t.Error("vk team should match")
	}
	if !matched(Match{VKCustomer: "c-1"}, ToolCallShape{TenantID: "t", VK: &VKView{ID: "v", Customer: "c-1"}}) {
		t.Error("vk customer should match")
	}
	if matched(Match{VKTeam: "tm-9"}, ToolCallShape{TenantID: "t", VK: &VKView{ID: "v", Team: "tm-1"}}) {
		t.Error("wrong team should not match")
	}
}

func TestMatch_CacheWouldHit(t *testing.T) {
	if !matched(Match{CacheWouldHit: ptrBool(true)},
		ToolCallShape{TenantID: "t", Cache: &CacheView{WouldHit: true}}) {
		t.Error("cache_would_hit=true should match a would-hit call")
	}
	if matched(Match{CacheWouldHit: ptrBool(true)},
		ToolCallShape{TenantID: "t", Cache: &CacheView{WouldHit: false}}) {
		t.Error("cache_would_hit=true should not match a miss")
	}
	if matched(Match{CacheWouldHit: ptrBool(false)},
		ToolCallShape{TenantID: "t", Cache: &CacheView{WouldHit: false}}) == false {
		t.Error("cache_would_hit=false should match a miss")
	}
	if matched(Match{CacheWouldHit: ptrBool(true)}, ToolCallShape{TenantID: "t"}) {
		t.Error("no cache view should not match")
	}
}

func TestMatch_BudgetHeadroom(t *testing.T) {
	m := Match{BudgetHeadroomBelowPct: ptrF64(20)}
	if !matched(m, ToolCallShape{TenantID: "t", Budget: &BudgetView{LowestHeadroomPct: 10}}) {
		t.Error("10% headroom is below 20 → match")
	}
	if matched(m, ToolCallShape{TenantID: "t", Budget: &BudgetView{LowestHeadroomPct: 50}}) {
		t.Error("50% headroom is not below 20 → no match")
	}
	if matched(m, ToolCallShape{TenantID: "t"}) {
		t.Error("no budget view should not match")
	}
}

func TestModifierActions_RoundTripInFinalAction(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "bypass", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{VKs: []string{"v"}}},
			Actions:    Actions{Allow: true, ForceCacheBypass: true, DenyOnCacheMiss: true, ClampToCustomerBudget: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{TenantID: "t", VK: &VKView{ID: "v"}})
	if !res.FinalAction.ForceCacheBypass || !res.FinalAction.DenyOnCacheMiss || !res.FinalAction.ClampToCustomerBudget {
		t.Fatalf("modifier actions should surface in the final action: %+v", res.FinalAction)
	}
}
