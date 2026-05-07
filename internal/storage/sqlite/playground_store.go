package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// playgroundStore implements ifaces.PlaygroundStore over SQLite.
type playgroundStore struct {
	db *sql.DB
}

// nullString returns nil for empty strings so SQLite stores them as NULL.
// Local helper to keep the playground store self-contained.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Playground returns a PlaygroundStore backed by this DB (Phase 10).
func (d *DB) Playground() ifaces.PlaygroundStore {
	return &playgroundStore{db: d.sql}
}

// canonicalTagsJSON returns a stable canonical JSON for tags. Empty/nil -> "[]".
func canonicalTagsJSON(tags []string) (string, error) {
	if len(tags) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func parseTags(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *playgroundStore) UpsertCase(ctx context.Context, c *ifaces.PlaygroundCaseRecord) error {
	if c == nil {
		return errors.New("playground: nil case")
	}
	if c.TenantID == "" || c.CaseID == "" {
		return errors.New("playground: tenant_id and case_id required")
	}
	if c.Kind == "" || c.Target == "" {
		return errors.New("playground: kind and target required")
	}
	tags, err := canonicalTagsJSON(c.Tags)
	if err != nil {
		return fmt.Errorf("playground: marshal tags: %w", err)
	}
	payload := c.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	if !json.Valid(payload) {
		return errors.New("playground: payload is not valid JSON")
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO playground_cases (
			tenant_id, case_id, name, description, kind, target, payload, snapshot_id, tags, created_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tenant_id, case_id) DO UPDATE SET
			name=excluded.name,
			description=excluded.description,
			kind=excluded.kind,
			target=excluded.target,
			payload=excluded.payload,
			snapshot_id=excluded.snapshot_id,
			tags=excluded.tags,
			created_by=excluded.created_by`,
		c.TenantID, c.CaseID, c.Name, nullString(c.Description), c.Kind, c.Target,
		string(payload), nullString(c.SnapshotID), tags, c.CreatedAt.UTC().Format(time.RFC3339Nano),
		nullString(c.CreatedBy),
	)
	if err != nil {
		return fmt.Errorf("playground: upsert case: %w", err)
	}
	return nil
}

func (s *playgroundStore) GetCase(ctx context.Context, tenantID, caseID string) (*ifaces.PlaygroundCaseRecord, error) {
	if tenantID == "" || caseID == "" {
		return nil, ifaces.ErrNotFound
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, case_id, name, description, kind, target, payload, snapshot_id, tags, created_at, created_by
		 FROM playground_cases WHERE tenant_id = ? AND case_id = ?`,
		tenantID, caseID)
	var rec ifaces.PlaygroundCaseRecord
	var desc, snap, by, tags, payload, created sql.NullString
	if err := row.Scan(&rec.TenantID, &rec.CaseID, &rec.Name, &desc, &rec.Kind, &rec.Target,
		&payload, &snap, &tags, &created, &by); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrNotFound
		}
		return nil, err
	}
	rec.Description = desc.String
	rec.SnapshotID = snap.String
	rec.CreatedBy = by.String
	if payload.Valid && payload.String != "" {
		rec.Payload = json.RawMessage(payload.String)
	} else {
		rec.Payload = json.RawMessage("{}")
	}
	if tags.Valid && tags.String != "" {
		ts, err := parseTags(tags.String)
		if err != nil {
			return nil, fmt.Errorf("playground: parse tags: %w", err)
		}
		rec.Tags = ts
	}
	if created.Valid && created.String != "" {
		t, err := time.Parse(time.RFC3339Nano, created.String)
		if err == nil {
			rec.CreatedAt = t
		}
	}
	return &rec, nil
}

// ListCases threads filter + pagination + cursor branches. Pulling the
// WHERE-clause builder into a helper would just relocate the cyclomatic
// mass without making the SQL clearer to read.
//
//nolint:gocyclo // structural complexity from filter+pagination+cursor.
func (s *playgroundStore) ListCases(ctx context.Context, tenantID string, q ifaces.PlaygroundCasesQuery) ([]*ifaces.PlaygroundCaseRecord, string, error) {
	if tenantID == "" {
		return nil, "", errors.New("playground: tenant_id required")
	}
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{tenantID}
	where := "WHERE tenant_id = ?"
	if q.Kind != "" {
		where += " AND kind = ?"
		args = append(args, q.Kind)
	}
	if q.Cursor != "" {
		where += " AND case_id < ?"
		args = append(args, q.Cursor)
	}
	args = append(args, limit)
	//nolint:gosec // dynamic clause assembly with parameterised values
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, case_id, name, description, kind, target, payload, snapshot_id, tags, created_at, created_by
		 FROM playground_cases `+where+`
		 ORDER BY case_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]*ifaces.PlaygroundCaseRecord, 0, limit)
	var lastID string
	for rows.Next() {
		var rec ifaces.PlaygroundCaseRecord
		var desc, snap, by, tags, payload, created sql.NullString
		if err := rows.Scan(&rec.TenantID, &rec.CaseID, &rec.Name, &desc, &rec.Kind, &rec.Target,
			&payload, &snap, &tags, &created, &by); err != nil {
			return nil, "", err
		}
		rec.Description = desc.String
		rec.SnapshotID = snap.String
		rec.CreatedBy = by.String
		if payload.Valid && payload.String != "" {
			rec.Payload = json.RawMessage(payload.String)
		} else {
			rec.Payload = json.RawMessage("{}")
		}
		if tags.Valid && tags.String != "" {
			ts, _ := parseTags(tags.String)
			rec.Tags = ts
		}
		if created.Valid && created.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, created.String)
			rec.CreatedAt = t
		}
		// Tag filter applied in-Go to avoid JSON functions in SQLite.
		if q.Tag != "" {
			matched := false
			for _, tg := range rec.Tags {
				if tg == q.Tag {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, &rec)
		lastID = rec.CaseID
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) == limit {
		next = lastID
	}
	return out, next, nil
}

