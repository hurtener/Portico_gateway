package ifaces

import (
	"context"
	"errors"
)

// AgentProfile is a tenant-scoped consumer-binding: which MCP servers/tools,
// Skill Packs, LLM aliases, and A2A peers/tasks a logical agent may use, plus
// its scope set. The six allowlist slices are loaded from their join tables.
// Timestamps are RFC3339 UTC strings (matching the other stores).
type AgentProfile struct {
	TenantID            string
	ID                  string
	Name                string
	Description         string
	AllowedMCPServers   []string // server names
	AllowedTools        []string // namespaced "server.tool"; empty = all tools in AllowedMCPServers
	AllowedSkills       []string // Skill Pack ids
	AllowedModelAliases []string // LLM aliases
	AllowedA2APeers     []string // A2A peer names; empty = all peers
	AllowedA2ATasks     []string // namespaced "peer.task"; empty = all tasks of allowed peers
	// MCP<->A2A bridge routes (Phase 16): cross-protocol dispatch declared on
	// this profile. Empty = no bridging.
	MCPToA2ABridges []MCPToA2ABridge
	A2AToMCPBridges []A2AToMCPBridge
	Scopes          []string // scope set this profile grants
	PolicyBundleRef string
	ParentProfileID string // reserved for future inheritance
	Enabled         bool
	CreatedAt       string
	UpdatedAt       string
}

// MCPToA2ABridge routes an MCP tools/call to an A2A peer task: a call for
// MCPTool ("server.tool") dispatches to peer A2APeer's task A2ATask.
type MCPToA2ABridge struct {
	MCPTool string
	A2APeer string
	A2ATask string
}

// A2AToMCPBridge routes an inbound A2A task to an MCP tool: a task named
// A2ATask dispatches to MCP tool MCPTool ("server.tool").
type A2AToMCPBridge struct {
	A2ATask string
	MCPTool string
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
	// Put upserts the profile row AND replaces its six allowlists atomically
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
