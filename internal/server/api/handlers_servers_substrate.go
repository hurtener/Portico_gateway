// Phase 10.6 substrate enrichment.
//
// Earlier phases shipped a thin /v1/servers row (id, transport, runtime,
// status, enabled). Operators couldn't see what a server actually carries
// — capability counts, attached skills, policy/auth state, last-seen —
// without drilling into the detail page. The Console redesign needs all
// those dimensions in the list response so the table cells can render
// directly without N round-trips per row.
//
// This file holds the per-tenant aggregate that feeds those dimensions
// plus the per-row derivation. The list and get handlers each call
// prepareTenantSubstrate once and then deriveServerSubstrate per row, so
// the cost is one extra DB read per request, not one per server.
package api

import (
	"context"
	"strings"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/registry"
)

// serverSubstrate is the per-row aggregate we add on top of the existing
// snapshotJSON output. Field names mirror the JSON shape the typed
// console client consumes.
type serverSubstrate struct {
	Capabilities serverCapabilities `json:"capabilities"`
	SkillsCount  int                `json:"skills_count"`
	PolicyState  string             `json:"policy_state"` // "none" | "enforced" | "approval" | "disabled"
	AuthState    string             `json:"auth_state"`   // "none" | "env" | "header" | "oauth" | "vault_ref"
	LastSeen     string             `json:"last_seen,omitempty"`
}

type serverCapabilities struct {
	Tools     int `json:"tools"`
	Resources int `json:"resources"`
	Prompts   int `json:"prompts"`
	Apps      int `json:"apps"`
}

// tenantSubstrate holds the per-tenant aggregates the row derivation
// needs. Cheap to compute once, expensive to compute per row.
type tenantSubstrate struct {
	// Latest catalog snapshot for the tenant; nil if no session has
	// generated one yet. Source for capabilities counts.
	latest *snapshots.Snapshot
	// All policy rules for the tenant (may be nil).
	rules []policy.Rule
	// Skill server-dependency map: server.id → count of skills depending on it.
	skillsByServer map[string]int
	// Server display + transport map for cross-references; populated as
	// rows are derived (cheap to maintain).
}

// prepareTenantSubstrate fetches the per-tenant aggregates once. Returns
// a populated struct even on partial failure — substrate is best-effort
// so a missing snapshot or a misconfigured policy rule store should not
// make the list endpoint fail.
func prepareTenantSubstrate(ctx context.Context, d Deps, tenantID string) tenantSubstrate {
	out := tenantSubstrate{skillsByServer: map[string]int{}}

	// Latest catalog snapshot — limit=1 returns just the most recent.
	if d.Snapshots != nil {
		if list, _, err := d.Snapshots.List(ctx, tenantID, snapshots.ListQuery{Limit: 1}); err == nil && len(list) > 0 {
			out.latest = list[0]
		}
	}

	// Policy rules — RuleSet.Rules is the slice we scan per server.
	if d.PolicyRules != nil {
		if set, err := d.PolicyRules.List(ctx, tenantID); err == nil {
			out.rules = set.Rules
		}
	}

	// Skills runtime → count per attached server. The catalog returns
	// every skill visible to the tenant; ServerDependencies on the
	// manifest is the canonical link. Defensive about a typed-nil
	// interface (cmd_serve.go now guards at the assignment, but a
	// future caller could regress) — the recover swallows any panic
	// and leaves skills_count at zero.
	func() {
		defer func() { _ = recover() }()
		mgr := skillsMgr(d)
		if mgr == nil {
			return
		}
		cat := mgr.Catalog()
		if cat == nil {
			return
		}
		for _, s := range cat.ForTenant(tenantID, nil, "") {
			if s == nil || s.Manifest == nil {
				continue
			}
			for _, srv := range s.Manifest.Binding.ServerDependencies {
				if srv == "" {
					continue
				}
				out.skillsByServer[srv]++
			}
		}
	}()

	return out
}

// deriveServerSubstrate computes the per-row substrate for a single
// server. Pure function over the prefetched aggregate — no extra I/O.
func deriveServerSubstrate(snap *registry.Snapshot, agg tenantSubstrate) serverSubstrate {
	out := serverSubstrate{
		AuthState:   deriveAuthState(snap),
		PolicyState: derivePolicyState(snap.Record.ID, agg.rules),
		SkillsCount: agg.skillsByServer[snap.Record.ID],
	}
	// last_seen: registry's UpdatedAt is the closest signal we have.
	// Phase 11 telemetry will replace this with the supervisor's
	// per-instance last-acquire timestamp once that surface exists.
	if !snap.Record.UpdatedAt.IsZero() {
		out.LastSeen = snap.Record.UpdatedAt.UTC().Format(time.RFC3339)
	}
	// Capabilities — only populated when a snapshot exists. Without a
	// session ever having run, we don't know what the server exposes.
	if agg.latest != nil {
		serverID := snap.Record.ID
		for _, t := range agg.latest.Tools {
			if t.ServerID == serverID {
				out.Capabilities.Tools++
			}
		}
		for _, r := range agg.latest.Resources {
			if r.ServerID == serverID {
				out.Capabilities.Resources++
			}
		}
		for _, p := range agg.latest.Prompts {
			if p.ServerID == serverID {
				out.Capabilities.Prompts++
			}
		}
		// Apps (ui:// resources) come through the resource list with a
		// ui:// scheme — count separately so the row shows them as a
		// distinct dimension.
		for _, r := range agg.latest.Resources {
			if r.ServerID == serverID && strings.HasPrefix(r.URI, "ui://") {
				out.Capabilities.Apps++
			}
		}
	}
	return out
}

// deriveAuthState collapses spec.Auth.Strategy into the small enum the
// Console renders as a badge. Empty strategy → "none". Unknown
// strategies fall through as the literal value so future strategies
// surface in the UI without code changes here (the Console just renders
// the unknown string in a neutral badge).
func deriveAuthState(snap *registry.Snapshot) string {
	if snap == nil || snap.Spec.Auth == nil {
		return "none"
	}
	switch snap.Spec.Auth.Strategy {
	case "":
		return "none"
	case "env_inject":
		return "env"
	case "http_header_inject":
		return "header"
	case "oauth2_token_exchange":
		return "oauth"
	case "secret_reference", "credential_shim":
		return "vault_ref"
	default:
		return snap.Spec.Auth.Strategy
	}
}

// derivePolicyState scans the tenant's rules for ones that match the
// supplied server. The mapping is intentionally coarse:
//
//	deny → disabled, require_approval → approval, allow → enforced.
//
// Empty rule set, or rules that don't constrain by server, return "none".
// A more granular state lives in the inspector's Policy tab.
func derivePolicyState(serverID string, rules []policy.Rule) string {
	if len(rules) == 0 || serverID == "" {
		return "none"
	}
	matchesServer := func(r policy.Rule) bool {
		// A rule with no server constraint applies to every server.
		if len(r.Conditions.Match.Servers) == 0 {
			return true
		}
		for _, s := range r.Conditions.Match.Servers {
			if s == serverID {
				return true
			}
		}
		return false
	}
	hasDeny, hasApproval, hasAllow := false, false, false
	for _, r := range rules {
		if !r.Enabled || !matchesServer(r) {
			continue
		}
		if r.Actions.Deny {
			hasDeny = true
		}
		if r.Actions.RequireApproval {
			hasApproval = true
		}
		if r.Actions.Allow {
			hasAllow = true
		}
	}
	switch {
	case hasDeny:
		return "disabled"
	case hasApproval:
		return "approval"
	case hasAllow:
		return "enforced"
	default:
		return "none"
	}
}
