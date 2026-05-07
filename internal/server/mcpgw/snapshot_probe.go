package mcpgw

import (
	"context"
	"errors"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/namespace"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/skills/runtime"
)

// SnapshotProbe satisfies snapshots.CatalogProbe by delegating to the
// southbound manager, registry, policy engine, and skills runtime that
// already live in the gateway. Constructed once at boot and handed to
// the snapshot service.
type SnapshotProbe struct {
	manager   *southboundmgr.Manager
	registry  *registry.Registry
	skills    *runtime.Manager
	enable    *runtime.Enablement
	policy    *policy.Engine
	policyRes policy.Resolver
}

// NewSnapshotProbe wires the probe.
func NewSnapshotProbe(
	manager *southboundmgr.Manager,
	reg *registry.Registry,
	skills *runtime.Manager,
	enable *runtime.Enablement,
	pol *policy.Engine,
	policyRes policy.Resolver,
) *SnapshotProbe {
	return &SnapshotProbe{
		manager:   manager,
		registry:  reg,
		skills:    skills,
		enable:    enable,
		policy:    pol,
		policyRes: policyRes,
	}
}

// ServerInfos enumerates the per-tenant server set with transport +
// runtime mode. Health is best-effort: defaults to "unknown" when the
// registry doesn't carry a current value.
func (p *SnapshotProbe) ServerInfos(ctx context.Context, tenantID string) ([]snapshots.ServerInfo, error) {
	if p == nil || p.registry == nil {
		return nil, errors.New("snapshot probe: registry not configured")
	}
	snaps, err := p.registry.List(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]snapshots.ServerInfo, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, snapshots.ServerInfo{
			ID:          s.Spec.ID,
			DisplayName: s.Spec.DisplayName,
			Transport:   s.Spec.Transport,
			RuntimeMode: s.Spec.RuntimeMode,
			Health:      s.Record.Status,
		})
	}
	return out, nil
}

// ListTools fans out tools/list across the tenant's enabled servers and
// returns the namespaced view. Errors on a single server are absorbed —
// the snapshot still records the rest, with a warning.
func (p *SnapshotProbe) ListTools(ctx context.Context, tenantID, sessionID string) ([]snapshots.NamespacedTool, error) {
	if p == nil || p.manager == nil {
		return nil, nil
	}
	snaps, err := p.manager.Servers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]snapshots.NamespacedTool, 0)
	listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, s := range snaps {
		if !s.Record.Enabled {
			continue
		}
		client, err := p.manager.Acquire(listCtx, southboundmgr.AcquireRequest{
			TenantID:  tenantID,
			SessionID: sessionID,
			ServerID:  s.Spec.ID,
		})
		if err != nil {
			continue
		}
		tools, err := client.ListTools(listCtx)
		if err != nil {
			continue
		}
		for _, t := range tools {
			out = append(out, snapshots.NamespacedTool{
				NamespacedName: namespace.JoinTool(s.Spec.ID, t.Name),
				ServerID:       s.Spec.ID,
				Tool:           t,
			})
		}
	}
	return out, nil
}

// ListResources fans out resources/list. Same partial-failure semantics
// as ListTools.
func (p *SnapshotProbe) ListResources(ctx context.Context, tenantID, sessionID string) ([]snapshots.NamespacedResource, error) {
	if p == nil || p.manager == nil {
		return nil, nil
	}
	snaps, err := p.manager.Servers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := make([]snapshots.NamespacedResource, 0)
	for _, s := range snaps {
		if !s.Record.Enabled {
			continue
		}
		client, err := p.manager.Acquire(listCtx, southboundmgr.AcquireRequest{
			TenantID:  tenantID,
			SessionID: sessionID,
			ServerID:  s.Spec.ID,
		})
		if err != nil {
			continue
		}
		rs, _, err := client.ListResources(listCtx, "")
		if err != nil {
			if protocol.IsMethodNotFound(err) {
				continue
			}
			continue
		}
		for _, r := range rs {
			rew, _ := namespace.RewriteResourceURI(s.Spec.ID, r.URI)
			out = append(out, snapshots.NamespacedResource{
				URI:         rew,
				UpstreamURI: r.URI,
				ServerID:    s.Spec.ID,
				MIMEType:    r.MimeType,
			})
		}
	}
	return out, nil
}

