package codemode

import "testing"

func TestPolicy_ZeroValue_Permissive(t *testing.T) {
	var p Policy
	d := p.Evaluate(EvalInput{Enabled: true, BindingLevel: "server", CodeBytes: 10_000})
	if d.Deny || d.RequireApproval {
		t.Errorf("zero policy must be permissive, got %+v", d)
	}
}

func TestPolicy_NonCodeMode_NoOp(t *testing.T) {
	p := Policy{Disabled: true, MaxExecutionBytes: 1, RequireApprovalOnExecute: true}
	if d := p.Evaluate(EvalInput{Enabled: false, CodeBytes: 999}); d.Deny || d.RequireApproval {
		t.Errorf("non-code-mode call must not be gated, got %+v", d)
	}
}

func TestPolicy_Disabled_Denies(t *testing.T) {
	p := Policy{Disabled: true}
	d := p.Evaluate(EvalInput{Enabled: true})
	if !d.Deny || d.Reason != ReasonDisabled {
		t.Errorf("disabled policy must deny with %q, got %+v", ReasonDisabled, d)
	}
}

func TestPolicy_MaxExecutionBytes(t *testing.T) {
	p := Policy{MaxExecutionBytes: 100}
	if d := p.Evaluate(EvalInput{Enabled: true, CodeBytes: 100}); d.Deny {
		t.Error("exactly at the limit must be allowed")
	}
	d := p.Evaluate(EvalInput{Enabled: true, CodeBytes: 101})
	if !d.Deny || d.Reason != ReasonExecutionTooLarge {
		t.Errorf("over the limit must deny with %q, got %+v", ReasonExecutionTooLarge, d)
	}
}

func TestPolicy_BindingLevelAllowlist(t *testing.T) {
	p := Policy{AllowedBindingLevels: []string{"server"}}
	if d := p.Evaluate(EvalInput{Enabled: true, BindingLevel: "server"}); d.Deny {
		t.Error("allowed binding level must pass")
	}
	d := p.Evaluate(EvalInput{Enabled: true, BindingLevel: "tool"})
	if !d.Deny || d.Reason != ReasonBindingLevelDenied {
		t.Errorf("disallowed binding level must deny with %q, got %+v", ReasonBindingLevelDenied, d)
	}
}

func TestPolicy_RequireApproval(t *testing.T) {
	p := Policy{RequireApprovalOnExecute: true}
	d := p.Evaluate(EvalInput{Enabled: true, CodeBytes: 10})
	if d.Deny || !d.RequireApproval {
		t.Errorf("require-approval policy must request approval, got %+v", d)
	}
}

func TestPolicy_Resume_SkipsGate(t *testing.T) {
	// A resume must not be re-gated even under a strict policy.
	p := Policy{MaxExecutionBytes: 1, AllowedBindingLevels: []string{"none"}, RequireApprovalOnExecute: true}
	d := p.Evaluate(EvalInput{Enabled: true, BindingLevel: "server", CodeBytes: 9_999, IsResume: true})
	if d.Deny || d.RequireApproval {
		t.Errorf("resume must skip the pre-exec gate, got %+v", d)
	}
}

func TestPolicy_DenyShortCircuitsApproval(t *testing.T) {
	// When a snippet is both too large AND approval is required, the deny wins.
	p := Policy{MaxExecutionBytes: 10, RequireApprovalOnExecute: true}
	d := p.Evaluate(EvalInput{Enabled: true, CodeBytes: 50})
	if !d.Deny || d.RequireApproval {
		t.Errorf("deny must short-circuit before approval, got %+v", d)
	}
}

func TestPolicy_EffectiveMaxToolCalls(t *testing.T) {
	cases := []struct {
		name       string
		policyMax  int
		sessionMax int
		want       int
	}{
		{"no policy cap defers to session", 0, 20, 20},
		{"policy cap lowers a larger session", 5, 20, 5},
		{"policy cap does not raise a smaller session", 30, 10, 10},
		{"policy cap applies when session unset", 5, 0, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Policy{MaxToolCallsInside: c.policyMax}
			if got := p.EffectiveMaxToolCalls(c.sessionMax); got != c.want {
				t.Errorf("EffectiveMaxToolCalls(%d) with policy %d = %d, want %d", c.sessionMax, c.policyMax, got, c.want)
			}
		})
	}
}
