package ifaces

import (
	"context"
	"errors"
)

// LLMModel is a per-tenant alias that resolves to a provider + provider model.
type LLMModel struct {
	TenantID          string
	Alias             string
	ProviderName      string
	ProviderModel     string
	DefaultParamsJSON string // canonical JSON; '{}' when empty
	Capabilities      string // JSON array; '[]' when empty
	Enabled           bool
	CreatedAt         string
	UpdatedAt         string
}

// ErrLLMModelNotFound is returned when a model alias row is absent.
var ErrLLMModelNotFound = errors.New("storage: llm model not found")

// LLMModelStore is the per-tenant model-alias registry. Every method is
// tenant-scoped: it takes tenantID and filters WHERE tenant_id = ?.
type LLMModelStore interface {
	CreateModel(ctx context.Context, m *LLMModel) error
	GetModel(ctx context.Context, tenantID, alias string) (*LLMModel, error) // the alias resolver
	ListModels(ctx context.Context, tenantID string) ([]*LLMModel, error)
	UpdateModel(ctx context.Context, m *LLMModel) error
	DeleteModel(ctx context.Context, tenantID, alias string) error
}
