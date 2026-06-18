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

// governanceStore is the SQLite-backed ifaces.GovernanceStore. Every statement
// is parameterised and tenant-scoped (§6/§9), EXCEPT LookupVirtualKeyByID which
// is the documented auth-boundary resolver path (a presented VK carries no
// tenant; the VK id is a globally-unique ULID).
type governanceStore struct {
	db *sql.DB
}

// --- Customers ---

func (s *governanceStore) PutCustomer(ctx context.Context, c *ifaces.Customer) error {
	if c == nil {
		return errors.New("sqlite: nil customer")
	}
	if c.TenantID == "" || c.ID == "" || c.Name == "" {
		return errors.New("sqlite: customer requires tenant_id, id, and name")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	createdAt, err := s.existingCreatedAt(ctx, "governance_customers", c.TenantID, c.ID, now)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO governance_customers(tenant_id, id, name, description, webhook_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			name        = excluded.name,
			description = excluded.description,
			webhook_url = excluded.webhook_url,
			updated_at  = excluded.updated_at
	`, c.TenantID, c.ID, c.Name, c.Description, c.WebhookURL, createdAt, now)
	if err != nil {
		return fmt.Errorf("sqlite: put customer: %w", err)
	}
	return nil
}

func (s *governanceStore) GetCustomer(ctx context.Context, tenantID, id string) (*ifaces.Customer, error) {
	if tenantID == "" || id == "" {
		return nil, errors.New("sqlite: get customer requires tenant_id and id")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, id, name, description, webhook_url, created_at, updated_at
		FROM governance_customers WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	c, err := scanCustomer(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrGovernanceNotFound
		}
		return nil, err
	}
	return c, nil
}

func (s *governanceStore) ListCustomers(ctx context.Context, tenantID string) ([]*ifaces.Customer, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: list customers requires tenant_id")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, id, name, description, webhook_url, created_at, updated_at
		FROM governance_customers WHERE tenant_id = ? ORDER BY name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list customers: %w", err)
	}
	defer rows.Close()

	var out []*ifaces.Customer
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate customers: %w", err)
	}
	return out, nil
}

func (s *governanceStore) DeleteCustomer(ctx context.Context, tenantID, id string) error {
	if tenantID == "" || id == "" {
		return errors.New("sqlite: delete customer requires tenant_id and id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: delete customer: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Orphan child teams first (the composite FK has no ON DELETE SET NULL —
	// see migration 0021). This keeps tenant_id intact while clearing the link.
	if _, err := tx.ExecContext(ctx, `
		UPDATE governance_teams SET customer_id = NULL WHERE tenant_id = ? AND customer_id = ?
	`, tenantID, id); err != nil {
		return fmt.Errorf("sqlite: delete customer: orphan teams: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		DELETE FROM governance_customers WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete customer: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ifaces.ErrGovernanceNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: delete customer: commit: %w", err)
	}
	return nil
}

func scanCustomer(row interface{ Scan(...any) error }) (*ifaces.Customer, error) {
	var (
		c       ifaces.Customer
		desc    sql.NullString
		webhook sql.NullString
	)
	if err := row.Scan(&c.TenantID, &c.ID, &c.Name, &desc, &webhook, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("sqlite: scan customer: %w", err)
	}
	c.Description = desc.String
	c.WebhookURL = webhook.String
	return &c, nil
}

// --- Teams ---

func (s *governanceStore) PutTeam(ctx context.Context, tm *ifaces.Team) error {
	if tm == nil {
		return errors.New("sqlite: nil team")
	}
	if tm.TenantID == "" || tm.ID == "" || tm.Name == "" {
		return errors.New("sqlite: team requires tenant_id, id, and name")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	createdAt, err := s.existingCreatedAt(ctx, "governance_teams", tm.TenantID, tm.ID, now)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO governance_teams(tenant_id, id, customer_id, name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			customer_id = excluded.customer_id,
			name        = excluded.name,
			description = excluded.description,
			updated_at  = excluded.updated_at
	`, tm.TenantID, tm.ID, nullStr(tm.CustomerID), tm.Name, tm.Description, createdAt, now)
	if err != nil {
		return fmt.Errorf("sqlite: put team: %w", err)
	}
	return nil
}

func (s *governanceStore) GetTeam(ctx context.Context, tenantID, id string) (*ifaces.Team, error) {
	if tenantID == "" || id == "" {
		return nil, errors.New("sqlite: get team requires tenant_id and id")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, id, customer_id, name, description, created_at, updated_at
		FROM governance_teams WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	tm, err := scanTeam(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrGovernanceNotFound
		}
		return nil, err
	}
	return tm, nil
}

func (s *governanceStore) ListTeams(ctx context.Context, tenantID string) ([]*ifaces.Team, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: list teams requires tenant_id")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, id, customer_id, name, description, created_at, updated_at
		FROM governance_teams WHERE tenant_id = ? ORDER BY name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list teams: %w", err)
	}
	defer rows.Close()

	var out []*ifaces.Team
	for rows.Next() {
		tm, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate teams: %w", err)
	}
	return out, nil
}

