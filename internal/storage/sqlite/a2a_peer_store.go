package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// a2aPeerStore is the SQLite-backed ifaces.A2APeerStore. Every statement is
// parameterised and tenant-scoped (§6/§9). It mirrors the Phase 15.5
// governance customer store shape — flat tenant-scoped CRUD table, no
// allowlists, no join tables (Phase 17 may extend; out of scope here).
type a2aPeerStore struct {
	db *sql.DB
}

func (s *a2aPeerStore) PutPeer(ctx context.Context, p *ifaces.A2APeer) error {
	if p == nil {
		return errors.New("sqlite: nil a2a peer")
	}
	if p.TenantID == "" || p.ID == "" || p.Name == "" || p.Endpoint == "" {
		return errors.New("sqlite: a2a peer requires tenant_id, id, name, and endpoint")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var createdAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT created_at FROM a2a_peers WHERE tenant_id = ? AND id = ?
	`, p.TenantID, p.ID).Scan(&createdAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("sqlite: put a2a peer: check existing: %w", err)
	}
	if createdAt == "" {
		createdAt = now
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO a2a_peers(
			tenant_id, id, name, endpoint, egress_auth_ref, agent_card_json,
			enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			name            = excluded.name,
			endpoint        = excluded.endpoint,
			egress_auth_ref = excluded.egress_auth_ref,
			agent_card_json = excluded.agent_card_json,
			enabled         = excluded.enabled,
			updated_at      = excluded.updated_at
	`, p.TenantID, p.ID, p.Name, p.Endpoint, nullStr(p.EgressAuthRef),
		nullStr(p.AgentCardJSON), boolToInt(p.Enabled), createdAt, now); err != nil {
		return fmt.Errorf("sqlite: put a2a peer: %w", err)
	}
	return nil
}

func (s *a2aPeerStore) GetPeer(ctx context.Context, tenantID, id string) (*ifaces.A2APeer, error) {
	if tenantID == "" || id == "" {
		return nil, errors.New("sqlite: get a2a peer requires tenant_id and id")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, id, name, endpoint, egress_auth_ref, agent_card_json,
		       enabled, created_at, updated_at
		FROM a2a_peers WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	p, err := scanA2APeer(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrA2APeerNotFound
		}
		return nil, err
	}
	return p, nil
}

func (s *a2aPeerStore) ListPeers(ctx context.Context, tenantID string) ([]*ifaces.A2APeer, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: list a2a peers requires tenant_id")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, id, name, endpoint, egress_auth_ref, agent_card_json,
		       enabled, created_at, updated_at
		FROM a2a_peers WHERE tenant_id = ? ORDER BY name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list a2a peers: %w", err)
	}
	defer rows.Close()

	var out []*ifaces.A2APeer
	for rows.Next() {
		p, err := scanA2APeer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate a2a peers: %w", err)
	}
	return out, nil
}

func (s *a2aPeerStore) DeletePeer(ctx context.Context, tenantID, id string) error {
	if tenantID == "" || id == "" {
		return errors.New("sqlite: delete a2a peer requires tenant_id and id")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM a2a_peers WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete a2a peer: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ifaces.ErrA2APeerNotFound
	}
	return nil
}

func scanA2APeer(row interface{ Scan(...any) error }) (*ifaces.A2APeer, error) {
	var (
		p          ifaces.A2APeer
		egressRef  sql.NullString
		agentCard  sql.NullString
		enabledInt int
	)
	if err := row.Scan(
		&p.TenantID, &p.ID, &p.Name, &p.Endpoint, &egressRef, &agentCard,
		&enabledInt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("sqlite: scan a2a peer: %w", err)
	}
	p.EgressAuthRef = egressRef.String
	p.AgentCardJSON = agentCard.String
	p.Enabled = enabledInt != 0
	return &p, nil
}
