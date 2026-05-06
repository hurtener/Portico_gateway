package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/runtime"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// fakeServers implements policy.ServerLookup with a fixed snapshot list.
type fakeServers struct {
	snaps map[string][]*registry.Snapshot // tenantID -> snapshots
}

func (f *fakeServers) Servers(_ context.Context, tenantID string) ([]*registry.Snapshot, error) {
	return f.snaps[tenantID], nil
}

func enabledSnap(spec *registry.ServerSpec) *registry.Snapshot {
	return &registry.Snapshot{Spec: *spec, Record: ifaces.ServerRecord{Enabled: true}}
}

func disabledSnap(spec *registry.ServerSpec) *registry.Snapshot {
	return &registry.Snapshot{Spec: *spec, Record: ifaces.ServerRecord{Enabled: false}}
}

func TestEvaluate_AllowDefault(t *testing.T) {
	servers := &fakeServers{snaps: map[string][]*registry.Snapshot{
		"acme": {enabledSnap(&registry.ServerSpec{ID: "github"})},
	}}
	e := policy.New(servers, nil, nil, nil, policy.EngineConfig{})
	dec, err := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.list_repos")
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allow || dec.Reason != policy.ReasonPasses {
		t.Errorf("expected allow/passes, got %+v", dec)
	}
	if dec.RiskClass != policy.RiskWrite {
		t.Errorf("default risk should be 'write', got %q", dec.RiskClass)
	}
	if dec.RequiresApproval {
		t.Errorf("write should not require approval by default")
	}
}

func TestEvaluate_ToolNotFound_BadName(t *testing.T) {
	e := policy.New(nil, nil, nil, nil, policy.EngineConfig{})
	dec, _ := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "bareToolName")
	if dec.Reason != policy.ReasonNotFound {
		t.Errorf("expected tool_not_found; got %+v", dec)
	}
}

func TestEvaluate_ServerDisabled(t *testing.T) {
	servers := &fakeServers{snaps: map[string][]*registry.Snapshot{
		"acme": {disabledSnap(&registry.ServerSpec{ID: "github"})},
	}}
	e := policy.New(servers, nil, nil, nil, policy.EngineConfig{})
	dec, _ := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.x")
	if dec.Reason != policy.ReasonServerDisabled {
		t.Errorf("expected server_disabled; got %+v", dec)
	}
}

func TestEvaluate_DenyByList(t *testing.T) {
	servers := &fakeServers{snaps: map[string][]*registry.Snapshot{
		"acme": {enabledSnap(&registry.ServerSpec{ID: "github"})},
	}}
	pol := func(_ string) policy.Policy {
		return policy.Policy{ToolDenylist: []string{"github.delete_*"}}
	}
	e := policy.New(servers, nil, nil, pol, policy.EngineConfig{})
	dec, _ := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.delete_repo")
	if dec.Reason != policy.ReasonDenied {
		t.Errorf("expected denied; got %+v", dec)
	}
	dec, _ = e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.list_repos")
	if !dec.Allow {
		t.Errorf("non-matching tool should pass; got %+v", dec)
	}
}

func TestEvaluate_AllowlistMiss(t *testing.T) {
	servers := &fakeServers{snaps: map[string][]*registry.Snapshot{
		"acme": {enabledSnap(&registry.ServerSpec{ID: "github"})},
	}}
	pol := func(_ string) policy.Policy {
		return policy.Policy{ToolAllowlist: []string{"github.list_*"}}
	}
	e := policy.New(servers, nil, nil, pol, policy.EngineConfig{})
	dec, _ := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.create_repo")
	if dec.Reason != policy.ReasonNotAllowed {
		t.Errorf("expected not_allowed; got %+v", dec)
	}
	dec, _ = e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.list_repos")
	if !dec.Allow {
		t.Errorf("listed tool should pass; got %+v", dec)
	}
}

func TestEvaluate_ServerDefaultRiskClass(t *testing.T) {
	spec := &registry.ServerSpec{
		ID:   "github",
		Auth: &registry.AuthSpec{DefaultRiskClass: policy.RiskExternalSideEffect},
	}
	servers := &fakeServers{snaps: map[string][]*registry.Snapshot{
		"acme": {enabledSnap(spec)},
	}}
	e := policy.New(servers, nil, nil, nil, policy.EngineConfig{})
	dec, _ := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.create_review_comment")
	if dec.RiskClass != policy.RiskExternalSideEffect {
		t.Errorf("expected external_side_effect; got %q", dec.RiskClass)
	}
	if !dec.RequiresApproval {
		t.Errorf("external_side_effect should require approval by default")
	}
}

