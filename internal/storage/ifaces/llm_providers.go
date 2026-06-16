package ifaces

import (
	"context"
	"errors"
)

// LLMProvider is a per-tenant LLM provider configuration row.
type LLMProvider struct {
	TenantID      string
	Name          string
	Driver        string
	ConfigJSON    string // canonical JSON; '{}' when empty
	CredentialRef string // optional default vault key
	Enabled       bool
	CreatedAt     string
	UpdatedAt     string
}

// LLMProviderKey is one weighted credential for a provider (Bifrost-style multi-key routing).
type LLMProviderKey struct {
	TenantID       string
	ProviderName   string
	KeyID          string
	CredentialRef  string
	Weight         float64
	ModelAllowlist string // JSON array; '[]' = all models
	Enabled        bool
	CreatedAt      string
}

// ErrLLMProviderNotFound is returned when a provider row is absent.
var ErrLLMProviderNotFound = errors.New("storage: llm provider not found")

// LLMProviderStore is the per-tenant registry for LLM providers and their keys.
// Every method is tenant-scoped: it takes tenantID and filters WHERE tenant_id = ?.
type LLMProviderStore interface {
	CreateProvider(ctx context.Context, p *LLMProvider) error
	GetProvider(ctx context.Context, tenantID, name string) (*LLMProvider, error)
	ListProviders(ctx context.Context, tenantID string) ([]*LLMProvider, error)
	UpdateProvider(ctx context.Context, p *LLMProvider) error
	DeleteProvider(ctx context.Context, tenantID, name string) error

	AddKey(ctx context.Context, k *LLMProviderKey) error
	ListKeys(ctx context.Context, tenantID, providerName string) ([]*LLMProviderKey, error)
	DeleteKey(ctx context.Context, tenantID, providerName, keyID string) error
}
