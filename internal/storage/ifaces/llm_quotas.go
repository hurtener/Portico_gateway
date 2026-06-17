package ifaces

import (
	"context"
	"errors"
)

// LLMQuota holds a tenant's LLM rate/usage limits.
type LLMQuota struct {
	TenantID          string
	RequestsPerMinute int
	TokensPerMinute   int
	TokensPerDay      int
	CostUSDPerDay     float64
	UpdatedAt         string
}

// DefaultLLMQuota returns the built-in default limits for a tenant that has not
// set its own (matches the migration defaults).
func DefaultLLMQuota(tenantID string) LLMQuota {
	return LLMQuota{
		TenantID:          tenantID,
		RequestsPerMinute: 600,
		TokensPerMinute:   200000,
		TokensPerDay:      4000000,
		CostUSDPerDay:     100.0,
	}
}

// ErrLLMQuotaNotFound is returned when no quota row exists for a tenant.
var ErrLLMQuotaNotFound = errors.New("storage: llm quota not found")

// LLMQuotaStore is the per-tenant LLM quota registry (one row per tenant).
type LLMQuotaStore interface {
	// GetQuota returns the tenant's row, or ErrLLMQuotaNotFound if unset.
	GetQuota(ctx context.Context, tenantID string) (*LLMQuota, error)
	// GetOrDefault returns the tenant's row, or DefaultLLMQuota if unset (never errors on absence).
	GetOrDefault(ctx context.Context, tenantID string) (*LLMQuota, error)
	// SetQuota upserts the tenant's limits.
	SetQuota(ctx context.Context, q *LLMQuota) error
	DeleteQuota(ctx context.Context, tenantID string) error
}
