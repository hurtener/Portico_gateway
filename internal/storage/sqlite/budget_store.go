package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// budgetStore is the SQLite-backed ifaces.BudgetStore. Parameterised and
// tenant-scoped (§6/§9). ReconcileUsage updates all ledger levels atomically in
// one transaction so a partial post-call reconcile is impossible.
type budgetStore struct {
	db *sql.DB
}

func (s *budgetStore) PutBudget(ctx context.Context, b *ifaces.Budget) error {
	if b == nil {
		return errors.New("sqlite: nil budget")
	}
	if b.TenantID == "" || b.ID == "" || b.ScopeKind == "" || b.ScopeID == "" || b.Metric == "" || b.Period == "" {
		return errors.New("sqlite: budget requires tenant_id, id, scope_kind, scope_id, metric, period")
	}
	if b.Alignment == "" {
		b.Alignment = "rolling"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var createdAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT created_at FROM governance_budgets WHERE tenant_id = ? AND id = ?
	`, b.TenantID, b.ID).Scan(&createdAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("sqlite: put budget: check existing: %w", err)
	}
	if createdAt == "" {
		createdAt = now
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO governance_budgets(
			tenant_id, id, scope_kind, scope_id, metric, period, alignment,
			limit_val, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			scope_kind = excluded.scope_kind,
			scope_id   = excluded.scope_id,
			metric     = excluded.metric,
			period     = excluded.period,
			alignment  = excluded.alignment,
			limit_val  = excluded.limit_val,
			enabled    = excluded.enabled,
			updated_at = excluded.updated_at
	`, b.TenantID, b.ID, b.ScopeKind, b.ScopeID, b.Metric, b.Period, b.Alignment,
		b.LimitVal, boolToInt(b.Enabled), createdAt, now)
	if err != nil {
		return fmt.Errorf("sqlite: put budget: %w", err)
	}
	return nil
}

func (s *budgetStore) GetBudget(ctx context.Context, tenantID, id string) (*ifaces.Budget, error) {
	if tenantID == "" || id == "" {
		return nil, errors.New("sqlite: get budget requires tenant_id and id")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, id, scope_kind, scope_id, metric, period, alignment, limit_val, enabled, created_at, updated_at
		FROM governance_budgets WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	b, err := scanBudget(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrBudgetNotFound
		}
		return nil, err
	}
	return b, nil
}

func (s *budgetStore) ListBudgets(ctx context.Context, tenantID string) ([]*ifaces.Budget, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: list budgets requires tenant_id")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, id, scope_kind, scope_id, metric, period, alignment, limit_val, enabled, created_at, updated_at
		FROM governance_budgets WHERE tenant_id = ? ORDER BY scope_kind, scope_id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list budgets: %w", err)
	}
	return collectBudgets(rows)
}

func (s *budgetStore) ListBudgetsByScope(ctx context.Context, tenantID, scopeKind, scopeID string) ([]*ifaces.Budget, error) {
	if tenantID == "" || scopeKind == "" || scopeID == "" {
		return nil, errors.New("sqlite: list budgets by scope requires tenant_id, scope_kind, scope_id")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, id, scope_kind, scope_id, metric, period, alignment, limit_val, enabled, created_at, updated_at
		FROM governance_budgets
		WHERE tenant_id = ? AND scope_kind = ? AND scope_id = ? AND enabled = 1
		ORDER BY metric, period
	`, tenantID, scopeKind, scopeID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list budgets by scope: %w", err)
	}
	return collectBudgets(rows)
}

func (s *budgetStore) DeleteBudget(ctx context.Context, tenantID, id string) error {
	if tenantID == "" || id == "" {
		return errors.New("sqlite: delete budget requires tenant_id and id")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM governance_budgets WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete budget: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ifaces.ErrBudgetNotFound
	}
	return nil
}

func (s *budgetStore) GetLedger(ctx context.Context, tenantID, budgetID, windowKey string) (*ifaces.LedgerEntry, bool, error) {
	if tenantID == "" || budgetID == "" || windowKey == "" {
		return nil, false, errors.New("sqlite: get ledger requires tenant_id, budget_id, window_key")
	}
	var e ifaces.LedgerEntry
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, budget_id, window_key, used, resets_at, last_warning_level
		FROM governance_budget_ledger WHERE tenant_id = ? AND budget_id = ? AND window_key = ?
	`, tenantID, budgetID, windowKey).Scan(&e.TenantID, &e.BudgetID, &e.WindowKey, &e.Used, &e.ResetsAt, &e.LastWarningLevel)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("sqlite: get ledger: %w", err)
	}
	return &e, true, nil
}

func (s *budgetStore) ReconcileUsage(ctx context.Context, tenantID string, updates []ifaces.LedgerUpdate) error {
	if tenantID == "" {
		return errors.New("sqlite: reconcile usage requires tenant_id")
	}
	if len(updates) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: reconcile usage: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, u := range updates {
		if u.BudgetID == "" || u.WindowKey == "" {
			return errors.New("sqlite: reconcile update requires budget_id and window_key")
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO governance_budget_ledger(tenant_id, budget_id, window_key, used, resets_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(tenant_id, budget_id, window_key) DO UPDATE SET
				used = used + excluded.used
		`, tenantID, u.BudgetID, u.WindowKey, u.Delta, u.ResetsAt)
		if err != nil {
			return fmt.Errorf("sqlite: reconcile usage: upsert ledger %s/%s: %w", u.BudgetID, u.WindowKey, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: reconcile usage: commit: %w", err)
	}
	return nil
}

func (s *budgetStore) SetLedgerWarningLevel(ctx context.Context, tenantID, budgetID, windowKey string, level int) error {
	if tenantID == "" || budgetID == "" || windowKey == "" {
		return errors.New("sqlite: set ledger warning level requires tenant_id, budget_id, window_key")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO governance_budget_ledger(tenant_id, budget_id, window_key, used, resets_at, last_warning_level)
		VALUES (?, ?, ?, 0, '', ?)
		ON CONFLICT(tenant_id, budget_id, window_key) DO UPDATE SET
			last_warning_level = excluded.last_warning_level
	`, tenantID, budgetID, windowKey, level)
	if err != nil {
		return fmt.Errorf("sqlite: set ledger warning level: %w", err)
	}
	return nil
}

func collectBudgets(rows *sql.Rows) ([]*ifaces.Budget, error) {
	defer rows.Close()
	var out []*ifaces.Budget
	for rows.Next() {
		b, err := scanBudget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate budgets: %w", err)
	}
	return out, nil
}

func scanBudget(row interface{ Scan(...any) error }) (*ifaces.Budget, error) {
	var b ifaces.Budget
	if err := row.Scan(
		&b.TenantID, &b.ID, &b.ScopeKind, &b.ScopeID, &b.Metric, &b.Period,
		&b.Alignment, &b.LimitVal, &b.Enabled, &b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("sqlite: scan budget: %w", err)
	}
	return &b, nil
}
