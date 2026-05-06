// Package snapshots owns the per-session catalog snapshot service. A
// snapshot freezes everything a session can see: server fleet, tools (with
// schemas), resources, prompts, skills, policy resolution, credential
// strategies. The snapshot is the audit-trail anchor — every persisted
// event in a session points at the snapshot id, so an operator reading
// "tool X was called" six months later can recover what X actually meant.
//
// The package also owns deterministic fingerprinting (a canonical JSON
// serialiser the drift detector consults) and the drift detector itself,
// which runs as a background goroutine comparing live downstream tool
// lists against active snapshots and emitting `schema.drift` audit events
// on divergence.
package snapshots

import (
	"encoding/json"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// Snapshot is the canonical immutable view of a tenant's catalog at the
// point a session was created (or refreshed).
type Snapshot struct {
	ID          string           `json:"id"`
	TenantID    string           `json:"tenant_id"`
	SessionID   string           `json:"session_id,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	Servers     []ServerInfo     `json:"servers"`
	Tools       []ToolInfo       `json:"tools"`
	Resources   []ResourceInfo   `json:"resources"`
	Prompts     []PromptInfo     `json:"prompts"`
	Skills      []SkillInfo      `json:"skills"`
	Policies    PoliciesInfo     `json:"policies"`
	Credentials []CredentialInfo `json:"credentials"`
	Warnings    []string         `json:"warnings,omitempty"`
	OverallHash string           `json:"overall_hash"`
}

// ServerInfo records the per-server fingerprint at snapshot time. SchemaHash
// is the fingerprint of the server's `tools/list` response; the drift
// detector compares the live recompute against this value.
type ServerInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Transport   string `json:"transport"`
	RuntimeMode string `json:"runtime_mode,omitempty"`
	SchemaHash  string `json:"schema_hash"`
	Health      string `json:"health"`
}

// ToolInfo is one row of the namespaced tool catalog the session sees.
// `Hash` is the per-tool canonical hash; `Annotations` is the MCP
// upstream-supplied annotation block when present.
type ToolInfo struct {
	NamespacedName   string                    `json:"namespaced_name"`
	ServerID         string                    `json:"server_id"`
	Description      string                    `json:"description,omitempty"`
	InputSchema      json.RawMessage           `json:"input_schema,omitempty"`
	Annotations      *protocol.ToolAnnotations `json:"annotations,omitempty"`
	RiskClass        string                    `json:"risk_class"`
	RequiresApproval bool                      `json:"requires_approval"`
	SkillID          string                    `json:"skill_id,omitempty"`
	Hash             string                    `json:"hash"`
}

// ResourceInfo records the namespaced resource URI plus the upstream URI
// it was rewritten from.
type ResourceInfo struct {
	URI         string `json:"uri"`
	UpstreamURI string `json:"upstream_uri,omitempty"`
	ServerID    string `json:"server_id"`
	MIMEType    string `json:"mime_type,omitempty"`
}

// PromptInfo records the namespaced prompt name.
type PromptInfo struct {
	NamespacedName string                    `json:"namespaced_name"`
	ServerID       string                    `json:"server_id"`
	Arguments      []protocol.PromptArgument `json:"arguments,omitempty"`
}

// SkillInfo records the per-session skill enablement at snapshot time.
type SkillInfo struct {
	ID                string   `json:"id"`
	Version           string   `json:"version"`
	EnabledForSession bool     `json:"enabled_for_session"`
	MissingTools      []string `json:"missing_tools,omitempty"`
}

// PoliciesInfo summarises the per-tenant policy block the dispatcher will
// apply for the session.
type PoliciesInfo struct {
	AllowList        []string      `json:"allow_list,omitempty"`
	DenyList         []string      `json:"deny_list,omitempty"`
	ApprovalTimeout  time.Duration `json:"approval_timeout,omitempty"`
	DefaultRiskClass string        `json:"default_risk_class,omitempty"`
}

// CredentialInfo records the per-server strategy + secret refs (names
// only, never values).
type CredentialInfo struct {
	ServerID   string   `json:"server_id"`
	Strategy   string   `json:"strategy,omitempty"`
	SecretRefs []string `json:"secret_refs,omitempty"`
}

// Diff is the structured shape returned by Service.Diff and embedded in
// `schema.drift` audit events.
type Diff struct {
	Tools     ToolDiff     `json:"tools"`
	Resources ResourceDiff `json:"resources"`
	Prompts   PromptDiff   `json:"prompts"`
	Skills    SkillDiff    `json:"skills"`
}

// ToolDiff classifies tool list changes.
type ToolDiff struct {
	Added    []string       `json:"added,omitempty"`
	Removed  []string       `json:"removed,omitempty"`
	Modified []ModifiedTool `json:"modified,omitempty"`
}

// ResourceDiff classifies resource list changes.
type ResourceDiff struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// PromptDiff classifies prompt list changes.
type PromptDiff struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// SkillDiff classifies skill list changes.
type SkillDiff struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// ModifiedTool records a tool whose schema or annotations changed.
type ModifiedTool struct {
	Name          string   `json:"name"`
	FieldsChanged []string `json:"fields_changed"`
	OldHash       string   `json:"old_hash"`
	NewHash       string   `json:"new_hash"`
}

// IsEmpty reports whether the diff carries any change.
func (d *Diff) IsEmpty() bool {
	if d == nil {
		return true
	}
	return len(d.Tools.Added) == 0 && len(d.Tools.Removed) == 0 && len(d.Tools.Modified) == 0 &&
		len(d.Resources.Added) == 0 && len(d.Resources.Removed) == 0 &&
		len(d.Prompts.Added) == 0 && len(d.Prompts.Removed) == 0 &&
		len(d.Skills.Added) == 0 && len(d.Skills.Removed) == 0
}
