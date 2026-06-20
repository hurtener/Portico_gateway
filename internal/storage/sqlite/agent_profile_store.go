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

// agentProfileStore is the SQLite-backed ifaces.AgentProfileStore. Every
// statement is parameterised and tenant-scoped (§6/§9).
type agentProfileStore struct {
	db *sql.DB
}

func (s *agentProfileStore) List(ctx context.Context, tenantID string) ([]*ifaces.AgentProfile, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: list agent profiles requires tenant_id")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, id, name, description, scopes, policy_bundle_ref,
		       parent_profile_id, enabled, created_at, updated_at
		FROM agent_profiles
		WHERE tenant_id = ?
		ORDER BY name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list agent profiles: %w", err)
	}
	defer rows.Close()

	var profiles []*ifaces.AgentProfile
	for rows.Next() {
		p, err := s.scanProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate agent profiles: %w", err)
	}

	// Load allowlists for each profile in a second pass to avoid nested queries
	for _, p := range profiles {
		if err := s.loadAllowlists(ctx, p); err != nil {
			return nil, err
		}
	}
	return profiles, nil
}

func (s *agentProfileStore) Get(ctx context.Context, tenantID, id string) (*ifaces.AgentProfile, error) {
	if tenantID == "" || id == "" {
		return nil, errors.New("sqlite: get agent profile requires tenant_id and id")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, id, name, description, scopes, policy_bundle_ref,
		       parent_profile_id, enabled, created_at, updated_at
		FROM agent_profiles
		WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	p, err := s.scanProfile(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrAgentProfileNotFound
		}
		return nil, err
	}
	if err := s.loadAllowlists(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *agentProfileStore) Put(ctx context.Context, p *ifaces.AgentProfile) error {
	if p == nil {
		return errors.New("sqlite: nil profile")
	}
	if p.TenantID == "" || p.ID == "" || p.Name == "" {
		return errors.New("sqlite: profile requires tenant_id, id, and name")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: put agent profile: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// Marshal scopes to JSON
	scopesJSON := "[]"
	if len(p.Scopes) > 0 {
		b, err := json.Marshal(p.Scopes)
		if err != nil {
			return fmt.Errorf("sqlite: put agent profile: marshal scopes: %w", err)
		}
		scopesJSON = string(b)
	}

	// Check if profile exists to determine if we need to set created_at
	var existingCreatedAt string
	err = tx.QueryRowContext(ctx, `
		SELECT created_at FROM agent_profiles WHERE tenant_id = ? AND id = ?
	`, p.TenantID, p.ID).Scan(&existingCreatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("sqlite: put agent profile: check existing: %w", err)
	}

	createdAt := now
	if existingCreatedAt != "" {
		createdAt = existingCreatedAt
	}

	// Upsert the profile row
	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_profiles(
			tenant_id, id, name, description, scopes, policy_bundle_ref,
			parent_profile_id, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			name               = excluded.name,
			description        = excluded.description,
			scopes             = excluded.scopes,
			policy_bundle_ref  = excluded.policy_bundle_ref,
			parent_profile_id  = excluded.parent_profile_id,
			enabled            = excluded.enabled,
			updated_at         = excluded.updated_at
	`, p.TenantID, p.ID, p.Name, p.Description, scopesJSON,
		p.PolicyBundleRef, p.ParentProfileID, boolToInt(p.Enabled), createdAt, now)
	if err != nil {
		return fmt.Errorf("sqlite: put agent profile: upsert: %w", err)
	}

	// Replace allowlists: delete old, insert new. The six allowlist slices are
	// mirrored onto six (table,column) pairs; fold them into a tiny loop so the
	// Put body stays under the gocyclo ceiling.
	allowlists := []struct {
		table  string
		column string
		items  []string
	}{
		{"agent_profile_mcp_servers", "server_name", p.AllowedMCPServers},
		{"agent_profile_tools", "namespaced_id", p.AllowedTools},
		{"agent_profile_skills", "skill_id", p.AllowedSkills},
		{"agent_profile_models", "alias", p.AllowedModelAliases},
		{"agent_profile_a2a_peers", "peer_name", p.AllowedA2APeers},
		{"agent_profile_a2a_tasks", "namespaced_id", p.AllowedA2ATasks},
	}
	for _, al := range allowlists {
		if err := s.replaceAllowlist(ctx, tx, p.TenantID, p.ID, al.table, al.column, al.items); err != nil {
			return err
		}
	}
	if err := s.replaceBridges(ctx, tx, p); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: put agent profile: commit: %w", err)
	}
	return nil
}

func (s *agentProfileStore) replaceAllowlist(ctx context.Context, tx *sql.Tx, tenantID, profileID, table, column string, items []string) error {
	// Delete existing rows
	// nolint:gosec // table/column are internal constants, not user input
	_, err := tx.ExecContext(ctx, `
		DELETE FROM `+table+` WHERE tenant_id = ? AND profile_id = ?
	`, tenantID, profileID)
	if err != nil {
		return fmt.Errorf("sqlite: replace allowlist %s: delete: %w", table, err)
	}
	// Insert new rows
	for _, item := range items {
		if item == "" {
			continue
		}
		// nolint:gosec // table/column are internal constants, not user input
		_, err := tx.ExecContext(ctx, `
			INSERT INTO `+table+`(tenant_id, profile_id, `+column+`) VALUES (?, ?, ?)
		`, tenantID, profileID, item)
		if err != nil {
			return fmt.Errorf("sqlite: replace allowlist %s: insert: %w", table, err)
		}
	}
	return nil
}

// replaceBridges deletes and re-inserts the profile's MCP<->A2A bridge rows
// inside the Put transaction (Phase 16). Empty-field rows are skipped.
func (s *agentProfileStore) replaceBridges(ctx context.Context, tx *sql.Tx, p *ifaces.AgentProfile) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM agent_profile_mcp_to_a2a_bridges WHERE tenant_id = ? AND profile_id = ?`,
		p.TenantID, p.ID); err != nil {
		return fmt.Errorf("sqlite: replace mcp_to_a2a bridges: delete: %w", err)
	}
	for _, b := range p.MCPToA2ABridges {
		if b.MCPTool == "" || b.A2APeer == "" || b.A2ATask == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO agent_profile_mcp_to_a2a_bridges(tenant_id, profile_id, mcp_tool, a2a_peer, a2a_task) VALUES (?, ?, ?, ?, ?)`,
			p.TenantID, p.ID, b.MCPTool, b.A2APeer, b.A2ATask); err != nil {
			return fmt.Errorf("sqlite: replace mcp_to_a2a bridges: insert: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM agent_profile_a2a_to_mcp_bridges WHERE tenant_id = ? AND profile_id = ?`,
		p.TenantID, p.ID); err != nil {
		return fmt.Errorf("sqlite: replace a2a_to_mcp bridges: delete: %w", err)
	}
	for _, b := range p.A2AToMCPBridges {
		if b.A2ATask == "" || b.MCPTool == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO agent_profile_a2a_to_mcp_bridges(tenant_id, profile_id, a2a_task, mcp_tool) VALUES (?, ?, ?, ?)`,
			p.TenantID, p.ID, b.A2ATask, b.MCPTool); err != nil {
			return fmt.Errorf("sqlite: replace a2a_to_mcp bridges: insert: %w", err)
		}
	}
	return nil
}

func (s *agentProfileStore) loadMCPToA2ABridges(ctx context.Context, tenantID, profileID string) ([]ifaces.MCPToA2ABridge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT mcp_tool, a2a_peer, a2a_task FROM agent_profile_mcp_to_a2a_bridges WHERE tenant_id = ? AND profile_id = ? ORDER BY mcp_tool`,
		tenantID, profileID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: load mcp_to_a2a bridges: %w", err)
	}
	defer rows.Close()
	var out []ifaces.MCPToA2ABridge
	for rows.Next() {
		var b ifaces.MCPToA2ABridge
		if err := rows.Scan(&b.MCPTool, &b.A2APeer, &b.A2ATask); err != nil {
			return nil, fmt.Errorf("sqlite: scan mcp_to_a2a bridge: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *agentProfileStore) loadA2AToMCPBridges(ctx context.Context, tenantID, profileID string) ([]ifaces.A2AToMCPBridge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a2a_task, mcp_tool FROM agent_profile_a2a_to_mcp_bridges WHERE tenant_id = ? AND profile_id = ? ORDER BY a2a_task`,
		tenantID, profileID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: load a2a_to_mcp bridges: %w", err)
	}
	defer rows.Close()
	var out []ifaces.A2AToMCPBridge
	for rows.Next() {
		var b ifaces.A2AToMCPBridge
		if err := rows.Scan(&b.A2ATask, &b.MCPTool); err != nil {
			return nil, fmt.Errorf("sqlite: scan a2a_to_mcp bridge: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *agentProfileStore) Delete(ctx context.Context, tenantID, id string) error {
	if tenantID == "" || id == "" {
		return errors.New("sqlite: delete agent profile requires tenant_id and id")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM agent_profiles WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete agent profile: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrAgentProfileNotFound
	}
	return nil
}

func (s *agentProfileStore) PutJWTBinding(ctx context.Context, tenantID, jwtSub, profileID string) error {
	if tenantID == "" || jwtSub == "" || profileID == "" {
		return errors.New("sqlite: put jwt binding requires tenant_id, jwt_sub, and profile_id")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_profile_jwt_bindings(tenant_id, jwt_sub, profile_id)
		VALUES (?, ?, ?)
		ON CONFLICT(tenant_id, jwt_sub) DO UPDATE SET profile_id = excluded.profile_id
	`, tenantID, jwtSub, profileID)
	if err != nil {
		return fmt.Errorf("sqlite: put jwt binding: %w", err)
	}
	return nil
}

func (s *agentProfileStore) DeleteJWTBinding(ctx context.Context, tenantID, jwtSub string) error {
	if tenantID == "" || jwtSub == "" {
		return errors.New("sqlite: delete jwt binding requires tenant_id and jwt_sub")
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM agent_profile_jwt_bindings WHERE tenant_id = ? AND jwt_sub = ?
	`, tenantID, jwtSub)
	if err != nil {
		return fmt.Errorf("sqlite: delete jwt binding: %w", err)
	}
	return nil
}

func (s *agentProfileStore) ResolveJWTBinding(ctx context.Context, tenantID, jwtSub string) (*ifaces.AgentProfile, error) {
	if tenantID == "" || jwtSub == "" {
		return nil, ifaces.ErrAgentProfileNotFound
	}
	var profileID string
	err := s.db.QueryRowContext(ctx, `
		SELECT profile_id FROM agent_profile_jwt_bindings WHERE tenant_id = ? AND jwt_sub = ?
	`, tenantID, jwtSub).Scan(&profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrAgentProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: resolve jwt binding: %w", err)
	}
	return s.Get(ctx, tenantID, profileID)
}

func (s *agentProfileStore) scanProfile(row interface{ Scan(...any) error }) (*ifaces.AgentProfile, error) {
	var (
		p            ifaces.AgentProfile
		scopesJSON   string
		parentID     sql.NullString
		policyBundle sql.NullString
		desc         sql.NullString
	)
	if err := row.Scan(
		&p.TenantID, &p.ID, &p.Name, &desc, &scopesJSON, &policyBundle,
		&parentID, &p.Enabled, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("sqlite: scan agent profile: %w", err)
	}
	p.Description = desc.String
	p.PolicyBundleRef = policyBundle.String
	p.ParentProfileID = parentID.String
	// p.Enabled is already set from scan

	// Unmarshal scopes JSON
	if scopesJSON != "" && scopesJSON != "[]" {
		if err := json.Unmarshal([]byte(scopesJSON), &p.Scopes); err != nil {
			return nil, fmt.Errorf("sqlite: unmarshal scopes: %w", err)
		}
	}
	if p.Scopes == nil {
		p.Scopes = []string{}
	}
	// Ensure non-nil slices
	if p.AllowedMCPServers == nil {
		p.AllowedMCPServers = []string{}
	}
	if p.AllowedTools == nil {
		p.AllowedTools = []string{}
	}
	if p.AllowedSkills == nil {
		p.AllowedSkills = []string{}
	}
	if p.AllowedModelAliases == nil {
		p.AllowedModelAliases = []string{}
	}
	if p.AllowedA2APeers == nil {
		p.AllowedA2APeers = []string{}
	}
	if p.AllowedA2ATasks == nil {
		p.AllowedA2ATasks = []string{}
	}
	if p.MCPToA2ABridges == nil {
		p.MCPToA2ABridges = []ifaces.MCPToA2ABridge{}
	}
	if p.A2AToMCPBridges == nil {
		p.A2AToMCPBridges = []ifaces.A2AToMCPBridge{}
	}
	return &p, nil
}

func (s *agentProfileStore) loadAllowlists(ctx context.Context, p *ifaces.AgentProfile) error {
	var err error

	p.AllowedMCPServers, err = s.loadStringSlice(ctx, "agent_profile_mcp_servers", "server_name", p.TenantID, p.ID)
	if err != nil {
		return err
	}
	p.AllowedTools, err = s.loadStringSlice(ctx, "agent_profile_tools", "namespaced_id", p.TenantID, p.ID)
	if err != nil {
		return err
	}
	p.AllowedSkills, err = s.loadStringSlice(ctx, "agent_profile_skills", "skill_id", p.TenantID, p.ID)
	if err != nil {
		return err
	}
	p.AllowedModelAliases, err = s.loadStringSlice(ctx, "agent_profile_models", "alias", p.TenantID, p.ID)
	if err != nil {
		return err
	}
	p.AllowedA2APeers, err = s.loadStringSlice(ctx, "agent_profile_a2a_peers", "peer_name", p.TenantID, p.ID)
	if err != nil {
		return err
	}
	p.AllowedA2ATasks, err = s.loadStringSlice(ctx, "agent_profile_a2a_tasks", "namespaced_id", p.TenantID, p.ID)
	if err != nil {
		return err
	}
	p.MCPToA2ABridges, err = s.loadMCPToA2ABridges(ctx, p.TenantID, p.ID)
	if err != nil {
		return err
	}
	p.A2AToMCPBridges, err = s.loadA2AToMCPBridges(ctx, p.TenantID, p.ID)
	if err != nil {
		return err
	}
	return nil
}

func (s *agentProfileStore) loadStringSlice(ctx context.Context, table, column, tenantID, profileID string) ([]string, error) {
	// nolint:gosec // table/column are internal constants, not user input
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+column+` FROM `+table+` WHERE tenant_id = ? AND profile_id = ? ORDER BY `+column+`
	`, tenantID, profileID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: load allowlist %s: %w", table, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("sqlite: scan allowlist %s: %w", table, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate allowlist %s: %w", table, err)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}