func TestEvaluate_DestructiveAlwaysApproves(t *testing.T) {
	spec := &registry.ServerSpec{
		ID:   "k8s",
		Auth: &registry.AuthSpec{DefaultRiskClass: policy.RiskDestructive},
	}
	servers := &fakeServers{snaps: map[string][]*registry.Snapshot{
		"acme": {enabledSnap(spec)},
	}}
	e := policy.New(servers, nil, nil, nil, policy.EngineConfig{})
	dec, _ := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "k8s.delete_pod")
	if !dec.Allow || !dec.RequiresApproval {
		t.Errorf("destructive should allow + require approval; got %+v", dec)
	}
}

func TestEvaluate_SkillRiskOverride(t *testing.T) {
	spec := &registry.ServerSpec{ID: "github"} // no default risk class
	servers := &fakeServers{snaps: map[string][]*registry.Snapshot{
		"acme": {enabledSnap(spec)},
	}}
	cat := runtime.NewCatalog()
	cat.Set(&runtime.Skill{
		Manifest: &manifest.Manifest{
			ID: "github.review",
			Binding: manifest.Binding{
				RequiredTools: []string{"github.create_review_comment"},
				Policy: manifest.Policy{
					RiskClasses:      map[string]string{"github.create_review_comment": policy.RiskExternalSideEffect},
					RequiresApproval: []string{"github.create_review_comment"},
				},
			},
		},
	})
	en := runtime.NewEnablement(nil, runtime.ModeAuto)
	e := policy.New(servers, cat, en, nil, policy.EngineConfig{})
	dec, _ := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.create_review_comment")
	if dec.RiskClass != policy.RiskExternalSideEffect {
		t.Errorf("expected external_side_effect from skill override; got %q", dec.RiskClass)
	}
	if !dec.RequiresApproval {
		t.Errorf("expected approval requirement from skill")
	}
	if dec.SkillID != "github.review" {
		t.Errorf("expected skill id propagated; got %q", dec.SkillID)
	}
}

func TestEvaluate_SkillApprovalEvenForReadRisk(t *testing.T) {
	// Skill flips approval ON even when risk_class would default to off.
	spec := &registry.ServerSpec{ID: "github"}
	servers := &fakeServers{snaps: map[string][]*registry.Snapshot{
		"acme": {enabledSnap(spec)},
	}}
	cat := runtime.NewCatalog()
	cat.Set(&runtime.Skill{
		Manifest: &manifest.Manifest{
			ID: "github.audit",
			Binding: manifest.Binding{
				RequiredTools: []string{"github.list_secrets"},
				Policy: manifest.Policy{
					RequiresApproval: []string{"github.list_secrets"},
					RiskClasses:      map[string]string{"github.list_secrets": policy.RiskRead},
				},
			},
		},
	})
	en := runtime.NewEnablement(nil, runtime.ModeAuto)
	e := policy.New(servers, cat, en, nil, policy.EngineConfig{})
	dec, _ := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.list_secrets")
	if dec.RiskClass != policy.RiskRead {
		t.Errorf("expected read; got %q", dec.RiskClass)
	}
	if !dec.RequiresApproval {
		t.Errorf("read+skill should still require approval when skill demands it")
	}
}

func TestEvaluate_TenantApprovalTimeoutOverride(t *testing.T) {
	servers := &fakeServers{snaps: map[string][]*registry.Snapshot{
		"acme": {enabledSnap(&registry.ServerSpec{ID: "github"})},
	}}
	pol := func(_ string) policy.Policy {
		return policy.Policy{ApprovalTimeout: 30 * time.Second}
	}
	e := policy.New(servers, nil, nil, pol, policy.EngineConfig{})
	dec, _ := e.EvaluateToolCall(context.Background(), "acme", "s1", "u1", "github.x")
	if dec.ApprovalTimeout != 30*time.Second {
		t.Errorf("expected 30s timeout; got %s", dec.ApprovalTimeout)
	}
}
