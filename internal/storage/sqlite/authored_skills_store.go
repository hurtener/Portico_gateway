// SQLite implementation of the Phase 8 authored-skill repository.
// Tenant-scoped per CLAUDE.md §6. Multi-statement work runs inside a
// single BEGIN IMMEDIATE transaction to serialize concurrent publishes
// (the active-version pointer must never see a torn state).

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type authoredSkillStore struct {
	db *sql.DB
}

func (s *authoredSkillStore) CreateDraft(ctx context.Context, rec *ifaces.AuthoredSkillRecord, files []ifaces.AuthoredFileRecord) error {
	if rec == nil {
		return errors.New("sqlite: nil authored skill record")
	}
	if rec.TenantID == "" || rec.SkillID == "" || rec.Version == "" {
		return errors.New("sqlite: tenant_id, skill_id, version required")
	}
	if rec.Status == "" {
		rec.Status = "draft"
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin authored.create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tenant_authored_skills(
			tenant_id, skill_id, version, status, manifest_json, checksum,
			author_user_id, created_at, published_at, archived_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)
	`,
		rec.TenantID, rec.SkillID, rec.Version, rec.Status,
		string(rec.ManifestJSON), rec.Checksum,
		nullableString(rec.AuthorUserID), rec.CreatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("sqlite: insert tenant_authored_skills: %w", err)
	}

	if err := insertAuthoredFilesTx(ctx, tx, rec.TenantID, rec.SkillID, rec.Version, files); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *authoredSkillStore) UpdateDraft(ctx context.Context, rec *ifaces.AuthoredSkillRecord, files []ifaces.AuthoredFileRecord) error {
	if rec == nil {
		return errors.New("sqlite: nil authored skill record")
	}
	if rec.TenantID == "" || rec.SkillID == "" || rec.Version == "" {
		return errors.New("sqlite: tenant_id, skill_id, version required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin authored.update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Refuse to overwrite a published row.
	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM tenant_authored_skills WHERE tenant_id = ? AND skill_id = ? AND version = ?`,
		rec.TenantID, rec.SkillID, rec.Version).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ifaces.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("sqlite: select status: %w", err)
	}
	if status != "draft" {
		return fmt.Errorf("sqlite: refusing to update %s version %q", status, rec.Version)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tenant_authored_skills
		SET manifest_json = ?, checksum = ?, author_user_id = ?
		WHERE tenant_id = ? AND skill_id = ? AND version = ?
	`,
		string(rec.ManifestJSON), rec.Checksum, nullableString(rec.AuthorUserID),
		rec.TenantID, rec.SkillID, rec.Version,
	); err != nil {
		return fmt.Errorf("sqlite: update tenant_authored_skills: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM tenant_authored_skill_files WHERE tenant_id = ? AND skill_id = ? AND version = ?`,
		rec.TenantID, rec.SkillID, rec.Version,
	); err != nil {
		return fmt.Errorf("sqlite: delete authored files: %w", err)
	}

	if err := insertAuthoredFilesTx(ctx, tx, rec.TenantID, rec.SkillID, rec.Version, files); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *authoredSkillStore) Publish(ctx context.Context, tenantID, skillID, version string, when time.Time) (*ifaces.AuthoredSkillRecord, error) {
	if tenantID == "" || skillID == "" || version == "" {
		return nil, errors.New("sqlite: tenant_id, skill_id, version required")
	}
	if when.IsZero() {
		when = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite: begin publish: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM tenant_authored_skills WHERE tenant_id = ? AND skill_id = ? AND version = ?`,
		tenantID, skillID, version).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: select status: %w", err)
	}
	if status == "published" {
		return s.getInTx(ctx, tx, tenantID, skillID, version)
	}
	if status != "draft" {
		return nil, fmt.Errorf("sqlite: refusing to publish %s version", status)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tenant_authored_skills SET status = 'published', published_at = ?
		WHERE tenant_id = ? AND skill_id = ? AND version = ?
	`, when.Format(time.RFC3339Nano), tenantID, skillID, version); err != nil {
		return nil, fmt.Errorf("sqlite: publish update: %w", err)
	}

	// Flip the active pointer.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tenant_authored_active_skill(tenant_id, skill_id, active_version)
		VALUES (?, ?, ?)
		ON CONFLICT(tenant_id, skill_id) DO UPDATE SET active_version = excluded.active_version
	`, tenantID, skillID, version); err != nil {
		return nil, fmt.Errorf("sqlite: active pointer: %w", err)
	}

	rec, err := s.getInTx(ctx, tx, tenantID, skillID, version)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sqlite: commit publish: %w", err)
	}
	return rec, nil
}

func (s *authoredSkillStore) Archive(ctx context.Context, tenantID, skillID, version string, when time.Time) error {
	if tenantID == "" || skillID == "" || version == "" {
		return errors.New("sqlite: tenant_id, skill_id, version required")
	}
	if when.IsZero() {
		when = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin archive: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE tenant_authored_skills SET status = 'archived', archived_at = ?
		WHERE tenant_id = ? AND skill_id = ? AND version = ?
		  AND status IN ('draft','published')
	`, when.Format(time.RFC3339Nano), tenantID, skillID, version)
	if err != nil {
		return fmt.Errorf("sqlite: archive update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM tenant_authored_active_skill WHERE tenant_id = ? AND skill_id = ? AND active_version = ?`,
		tenantID, skillID, version); err != nil {
		return fmt.Errorf("sqlite: clear active pointer: %w", err)
	}
	return tx.Commit()
}

