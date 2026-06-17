package codemode

// Code Mode policy is the operator-tunable posture evaluated for each
// executeToolCode call, BEFORE the snippet runs. It is the default-deny lever
// from the threat model (docs/security/code-mode-threat-model.md, "Default-deny
// posture"): conservative limits an operator raises deliberately, plus an
// optional whole-execution approval gate.
//
// It does NOT replace the per-tool-call governance of in-sandbox calls — every
// tool a snippet calls still traverses the full dispatcher envelope (tenant,
// scope, policy, approval, vault, audit). This policy governs the
// executeToolCode meta-tool itself: how big a snippet may be, which binding
// levels may run it, and whether the whole run needs operator sign-off.
//
// The matchers map to the plan's code_mode.* matcher names; the deny outcomes
// map to its actions:
//   - code_mode.enabled            -> EvalInput.Enabled
//   - code_mode.binding_level      -> EvalInput.BindingLevel / Policy.AllowedBindingLevels
//   - code_mode.execution_size_bytes -> EvalInput.CodeBytes / Policy.MaxExecutionBytes
//   - code_mode.tool_calls_inside  -> Policy.MaxToolCallsInside (applied to the runtime budget)
//   - deny_on_unsafe_starlark      -> Policy.DenyUnsafeStarlark (escalates a static-gate
//     rejection to an audited policy denial)
//   - require_approval_on_executeToolCode -> Policy.RequireApprovalOnExecute

// Decision reason codes. They travel in the JSON-RPC error's Data.code field
// (under ErrCodeModeExecution) and in audit, naming the precise policy outcome.
const (
	// ReasonExecutionTooLarge — the snippet exceeded Policy.MaxExecutionBytes.
	ReasonExecutionTooLarge = "code_mode.execution_too_large"
	// ReasonBindingLevelDenied — the session's binding level is not allowed.
	ReasonBindingLevelDenied = "code_mode.binding_level_denied"
	// ReasonDisabled — code mode is globally disabled by policy.
	ReasonDisabled = "code_mode.disabled_by_policy"
)

// Policy is the Code Mode posture. The zero value is fully permissive (no size
// limit, any binding level, no approval gate) — code mode is open within a
// tenant by default and operators tighten it (plan §6).
type Policy struct {
	// Disabled turns code mode off entirely: every executeToolCode is denied with
	// ReasonDisabled regardless of the session opt-in. A blunt kill switch.
	Disabled bool
	// MaxExecutionBytes rejects a snippet whose source exceeds this many bytes.
	// 0 means no limit. (code_mode.execution_size_bytes)
	MaxExecutionBytes int
	// MaxToolCallsInside caps tool calls per execution. 0 means "use the runtime
	// default". The handler applies it to the runtime Budget; it is the policy
	// expression of code_mode.tool_calls_inside.
	MaxToolCallsInside int
	// AllowedBindingLevels restricts which binding levels may run code mode
	// (e.g. ["server"] to forbid tool-level). Empty means any level is allowed.
	// (code_mode.binding_level)
	AllowedBindingLevels []string
	// RequireApprovalOnExecute gates every executeToolCode behind the approval
	// flow before the snippet runs. (require_approval_on_executeToolCode)
	RequireApprovalOnExecute bool
	// DenyUnsafeStarlark escalates a static-gate rejection (an unsafe snippet) to
	// an audited policy denial. The static gate already rejects unsafe Starlark;
	// this makes the posture explicit and auditable. (deny_on_unsafe_starlark)
	DenyUnsafeStarlark bool
}

// EvalInput is the per-call context a policy evaluation sees. It carries exactly
// the matcher inputs.
type EvalInput struct {
	// Enabled reports whether the session opted into code mode (code_mode.enabled).
	Enabled bool
	// BindingLevel is the session's stub binding level ("server"|"tool").
	BindingLevel string
	// CodeBytes is the byte length of the snippet (code_mode.execution_size_bytes).
	CodeBytes int
	// IsResume reports whether this is a continuation resume. A resume is NOT
	// re-gated on size/binding/approval — the original execution already passed
	// the gate, and the continuation is single-use, tenant-scoped, and TTL-bounded.
	IsResume bool
}

// Decision is the policy outcome for one executeToolCode call.
type Decision struct {
	// Deny reports the call must be rejected before running. Reason names why.
	Deny bool
	// Reason is the code_mode.* reason when Deny is true.
	Reason string
	// RequireApproval reports the whole execution must be approved before running.
	// Mutually informative with Deny: a Deny short-circuits before approval.
	RequireApproval bool
}

// Evaluate applies the policy to one executeToolCode call. It is a pure function
// of (Policy, EvalInput) so it is trivially testable and deterministic.
func (p Policy) Evaluate(in EvalInput) Decision {
	// A non-code-mode call is never gated here (defensive — the handler only
	// evaluates code mode requests).
	if !in.Enabled {
		return Decision{}
	}
	// A resume continues an already-gated execution; do not re-gate it.
	if in.IsResume {
		return Decision{}
	}
	if p.Disabled {
		return Decision{Deny: true, Reason: ReasonDisabled}
	}
	if p.MaxExecutionBytes > 0 && in.CodeBytes > p.MaxExecutionBytes {
		return Decision{Deny: true, Reason: ReasonExecutionTooLarge}
	}
	if len(p.AllowedBindingLevels) > 0 && !containsLevel(p.AllowedBindingLevels, in.BindingLevel) {
		return Decision{Deny: true, Reason: ReasonBindingLevelDenied}
	}
	return Decision{RequireApproval: p.RequireApprovalOnExecute}
}

// EffectiveMaxToolCalls returns the tool-call cap to apply, given the session's
// requested cap. The policy cap, when set, is a CEILING: it lowers a larger
// session request but never raises a smaller one (operators tighten, sessions
// cannot loosen past the policy). A zero policy cap defers entirely to the
// session/runtime default.
func (p Policy) EffectiveMaxToolCalls(sessionMax int) int {
	if p.MaxToolCallsInside <= 0 {
		return sessionMax
	}
	if sessionMax <= 0 || p.MaxToolCallsInside < sessionMax {
		return p.MaxToolCallsInside
	}
	return sessionMax
}

func containsLevel(levels []string, level string) bool {
	for _, l := range levels {
		if l == level {
			return true
		}
	}
	return false
}
