package policy

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/namespace"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/skills/runtime"
)

// Decision is the runtime answer for one tools/call. Allow=false short-
// circuits the dispatcher with a -32003 policy_denied response carrying
// Reason in the structured payload.
type Decision struct {
	Allow            bool
	Reason           string
	Tool             string
	ServerID         string
	SkillID          string
	RiskClass        string
	RequiresApproval bool
	ApprovalTimeout  time.Duration
	Notes            []string
}

// Reason strings exported so audit/REST/console all share the same
// vocabulary. The dispatcher renders them into the policy.denied event
// and the JSON-RPC error data.
const (
	ReasonPasses          = "passes"
	ReasonNotFound        = "tool_not_found"
	ReasonNotAllowed      = "not_allowed"
	ReasonDenied          = "denied"
	ReasonServerDisabled  = "server_disabled"
	ReasonSkillDisabled   = "skill_disabled"
	ReasonUserDenied      = "user_denied"
	ReasonApprovalTimeout = "approval_timeout"
)

// PolicyResolver returns the per-tenant policy snapshot. The runtime
// builds it from configured TenantConfig.Policy at boot and refreshes on
// hot-reload; tests pass a fixed map.
type PolicyResolver func(tenantID string) Policy

// Policy is the per-tenant override layer applied on top of registry +
// skill defaults.
type Policy struct {
	ToolAllowlist   []string      // glob patterns; empty = allow all (subject to per-server)
	ToolDenylist    []string      // glob patterns
	ApprovalTimeout time.Duration // per-tenant override; 0 means engine default
}

// EngineConfig groups defaults the runtime supplies once at construction.
type EngineConfig struct {
	DefaultRiskClass       string        // baseline when neither server nor skill carries one
	DefaultApprovalTimeout time.Duration // default 5m
	Logger                 *slog.Logger
}

// ServerLookup returns the registry snapshot for (tenant, server). Mirrors
// the southbound manager's seam but keeps the policy package free of the
// heavier southbound dependency.
type ServerLookup interface {
	Servers(ctx context.Context, tenantID string) ([]*registry.Snapshot, error)
}

// Engine is the gateway's authorization core. Cheap to construct; safe
// for concurrent callers.
type Engine struct {
	servers    ServerLookup
	skills     *runtime.Catalog
	enable     *runtime.Enablement
	policies   PolicyResolver
	cfg        EngineConfig
	defaultTOL time.Duration
}

// New constructs an Engine. servers/skills/enable may be nil only in
// tests that exercise narrow policy paths (e.g. allowlist matching).
func New(servers ServerLookup, skills *runtime.Catalog, enable *runtime.Enablement, policies PolicyResolver, cfg EngineConfig) *Engine {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.DefaultRiskClass == "" {
		cfg.DefaultRiskClass = RiskWrite
	}
	if cfg.DefaultApprovalTimeout <= 0 {
		cfg.DefaultApprovalTimeout = 5 * time.Minute
	}
	return &Engine{
		servers:    servers,
		skills:     skills,
		enable:     enable,
		policies:   policies,
		cfg:        cfg,
		defaultTOL: cfg.DefaultApprovalTimeout,
	}
}

