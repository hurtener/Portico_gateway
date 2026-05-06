package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type approvalStore struct {
	db *sql.DB
}

func (s *approvalStore) Insert(ctx context.Context, a *ifaces.ApprovalRecord) error {
	if a == nil {
		return errors.New("sqlite: nil approval")
	}
	if a.TenantID == "" || a.ID == "" {
		return errors.New("sqlite: approval requires tenant_id and id")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO approvals(
			id, tenant_id, session_id, user_id, tool, args_summary, risk_class,
			status, created_at, decided_at, expires_at, metadata_json
		) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''),
			?, ?, NULLIF(?, ''), ?, NULLIF(?, ''))
	`,
		a.ID, a.TenantID, a.SessionID, nullableUser(a.UserID), a.Tool,
		a.ArgsSummary, a.RiskClass,
		a.Status,
		a.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		formatNullableTime(a.DecidedAt),
		a.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		a.MetadataJSON,
	)
	if err != nil {
		return fmt.Errorf("sqlite: insert approval: %w", err)
	}
	return nil
}

func (s *approvalStore) UpdateStatus(ctx context.Context, tenantID, id, status string, decidedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE approvals
		SET status = ?, decided_at = ?
		WHERE tenant_id = ? AND id = ?
	`, status, decidedAt.UTC().Format("2006-01-02T15:04:05.000Z"), tenantID, id)
	if err != nil {
		return fmt.Errorf("sqlite: update approval: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *approvalStore) Get(ctx context.Context, tenantID, id string) (*ifaces.ApprovalRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, session_id, user_id, tool, args_summary, risk_class,
		       status, created_at, decided_at, expires_at, metadata_json
		FROM approvals
		WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	return scanApproval(row)
}

func (s *approvalStore) ListPending(ctx context.Context, tenantID string) ([]*ifaces.ApprovalRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, session_id, user_id, tool, args_summary, risk_class,
		       status, created_at, decided_at, expires_at, metadata_json
		FROM approvals
		WHERE tenant_id = ? AND status = 'pending'
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*ifaces.ApprovalRecord, 0)
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *approvalStore) ExpireOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE approvals
		SET status = 'expired', decided_at = ?
		WHERE status = 'pending' AND expires_at < ?
	`, cutoff.UTC().Format("2006-01-02T15:04:05.000Z"), cutoff.UTC().Format("2006-01-02T15:04:05.000Z"))
	if err != nil {
		return 0, fmt.Errorf("sqlite: expire approvals: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// scanner is the contract shared by *sql.Row and *sql.Rows for the
// scanApproval helper.
type scanner interface {
	Scan(dest ...any) error
}

func scanApproval(s scanner) (*ifaces.ApprovalRecord, error) {
	var (
		a                                  ifaces.ApprovalRecord
		sess, user, args, risk, decided    sql.NullString
		metadata                            sql.NullString
		created, expires                    string
	)
	if err := s.Scan(&a.ID, &a.TenantID, &sess, &user, &a.Tool, &args, &risk,
		&a.Status, &created, &decided, &expires, &metadata); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: scan approval: %w", err)
	}
	a.SessionID = sess.String
	a.UserID = user.String
	a.ArgsSummary = args.String
	a.RiskClass = risk.String
	a.MetadataJSON = metadata.String
	if t, err := time.Parse("2006-01-02T15:04:05.000Z", created); err == nil {
		a.CreatedAt = t
	}
	if t, err := time.Parse("2006-01-02T15:04:05.000Z", expires); err == nil {
		a.ExpiresAt = t
	}
	if decided.Valid && decided.String != "" {
		if t, err := time.Parse("2006-01-02T15:04:05.000Z", decided.String); err == nil {
			a.DecidedAt = &t
		}
	}
	return &a, nil
}

func nullableUser(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func formatNullableTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
