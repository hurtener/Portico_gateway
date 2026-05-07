package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// policyRulesStore is the sqlite-backed PolicyRulesStore. The conditions
// and actions columns hold canonical JSON produced by the editor — the
// store does not interpret them.
type policyRulesStore struct {
	db *sql.DB
}

const policyRuleSelect = `
	SELECT tenant_id, rule_id, priority, enabled, risk_class,
	       conditions, actions, COALESCE(notes, ''), updated_at, COALESCE(updated_by, '')
	FROM tenant_policy_rules
`

func (s *policyRulesStore) List(ctx context.Context, tenantID string) ([]*ifaces.PolicyRuleRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		policyRuleSelect+`WHERE tenant_id = ? ORDER BY priority ASC, rule_id ASC`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list policy rules: %w", err)
	}
	defer rows.Close()
	var out []*ifaces.PolicyRuleRecord
	for rows.Next() {
		r, err := scanPolicyRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *policyRulesStore) Get(ctx context.Context, tenantID, ruleID string) (*ifaces.PolicyRuleRecord, error) {
	row := s.db.QueryRowContext(ctx,
		policyRuleSelect+`WHERE tenant_id = ? AND rule_id = ?`,
		tenantID, ruleID)
	r, err := scanPolicyRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrNotFound
	}
	return r, err
}

func (s *policyRulesStore) Upsert(ctx context.Context, r *ifaces.PolicyRuleRecord) error {
	if r == nil || r.TenantID == "" || r.RuleID == "" {
		return errors.New("sqlite: tenant_id and rule_id required")
	}
	updatedAt := r.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenant_policy_rules
		    (tenant_id, rule_id, priority, enabled, risk_class,
		     conditions, actions, notes, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, rule_id) DO UPDATE SET
		    priority   = excluded.priority,
		    enabled    = excluded.enabled,
		    risk_class = excluded.risk_class,
		    conditions = excluded.conditions,
		    actions    = excluded.actions,
		    notes      = excluded.notes,
		    updated_at = excluded.updated_at,
		    updated_by = excluded.updated_by
	`,
		r.TenantID, r.RuleID, r.Priority, boolToInt(r.Enabled), r.RiskClass,
		string(r.Conditions), string(r.Actions), r.Notes,
		updatedAt.UTC().Format("2006-01-02T15:04:05.000Z"), r.UpdatedBy,
	)
	if err != nil {
		return fmt.Errorf("sqlite: upsert policy rule %s/%s: %w", r.TenantID, r.RuleID, err)
	}
	return nil
}

func (s *policyRulesStore) Delete(ctx context.Context, tenantID, ruleID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM tenant_policy_rules WHERE tenant_id = ? AND rule_id = ?`,
		tenantID, ruleID)
	if err != nil {
		return fmt.Errorf("sqlite: delete policy rule %s/%s: %w", tenantID, ruleID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *policyRulesStore) ReplaceAll(ctx context.Context, tenantID string, rules []*ifaces.PolicyRuleRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: replace policy rules begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM tenant_policy_rules WHERE tenant_id = ?`, tenantID); err != nil {
		return fmt.Errorf("sqlite: replace policy rules clear: %w", err)
	}
	for _, r := range rules {
		if r == nil {
			continue
		}
		updatedAt := r.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = time.Now().UTC()
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tenant_policy_rules
			    (tenant_id, rule_id, priority, enabled, risk_class,
			     conditions, actions, notes, updated_at, updated_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			tenantID, r.RuleID, r.Priority, boolToInt(r.Enabled), r.RiskClass,
			string(r.Conditions), string(r.Actions), r.Notes,
			updatedAt.UTC().Format("2006-01-02T15:04:05.000Z"), r.UpdatedBy,
		); err != nil {
			return fmt.Errorf("sqlite: replace policy rule %s: %w", r.RuleID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: replace policy rules commit: %w", err)
	}
	return nil
}

func scanPolicyRule(rs rowScanner) (*ifaces.PolicyRuleRecord, error) {
	var (
		r        ifaces.PolicyRuleRecord
		updated  string
		enabled  int
		condText string
		actText  string
	)
	if err := rs.Scan(
		&r.TenantID, &r.RuleID, &r.Priority, &enabled, &r.RiskClass,
		&condText, &actText, &r.Notes, &updated, &r.UpdatedBy,
	); err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	r.Conditions = []byte(condText)
	r.Actions = []byte(actText)
	r.UpdatedAt, _ = parseSQLiteTime(updated)
	return &r, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