func (s *authoredSkillStore) DeleteDraft(ctx context.Context, tenantID, skillID, version string) error {
	if tenantID == "" || skillID == "" || version == "" {
		return errors.New("sqlite: tenant_id, skill_id, version required")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM tenant_authored_skills
		WHERE tenant_id = ? AND skill_id = ? AND version = ? AND status = 'draft'
	`, tenantID, skillID, version)
	if err != nil {
		return fmt.Errorf("sqlite: delete draft: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Either missing or already published — surface NotFound.
		return ifaces.ErrNotFound
	}
	// FK ON DELETE CASCADE clears the file rows.
	return nil
}

func (s *authoredSkillStore) Get(ctx context.Context, tenantID, skillID, version string) (*ifaces.AuthoredSkillRecord, []ifaces.AuthoredFileRecord, error) {
	rec, err := s.getOne(ctx, tenantID, skillID, version)
	if err != nil {
		return nil, nil, err
	}
	files, err := s.listFiles(ctx, tenantID, skillID, version)
	if err != nil {
		return nil, nil, err
	}
	return rec, files, nil
}

func (s *authoredSkillStore) History(ctx context.Context, tenantID, skillID string) ([]*ifaces.AuthoredSkillRecord, error) {
	if tenantID == "" || skillID == "" {
		return nil, errors.New("sqlite: tenant_id and skill_id required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, skill_id, version, status, manifest_json, checksum,
		       COALESCE(author_user_id,''), created_at,
		       COALESCE(published_at,''), COALESCE(archived_at,'')
		FROM tenant_authored_skills
		WHERE tenant_id = ? AND skill_id = ?
		ORDER BY created_at DESC
	`, tenantID, skillID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: history: %w", err)
	}
	defer rows.Close()
	var out []*ifaces.AuthoredSkillRecord
	for rows.Next() {
		r, err := scanAuthored(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *authoredSkillStore) ListAuthored(ctx context.Context, tenantID string) ([]*ifaces.AuthoredSkillRecord, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: tenant_id required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, skill_id, version, status, manifest_json, checksum,
		       COALESCE(author_user_id,''), created_at,
		       COALESCE(published_at,''), COALESCE(archived_at,'')
		FROM tenant_authored_skills
		WHERE tenant_id = ?
		ORDER BY skill_id ASC, created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list authored: %w", err)
	}
	defer rows.Close()
	var out []*ifaces.AuthoredSkillRecord
	for rows.Next() {
		r, err := scanAuthored(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *authoredSkillStore) ActiveVersion(ctx context.Context, tenantID, skillID string) (string, error) {
	if tenantID == "" || skillID == "" {
		return "", errors.New("sqlite: tenant_id and skill_id required")
	}
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT active_version FROM tenant_authored_active_skill WHERE tenant_id = ? AND skill_id = ?`,
		tenantID, skillID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ifaces.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("sqlite: active version: %w", err)
	}
	return v, nil
}

func (s *authoredSkillStore) ListPublished(ctx context.Context, tenantID string) ([]ifaces.AuthoredSkillRecord, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: tenant_id required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.tenant_id, a.skill_id, a.version, a.status, a.manifest_json, a.checksum,
		       COALESCE(a.author_user_id,''), a.created_at,
		       COALESCE(a.published_at,''), COALESCE(a.archived_at,'')
		FROM tenant_authored_skills a
		JOIN tenant_authored_active_skill p
		  ON p.tenant_id = a.tenant_id AND p.skill_id = a.skill_id AND p.active_version = a.version
		WHERE a.tenant_id = ? AND a.status = 'published'
		ORDER BY a.skill_id ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list published: %w", err)
	}
	defer rows.Close()
	var out []ifaces.AuthoredSkillRecord
	for rows.Next() {
		r, err := scanAuthored(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// --- helpers --------------------------------------------------------

func (s *authoredSkillStore) getOne(ctx context.Context, tenantID, skillID, version string) (*ifaces.AuthoredSkillRecord, error) {
	if tenantID == "" || skillID == "" || version == "" {
		return nil, errors.New("sqlite: tenant_id, skill_id, version required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, skill_id, version, status, manifest_json, checksum,
		       COALESCE(author_user_id,''), created_at,
		       COALESCE(published_at,''), COALESCE(archived_at,'')
		FROM tenant_authored_skills WHERE tenant_id = ? AND skill_id = ? AND version = ?
	`, tenantID, skillID, version)
	r, err := scanAuthored(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrNotFound
	}
	return r, err
}

func (s *authoredSkillStore) getInTx(ctx context.Context, tx *sql.Tx, tenantID, skillID, version string) (*ifaces.AuthoredSkillRecord, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT tenant_id, skill_id, version, status, manifest_json, checksum,
		       COALESCE(author_user_id,''), created_at,
		       COALESCE(published_at,''), COALESCE(archived_at,'')
		FROM tenant_authored_skills WHERE tenant_id = ? AND skill_id = ? AND version = ?
	`, tenantID, skillID, version)
	r, err := scanAuthored(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrNotFound
	}
	return r, err
}

func (s *authoredSkillStore) listFiles(ctx context.Context, tenantID, skillID, version string) ([]ifaces.AuthoredFileRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT relpath, mime_type, contents
		FROM tenant_authored_skill_files
		WHERE tenant_id = ? AND skill_id = ? AND version = ?
		ORDER BY relpath ASC
	`, tenantID, skillID, version)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list authored files: %w", err)
	}
	defer rows.Close()
	out := make([]ifaces.AuthoredFileRecord, 0)
	for rows.Next() {
		var f ifaces.AuthoredFileRecord
		if err := rows.Scan(&f.RelPath, &f.MIMEType, &f.Contents); err != nil {
			return nil, fmt.Errorf("sqlite: scan authored file: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func insertAuthoredFilesTx(ctx context.Context, tx *sql.Tx, tenantID, skillID, version string, files []ifaces.AuthoredFileRecord) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO tenant_authored_skill_files(tenant_id, skill_id, version, relpath, mime_type, contents)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("sqlite: prepare insert authored file: %w", err)
	}
	defer stmt.Close()
	for _, f := range files {
		if f.RelPath == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, tenantID, skillID, version, f.RelPath, f.MIMEType, f.Contents); err != nil {
			return fmt.Errorf("sqlite: insert authored file %q: %w", f.RelPath, err)
		}
	}
	return nil
}

func scanAuthored(r scanner) (*ifaces.AuthoredSkillRecord, error) {
	var rec ifaces.AuthoredSkillRecord
	var manifest, createdAt, publishedAt, archivedAt string
	if err := r.Scan(
		&rec.TenantID, &rec.SkillID, &rec.Version, &rec.Status,
		&manifest, &rec.Checksum, &rec.AuthorUserID, &createdAt,
		&publishedAt, &archivedAt,
	); err != nil {
		return nil, err
	}
	rec.ManifestJSON = []byte(manifest)
	rec.CreatedAt, _ = parseSQLiteTime(createdAt)
	if publishedAt != "" {
		t, _ := parseSQLiteTime(publishedAt)
		if !t.IsZero() {
			rec.PublishedAt = &t
		}
	}
	if archivedAt != "" {
		t, _ := parseSQLiteTime(archivedAt)
		if !t.IsZero() {
			rec.ArchivedAt = &t
		}
	}
	return &rec, nil
}