func (s *governanceStore) DeleteTeam(ctx context.Context, tenantID, id string) error {
	if tenantID == "" || id == "" {
		return errors.New("sqlite: delete team requires tenant_id and id")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM governance_teams WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete team: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ifaces.ErrGovernanceNotFound
	}
	return nil
}

func scanTeam(row interface{ Scan(...any) error }) (*ifaces.Team, error) {
	var (
		tm   ifaces.Team
		cust sql.NullString
		desc sql.NullString
	)
	if err := row.Scan(&tm.TenantID, &tm.ID, &cust, &tm.Name, &desc, &tm.CreatedAt, &tm.UpdatedAt); err != nil {
		return nil, fmt.Errorf("sqlite: scan team: %w", err)
	}
	tm.CustomerID = cust.String
	tm.Description = desc.String
	return &tm, nil
}

// --- Virtual keys ---

func (s *governanceStore) PutVirtualKey(ctx context.Context, vk *ifaces.VirtualKey) error {
	if vk == nil {
		return errors.New("sqlite: nil virtual key")
	}
	if vk.TenantID == "" || vk.ID == "" || vk.Name == "" {
		return errors.New("sqlite: virtual key requires tenant_id, id, and name")
	}
	if len(vk.Salt) == 0 || len(vk.HMAC) == 0 {
		return errors.New("sqlite: virtual key requires salt and hmac (secret never stored)")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: put virtual key: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)
	var createdAt string
	err = tx.QueryRowContext(ctx, `
		SELECT created_at FROM governance_virtual_keys WHERE tenant_id = ? AND id = ?
	`, vk.TenantID, vk.ID).Scan(&createdAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("sqlite: put virtual key: check existing: %w", err)
	}
	if createdAt == "" {
		createdAt = now
	}

	parentKind := vk.ParentKind
	if parentKind == "" {
		parentKind = "none"
	}
	scopesJSON, err := marshalStrings(vk.Scopes)
	if err != nil {
		return fmt.Errorf("sqlite: put virtual key: marshal scopes: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO governance_virtual_keys(
			tenant_id, id, name, salt, hmac, parent_kind, parent_id, profile_id,
			scopes, enabled, created_at, rotated_at, revoked_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			name        = excluded.name,
			salt        = excluded.salt,
			hmac        = excluded.hmac,
			parent_kind = excluded.parent_kind,
			parent_id   = excluded.parent_id,
			profile_id  = excluded.profile_id,
			scopes      = excluded.scopes,
			enabled     = excluded.enabled,
			rotated_at  = excluded.rotated_at,
			revoked_at  = excluded.revoked_at
	`, vk.TenantID, vk.ID, vk.Name, vk.Salt, vk.HMAC, parentKind, nullStr(vk.ParentID),
		nullStr(vk.ProfileID), scopesJSON, boolToInt(vk.Enabled), createdAt,
		nullStr(vk.RotatedAt), nullStr(vk.RevokedAt))
	if err != nil {
		return fmt.Errorf("sqlite: put virtual key: upsert: %w", err)
	}

	if err := replaceVKAllowlist(ctx, tx, vk.TenantID, vk.ID, "vk_provider_allowlist", "provider_driver", vk.ProviderAllowlist); err != nil {
		return err
	}
	if err := replaceVKAllowlist(ctx, tx, vk.TenantID, vk.ID, "vk_model_allowlist", "alias", vk.ModelAllowlist); err != nil {
		return err
	}
	if err := replaceVKAllowlist(ctx, tx, vk.TenantID, vk.ID, "vk_mcp_server_allowlist", "server_id", vk.MCPServerAllowlist); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: put virtual key: commit: %w", err)
	}
	return nil
}

func replaceVKAllowlist(ctx context.Context, tx *sql.Tx, tenantID, vkID, table, column string, items []string) error {
	// nolint:gosec // table/column are internal constants, not user input
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE tenant_id = ? AND vk_id = ?`, tenantID, vkID); err != nil {
		return fmt.Errorf("sqlite: replace vk allowlist %s: delete: %w", table, err)
	}
	for _, item := range items {
		if item == "" {
			continue
		}
		// nolint:gosec // table/column are internal constants, not user input
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+table+`(tenant_id, vk_id, `+column+`) VALUES (?, ?, ?)`, tenantID, vkID, item); err != nil {
			return fmt.Errorf("sqlite: replace vk allowlist %s: insert: %w", table, err)
		}
	}
	return nil
}

