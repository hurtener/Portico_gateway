// Package policy owns the gateway's authorization layer: tool allow/deny,
// risk classification, and the boolean "does this call need approval"
// gate. The engine is a single pass over (registry, skill catalog, tenant
// policy) so dispatchers can call it on every tools/call without paying
// for per-call DB hops.
package policy

// Risk classes drive the default approval requirement. Operators can
// override per-tool via Skill manifests; per-server defaults live on
// ServerSpec.Auth.DefaultRiskClass.
const (
	// RiskRead — tool reads tenant-visible data with no side effects. No
	// approval by default.
	RiskRead = "read"

	// RiskWrite — tool mutates tenant data within the integration but
	// produces no externally-visible side effect. Approval is policy-
	// dependent (defaults to false; operators may flip the per-tool
	// override).
	RiskWrite = "write"

	// RiskSensitiveRead — read operation that returns sensitive payloads
	// (PII, financial, health). Approval ON by default.
	RiskSensitiveRead = "sensitive_read"

	// RiskExternalSideEffect — tool emits changes outside Portico (e.g.
	// posting a comment, opening a ticket). Approval ON by default.
	RiskExternalSideEffect = "external_side_effect"

	// RiskDestructive — tool removes or irreversibly mutates state.
	// Approval ON by default; cannot be overridden to off.
	RiskDestructive = "destructive"
)

// validRiskClasses is the set the validator accepts. Unknown values get
// reduced to RiskWrite at runtime so a typo doesn't silently bypass
// approval.
var validRiskClasses = map[string]struct{}{
	RiskRead:               {},
	RiskWrite:              {},
	RiskSensitiveRead:      {},
	RiskExternalSideEffect: {},
	RiskDestructive:        {},
}

// requiresApprovalDefault reports the baseline approval requirement for
// the class. Per-tool overrides sit on top of this default.
func requiresApprovalDefault(class string) bool {
	switch class {
	case RiskDestructive, RiskExternalSideEffect, RiskSensitiveRead:
		return true
	default:
		return false
	}
}

// canonicalRisk normalises a config-supplied class. Empty / unknown values
// land on RiskWrite (the safer default short of RiskExternalSideEffect).
func canonicalRisk(class string) string {
	if _, ok := validRiskClasses[class]; ok {
		return class
	}
	return RiskWrite
}
