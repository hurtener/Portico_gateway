package ifaces

import (
	"context"
	"errors"
)

// A2APeer is a tenant-scoped registered Agent-to-Agent peer endpoint (Phase 16).
// AgentCardJSON caches the peer's discovered agent card. Timestamps are RFC3339
// UTC strings (matching the other stores).
type A2APeer struct {
	TenantID      string
	ID            string
	Name          string
	Endpoint      string
	EgressAuthRef string
	AgentCardJSON string
	Enabled       bool
	CreatedAt     string
	UpdatedAt     string
}

// ErrA2APeerNotFound is returned when no peer matches.
var ErrA2APeerNotFound = errors.New("storage: a2a peer not found")

// A2APeerStore persists registered A2A peers. Tenant-scoped (§6: WHERE tenant_id = ?).
type A2APeerStore interface {
	// Put upserts a peer (preserve created_at on update; always set updated_at).
	PutPeer(ctx context.Context, p *A2APeer) error
	// GetPeer returns one peer; ErrA2APeerNotFound on miss.
	GetPeer(ctx context.Context, tenantID, id string) (*A2APeer, error)
	// ListPeers returns the tenant's peers, name ASC.
	ListPeers(ctx context.Context, tenantID string) ([]*A2APeer, error)
	// DeletePeer removes a peer; ErrA2APeerNotFound when absent.
	DeletePeer(ctx context.Context, tenantID, id string) error
}
