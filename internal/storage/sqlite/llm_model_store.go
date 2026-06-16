package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type llmModelStore struct {
	db *sql.DB
}

func (s *llmModelStore) CreateModel(ctx context.Context, m *ifaces.LLMModel) error {
	if m == nil {
		return errors.New("sqlite: nil model")
	}
	if m.TenantID == "" || m.Alias == "" {
		return errors.New("sqlite: model requires tenant_id and alias")
	}
	if m.ProviderName == "" || m.ProviderModel == "" {
		return errors.New("sqlite: model requires provider_name and provider_model")
	}
	if m.DefaultParamsJSON == "" {
		m.DefaultParamsJSON = "{}"
	}
	if m.Capabilities == "" {
		m.Capabilities = "[]"
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	m.CreatedAt = now
	m.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO tenant_llm_models(
			tenant_id, alias, provider_name, provider_model, default_params_json, capabilities, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		m.TenantID, m.Alias, m.ProviderName, m.ProviderModel, m.DefaultParamsJSON, m.Capabilities,
		boolToInt(m.Enabled), now, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create model: %w", err)
	}
	return nil
}

func (s *llmModelStore) GetModel(ctx context.Context, tenantID, alias string) (*ifaces.LLMModel, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, alias, provider_name, provider_model, default_params_json, capabilities, enabled, created_at, updated_at
		FROM tenant_llm_models
		WHERE tenant_id = ? AND alias = ?
	`, tenantID, alias)
	return scanLLMModel(row)
}

func (s *llmModelStore) ListModels(ctx context.Context, tenantID string) ([]*ifaces.LLMModel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, alias, provider_name, provider_model, default_params_json, capabilities, enabled, created_at, updated_at
		FROM tenant_llm_models
		WHERE tenant_id = ?
		ORDER BY alias ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list models: %w", err)
	}
	defer rows.Close()
	out := make([]*ifaces.LLMModel, 0)
	for rows.Next() {
		m, err := scanLLMModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *llmModelStore) UpdateModel(ctx context.Context, m *ifaces.LLMModel) error {
	if m == nil {
		return errors.New("sqlite: nil model")
	}
	if m.TenantID == "" || m.Alias == "" {
		return errors.New("sqlite: model requires tenant_id and alias")
	}
	if m.ProviderName == "" || m.ProviderModel == "" {
		return errors.New("sqlite: model requires provider_name and provider_model")
	}
	if m.DefaultParamsJSON == "" {
		m.DefaultParamsJSON = "{}"
	}
	if m.Capabilities == "" {
		m.Capabilities = "[]"
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	m.UpdatedAt = now
	res, err := s.db.ExecContext(ctx, `
		UPDATE tenant_llm_models
		SET provider_name = ?, provider_model = ?, default_params_json = ?, capabilities = ?, enabled = ?, updated_at = ?
		WHERE tenant_id = ? AND alias = ?
	`,
		m.ProviderName, m.ProviderModel, m.DefaultParamsJSON, m.Capabilities, boolToInt(m.Enabled), now,
		m.TenantID, m.Alias,
	)
	if err != nil {
		return fmt.Errorf("sqlite: update model: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrLLMModelNotFound
	}
	return nil
}

func (s *llmModelStore) DeleteModel(ctx context.Context, tenantID, alias string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM tenant_llm_models
		WHERE tenant_id = ? AND alias = ?
	`, tenantID, alias)
	if err != nil {
		return fmt.Errorf("sqlite: delete model: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrLLMModelNotFound
	}
	return nil
}

func scanLLMModel(s llmScanner) (*ifaces.LLMModel, error) {
	var (
		m                   ifaces.LLMModel
		defaultParams, caps string
		enabled             int
		created, updated    string
	)
	if err := s.Scan(&m.TenantID, &m.Alias, &m.ProviderName, &m.ProviderModel, &defaultParams, &caps, &enabled, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrLLMModelNotFound
		}
		return nil, fmt.Errorf("sqlite: scan model: %w", err)
	}
	m.DefaultParamsJSON = defaultParams
	m.Capabilities = caps
	m.Enabled = enabled != 0
	m.CreatedAt = created
	m.UpdatedAt = updated
	return &m, nil
}
