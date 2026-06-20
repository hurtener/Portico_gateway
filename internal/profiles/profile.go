// Package profiles is the Agent Profile domain layer (Phase 14): the resolver
// that maps a request principal to its bound profile (or a synthesised default),
// the entitlement decision methods every gating surface calls, and the request-
// context plumbing. Storage lives in internal/storage/{ifaces,sqlite}; this
// package depends only on the ifaces.AgentProfileStore seam.
//
// A profile is the single source of truth for consumer entitlement: which MCP
// servers/tools, Skill Packs, and LLM aliases a logical agent may use. Any code
// path that gates "can this caller see X" calls a Profile.Allows* method on the
// profile resolved into the request context — never a parallel allowlist.
package profiles

import (
	"strings"

	"github.com/hurtener/Portico_gateway/internal/catalog/namespace"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Profile is the resolved, read-only entitlement for one principal. It mirrors
// the stored ifaces.AgentProfile plus the IsDefault flag that marks the
// synthesised full-surface profile (back-compat: a principal with no profile
// bound is allowed the tenant's full surface).
type Profile struct {
	TenantID            string
	ID                  string
	Name                string
	AllowedMCPServers   []string
	AllowedTools        []string // namespaced "server.tool"; empty = all tools in the allowed servers
	AllowedSkills       []string
	AllowedModelAliases []string
	AllowedA2APeers     []string
	AllowedA2ATasks     []string // namespaced "peer.task"; empty = all tasks of allowed peers
	// MCP<->A2A bridge routes (Phase 16): cross-protocol dispatch this profile
	// declares. Routing, not entitlement — the bridge target is still gated by
	// the Allows* methods.
	MCPToA2ABridges []ifaces.MCPToA2ABridge
	A2AToMCPBridges []ifaces.A2AToMCPBridge
	Scopes          []string
	// IsDefault marks the synthesised default profile (no restriction). All
	// Allows* methods short-circuit to true for it.
	IsDefault bool
}

// FromStore converts a stored profile into the resolved domain type. It is the
// exported seam offline callers (the CLI, the surface handler) use to obtain a
// *Profile with the Allows* decision methods without going through the resolver.
// A stored profile is never the synthesised default (IsDefault stays false).
func FromStore(p *ifaces.AgentProfile) *Profile { return fromStore(p) }

// fromStore converts a stored profile into the resolved domain type.
func fromStore(p *ifaces.AgentProfile) *Profile {
	if p == nil {
		return nil
	}
	return &Profile{
		TenantID:            p.TenantID,
		ID:                  p.ID,
		Name:                p.Name,
		AllowedMCPServers:   p.AllowedMCPServers,
		AllowedTools:        p.AllowedTools,
		AllowedSkills:       p.AllowedSkills,
		AllowedModelAliases: p.AllowedModelAliases,
		AllowedA2APeers:     p.AllowedA2APeers,
		AllowedA2ATasks:     p.AllowedA2ATasks,
		MCPToA2ABridges:     p.MCPToA2ABridges,
		A2AToMCPBridges:     p.A2AToMCPBridges,
		Scopes:              p.Scopes,
	}
}

// BridgeForMCPTool returns the MCP→A2A bridge this profile declares for the
// given namespaced MCP tool ("server.tool"), if any. Bridges are routing, not
// entitlement: a caller dispatching through a bridge is still gated on the
// target via AllowsA2APeer/AllowsA2ATask. A nil profile has no bridges.
func (p *Profile) BridgeForMCPTool(namespacedTool string) (ifaces.MCPToA2ABridge, bool) {
	if p == nil {
		return ifaces.MCPToA2ABridge{}, false
	}
	for _, b := range p.MCPToA2ABridges {
		if b.MCPTool == namespacedTool {
			return b, true
		}
	}
	return ifaces.MCPToA2ABridge{}, false
}

// BridgeForA2ATask returns the A2A→MCP bridge this profile declares for the
// given inbound A2A task name, if any. A nil profile has no bridges.
func (p *Profile) BridgeForA2ATask(task string) (ifaces.A2AToMCPBridge, bool) {
	if p == nil {
		return ifaces.A2AToMCPBridge{}, false
	}
	for _, b := range p.A2AToMCPBridges {
		if b.A2ATask == task {
			return b, true
		}
	}
	return ifaces.A2AToMCPBridge{}, false
}

// AllowsServer reports whether the profile permits reaching the named MCP
// server. A nil profile or the default profile allows everything (back-compat).
func (p *Profile) AllowsServer(serverName string) bool {
	if p == nil || p.IsDefault {
		return true
	}
	return contains(p.AllowedMCPServers, serverName)
}

// AllowsTool reports whether the profile permits a namespaced tool
// ("server.tool"). The tool's server must be allowed, AND — when AllowedTools is
// non-empty — the tool itself must be listed. An empty AllowedTools means "all
// tools in the allowed servers". A nil/default profile allows everything.
func (p *Profile) AllowsTool(namespacedTool string) bool {
	if p == nil || p.IsDefault {
		return true
	}
	serverID, _, ok := namespace.SplitTool(namespacedTool)
	if !ok {
		return false // a non-namespaced tool is never in-surface under a restrictive profile
	}
	if !contains(p.AllowedMCPServers, serverID) {
		return false
	}
	if len(p.AllowedTools) == 0 {
		return true
	}
	return contains(p.AllowedTools, namespacedTool)
}

// AllowsSkill reports whether the profile permits a Skill Pack id.
func (p *Profile) AllowsSkill(skillID string) bool {
	if p == nil || p.IsDefault {
		return true
	}
	return contains(p.AllowedSkills, skillID)
}

// AllowsAlias reports whether the profile permits an LLM model alias.
func (p *Profile) AllowsAlias(alias string) bool {
	if p == nil || p.IsDefault {
		return true
	}
	return contains(p.AllowedModelAliases, alias)
}

// AllowsA2APeer reports whether the profile permits reaching the named A2A
// peer. A nil/default profile allows everything (back-compat).
func (p *Profile) AllowsA2APeer(peerName string) bool {
	if p == nil || p.IsDefault {
		return true
	}
	return contains(p.AllowedA2APeers, peerName)
}

// AllowsA2ATask reports whether the profile permits a namespaced A2A task
// ("peer.task"). The task's peer must be allowed, AND — when AllowedA2ATasks
// is non-empty — the task itself must be listed. Empty AllowedA2ATasks means
// "all tasks of allowed peers". A nil/default profile allows everything.
func (p *Profile) AllowsA2ATask(namespacedTask string) bool {
	if p == nil || p.IsDefault {
		return true
	}
	peer, _, ok := strings.Cut(namespacedTask, ".")
	if !ok {
		return false
	}
	if !contains(p.AllowedA2APeers, peer) {
		return false
	}
	if len(p.AllowedA2ATasks) == 0 {
		return true
	}
	return contains(p.AllowedA2ATasks, namespacedTask)
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