// ListPrompts fans out prompts/list.
func (p *SnapshotProbe) ListPrompts(ctx context.Context, tenantID, sessionID string) ([]snapshots.NamespacedPrompt, error) {
	if p == nil || p.manager == nil {
		return nil, nil
	}
	snaps, err := p.manager.Servers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := make([]snapshots.NamespacedPrompt, 0)
	for _, s := range snaps {
		if !s.Record.Enabled {
			continue
		}
		client, err := p.manager.Acquire(listCtx, southboundmgr.AcquireRequest{
			TenantID:  tenantID,
			SessionID: sessionID,
			ServerID:  s.Spec.ID,
		})
		if err != nil {
			continue
		}
		ps, _, err := client.ListPrompts(listCtx, "")
		if err != nil {
			if protocol.IsMethodNotFound(err) {
				continue
			}
			continue
		}
		for _, pr := range ps {
			out = append(out, snapshots.NamespacedPrompt{
				NamespacedName: namespace.JoinTool(s.Spec.ID, pr.Name),
				ServerID:       s.Spec.ID,
				Arguments:      pr.Arguments,
			})
		}
	}
	return out, nil
}

// SkillInfos returns the per-session skill enablement summary.
func (p *SnapshotProbe) SkillInfos(ctx context.Context, tenantID, sessionID string) ([]snapshots.SkillInfo, error) {
	if p == nil || p.skills == nil {
		return nil, nil
	}
	out := make([]snapshots.SkillInfo, 0)
	for _, s := range p.skills.Catalog().List() {
		if s == nil || s.Manifest == nil {
			continue
		}
		// Phase 8: tenant-scoped authored skills must not leak across
		// tenants. Skills with TenantID set are visible only to that
		// tenant; an empty TenantID indicates a global pack.
		if s.TenantID != "" && s.TenantID != tenantID {
			continue
		}
		on := false
		if p.enable != nil {
			on, _ = p.enable.IsEnabled(ctx, tenantID, sessionID, s.Manifest.ID)
		}
		out = append(out, snapshots.SkillInfo{
			ID:                s.Manifest.ID,
			Version:           s.Manifest.Version,
			EnabledForSession: on,
		})
	}
	return out, nil
}

// CredentialInfos summarises the per-server auth strategy + secret-ref
// names. Never includes values.
func (p *SnapshotProbe) CredentialInfos(ctx context.Context, tenantID string) ([]snapshots.CredentialInfo, error) {
	if p == nil || p.registry == nil {
		return nil, nil
	}
	snaps, err := p.registry.List(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]snapshots.CredentialInfo, 0, len(snaps))
	for _, s := range snaps {
		ci := snapshots.CredentialInfo{ServerID: s.Spec.ID}
		if s.Spec.Auth != nil {
			ci.Strategy = s.Spec.Auth.Strategy
			if s.Spec.Auth.SecretRef != "" {
				ci.SecretRefs = append(ci.SecretRefs, s.Spec.Auth.SecretRef)
			}
		}
		out = append(out, ci)
	}
	return out, nil
}

// PoliciesInfo returns the resolved per-tenant policy summary.
func (p *SnapshotProbe) PoliciesInfo(_ context.Context, tenantID string) snapshots.PoliciesInfo {
	out := snapshots.PoliciesInfo{
		DefaultRiskClass: policy.RiskWrite,
	}
	if p == nil || p.policyRes == nil {
		return out
	}
	pol := p.policyRes(tenantID)
	out.AllowList = append(out.AllowList, pol.ToolAllowlist...)
	out.DenyList = append(out.DenyList, pol.ToolDenylist...)
	out.ApprovalTimeout = pol.ApprovalTimeout
	return out
}

// ResolveToolPolicy delegates to the policy engine.
func (p *SnapshotProbe) ResolveToolPolicy(ctx context.Context, tenantID, sessionID, qualifiedName string) (string, bool, string) {
	if p == nil || p.policy == nil {
		return policy.RiskWrite, false, ""
	}
	dec, err := p.policy.EvaluateToolCall(ctx, tenantID, sessionID, "", qualifiedName)
	if err != nil {
		return policy.RiskWrite, false, ""
	}
	return dec.RiskClass, dec.RequiresApproval, dec.SkillID
}
