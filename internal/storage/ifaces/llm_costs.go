package ifaces

import (
	"context"
	"errors"
)

// LLMUnitCost is a per-provider-model price (GLOBAL, not tenant-scoped).
type LLMUnitCost struct {
	ProviderDriver string
	ProviderModel  string
	InputPer1K     float64
	OutputPer1K    float64
}

// LLMCostDaily is a per-tenant, per-day, per-alias cost rollup.
type LLMCostDaily struct {
	TenantID  string
	Day       string // YYYY-MM-DD UTC
	Alias     string
	Requests  int
	InputTok  int
	OutputTok int
	CostUSD   float64
}

// ErrLLMUnitCostNotFound is returned when no unit-cost row exists.
var ErrLLMUnitCostNotFound = errors.New("storage: llm unit cost not found")

// LLMCostStore manages the global price book + per-tenant daily cost rollups.
type LLMCostStore interface {
	// Unit costs (global).
	SetUnitCost(ctx context.Context, c *LLMUnitCost) error
	GetUnitCost(ctx context.Context, driver, model string) (*LLMUnitCost, error) // ErrLLMUnitCostNotFound on miss
	ListUnitCosts(ctx context.Context) ([]*LLMUnitCost, error)

	// Daily rollup (per tenant). AddUsage upserts and ACCUMULATES (requests/tokens/cost added to the existing row for that day+alias).
	AddUsage(ctx context.Context, tenantID, day, alias string, requests, inputTok, outputTok int, costUSD float64) error
	// ListDaily returns the tenant's rollup rows for a day range (or all if from/to empty), most-recent first.
	ListDaily(ctx context.Context, tenantID, fromDay, toDay string) ([]*LLMCostDaily, error)
}
