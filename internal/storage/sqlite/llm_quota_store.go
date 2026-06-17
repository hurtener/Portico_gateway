package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type llmQuotaStore struct {
	db *sql.DB
}

func (s *llmQuotaStore) GetQuota(ctx context.Context, tenantID string) (*ifaces.LLMQuota, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, requests_per_minute, tokens_per_minute, tokens_per_day, cost_usd_per_day, updated_at
		FROM tenant_llm_quotas
		WHERE tenant_id = ?
	`, tenantID)
	return scanLLMQuota(row)
}

func (s *llmQuotaStore) GetOrDefault(ctx context.Context, tenantID string) (*ifaces.LLMQuota, error) {
	q, err := s.GetQuota(ctx, tenantID)
	if err != nil {
		if errors.Is(err, ifaces.ErrLLMQuotaNotFound) {
			def := ifaces.DefaultLLMQuota(tenantID)
			return &def, nil
		}
		return nil, err
	}
	return q, nil
}

func (s *llmQuotaStore) SetQuota(ctx context.Context, q *ifaces.LLMQuota) error {
	if q == nil {
		return errors.New("sqlite: nil quota")
	}
	if q.TenantID == "" {
		return errors.New("sqlite: quota requires tenant_id")
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	q.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO tenant_llm_quotas(
			tenant_id, requests_per_minute, tokens_per_minute, tokens_per_day, cost_usd_per_day, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`,
		q.TenantID, q.RequestsPerMinute, q.TokensPerMinute, q.TokensPerDay, q.CostUSDPerDay, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: set quota: %w", err)
	}
	return nil
}

func (s *llmQuotaStore) DeleteQuota(ctx context.Context, tenantID string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM tenant_llm_quotas
		WHERE tenant_id = ?
	`, tenantID)
	if err != nil {
		return fmt.Errorf("sqlite: delete quota: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrLLMQuotaNotFound
	}
	return nil
}

func scanLLMQuota(s llmScanner) (*ifaces.LLMQuota, error) {
	var (
		q       ifaces.LLMQuota
		updated sql.NullString
	)
	if err := s.Scan(&q.TenantID, &q.RequestsPerMinute, &q.TokensPerMinute, &q.TokensPerDay, &q.CostUSDPerDay, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrLLMQuotaNotFound
		}
		return nil, fmt.Errorf("sqlite: scan quota: %w", err)
	}
	q.UpdatedAt = updated.String
	return &q, nil
}
