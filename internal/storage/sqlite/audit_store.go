package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type auditStore struct {
	db *sql.DB
}

// Append inserts an event. Phase 5 adds redaction + buffering on top.
func (s *auditStore) Append(ctx context.Context, e *ifaces.AuditEvent) error {
	if e == nil {
		return errors.New("sqlite: nil audit event")
	}
	if e.TenantID == "" || e.Type == "" {
		return errors.New("sqlite: audit event requires tenant_id and type")
	}
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("sqlite: marshal audit payload: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_events(id, tenant_id, type, session_id, user_id, occurred_at, trace_id, span_id, payload_json)
		VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), ?)
	`, e.ID, e.TenantID, e.Type, e.SessionID, e.UserID,
		e.OccurredAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		e.TraceID, e.SpanID, string(payload))
	if err != nil {
		return fmt.Errorf("sqlite: append audit: %w", err)
	}
	return nil
}

// Query returns events for a tenant. Phase 0 ships a minimal implementation;
// Phase 5 layers filtering + cursor pagination on top of this same path.
func (s *auditStore) Query(ctx context.Context, q ifaces.AuditQuery) ([]*ifaces.AuditEvent, string, error) {
	if q.TenantID == "" {
		return nil, "", errors.New("sqlite: audit query requires tenant_id")
	}
	limit := q.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, type, COALESCE(session_id,''), COALESCE(user_id,''),
		       occurred_at, COALESCE(trace_id,''), COALESCE(span_id,''), payload_json
		FROM audit_events
		WHERE tenant_id = ?
		ORDER BY occurred_at DESC
		LIMIT ?
	`, q.TenantID, limit)
	if err != nil {
		return nil, "", fmt.Errorf("sqlite: audit query: %w", err)
	}
	defer rows.Close()

	var out []*ifaces.AuditEvent
	for rows.Next() {
		var e ifaces.AuditEvent
		var occurred string
		var payload string
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Type, &e.SessionID, &e.UserID,
			&occurred, &e.TraceID, &e.SpanID, &payload); err != nil {
			return nil, "", err
		}
		e.OccurredAt, _ = parseSQLiteTime(occurred)
		if payload != "" {
			_ = json.Unmarshal([]byte(payload), &e.Payload)
		}
		out = append(out, &e)
	}
	return out, "", rows.Err()
}
