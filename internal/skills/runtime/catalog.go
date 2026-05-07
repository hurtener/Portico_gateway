// Package runtime owns the live state of every loaded Skill Pack:
// the catalog, the enablement registry, the synthetic resource
// provider, and the skill://_index generator.
package runtime

import (
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

// Skill is the in-memory record for one loaded pack.
type Skill struct {
	Manifest *manifest.Manifest
	Source   source.Source
	Ref      source.Ref
	LoadedAt time.Time
	Warnings []string

	// TenantID is set when the pack came from a tenant-scoped source
	// (Phase 8 authored skills). Empty when the pack is global
	// (LocalDir / Git / HTTP today; future tenant-scoped external
	// sources may set it). The catalog's ForTenant filter respects
	// this — a non-empty TenantID is matched against the asker's
	// tenant; empty means "visible to every tenant".
	TenantID string
}

// Namespace returns the part before the first dot in the skill id, or
// the full id when no dot is present. Used for namespacing skill://
// URIs.
func (s *Skill) Namespace() string {
	if s == nil || s.Manifest == nil {
		return ""
	}
	id := s.Manifest.ID
	if i := strings.IndexByte(id, '.'); i > 0 {
		return id[:i]
	}
	return id
}

// Name returns the part after the first dot in the skill id.
func (s *Skill) Name() string {
	if s == nil || s.Manifest == nil {
		return ""
	}
	id := s.Manifest.ID
	if i := strings.IndexByte(id, '.'); i > 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}

// ChangeKind classifies a catalog mutation.
type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeUpdated ChangeKind = "updated"
	ChangeRemoved ChangeKind = "removed"
)

// ChangeEvent is published to every Subscribe channel on Set / Remove.
type ChangeEvent struct {
	Kind  ChangeKind
	Skill *Skill
}

// Catalog holds the live skills index. The catalog persists nothing —
// it is rebuilt from sources on startup.
type Catalog struct {
	mu     sync.RWMutex
	skills map[string]*Skill

	subMu   sync.RWMutex
	subs    map[chan ChangeEvent]struct{}
	subSize int
}

// NewCatalog constructs an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{
		skills:  make(map[string]*Skill),
		subs:    make(map[chan ChangeEvent]struct{}),
		subSize: 16,
	}
}

// catalogKey returns the lookup key for a skill record. Tenant-scoped
// skills use "<tenantID>:<id>" so two tenants can publish the same
// id without colliding; global skills keep the bare id.
func catalogKey(tenantID, id string) string {
	if tenantID == "" {
		return id
	}
	return tenantID + ":" + id
}

// Set inserts or replaces the Skill with the given id. Idempotent;
// repeated Set calls publish ChangeUpdated.
func (c *Catalog) Set(s *Skill) {
	if s == nil || s.Manifest == nil {
		return
	}
	key := catalogKey(s.TenantID, s.Manifest.ID)
	c.mu.Lock()
	_, existed := c.skills[key]
	c.skills[key] = s
	c.mu.Unlock()
	kind := ChangeAdded
	if existed {
		kind = ChangeUpdated
	}
	c.publish(ChangeEvent{Kind: kind, Skill: s})
}

// Remove deletes the skill with id (no-op when missing).
func (c *Catalog) Remove(id string) {
	c.removeKey(id, "")
}

// RemoveForTenant deletes a tenant-scoped skill. Use this for
// authored sources where two tenants may share an id.
func (c *Catalog) RemoveForTenant(tenantID, id string) {
	c.removeKey(id, tenantID)
}

func (c *Catalog) removeKey(id, tenantID string) {
	key := catalogKey(tenantID, id)
	c.mu.Lock()
	prev, ok := c.skills[key]
	if ok {
		delete(c.skills, key)
	}
	c.mu.Unlock()
	if ok {
		c.publish(ChangeEvent{Kind: ChangeRemoved, Skill: prev})
	}
}

// RemoveBySource drops every skill whose Source.Name matches name.
// Used when a Phase 8 source.Registry CRUD removal joins watcher
// goroutines and we need to flush the catalog to match.
//
// Pass tenantID to scope the removal to a single tenant — important
// because driver-level Source.Name() ("git", "http") is shared, but
// the registry attaches the operator-chosen row name as the source
// name. Pass "" to remove for every tenant.
func (c *Catalog) RemoveBySource(tenantID, name string) {
	c.mu.Lock()
	removed := make([]*Skill, 0)
	for key, s := range c.skills {
		if s == nil || s.Source == nil {
			continue
		}
		if s.Source.Name() != name {
			continue
		}
		if tenantID != "" && s.TenantID != "" && s.TenantID != tenantID {
			continue
		}
		removed = append(removed, s)
		delete(c.skills, key)
	}
	c.mu.Unlock()
	for _, s := range removed {
		c.publish(ChangeEvent{Kind: ChangeRemoved, Skill: s})
	}
}

