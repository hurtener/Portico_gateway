// Package sqlite is the SQLite-backed Store implementation.
//
// Schema lives in internal/storage/sqlite/migrations/0011_spanstore.sql.
// Every method round-trips through the (tenant_id, trace_id, span_id)
// primary key; tenant_id always leads so the per-tenant scan is cheap.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
)

// timeFormat is the canonical RFC3339 nanosecond layout used in every
// `spans.started_at` / `spans.ended_at` cell. UTC enforced at write
// time so cross-instance bundle compares are stable.
const timeFormat = time.RFC3339Nano

// Store is a SQLite-backed spanstore.Store.
type Store struct {
	db *sql.DB
}

// New wraps an open *sql.DB. The DB must already have run migrations
// 0011_spanstore.sql; New does not run migrations itself (callers go
// through the storage package which already does).
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Put writes a batch of spans in a single transaction. Idempotent:
// re-inserting the same primary key overwrites the row.
func (s *Store) Put(ctx context.Context, batch []spanstore.Span) error {
	if len(batch) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("spanstore: begin tx: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO spans (
		    tenant_id, session_id, trace_id, span_id, parent_id,
		    name, kind, started_at, ended_at, status, status_msg,
		    attrs_json, events_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("spanstore: prepare put: %w", err)
	}
	defer stmt.Close()

	for _, sp := range batch {
		if sp.TenantID == "" || sp.TraceID == "" || sp.SpanID == "" {
			return fmt.Errorf("spanstore: span missing required tenant_id/trace_id/span_id")
		}
		attrsJSON, err := canonicalJSON(sp.Attrs)
		if err != nil {
			return fmt.Errorf("spanstore: encode attrs for span %s: %w", sp.SpanID, err)
		}
		eventsJSON, err := canonicalJSON(sp.Events)
		if err != nil {
			return fmt.Errorf("spanstore: encode events for span %s: %w", sp.SpanID, err)
		}
		kind := sp.Kind
		if kind == "" {
			kind = spanstore.KindInternal
		}
		status := sp.Status
		if status == "" {
			status = spanstore.StatusUnset
		}
		_, err = stmt.ExecContext(ctx,
			sp.TenantID,
			sp.SessionID,
			sp.TraceID,
			sp.SpanID,
			sp.ParentID,
			sp.Name,
			kind,
			sp.StartedAt.UTC().Format(timeFormat),
			sp.EndedAt.UTC().Format(timeFormat),
			status,
			sp.StatusMsg,
			attrsJSON,
			eventsJSON,
		)
		if err != nil {
			return fmt.Errorf("spanstore: insert span %s: %w", sp.SpanID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("spanstore: commit: %w", err)
	}
	rollback = false
	return nil
}

// QueryBySession returns spans matching (tenant, session), ordered by
// started_at ASC.
func (s *Store) QueryBySession(ctx context.Context, tenantID, sessionID string) ([]spanstore.Span, error) {
	if tenantID == "" {
		return nil, errors.New("spanstore: tenantID required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, session_id, trace_id, span_id, parent_id,
		       name, kind, started_at, ended_at, status, status_msg,
		       attrs_json, events_json
		  FROM spans
		 WHERE tenant_id = ? AND session_id = ?
		 ORDER BY started_at ASC
	`, tenantID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("spanstore: query by session: %w", err)
	}
	return scanSpans(rows)
}

// QueryByTrace returns every span in a trace for a tenant.
func (s *Store) QueryByTrace(ctx context.Context, tenantID, traceID string) ([]spanstore.Span, error) {
	if tenantID == "" {
		return nil, errors.New("spanstore: tenantID required")
	}
	if traceID == "" {
		return nil, errors.New("spanstore: traceID required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, session_id, trace_id, span_id, parent_id,
		       name, kind, started_at, ended_at, status, status_msg,
		       attrs_json, events_json
		  FROM spans
		 WHERE tenant_id = ? AND trace_id = ?
		 ORDER BY started_at ASC
	`, tenantID, traceID)
	if err != nil {
		return nil, fmt.Errorf("spanstore: query by trace: %w", err)
	}
	return scanSpans(rows)
}

// Purge deletes spans whose ended_at is before the cutoff.
func (s *Store) Purge(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM spans WHERE ended_at < ?`,
		before.UTC().Format(timeFormat),
	)
	if err != nil {
		return 0, fmt.Errorf("spanstore: purge: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func scanSpans(rows *sql.Rows) ([]spanstore.Span, error) {
	defer rows.Close()
	var out []spanstore.Span
	for rows.Next() {
		var sp spanstore.Span
		var startedStr, endedStr, attrsJSON, eventsJSON string
		var sessionID, parentID, statusMsg sql.NullString
		err := rows.Scan(
			&sp.TenantID, &sessionID, &sp.TraceID, &sp.SpanID, &parentID,
			&sp.Name, &sp.Kind, &startedStr, &endedStr, &sp.Status, &statusMsg,
			&attrsJSON, &eventsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("spanstore: scan span: %w", err)
		}
		sp.SessionID = sessionID.String
		sp.ParentID = parentID.String
		sp.StatusMsg = statusMsg.String
		sp.StartedAt, err = time.Parse(timeFormat, startedStr)
		if err != nil {
			return nil, fmt.Errorf("spanstore: parse started_at: %w", err)
		}
		sp.EndedAt, err = time.Parse(timeFormat, endedStr)
		if err != nil {
			return nil, fmt.Errorf("spanstore: parse ended_at: %w", err)
		}
		if attrsJSON != "" {
			if err := json.Unmarshal([]byte(attrsJSON), &sp.Attrs); err != nil {
				return nil, fmt.Errorf("spanstore: decode attrs: %w", err)
			}
		}
		if eventsJSON != "" {
			if err := json.Unmarshal([]byte(eventsJSON), &sp.Events); err != nil {
				return nil, fmt.Errorf("spanstore: decode events: %w", err)
			}
		}
		out = append(out, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spanstore: rows: %w", err)
	}
	return out, nil
}

// canonicalJSON encodes a value with deterministic key ordering — bundle
// determinism (Phase 11 requirement) depends on this. Map keys are
// sorted alphabetically; nested maps recurse.
func canonicalJSON(v any) (string, error) {
	if v == nil {
		// Distinguish "no attrs" from "empty attrs" — both serialise as
		// the empty object so consumers don't need to handle nullability.
		return "{}", nil
	}
	canon := canonicalize(v)
	b, err := json.Marshal(canon)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// Use json.RawMessage entries in a key-ordered slice via a
		// custom marshaler — simpler: emit a sorted slice of {k,v}
		// pairs and let Marshal walk it. But map[string]any with
		// json.Encoder.SetSortMaps doesn't exist; we have to emit the
		// sorted-key map via a slice + a stable encoder. Easiest:
		// rebuild as a json.RawMessage built by hand.
		var sb strings.Builder
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			sb.Write(kb)
			sb.WriteByte(':')
			vb, err := json.Marshal(canonicalize(t[k]))
			if err != nil {
				return ""
			}
			sb.Write(vb)
		}
		sb.WriteByte('}')
		return json.RawMessage(sb.String())
	case []any:
		out := make([]any, len(t))
		for i, v := range t {
			out[i] = canonicalize(v)
		}
		return out
	default:
		return v
	}
}
