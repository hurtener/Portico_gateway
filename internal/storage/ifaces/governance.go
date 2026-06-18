package ifaces

import (
	"context"
	"errors"
)

// Customer is a tenant-scoped governance grouping (e.g. an external customer
// or a billing account). Timestamps are RFC3339 UTC strings.
type Customer struct {
	TenantID    string
	ID          string
	Name        string
	Description string
	WebhookURL  string
	CreatedAt   string
	UpdatedAt   string
}

// Team is a tenant-scoped group that optionally belongs to one Customer.
type Team struct {
	TenantID    string
	ID          string
	CustomerID  string // "" when standalone under the tenant
	Name        string
	Description string
	CreatedAt   string
	UpdatedAt   string
}

// VirtualKey is a Portico-side credential (pk-portico-*). Only salt + HMAC of
// the secret are persisted — never the secret itself (§7). ParentKind is one of
// "none"|"team"|"customer"; ParentID references the team/customer when set.
// ProfileID is the reserved Phase 14 Agent Profile linkage.
type VirtualKey struct {
	TenantID           string
	ID                 string
	Name               string
	Salt               []byte
	HMAC               []byte
	ParentKind         string
	ParentID           string
	ProfileID          string
	Scopes             []string
	ProviderAllowlist  []string // provider drivers; empty = all
	ModelAllowlist     []string // model aliases; empty = all
	MCPServerAllowlist []string // server ids; empty = all
	Enabled            bool
	CreatedAt          string
	RotatedAt          string
	RevokedAt          string
}

// ErrGovernanceNotFound is returned when no customer/team/VK matches.
var ErrGovernanceNotFound = errors.New("storage: governance entity not found")

// GovernanceStore persists customers, teams, and virtual keys. Every method is
// tenant-scoped (§6: WHERE tenant_id = ?), EXCEPT LookupVirtualKeyByID which is
// the auth-boundary resolver path (a presented VK carries no tenant; the VK id
// is a globally-unique ULID, like JWT->tenant resolution).
type GovernanceStore interface {
	// Customers
	PutCustomer(ctx context.Context, c *Customer) error
	GetCustomer(ctx context.Context, tenantID, id string) (*Customer, error)
	ListCustomers(ctx context.Context, tenantID string) ([]*Customer, error)
	DeleteCustomer(ctx context.Context, tenantID, id string) error

	// Teams
	PutTeam(ctx context.Context, tm *Team) error
	GetTeam(ctx context.Context, tenantID, id string) (*Team, error)
	ListTeams(ctx context.Context, tenantID string) ([]*Team, error)
	DeleteTeam(ctx context.Context, tenantID, id string) error

	// Virtual keys. PutVirtualKey upserts the row AND replaces the three
	// allowlists in one transaction (clone agent_profiles Put). Scopes is a JSON
	// array column.
	PutVirtualKey(ctx context.Context, vk *VirtualKey) error
	GetVirtualKey(ctx context.Context, tenantID, id string) (*VirtualKey, error)
	ListVirtualKeys(ctx context.Context, tenantID string) ([]*VirtualKey, error)
	DeleteVirtualKey(ctx context.Context, tenantID, id string) error
	// LookupVirtualKeyByID resolves a presented VK by its globally-unique id
	// (auth boundary). Returns the full row incl. tenant_id + salt + hmac.
	LookupVirtualKeyByID(ctx context.Context, id string) (*VirtualKey, error)
}
