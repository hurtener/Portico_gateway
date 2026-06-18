package virtualkeys

import (
	"context"
	"errors"
	"fmt"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Resolved is the hydrated, request-scoped view of an authenticated Virtual Key.
// It carries everything downstream gates need: the tenant the VK belongs to, its
// scope set, its provider/model/MCP allowlists, the attached Agent Profile (if
// any), and its budget parent (team/customer) for the hierarchical enforcer.
// It NEVER contains the secret, salt, or HMAC.
type Resolved struct {
	VKID               string
	TenantID           string
	Name               string
	Scopes             []string
	ProviderAllowlist  []string // provider drivers; empty = all allowed
	ModelAllowlist     []string // model aliases; empty = all allowed
	MCPServerAllowlist []string // server ids; empty = all allowed
	ProfileID          string   // attached Agent Profile id; "" = none
	ParentKind         string   // none|team|customer (budget hierarchy)
	ParentID           string
}

// HasScope reports whether the VK carries the named scope.
func (r *Resolved) HasScope(s string) bool {
	for _, x := range r.Scopes {
		if x == s {
			return true
		}
	}
	return false
}

// AllowsProvider reports whether the VK may call the given provider driver. An
// empty allowlist means "all providers" (the VK does not narrow providers).
func (r *Resolved) AllowsProvider(driver string) bool {
	return allowed(r.ProviderAllowlist, driver)
}

// AllowsModel reports whether the VK may call the given model alias. Empty = all.
func (r *Resolved) AllowsModel(alias string) bool {
	return allowed(r.ModelAllowlist, alias)
}

// AllowsServer reports whether the VK may reach the given MCP server id. Empty = all.
func (r *Resolved) AllowsServer(serverID string) bool {
	return allowed(r.MCPServerAllowlist, serverID)
}

func allowed(list []string, v string) bool {
	if len(list) == 0 {
		return true
	}
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// --- request context ---

type ctxKey struct{}

// WithResolved attaches a resolved VK to the context (set by the auth middleware).
func WithResolved(ctx context.Context, r *Resolved) context.Context {
	return context.WithValue(ctx, ctxKey{}, r)
}

// FromContext returns the resolved VK, or (nil,false) when the request was not
// authenticated by a Virtual Key (e.g. a JWT request).
func FromContext(ctx context.Context) (*Resolved, bool) {
	r, ok := ctx.Value(ctxKey{}).(*Resolved)
	return r, ok
}

// --- lifecycle service ---

// Service is the VK lifecycle API over the governance store. It owns secret
// generation + HMAC binding so callers never touch raw secrets except the
// one-time value returned by Create/Rotate.
type Service struct {
	store ifaces.GovernanceStore
}

// NewService builds a VK service over the governance store.
func NewService(store ifaces.GovernanceStore) *Service { return &Service{store: store} }

// CreateParams describes a new Virtual Key.
type CreateParams struct {
	TenantID           string
	Name               string
	Scopes             []string
	ProviderAllowlist  []string
	ModelAllowlist     []string
	MCPServerAllowlist []string
	ProfileID          string
	ParentKind         string // none|team|customer (default none)
	ParentID           string
}

// Created is the result of Create/Rotate: the stored VK plus the one-time
// secret token that is shown to the operator exactly once.
type Created struct {
	VK    *ifaces.VirtualKey
	Token string // pk-portico-… — NEVER persisted; show once.
}

// Create issues a new Virtual Key: generates id + secret + salt, stores
// salt + HMAC(secret) (never the secret), and returns the one-time token.
func (s *Service) Create(ctx context.Context, p CreateParams) (*Created, error) {
	if p.TenantID == "" || p.Name == "" {
		return nil, errors.New("virtualkeys: tenant_id and name are required")
	}
	parentKind := p.ParentKind
	if parentKind == "" {
		parentKind = "none"
	}
	if parentKind != "none" && p.ParentID == "" {
		return nil, fmt.Errorf("virtualkeys: parent_kind %q requires parent_id", parentKind)
	}

	id, err := NewID()
	if err != nil {
		return nil, fmt.Errorf("virtualkeys: generate id: %w", err)
	}
	secret, err := newSecret()
	if err != nil {
		return nil, fmt.Errorf("virtualkeys: generate secret: %w", err)
	}
	salt, err := NewSalt()
	if err != nil {
		return nil, fmt.Errorf("virtualkeys: generate salt: %w", err)
	}

	vk := &ifaces.VirtualKey{
		TenantID:           p.TenantID,
		ID:                 id,
		Name:               p.Name,
		Salt:               salt,
		HMAC:               ComputeHMAC(salt, secret),
		ParentKind:         parentKind,
		ParentID:           p.ParentID,
		ProfileID:          p.ProfileID,
		Scopes:             p.Scopes,
		ProviderAllowlist:  p.ProviderAllowlist,
		ModelAllowlist:     p.ModelAllowlist,
		MCPServerAllowlist: p.MCPServerAllowlist,
		Enabled:            true,
	}
	if err := s.store.PutVirtualKey(ctx, vk); err != nil {
		return nil, fmt.Errorf("virtualkeys: store: %w", err)
	}
	return &Created{VK: vk, Token: ComposeToken(id, secret)}, nil
}

// Rotate issues a fresh secret for an existing VK, preserving its id (and thus
// its budgets + audit lineage). The old secret stops authenticating immediately
// once the resolver cache entry is invalidated. Returns the new one-time token.
func (s *Service) Rotate(ctx context.Context, tenantID, id string) (*Created, error) {
	vk, err := s.store.GetVirtualKey(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	secret, err := newSecret()
	if err != nil {
		return nil, fmt.Errorf("virtualkeys: generate secret: %w", err)
	}
	salt, err := NewSalt()
	if err != nil {
		return nil, fmt.Errorf("virtualkeys: generate salt: %w", err)
	}
	vk.Salt = salt
	vk.HMAC = ComputeHMAC(salt, secret)
	vk.RotatedAt = nowRFC3339()
	if err := s.store.PutVirtualKey(ctx, vk); err != nil {
		return nil, fmt.Errorf("virtualkeys: store rotated: %w", err)
	}
	return &Created{VK: vk, Token: ComposeToken(id, secret)}, nil
}

// Revoke disables a VK (sets revoked_at + enabled=0). The next resolution fails
// with ErrRevoked once the cache entry is invalidated. History is preserved.
func (s *Service) Revoke(ctx context.Context, tenantID, id string) error {
	vk, err := s.store.GetVirtualKey(ctx, tenantID, id)
	if err != nil {
		return err
	}
	vk.Enabled = false
	vk.RevokedAt = nowRFC3339()
	if err := s.store.PutVirtualKey(ctx, vk); err != nil {
		return fmt.Errorf("virtualkeys: store revoked: %w", err)
	}
	return nil
}