// Get returns the skill for id. When two skills share an id (a
// global skill and a tenant-scoped publish), the global wins for the
// bare-id lookup; callers needing tenant scoping use GetForTenant.
func (c *Catalog) Get(id string) (*Skill, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if s, ok := c.skills[id]; ok {
		return s, true
	}
	// Fall back: scan tenant-scoped variants and return the first match.
	for k, s := range c.skills {
		if s == nil || s.Manifest == nil {
			continue
		}
		if s.Manifest.ID == id {
			_ = k
			return s, true
		}
	}
	return nil, false
}

// GetForTenant returns the skill resolved for a tenant: tenant-scoped
// rows take precedence; global rows fall through.
func (c *Catalog) GetForTenant(tenantID, id string) (*Skill, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if tenantID != "" {
		if s, ok := c.skills[catalogKey(tenantID, id)]; ok {
			return s, true
		}
	}
	if s, ok := c.skills[id]; ok {
		return s, true
	}
	return nil, false
}

// List returns every skill, sorted by id.
func (c *Catalog) List() []*Skill {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*Skill, 0, len(c.skills))
	for _, s := range c.skills {
		out = append(out, s)
	}
	// caller-friendly stable order
	sortSkillsByID(out)
	return out
}

// ForTenant filters the catalog by the supplied entitlements. globs is
// a list of patterns (e.g. "github.*", "*"); plan is the tenant's plan
// tier ("free", "pro", "enterprise" or operator-defined).
//
// When globs is empty the tenant sees every skill (plan filter still
// applies). When plan is empty the manifest's plan list is ignored.
//
// Skills carrying a non-empty Skill.TenantID (Phase 8 authored skills,
// future tenant-scoped external sources) are visible only to that
// tenant; tenantID="" disables this filter (used by tests and the
// validate-skills CLI).
func (c *Catalog) ForTenant(tenantID string, globs []string, plan string) []*Skill {
	all := c.List()
	out := make([]*Skill, 0, len(all))
	for _, s := range all {
		if !globsMatch(globs, s.Manifest.ID) {
			continue
		}
		if !planAllowed(plan, s.Manifest.Binding.Entitlements.Plans) {
			continue
		}
		if s.TenantID != "" && tenantID != "" && s.TenantID != tenantID {
			continue
		}
		out = append(out, s)
	}
	return out
}

// Subscribe returns a channel that receives every catalog mutation.
// Drop-oldest on backpressure (16-deep buffer). Callers MUST call
// Unsubscribe to stop the publisher from holding the channel.
func (c *Catalog) Subscribe() <-chan ChangeEvent {
	ch := make(chan ChangeEvent, c.subSize)
	c.subMu.Lock()
	c.subs[ch] = struct{}{}
	c.subMu.Unlock()
	return ch
}

// Unsubscribe drops a subscriber. Pass the channel returned by
// Subscribe; mismatched channels are ignored.
func (c *Catalog) Unsubscribe(ch <-chan ChangeEvent) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	for k := range c.subs {
		if (<-chan ChangeEvent)(k) == ch {
			delete(c.subs, k)
			close(k)
			return
		}
	}
}

func (c *Catalog) publish(ev ChangeEvent) {
	c.subMu.RLock()
	subs := make([]chan ChangeEvent, 0, len(c.subs))
	for k := range c.subs {
		subs = append(subs, k)
	}
	c.subMu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// drop-oldest
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}

// sortSkillsByID sorts a slice in-place by Manifest.ID.
func sortSkillsByID(in []*Skill) {
	sort.Slice(in, func(i, j int) bool {
		return in[i].Manifest.ID < in[j].Manifest.ID
	})
}

// globsMatch reports whether name matches any glob in patterns.
// Supported syntax: a single trailing or leading "*" (path.Match
// semantics for cross-segment skill ids). Empty patterns matches all.
func globsMatch(patterns []string, name string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if p == "*" {
			return true
		}
		if matched, _ := path.Match(p, name); matched {
			return true
		}
	}
	return false
}

// planAllowed reports whether the tenant's plan satisfies the
// manifest's allow-list. Empty `plans` means "no plan restriction" (the
// skill is offered to everyone). Empty `plan` means "no plan filter
// configured" — Phase 4 ships the runtime in this mode (Phase 5 wires
// per-tenant plan lookups), so we pass through to keep skills visible.
func planAllowed(plan string, plans []string) bool {
	if len(plans) == 0 {
		return true
	}
	if plan == "" {
		return true
	}
	for _, p := range plans {
		if p == plan {
			return true
		}
	}
	return false
}
