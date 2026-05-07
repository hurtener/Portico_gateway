package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type entityActivityStore struct {
	db *sql.DB
}

func (s *entityActivityStore) Append(ctx context.Context, r *ifaces.EntityActivityRecord) error {
	if r == nil || r.TenantID == "" || r.EntityKind == "" || r.EntityID == "" {
		return errors.New("sqlite: tenant_id, entity_kind, entity_id required")
	}
	occurred := r.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	var diff sql.NullString
	if len(r.DiffJSON) > 0 {
		diff = sql.NullString{String: string(r.DiffJSON), Valid: true}
	}
	var actor sql.NullString
	if r.ActorUserID != "" {
		actor = sql.NullString{String: r.ActorUserID, Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO entity_activity
		    (tenant_id, entity_kind, entity_id, event_id, occurred_at,
		     actor_user_id, summary, diff_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.TenantID, r.EntityKind, r.EntityID, r.EventID,
		occurred.UTC().Format("2006-01-02T15:04:05.000Z"),
		actor, r.Summary, diff,
	)
	if err != nil {
		return fmt.Errorf("sqlite: append entity_activity: %w", err)
	}
	return nil
}

func (s *entityActivityStore) List(ctx context.Context, tenantID, kind, id string, limit int) ([]*ifaces.EntityActivityRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, entity_kind, entity_id, event_id, occurred_at,
		       COALESCE(actor_user_id, ''), summary, COALESCE(diff_json, '')
		FROM entity_activity
		WHERE tenant_id = ? AND entity_kind = ? AND entity_id = ?
		ORDER BY occurred_at DESC
		LIMIT ?
	`, tenantID, kind, id, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list entity_activity: %w", err)
	}
	defer rows.Close()
	var out []*ifaces.EntityActivityRecord
	for rows.Next() {
		var (
			r        ifaces.EntityActivityRecord
			occurred string
			diff     string
		)
		if err := rows.Scan(
			&r.TenantID, &r.EntityKind, &r.EntityID, &r.EventID, &occurred,
			&r.ActorUserID, &r.Summary, &diff,
		); err != nil {
			return nil, err
		}
		r.OccurredAt, _ = parseSQLiteTime(occurred)
		if diff != "" {
			r.DiffJSON = []byte(diff)
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}
