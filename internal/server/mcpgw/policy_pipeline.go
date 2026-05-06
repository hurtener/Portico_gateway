package mcpgw

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/secrets/inject"
)

// PolicyPipeline wires together the policy engine, approval flow, and
// credential injectors. The dispatcher calls Evaluate before every
// tools/call so the request goes through the full guardrail chain.
//
// The pipeline returns a Decision (allow/deny + risk class), an Outcome
// (approval result), and a PrepTarget (env/headers the supervisor /
// southbound HTTP client should apply for the call). When Allow is
// false, the dispatcher emits the appropriate JSON-RPC error and skips
// southbound entirely.
type PolicyPipeline struct {
	engine    *policy.Engine
	approvals *approval.Flow
	injectors *inject.Registry
	emitter   audit.Emitter
	registry  *registry.Registry
	log       *slog.Logger
}

// PipelineConfig groups the dependencies. emitter may be a NopEmitter for
// dev mode without persistent audit; injectors / approvals / registry
// nil-safe for the same reason.
type PipelineConfig struct {
	Engine    *policy.Engine
	Approvals *approval.Flow
	Injectors *inject.Registry
	Emitter   audit.Emitter
	Registry  *registry.Registry
	Logger    *slog.Logger
}

// NewPolicyPipeline constructs a pipeline from the runtime's components.
func NewPolicyPipeline(cfg PipelineConfig) *PolicyPipeline {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Emitter == nil {
		cfg.Emitter = audit.NopEmitter{}
	}
	return &PolicyPipeline{
		engine:    cfg.Engine,
		approvals: cfg.Approvals,
		injectors: cfg.Injectors,
		emitter:   cfg.Emitter,
		registry:  cfg.Registry,
		log:       cfg.Logger,
	}
}

// PipelineResult is what Evaluate returns. The dispatcher consults Decision
// and Outcome to know whether to allow / deny / emit -32001; PrepTarget
// is non-nil iff the call should proceed.
type PipelineResult struct {
	Decision        policy.Decision
	Outcome         approval.Outcome
	PrepTarget      *inject.PrepTarget
	StructuredError *protocol.Error // populated when Allow=false; ready to return
}

// Evaluate runs the full policy → approval → credentials pipeline.
func (p *PolicyPipeline) Evaluate(ctx context.Context, sess *Session, params protocol.CallToolParams) (*PipelineResult, error) {
	if p == nil || p.engine == nil {
		return nil, errors.New("policy pipeline: engine not configured")
	}
	dec, err := p.engine.EvaluateToolCall(ctx, sess.TenantID, sess.ID, sess.UserID, params.Name)
	if err != nil {
		return nil, err
	}
	res := &PipelineResult{Decision: dec}

	// Emit policy event regardless of outcome.
	if dec.Allow {
		p.emitter.Emit(ctx, audit.Event{
			Type:      audit.EventPolicyAllowed,
			TenantID:  sess.TenantID,
			SessionID: sess.ID,
			UserID:    sess.UserID,
			Payload: map[string]any{
				"tool":              dec.Tool,
				"risk_class":        dec.RiskClass,
				"requires_approval": dec.RequiresApproval,
				"skill_id":          dec.SkillID,
			},
		})
	} else {
		p.emitter.Emit(ctx, audit.Event{
			Type:      audit.EventPolicyDenied,
			TenantID:  sess.TenantID,
			SessionID: sess.ID,
			UserID:    sess.UserID,
			Payload: map[string]any{
				"tool":   dec.Tool,
				"reason": dec.Reason,
			},
		})
		res.StructuredError = policyDeniedError(dec)
		return res, nil
	}

	// Approval gate.
	if dec.RequiresApproval && p.approvals != nil {
		out, err := p.approvals.Run(ctx, sess.TenantID, sess.ID, sess.UserID, dec, approval.CallContext{
			Tool:      dec.Tool,
			Arguments: params.Arguments,
			SkillID:   dec.SkillID,
			RiskClass: dec.RiskClass,
		})
		if err != nil {
			return nil, err
		}
		res.Outcome = out
		switch {
		case out.FallbackRequired():
			res.StructuredError = approvalRequiredError(dec, out.Approval)
			res.Decision.Allow = false
			return res, nil
		case !out.Approved():
			// User-denied or expired.
			reason := policy.ReasonUserDenied
			if out.Decision == approval.StatusExpired {
				reason = policy.ReasonApprovalTimeout
			}
			res.Decision.Allow = false
			res.Decision.Reason = reason
			res.StructuredError = policyDeniedError(res.Decision)
			return res, nil
		}
	}

	// Credentials.
	target, err := p.resolveCredentials(ctx, sess, dec)
	if err != nil {
		// Map credential lookup failure to a structured policy error.
		res.Decision.Allow = false
		res.Decision.Reason = "credential_lookup_failed"
		res.StructuredError = protocol.NewError(protocol.ErrPolicyDenied, "credential resolution failed", map[string]any{
			"tool":  dec.Tool,
			"error": err.Error(),
		})
		return res, nil
	}
	res.PrepTarget = target
	return res, nil
}