func (s *governanceStore) GetVirtualKey(ctx context.Context, tenantID, id string) (*ifaces.VirtualKey, error) {
	if tenantID == "" || id == "" {
		return nil, errors.New("sqlite: get virtual key requires tenant_id and id")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, id, name, salt, hmac, parent_kind, parent_id, profile_id,
		       scopes, enabled, created_at, rotated_at, revoked_at
		FROM governance_virtual_keys WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	vk, err := scanVirtualKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrGovernanceNotFound
		}
		return nil, err
	}
	if err := s.loadVKAllowlists(ctx, vk); err != nil {
		return nil, err
	}
	return vk, nil
}

func (s *governanceStore) LookupVirtualKeyByID(ctx context.Context, id string) (*ifaces.VirtualKey, error) {
	if id == "" {
		return nil, ifaces.ErrGovernanceNotFound
	}
	// Auth-boundary path: a presented VK carries no tenant; the id is a
	// globally-unique ULID (indexed). All DATA operations remain tenant-scoped.
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, id, name, salt, hmac, parent_kind, parent_id, profile_id,
		       scopes, enabled, created_at, rotated_at, revoked_at
		FROM governance_virtual_keys WHERE id = ?
	`, id)
	vk, err := scanVirtualKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrGovernanceNotFound
		}
		return nil, err
	}
	if err := s.loadVKAllowlists(ctx, vk); err != nil {
		return nil, err
	}
	return vk, nil
}

func (s *governanceStore) ListVirtualKeys(ctx context.Context, tenantID string) ([]*ifaces.VirtualKey, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: list virtual keys requires tenant_id")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, id, name, salt, hmac, parent_kind, parent_id, profile_id,
		       scopes, enabled, created_at, rotated_at, revoked_at
		FROM governance_virtual_keys WHERE tenant_id = ? ORDER BY name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list virtual keys: %w", err)
	}
	defer rows.Close()

	var out []*ifaces.VirtualKey
	for rows.Next() {
		vk, err := scanVirtualKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, vk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate virtual keys: %w", err)
	}
	for _, vk := range out {
		if err := s.loadVKAllowlists(ctx, vk); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *governanceStore) DeleteVirtualKey(ctx context.Context, tenantID, id string) error {
	if tenantID == "" || id == "" {
		return errors.New("sqlite: delete virtual key requires tenant_id and id")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM governance_virtual_keys WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete virtual key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ifaces.ErrGovernanceNotFound
	}
	return nil
}

func scanVirtualKey(row interface{ Scan(...any) error }) (*ifaces.VirtualKey, error) {
	var (
		vk         ifaces.VirtualKey
		parentID   sql.NullString
		profileID  sql.NullString
		scopesJSON string
		rotatedAt  sql.NullString
		revokedAt  sql.NullString
	)
	if err := row.Scan(
		&vk.TenantID, &vk.ID, &vk.Name, &vk.Salt, &vk.HMAC, &vk.ParentKind, &parentID,
		&profileID, &scopesJSON, &vk.Enabled, &vk.CreatedAt, &rotatedAt, &revokedAt,
	); err != nil {
		return nil, fmt.Errorf("sqlite: scan virtual key: %w", err)
	}
	vk.ParentID = parentID.String
	vk.ProfileID = profileID.String
	vk.RotatedAt = rotatedAt.String
	vk.RevokedAt = revokedAt.String
	if scopesJSON != "" && scopesJSON != "[]" {
		if err := json.Unmarshal([]byte(scopesJSON), &vk.Scopes); err != nil {
			return nil, fmt.Errorf("sqlite: unmarshal vk scopes: %w", err)
		}
	}
	if vk.Scopes == nil {
		vk.Scopes = []string{}
	}
	vk.ProviderAllowlist = []string{}
	vk.ModelAllowlist = []string{}
	vk.MCPServerAllowlist = []string{}
	return &vk, nil
}

func (s *governanceStore) loadVKAllowlists(ctx context.Context, vk *ifaces.VirtualKey) error {
	var err error
	vk.ProviderAllowlist, err = s.loadVKStringSlice(ctx, "vk_provider_allowlist", "provider_driver", vk.TenantID, vk.ID)
	if err != nil {
		return err
	}
	vk.ModelAllowlist, err = s.loadVKStringSlice(ctx, "vk_model_allowlist", "alias", vk.TenantID, vk.ID)
	if err != nil {
		return err
	}
	vk.MCPServerAllowlist, err = s.loadVKStringSlice(ctx, "vk_mcp_server_allowlist", "server_id", vk.TenantID, vk.ID)
	if err != nil {
		return err
	}
	return nil
}

func (s *governanceStore) loadVKStringSlice(ctx context.Context, table, column, tenantID, vkID string) ([]string, error) {
	// nolint:gosec // table/column are internal constants, not user input
	rows, err := s.db.QueryContext(ctx, `SELECT `+column+` FROM `+table+` WHERE tenant_id = ? AND vk_id = ? ORDER BY `+column, tenantID, vkID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: load vk allowlist %s: %w", table, err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("sqlite: scan vk allowlist %s: %w", table, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate vk allowlist %s: %w", table, err)
	}
	return out, nil
}

// --- shared helpers ---

// existingCreatedAt returns the row's existing created_at (so updates preserve
// it) or the provided default when the row is new.
// nolint:gosec // table is an internal constant, not user input
func (s *governanceStore) existingCreatedAt(ctx context.Context, table, tenantID, id, def string) (string, error) {
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT created_at FROM `+table+` WHERE tenant_id = ? AND id = ?`, tenantID, id).Scan(&createdAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("sqlite: check existing %s: %w", table, err)
	}
	if createdAt == "" {
		return def, nil
	}
	return createdAt, nil
}

func marshalStrings(ss []string) (string, error) {
	if len(ss) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// nullStr maps "" to a NULL column value so optional FK/text columns store NULL
// rather than the empty string (keeps FK ON DELETE SET NULL semantics correct).
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
