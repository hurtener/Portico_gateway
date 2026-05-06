package telemetry

// Semantic attribute keys for the tracing surface. Code that creates
// spans references these constants instead of raw strings so a typo at
// one call site doesn't break trace querying.
const (
	AttrTenantID   = "tenant.id"
	AttrUserID     = "user.id"
	AttrSessionID  = "session.id"
	AttrRequestID  = "mcp.request_id"
	AttrMethod     = "mcp.method"
	AttrTool       = "mcp.tool"
	AttrServerID   = "mcp.server_id"
	AttrSkillID    = "mcp.skill_id"
	AttrTransport  = "mcp.transport"
	AttrPeerURL    = "peer.url"
	AttrSnapshotID = "snapshot.id"

	AttrPolicyAllow            = "policy.allow"
	AttrPolicyReason           = "policy.reason"
	AttrPolicyRequiresApproval = "policy.requires_approval"
	AttrPolicyRiskClass        = "policy.risk_class"

	AttrApprovalID      = "approval.id"
	AttrApprovalElicit  = "approval.elicit"
	AttrApprovalOutcome = "approval.outcome"

	//nolint:gosec // attribute key, not a credential
	AttrCredentialStrategy = "credential.strategy"
	//nolint:gosec // attribute key, not a credential
	AttrCredentialCacheHit = "credential.cache_hit"

	AttrAuditType = "audit.type"

	AttrSnapshotServers    = "snapshot.servers"
	AttrSnapshotToolsCount = "snapshot.tools_count"
	AttrDriftDetected      = "drift.detected"
)

// Span name constants. Same rationale as attribute keys.
const (
	SpanMCPSession     = "mcp.session"
	SpanMCPRequest     = "mcp.request"
	SpanMCPToolCall    = "mcp.tool_call"
	SpanPolicyEvaluate = "policy.evaluate"
	SpanApprovalFlow   = "approval.flow"
	//nolint:gosec // span name, not a credential
	SpanCredentialResolve = "credential.resolve"
	SpanSouthboundCall    = "southbound.call"
	SpanAuditEmit         = "audit.emit"
	SpanSnapshotCreate    = "snapshot.create"
	SpanSnapshotDrift     = "snapshot.drift_check"
)
