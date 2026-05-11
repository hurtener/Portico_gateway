// Package sqlite is the SQLite-backed implementation of the
// sessionbundle persistence interfaces (ImportedSink + reader).
//
// The imported_sessions table (migration 0013) holds one row per
// imported bundle: identifying fields + the canonical bundle bytes
// inline. Inline storage means the inspector can render an imported
// session without re-parsing the tar.gz on every page view.
//
// Phase 11.
package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/sessionbundle"
)

// Store wraps an *sql.DB with the typed methods the api package needs
// to surface imported bundles.
type Store struct {
	db *sql.DB
}

// New constructs a Store. db must already have migration 0013 applied.
func New(db *sql.DB) *Store { return &Store{db: db} }

// ImportedRow is a re-export of the parent package type so callers
// in this driver package don't have to qualify the import twice.
type ImportedRow = sessionbundle.ImportedRow

// RegisterImported satisfies sessionbundle.ImportedSink. It exports
// the bundle to canonical bytes and stores the result + metadata in
// one transaction.
//
// We accept the bundle pre-rewritten (synthetic session id, target
// tenant) so this method is purely a writer.
func (s *Store) RegisterImported(ctx context.Context, b *sessionbundle.Bundle) error {
	if s == nil || s.db == nil {
		return errors.New("sessionbundle/sqlite: nil store")
	}
	if b == nil {
		return errors.New("sessionbundle/sqlite: nil bundle")
	}

	// Re-export with payloads kept; the import path already verified
	// the original bytes, but here we want to store a canonical blob
	// keyed to the rewritten session id so the read path doesn't have
	// to reapply the rewrite on every load.
	var buf bytes.Buffer
	if err := sessionbundle.Export(ctx, b, &buf, sessionbundle.ExportOptions{}); err != nil {
		return fmt.Errorf("re-export: %w", err)
	}

	countsJSON, err := json.Marshal(b.Manifest.Counts)
	if err != nil {
		return fmt.Errorf("counts json: %w", err)
	}

	// Source identity — the importer captures these before rewriting
	// the manifest. Fall back to the rewritten ids if a caller didn't
	// populate them so the migration's NOT NULL constraints still
	// resolve.
	sourceTenantID := b.SourceTenantID
	if sourceTenantID == "" {
		sourceTenantID = b.Manifest.TenantID
	}
	sourceSessionID := b.SourceSessionID
	if sourceSessionID == "" {
		sourceSessionID = b.Session.ID
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO imported_sessions (
		    bundle_id, tenant_id, source_tenant_id,
		    session_id, source_session_id, imported_at,
		    range_from, range_to, counts_json, bundle_blob, checksum
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(bundle_id) DO UPDATE SET
		    tenant_id = excluded.tenant_id,
		    source_tenant_id = excluded.source_tenant_id,
		    session_id = excluded.session_id,
		    source_session_id = excluded.source_session_id,
		    imported_at = excluded.imported_at,
		    range_from = excluded.range_from,
		    range_to = excluded.range_to,
		    counts_json = excluded.counts_json,
		    bundle_blob = excluded.bundle_blob,
		    checksum = excluded.checksum
	`,
		b.Manifest.BundleID,
		b.Manifest.TenantID,
		sourceTenantID,
		b.Session.ID,
		sourceSessionID,
		time.Now().UTC().Format(time.RFC3339Nano),
		b.Manifest.Range.From.Format(time.RFC3339Nano),
		b.Manifest.Range.To.Format(time.RFC3339Nano),
		string(countsJSON),
		buf.Bytes(),
		b.Manifest.Checksum,
	)
	if err != nil {
		return fmt.Errorf("insert imported_sessions: %w", err)
	}
	return nil
}

// List returns every imported bundle for the tenant, newest first.
// Cap at 1000 rows; tenants with more than that are an operability
// problem and need a UI cleanup pass anyway.
func (s *Store) List(ctx context.Context, tenantID string) ([]ImportedRow, error) {
	if tenantID == "" {
		return nil, errors.New("sessionbundle/sqlite: tenant required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT bundle_id, tenant_id, source_tenant_id,
		       session_id, source_session_id, imported_at,
		       range_from, range_to, counts_json, checksum
		  FROM imported_sessions
		 WHERE tenant_id = ?
		 ORDER BY imported_at DESC
		 LIMIT 1000
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list imported: %w", err)
	}
	defer rows.Close()

	out := make([]ImportedRow, 0)
	for rows.Next() {
		var (
			r          ImportedRow
			imported   string
			rangeFrom  string
			rangeTo    string
			countsJSON string
		)
		if err := rows.Scan(
			&r.BundleID, &r.TenantID, &r.SourceTenantID,
			&r.SyntheticSession, &r.SourceSessionID, &imported,
			&rangeFrom, &rangeTo, &countsJSON, &r.Checksum,
		); err != nil {
			return nil, err
		}
		r.ImportedAt, _ = time.Parse(time.RFC3339Nano, imported)
		r.Range.From, _ = time.Parse(time.RFC3339Nano, rangeFrom)
		r.Range.To, _ = time.Parse(time.RFC3339Nano, rangeTo)
		_ = json.Unmarshal([]byte(countsJSON), &r.Counts)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LoadImported pulls the full Bundle for a synthetic session id. The
// caller is responsible for verifying the synthetic prefix; we treat
// `session_id` as the lookup key directly.
func (s *Store) LoadImported(ctx context.Context, tenantID, sessionID string) (*sessionbundle.Bundle, error) {
	if tenantID == "" || sessionID == "" {
		return nil, errors.New("sessionbundle/sqlite: tenant + session required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT bundle_blob FROM imported_sessions
		 WHERE tenant_id = ? AND session_id = ?
	`, tenantID, sessionID)
	var blob []byte
	if err := row.Scan(&blob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sessionbundle.ErrSessionNotFound
		}
		return nil, fmt.Errorf("load imported: %w", err)
	}
	b, err := sessionbundle.LoadFromReader(ctx, bytes.NewReader(blob))
	if err != nil {
		return nil, fmt.Errorf("decode imported bundle: %w", err)
	}
	return b, nil
}

// LoadImportedBytes returns the raw bundle bytes for a synthetic
// session id. The export endpoint uses this to stream the original
// archive back to the operator without re-canonicalising.
func (s *Store) LoadImportedBytes(ctx context.Context, tenantID, sessionID string) ([]byte, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT bundle_blob FROM imported_sessions
		 WHERE tenant_id = ? AND session_id = ?
	`, tenantID, sessionID)
	var blob []byte
	if err := row.Scan(&blob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sessionbundle.ErrSessionNotFound
		}
		return nil, fmt.Errorf("load imported bytes: %w", err)
	}
	return blob, nil
}