func (s *playgroundStore) DeleteCase(ctx context.Context, tenantID, caseID string) error {
	if tenantID == "" || caseID == "" {
		return ifaces.ErrNotFound
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM playground_cases WHERE tenant_id = ? AND case_id = ?`,
		tenantID, caseID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *playgroundStore) InsertRun(ctx context.Context, r *ifaces.PlaygroundRunRecord) error {
	if r == nil {
		return errors.New("playground: nil run")
	}
	if r.TenantID == "" || r.RunID == "" || r.SessionID == "" || r.SnapshotID == "" || r.Status == "" {
		return errors.New("playground: required run fields missing")
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = time.Now().UTC()
	}
	drift := 0
	if r.DriftDetected {
		drift = 1
	}
	var endedAt any
	if !r.EndedAt.IsZero() {
		endedAt = r.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO playground_runs (
			tenant_id, run_id, case_id, session_id, snapshot_id, started_at, ended_at, status, drift_detected, summary
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TenantID, r.RunID, nullString(r.CaseID), r.SessionID, r.SnapshotID,
		r.StartedAt.UTC().Format(time.RFC3339Nano), endedAt, r.Status, drift, nullString(r.Summary),
	)
	if err != nil {
		return fmt.Errorf("playground: insert run: %w", err)
	}
	return nil
}

func (s *playgroundStore) UpdateRun(ctx context.Context, r *ifaces.PlaygroundRunRecord) error {
	if r == nil || r.TenantID == "" || r.RunID == "" {
		return errors.New("playground: tenant_id and run_id required")
	}
	drift := 0
	if r.DriftDetected {
		drift = 1
	}
	var endedAt any
	if !r.EndedAt.IsZero() {
		endedAt = r.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE playground_runs
			SET ended_at = ?, status = ?, drift_detected = ?, summary = ?
		 WHERE tenant_id = ? AND run_id = ?`,
		endedAt, r.Status, drift, nullString(r.Summary), r.TenantID, r.RunID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *playgroundStore) GetRun(ctx context.Context, tenantID, runID string) (*ifaces.PlaygroundRunRecord, error) {
	if tenantID == "" || runID == "" {
		return nil, ifaces.ErrNotFound
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, run_id, case_id, session_id, snapshot_id, started_at, ended_at, status, drift_detected, summary
		 FROM playground_runs WHERE tenant_id = ? AND run_id = ?`,
		tenantID, runID)
	rec, err := scanPlaygroundRun(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrNotFound
		}
		return nil, err
	}
	return rec, nil
}

// scanFn unifies row.Scan / rows.Scan.
type scanFn func(dest ...any) error

func scanPlaygroundRun(scan scanFn) (*ifaces.PlaygroundRunRecord, error) {
	var rec ifaces.PlaygroundRunRecord
	var caseID, summary, ended, started sql.NullString
	var drift int
	if err := scan(&rec.TenantID, &rec.RunID, &caseID, &rec.SessionID, &rec.SnapshotID,
		&started, &ended, &rec.Status, &drift, &summary); err != nil {
		return nil, err
	}
	rec.CaseID = caseID.String
	rec.Summary = summary.String
	rec.DriftDetected = drift != 0
	if started.Valid && started.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, started.String); err == nil {
			rec.StartedAt = t
		}
	}
	if ended.Valid && ended.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, ended.String); err == nil {
			rec.EndedAt = t
		}
	}
	return &rec, nil
}

func (s *playgroundStore) ListRuns(ctx context.Context, tenantID string, q ifaces.PlaygroundRunsQuery) ([]*ifaces.PlaygroundRunRecord, string, error) {
	if tenantID == "" {
		return nil, "", errors.New("playground: tenant_id required")
	}
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{tenantID}
	where := "WHERE tenant_id = ?"
	if q.CaseID != "" {
		where += " AND case_id = ?"
		args = append(args, q.CaseID)
	}
	if q.Cursor != "" {
		where += " AND run_id < ?"
		args = append(args, q.Cursor)
	}
	args = append(args, limit)
	//nolint:gosec // dynamic clause assembly with parameterised values
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, run_id, case_id, session_id, snapshot_id, started_at, ended_at, status, drift_detected, summary
		 FROM playground_runs `+where+`
		 ORDER BY run_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]*ifaces.PlaygroundRunRecord, 0, limit)
	var lastID string
	for rows.Next() {
		rec, err := scanPlaygroundRun(rows.Scan)
		if err != nil {
			return nil, "", err
		}
		out = append(out, rec)
		lastID = rec.RunID
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) == limit {
		next = lastID
	}
	return out, next, nil
}
