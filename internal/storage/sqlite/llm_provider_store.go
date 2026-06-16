package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type llmProviderStore struct {
	db *sql.DB
}

func (s *llmProviderStore) CreateProvider(ctx context.Context, p *ifaces.LLMProvider) error {
	if p == nil {
		return errors.New("sqlite: nil provider")
	}
	if p.TenantID == "" || p.Name == "" {
		return errors.New("sqlite: provider requires tenant_id and name")
	}
	if p.ConfigJSON == "" {
		p.ConfigJSON = "{}"
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	p.CreatedAt = now
	p.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO tenant_llm_providers(
			tenant_id, name, driver, config_json, credential_ref, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
	`,
		p.TenantID, p.Name, p.Driver, p.ConfigJSON, p.CredentialRef,
		boolToInt(p.Enabled), now, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create provider: %w", err)
	}
	return nil
}

func (s *llmProviderStore) GetProvider(ctx context.Context, tenantID, name string) (*ifaces.LLMProvider, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, name, driver, config_json, credential_ref, enabled, created_at, updated_at
		FROM tenant_llm_providers
		WHERE tenant_id = ? AND name = ?
	`, tenantID, name)
	return scanLLMProvider(row)
}

func (s *llmProviderStore) ListProviders(ctx context.Context, tenantID string) ([]*ifaces.LLMProvider, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, name, driver, config_json, credential_ref, enabled, created_at, updated_at
		FROM tenant_llm_providers
		WHERE tenant_id = ?
		ORDER BY name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list providers: %w", err)
	}
	defer rows.Close()
	out := make([]*ifaces.LLMProvider, 0)
	for rows.Next() {
		p, err := scanLLMProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *llmProviderStore) UpdateProvider(ctx context.Context, p *ifaces.LLMProvider) error {
	if p == nil {
		return errors.New("sqlite: nil provider")
	}
	if p.TenantID == "" || p.Name == "" {
		return errors.New("sqlite: provider requires tenant_id and name")
	}
	if p.ConfigJSON == "" {
		p.ConfigJSON = "{}"
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	p.UpdatedAt = now
	res, err := s.db.ExecContext(ctx, `
		UPDATE tenant_llm_providers
		SET driver = ?, config_json = ?, credential_ref = NULLIF(?, ''), enabled = ?, updated_at = ?
		WHERE tenant_id = ? AND name = ?
	`,
		p.Driver, p.ConfigJSON, p.CredentialRef, boolToInt(p.Enabled), now,
		p.TenantID, p.Name,
	)
	if err != nil {
		return fmt.Errorf("sqlite: update provider: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrLLMProviderNotFound
	}
	return nil
}

func (s *llmProviderStore) DeleteProvider(ctx context.Context, tenantID, name string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM tenant_llm_providers
		WHERE tenant_id = ? AND name = ?
	`, tenantID, name)
	if err != nil {
		return fmt.Errorf("sqlite: delete provider: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrLLMProviderNotFound
	}
	return nil
}

func (s *llmProviderStore) AddKey(ctx context.Context, k *ifaces.LLMProviderKey) error {
	if k == nil {
		return errors.New("sqlite: nil provider key")
	}
	if k.TenantID == "" || k.ProviderName == "" || k.KeyID == "" {
		return errors.New("sqlite: provider key requires tenant_id, provider_name, and key_id")
	}
	if k.CredentialRef == "" {
		return errors.New("sqlite: provider key requires credential_ref")
	}
	if k.ModelAllowlist == "" {
		k.ModelAllowlist = "[]"
	}
	if k.Weight == 0 {
		k.Weight = 1.0
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	k.CreatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO tenant_llm_provider_keys(
			tenant_id, provider_name, key_id, credential_ref, weight, model_allowlist, enabled, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		k.TenantID, k.ProviderName, k.KeyID, k.CredentialRef,
		k.Weight, k.ModelAllowlist, boolToInt(k.Enabled), now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: add provider key: %w", err)
	}
	return nil
}

func (s *llmProviderStore) ListKeys(ctx context.Context, tenantID, providerName string) ([]*ifaces.LLMProviderKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, provider_name, key_id, credential_ref, weight, model_allowlist, enabled, created_at
		FROM tenant_llm_provider_keys
		WHERE tenant_id = ? AND provider_name = ?
		ORDER BY key_id ASC
	`, tenantID, providerName)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list provider keys: %w", err)
	}
	defer rows.Close()
	out := make([]*ifaces.LLMProviderKey, 0)
	for rows.Next() {
		k, err := scanLLMProviderKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *llmProviderStore) DeleteKey(ctx context.Context, tenantID, providerName, keyID string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM tenant_llm_provider_keys
		WHERE tenant_id = ? AND provider_name = ? AND key_id = ?
	`, tenantID, providerName, keyID)
	if err != nil {
		return fmt.Errorf("sqlite: delete provider key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrLLMProviderNotFound
	}
	return nil
}

type llmScanner interface {
	Scan(dest ...any) error
}

func scanLLMProvider(s llmScanner) (*ifaces.LLMProvider, error) {
	var (
		p                         ifaces.LLMProvider
		credRef, created, updated sql.NullString
		enabled                   int
	)
	if err := s.Scan(&p.TenantID, &p.Name, &p.Driver, &p.ConfigJSON, &credRef, &enabled, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrLLMProviderNotFound
		}
		return nil, fmt.Errorf("sqlite: scan provider: %w", err)
	}
	p.CredentialRef = credRef.String
	p.Enabled = enabled != 0
	p.CreatedAt = created.String
	p.UpdatedAt = updated.String
	return &p, nil
}

func scanLLMProviderKey(s llmScanner) (*ifaces.LLMProviderKey, error) {
	var (
		k              ifaces.LLMProviderKey
		modelAllowlist sql.NullString
		weight         float64
		enabled        int
		created        string
	)
	if err := s.Scan(&k.TenantID, &k.ProviderName, &k.KeyID, &k.CredentialRef, &weight, &modelAllowlist, &enabled, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrLLMProviderNotFound
		}
		return nil, fmt.Errorf("sqlite: scan provider key: %w", err)
	}
	k.Weight = weight
	k.ModelAllowlist = modelAllowlist.String
	k.Enabled = enabled != 0
	k.CreatedAt = created
	return &k, nil
}