func (p *PolicyPipeline) resolveCredentials(ctx context.Context, sess *Session, dec policy.Decision) (*inject.PrepTarget, error) {
	target := &inject.PrepTarget{
		Env:     map[string]string{},
		Headers: map[string]string{},
	}
	if p.injectors == nil || p.registry == nil {
		return target, nil
	}
	snap, err := p.registry.Get(ctx, sess.TenantID, dec.ServerID)
	if err != nil {
		return target, nil // server lookup miss → no injection
	}
	if snap.Spec.Auth == nil || snap.Spec.Auth.Strategy == "" {
		return target, nil
	}
	in, ok := p.injectors.Get(snap.Spec.Auth.Strategy)
	if !ok {
		return nil, errors.New("inject: no implementation registered for strategy " + snap.Spec.Auth.Strategy)
	}
	req := inject.PrepRequest{
		TenantID:     sess.TenantID,
		UserID:       sess.UserID,
		SessionID:    sess.ID,
		SubjectToken: sess.SubjectToken,
		ServerSpec:   &snap.Spec,
	}
	if err := in.Apply(ctx, req, target); err != nil {
		p.emitter.Emit(ctx, audit.Event{
			Type:      audit.EventCredentialExchangeNG,
			TenantID:  sess.TenantID,
			SessionID: sess.ID,
			UserID:    sess.UserID,
			Payload: map[string]any{
				"strategy":  snap.Spec.Auth.Strategy,
				"server_id": snap.Spec.ID,
				"error":     err.Error(),
			},
		})
		return nil, err
	}
	p.emitter.Emit(ctx, audit.Event{
		Type:      audit.EventCredentialInjected,
		TenantID:  sess.TenantID,
		SessionID: sess.ID,
		UserID:    sess.UserID,
		Payload: map[string]any{
			"strategy":  snap.Spec.Auth.Strategy,
			"server_id": snap.Spec.ID,
			"scope":     redactScope(snap.Spec.Auth),
		},
	})
	return target, nil
}

// redactScope summarises the auth block without leaking values. Used
// purely for audit payloads.
func redactScope(a *registry.AuthSpec) map[string]any {
	out := map[string]any{"strategy": a.Strategy}
	if len(a.Env) > 0 {
		out["env_keys"] = envKeys(a.Env)
	}
	if len(a.Headers) > 0 {
		hk := make([]string, 0, len(a.Headers))
		for k := range a.Headers {
			hk = append(hk, k)
		}
		out["header_keys"] = hk
	}
	if a.SecretRef != "" {
		out["secret_ref"] = a.SecretRef
	}
	if a.Exchange != nil {
		out["audience"] = a.Exchange.Audience
	}
	return out
}

func envKeys(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out = append(out, kv[:i])
				break
			}
		}
	}
	return out
}

func policyDeniedError(dec policy.Decision) *protocol.Error {
	data := map[string]any{
		"tool":   dec.Tool,
		"reason": dec.Reason,
	}
	if dec.SkillID != "" {
		data["skill_id"] = dec.SkillID
	}
	if dec.RiskClass != "" {
		data["risk_class"] = dec.RiskClass
	}
	return protocol.NewError(protocol.ErrPolicyDenied, "policy denied", data)
}

func approvalRequiredError(dec policy.Decision, a *approval.Approval) *protocol.Error {
	data := map[string]any{
		"tool":         dec.Tool,
		"risk_class":   dec.RiskClass,
		"approval_id":  a.ID,
		"expires_at":   a.ExpiresAt.UTC().Format(time.RFC3339),
		"args_summary": a.ArgsSummary,
	}
	if dec.SkillID != "" {
		data["skill_id"] = dec.SkillID
	}
	return protocol.NewError(protocol.ErrApprovalRequired, "approval_required", data)
}

// AsCallToolParams parses a tools/call request body.
func AsCallToolParams(raw json.RawMessage) (protocol.CallToolParams, error) {
	var p protocol.CallToolParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	return p, nil
}
