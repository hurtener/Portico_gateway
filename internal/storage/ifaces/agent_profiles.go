package ifaces

import (
	"context"
	"errors"
)

// AgentProfile is a tenant-scoped consumer-binding: which MCP servers/tools,
// Skill Packs, and LLM aliases a logical agent may use, plus its scope set.
// The four allowlist slices are loaded from their join tables. Timestamps are
// RFC3339 UTC strings (matching the other stores).
type AgentProfile struct {
	TenantID            string
	ID                  string
	Name                string
	Description         string
	AllowedMCPServers   []string // server names
	AllowedTools        []string // namespaced "server.tool"; empty = all tools in AllowedMCPServers
	AllowedSkills       []string // Skill Pack ids
	AllowedModelAliases []string // LLM aliases
	Scopes              []string // scope set this profile grants
	PolicyBundleRef     string
	ParentProfileID     string // reserved for future inheritance
	Enabled             bool
	CreatedAt           string
	UpdatedAt           string
}

// ErrAgentProfileNotFound is returned when no profile (or binding) matches.
var ErrAgentProfileNotFound = errors.New("storage: agent profile not found")

// AgentProfileStore persists agent profiles + their allowlists and JWT
// bindings. Every method is tenant-scoped (§6): filter WHERE tenant_id = ?.
type AgentProfileStore interface {
	// List returns the tenant's profiles (with allowlists loaded), name ASC.
	List(ctx context.Context, tenantID string) ([]*AgentProfile, error)
	// Get returns one profile with allowlists; ErrAgentProfileNotFound on miss.
	Get(ctx context.Context, tenantID, id string) (*AgentProfile, error)
	// Put upserts the profile row AND replaces its four allowlists atomically
	// (one transaction): delete the profile's existing allowlist rows, insert
	// the new ones, upsert the agent_profiles row. Sets CreatedAt on first
	// insert; always sets UpdatedAt. p.Scopes is stored as a JSON array column.
	Put(ctx context.Context, p *AgentProfile) error
	// Delete removes the profile; the join tables cascade.
	Delete(ctx context.Context, tenantID, id string) error
	// PutJWTBinding maps a JWT subject to a profile (upsert; one profile per sub).
	PutJWTBinding(ctx context.Context, tenantID, jwtSub, profileID string) error
	// DeleteJWTBinding removes a subject's binding (no error if absent).
	DeleteJWTBinding(ctx context.Context, tenantID, jwtSub string) error
	// ResolveJWTBinding returns the profile bound to jwtSub (allowlists loaded),
	// or ErrAgentProfileNotFound when the subject has no binding.
	ResolveJWTBinding(ctx context.Context, tenantID, jwtSub string) (*AgentProfile, error)
}