// EvaluateToolCall returns the Decision for a single namespaced tool. The
// rule order mirrors the phase plan:
//
//  1. Registry membership (tool exists for tenant via registered server)
//  2. Server enabled
//  3. Tenant denylist (first match wins)
//  4. Tenant allowlist (when present, tool must be listed)
//  5. Risk class lookup (skill override > server default > engine default)
//  6. Approval requirement (risk-class default OR explicit skill flag)
func (e *Engine) EvaluateToolCall(ctx context.Context, tenantID, sessionID, _ string, qualifiedName string) (Decision, error) {
	if e == nil {
		return Decision{}, errors.New("policy: engine not configured")
	}
	serverID, toolName, ok := namespace.SplitTool(qualifiedName)
	if !ok {
		return Decision{Reason: ReasonNotFound, Tool: qualifiedName}, nil
	}
	dec := Decision{
		Tool:            qualifiedName,
		ServerID:        serverID,
		ApprovalTimeout: e.defaultTOL,
	}
	pol := Policy{}
	if e.policies != nil {
		pol = e.policies(tenantID)
	}
	if pol.ApprovalTimeout > 0 {
		dec.ApprovalTimeout = pol.ApprovalTimeout
	}

	// 1. Registry membership.
	snap, err := e.lookupServer(ctx, tenantID, serverID)
	if err != nil {
		return Decision{}, err
	}
	if snap == nil {
		dec.Reason = ReasonNotFound
		return dec, nil
	}
	// 2. Enabled.
	if !snap.Record.Enabled {
		dec.Reason = ReasonServerDisabled
		return dec, nil
	}
	_ = toolName // lint: kept for future per-tool registry hooks
	// 3. Denylist.
	if matchAny(pol.ToolDenylist, qualifiedName) {
		dec.Reason = ReasonDenied
		return dec, nil
	}
	// 4. Allowlist.
	if len(pol.ToolAllowlist) > 0 && !matchAny(pol.ToolAllowlist, qualifiedName) {
		dec.Reason = ReasonNotAllowed
		return dec, nil
	}

	// 5. Risk class (skill override > server default > engine default).
	risk := e.cfg.DefaultRiskClass
	if d := getServerRisk(snap.Spec); d != "" {
		risk = canonicalRisk(d)
	}
	skillID := ""
	skillOverridesApproval := false
	if e.skills != nil {
		skill, riskOverride, requiresApproval, sid := e.findOwningSkill(ctx, tenantID, sessionID, qualifiedName)
		if riskOverride != "" {
			risk = canonicalRisk(riskOverride)
		}
		if requiresApproval {
			skillOverridesApproval = true
		}
		if skill {
			skillID = sid
		}
	}
	dec.RiskClass = risk
	dec.SkillID = skillID

	// 6. Approval.
	dec.RequiresApproval = requiresApprovalDefault(risk) || skillOverridesApproval
	dec.Allow = true
	dec.Reason = ReasonPasses
	return dec, nil
}

func (e *Engine) lookupServer(ctx context.Context, tenantID, serverID string) (*registry.Snapshot, error) {
	if e.servers == nil {
		return nil, nil
	}
	snaps, err := e.servers.Servers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, s := range snaps {
		if s.Spec.ID == serverID {
			return s, nil
		}
	}
	return nil, nil
}

// findOwningSkill walks the catalog looking for a skill that includes
// qualifiedName under required_tools (or optional_tools, which inherits
// the skill's policy). The first match wins.
func (e *Engine) findOwningSkill(ctx context.Context, tenantID, sessionID, qualifiedName string) (matched bool, riskOverride string, requiresApproval bool, skillID string) {
	if e.skills == nil {
		return false, "", false, ""
	}
	for _, s := range e.skills.List() {
		if s == nil || s.Manifest == nil {
			continue
		}
		if !skillCovers(s.Manifest.Binding.RequiredTools, qualifiedName) &&
			!skillCovers(s.Manifest.Binding.OptionalTools, qualifiedName) {
			continue
		}
		if e.enable != nil {
			on, err := e.enable.IsEnabled(ctx, tenantID, sessionID, s.Manifest.ID)
			if err != nil || !on {
				continue
			}
		}
		// Per-tool risk override.
		if rc, ok := s.Manifest.Binding.Policy.RiskClasses[qualifiedName]; ok {
			riskOverride = rc
		}
		// Approval flag declared by skill — applies whenever the skill
		// owns the call regardless of risk class.
		for _, t := range s.Manifest.Binding.Policy.RequiresApproval {
			if t == qualifiedName {
				requiresApproval = true
				break
			}
		}
		return true, riskOverride, requiresApproval, s.Manifest.ID
	}
	return false, "", false, ""
}

// matchAny reports whether name matches any glob pattern in patterns.
// Pattern syntax is path.Match — sufficient for `github.*` style rules.
func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if p == "*" {
			return true
		}
		if matched, err := path.Match(p, name); err == nil && matched {
			return true
		}
		// Fallback: treat patterns ending with ".*" specially so callers
		// can write `github.create_*` even when path.Match is too strict.
		if strings.HasSuffix(p, "*") && strings.HasPrefix(name, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

// skillCovers reports whether the skill's required/optional tool list
// includes name.
func skillCovers(list []string, name string) bool {
	for _, t := range list {
		if t == name {
			return true
		}
	}
	return false
}
