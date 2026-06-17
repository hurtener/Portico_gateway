package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type llmCostStore struct {
	db *sql.DB
}

func (s *llmCostStore) SetUnitCost(ctx context.Context, c *ifaces.LLMUnitCost) error {
	if c == nil {
		return errors.New("sqlite: nil unit cost")
	}
	if c.ProviderDriver == "" || c.ProviderModel == "" {
		return errors.New("sqlite: unit cost requires provider_driver and provider_model")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO llm_unit_costs(
			provider_driver, provider_model, input_per_1k, output_per_1k
		) VALUES (?, ?, ?, ?)
	`,
		c.ProviderDriver, c.ProviderModel, c.InputPer1K, c.OutputPer1K,
	)
	if err != nil {
		return fmt.Errorf("sqlite: set unit cost: %w", err)
	}
	return nil
}

func (s *llmCostStore) GetUnitCost(ctx context.Context, driver, model string) (*ifaces.LLMUnitCost, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT provider_driver, provider_model, input_per_1k, output_per_1k
		FROM llm_unit_costs
		WHERE provider_driver = ? AND provider_model = ?
	`, driver, model)
	return scanLLMUnitCost(row)
}

func (s *llmCostStore) ListUnitCosts(ctx context.Context) ([]*ifaces.LLMUnitCost, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider_driver, provider_model, input_per_1k, output_per_1k
		FROM llm_unit_costs
		ORDER BY provider_driver ASC, provider_model ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list unit costs: %w", err)
	}
	defer rows.Close()
	out := make([]*ifaces.LLMUnitCost, 0)
	for rows.Next() {
		c, err := scanLLMUnitCost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *llmCostStore) AddUsage(ctx context.Context, tenantID, day, alias string, requests, inputTok, outputTok int, costUSD float64) error {
	if tenantID == "" || day == "" || alias == "" {
		return errors.New("sqlite: add usage requires tenant_id, day, and alias")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenant_llm_cost_daily(
			tenant_id, day, alias, requests, input_tok, output_tok, cost_usd
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, day, alias) DO UPDATE SET
			requests = requests + excluded.requests,
			input_tok = input_tok + excluded.input_tok,
			output_tok = output_tok + excluded.output_tok,
			cost_usd = cost_usd + excluded.cost_usd
	`,
		tenantID, day, alias, requests, inputTok, outputTok, costUSD,
	)
	if err != nil {
		return fmt.Errorf("sqlite: add usage: %w", err)
	}
	return nil
}

func (s *llmCostStore) ListDaily(ctx context.Context, tenantID, fromDay, toDay string) ([]*ifaces.LLMCostDaily, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: list daily requires tenant_id")
	}
	var query string
	var args []any
	if fromDay != "" && toDay != "" {
		query = `
			SELECT tenant_id, day, alias, requests, input_tok, output_tok, cost_usd
			FROM tenant_llm_cost_daily
			WHERE tenant_id = ? AND day BETWEEN ? AND ?
			ORDER BY day DESC, alias ASC
		`
		args = []any{tenantID, fromDay, toDay}
	} else if fromDay != "" {
		query = `
			SELECT tenant_id, day, alias, requests, input_tok, output_tok, cost_usd
			FROM tenant_llm_cost_daily
			WHERE tenant_id = ? AND day >= ?
			ORDER BY day DESC, alias ASC
		`
		args = []any{tenantID, fromDay}
	} else if toDay != "" {
		query = `
			SELECT tenant_id, day, alias, requests, input_tok, output_tok, cost_usd
			FROM tenant_llm_cost_daily
			WHERE tenant_id = ? AND day <= ?
			ORDER BY day DESC, alias ASC
		`
		args = []any{tenantID, toDay}
	} else {
		query = `
			SELECT tenant_id, day, alias, requests, input_tok, output_tok, cost_usd
			FROM tenant_llm_cost_daily
			WHERE tenant_id = ?
			ORDER BY day DESC, alias ASC
		`
		args = []any{tenantID}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list daily: %w", err)
	}
	defer rows.Close()
	out := make([]*ifaces.LLMCostDaily, 0)
	for rows.Next() {
		d, err := scanLLMCostDaily(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanLLMUnitCost(s llmScanner) (*ifaces.LLMUnitCost, error) {
	var c ifaces.LLMUnitCost
	if err := s.Scan(&c.ProviderDriver, &c.ProviderModel, &c.InputPer1K, &c.OutputPer1K); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrLLMUnitCostNotFound
		}
		return nil, fmt.Errorf("sqlite: scan unit cost: %w", err)
	}
	return &c, nil
}

func scanLLMCostDaily(s llmScanner) (*ifaces.LLMCostDaily, error) {
	var d ifaces.LLMCostDaily
	if err := s.Scan(&d.TenantID, &d.Day, &d.Alias, &d.Requests, &d.InputTok, &d.OutputTok, &d.CostUSD); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sqlite: scan cost daily: %w", err)
		}
		return nil, fmt.Errorf("sqlite: scan cost daily: %w", err)
	}
	return &d, nil
}
