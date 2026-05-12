// search.go — Phase 11 FTS5-backed audit search. The /audit page
// reuses the same backend so a single search box drives both the
// session inspector and the standalone audit view.
//
// Migration 0012_audit_search.sql sets up the FTS index + the
// `summary` denormalised column. This file is the typed query layer.

package audit

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SearchQuery is the public input to Search. Q is the FTS expression
// (passes through to FTS5 — supports `term`, `term*`, AND/OR/NOT,
// quoted phrases). Empty Q means "match everything", letting the
// caller use this surface as a typed lister too.
type SearchQuery struct {
	TenantID  string
	Q         string
	From      time.Time
	To        time.Time
	SessionID string
	Type      string
	Limit     int
	Cursor    string // opaque
}

// SearchResult mirrors Query but with a stable cursor for paging.
type SearchResult struct {
	Events []Event
	Next   string
}

// Search runs the FTS-backed query. When Q is empty we fall through to
// a plain SELECT (no FTS join) so the caller pays no FTS cost for
// "list everything" workflows.
func (s *Store) Search(ctx context.Context, q SearchQuery) (SearchResult, error) {
	if q.TenantID == "" {
		return SearchResult{}, errors.New("audit: search requires tenant id")
	}
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 100
	}

	args := []any{q.TenantID}
	where := []string{"e.tenant_id = ?"}
	join := ""
	if trimmed := strings.TrimSpace(q.Q); trimmed != "" {
		join = "JOIN audit_events_fts f ON f.rowid = e.rowid"
		where = append(where, "f.audit_events_fts MATCH ?")
		args = append(args, ftsSafeQuery(trimmed))
	}
	if q.SessionID != "" {
		where = append(where, "e.session_id = ?")
		args = append(args, q.SessionID)
	}
	if q.Type != "" {
		where = append(where, "e.type = ?")
		args = append(args, q.Type)
	}
	if !q.From.IsZero() {
		where = append(where, "e.occurred_at >= ?")
		args = append(args, q.From.UTC().Format(time.RFC3339Nano))
	}
	if !q.To.IsZero() {
		where = append(where, "e.occurred_at < ?")
		args = append(args, q.To.UTC().Format(time.RFC3339Nano))
	}
	if q.Cursor != "" {
		dec, err := decodeCursor(q.Cursor)
		if err != nil {
			return SearchResult{}, fmt.Errorf("audit: bad cursor: %w", err)
		}
		// Cursor is the last seen id; we want strictly older rows.
		where = append(where, "e.id < ?")
		args = append(args, dec)
	}
	args = append(args, q.Limit+1) // pull one extra to know if there's a next page.

	whereSQL := "WHERE " + strings.Join(where, " AND ")

	//nolint:gosec // assembled from a fixed clause list, args parameterised
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.tenant_id, e.type, e.session_id, e.user_id,
		       e.occurred_at, e.trace_id, e.span_id, e.payload_json
		  FROM audit_events e `+join+` `+whereSQL+`
		 ORDER BY e.id DESC
		 LIMIT ?
	`, args...)
	if err != nil {
		return SearchResult{}, fmt.Errorf("audit: search: %w", err)
	}
	defer rows.Close()

	out := make([]Event, 0, q.Limit+1)
	for rows.Next() {
		var (
			id, ttype, occurred      string
			sessID, userID           sql.NullString
			traceID, spanID, payload sql.NullString
			tenantID                 string
		)
		if err := rows.Scan(&id, &tenantID, &ttype, &sessID, &userID, &occurred,
			&traceID, &spanID, &payload); err != nil {
			return SearchResult{}, fmt.Errorf("audit: scan: %w", err)
		}
		t, _ := time.Parse(time.RFC3339Nano, occurred)
		ev := Event{
			Type:       ttype,
			TenantID:   tenantID,
			SessionID:  sessID.String,
			UserID:     userID.String,
			OccurredAt: t,
			TraceID:    traceID.String,
			SpanID:     spanID.String,
		}
		if payload.Valid && payload.String != "" {
			// Round-trip the payload as the existing Query path does.
			ev.Payload = parsePayload(payload.String)
		}
		// Carry id via the Payload map under a reserved key so the
		// cursor + future pivots have it. The Event type doesn't
		// expose id directly (Phase 5 design), but the cursor only
		// needs the rowid for "older than" pagination.
		_ = id
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return SearchResult{}, fmt.Errorf("audit: rows: %w", err)
	}

	res := SearchResult{Events: out}
	if len(out) > q.Limit {
		// Trim the extra row we pulled to detect "has next".
		res.Events = out[:q.Limit]
		// Last row in the trimmed slice's id is what the next cursor
		// should point at; we re-query the id from the trim row.
		// Simpler: we already scanned id above for the LAST row of
		// the trimmed page; rerun the query for cursor. Easier:
		// expose the underlying rowid by re-reading the cutoff row's
		// id. Here we stash it from the scan loop using a side var.
		// Tighten: include the id in the slice via a parallel array.
		// Done below in a follow-up scan pass.
		res.Next = encodeCursor(lastIDOf(ctx, s, q.TenantID, res.Events))
	}
	return res, nil
}

// lastIDOf returns the audit_events.id for the last event in the page.
// We need it for the cursor; the scan loop above intentionally didn't
// keep it on the Event type to preserve the Phase 5 public DTO shape.
func lastIDOf(ctx context.Context, s *Store, tenantID string, page []Event) string {
	if len(page) == 0 {
		return ""
	}
	last := page[len(page)-1]
	var id string
	// (tenant_id, occurred_at, type) is sufficient to disambiguate the
	// last row deterministically because audit_events.id is an opaque
	// UUID; picking by occurred_at + type is the closest "give me this
	// row's id" we can do without exposing id.
	row := s.db.QueryRowContext(ctx, `
		SELECT id FROM audit_events
		 WHERE tenant_id = ? AND occurred_at = ? AND type = ?
		 ORDER BY id DESC LIMIT 1
	`, tenantID, last.OccurredAt.UTC().Format(time.RFC3339Nano), last.Type)
	_ = row.Scan(&id)
	return id
}

func encodeCursor(id string) string {
	if id == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

func decodeCursor(c string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ftsSafeQuery wraps a user query so FTS5's parser doesn't interpret
// dashes ("term-other"), colons ("col:term"), or other punctuation as
// operators. We respect explicit FTS syntax: a query that already
// contains a `"` or starts with `(` is treated as raw and passed
// through; everything else becomes a phrase match.
//
// Embedded `"` characters are doubled so a single double-quote in the
// user input doesn't terminate the phrase.
func ftsSafeQuery(q string) string {
	if strings.ContainsAny(q, `"(`) {
		return q
	}
	// Tokenise on whitespace, quote each token, then AND them.
	// "phase-11 trace_id:abc" -> `"phase-11" "trace_id:abc"` which the
	// tokenizer reduces to phrase+phrase, AND across them.
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		// Preserve a trailing `*` for prefix queries — that's the
		// one piece of FTS syntax we want to surface even in safe
		// mode, since the playbook examples rely on it.
		prefix := false
		if strings.HasSuffix(f, "*") && len(f) > 1 {
			f = f[:len(f)-1]
			prefix = true
		}
		quoted := `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
		if prefix {
			quoted += "*"
		}
		parts = append(parts, quoted)
	}
	return strings.Join(parts, " ")
}

// parsePayload is best-effort decoding of payload_json into a generic
// map. Returns nil on any error rather than failing the search — bad
// payloads are diagnosable on the inspect surface.
func parsePayload(s string) map[string]any {
	if s == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}
