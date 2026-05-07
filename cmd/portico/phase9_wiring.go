// Phase 9 wiring — bridges between the api Phase 9 contracts
// (PolicyRulesController, VaultRevealManager) and the concrete
// implementations under internal/policy + internal/secrets. Lives in
// cmd/portico per CLAUDE.md §4.4.

package main

import (
	"context"

	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/secrets"
)

// policyRulesAdapter implements api.PolicyRulesController on top of
// *policy.RuleStore. The handler-facing types are policy.Rule + RuleSet so
// the adapter is mostly a pass-through.
type policyRulesAdapter struct {
	store *policy.RuleStore
}

func newPolicyRulesAdapter(store *policy.RuleStore) *policyRulesAdapter {
	return &policyRulesAdapter{store: store}
}

func (a *policyRulesAdapter) List(ctx context.Context, tenantID string) (policy.RuleSet, error) {
	return a.store.List(ctx, tenantID)
}

func (a *policyRulesAdapter) Get(ctx context.Context, tenantID, ruleID string) (policy.Rule, error) {
	return a.store.Get(ctx, tenantID, ruleID)
}

func (a *policyRulesAdapter) Upsert(ctx context.Context, tenantID string, r policy.Rule) (policy.Rule, error) {
	return a.store.Upsert(ctx, tenantID, r)
}

func (a *policyRulesAdapter) Delete(ctx context.Context, tenantID, ruleID string) error {
	return a.store.Delete(ctx, tenantID, ruleID)
}

func (a *policyRulesAdapter) ReplaceAll(ctx context.Context, tenantID string, set policy.RuleSet) error {
	return a.store.ReplaceAll(ctx, tenantID, set)
}

// vaultRevealAdapter implements api.VaultRevealManager. The api package
// can't import internal/secrets directly because it would pull in the
// concrete vault driver chain — so we route through this thin adapter.
type vaultRevealAdapter struct {
	mgr *secrets.RevealManager
}

func newVaultRevealAdapter(mgr *secrets.RevealManager) *vaultRevealAdapter {
	return &vaultRevealAdapter{mgr: mgr}
}

func (a *vaultRevealAdapter) IssueRevealToken(ctx context.Context, tenant, name, actorID string) (secrets.RevealToken, error) {
	if a.mgr == nil {
		return secrets.RevealToken{}, secrets.ErrNotFound
	}
	return a.mgr.IssueRevealToken(ctx, tenant, name, actorID)
}

func (a *vaultRevealAdapter) ConsumeReveal(ctx context.Context, token string) (string, string, string, string, error) {
	if a.mgr == nil {
		return "", "", "", "", secrets.ErrNotFound
	}
	return a.mgr.ConsumeReveal(ctx, token)
}
