package ifaces

import (
	"context"
	"time"
)

// SkillEnablement is one row of the skill_enablement table. Per the
// schema, session_id is empty string for tenant-wide rules and a real
// session id for per-session overrides. Resolution: per-session row
// wins; tenant row is the fallback; manifest default is the last resort.
type SkillEnablement struct {
	TenantID  string    `json:"tenant_id"`
	SessionID string    `json:"session_id,omitempty"`
	SkillID   string    `json:"skill_id"`
	Enabled   bool      `json:"enabled"`
	EnabledAt time.Time `json:"enabled_at"`
}

// SkillEnablementStore is the persistent backing for per-tenant +
// per-session skill enablement. Phase 4 adds this; Phase 5+ may layer
// audit + admin views on top.
type SkillEnablementStore interface {
	// Set inserts or updates the (tenant, session, skill) row.
	// SessionID == "" means tenant-wide; non-empty means per-session.
	Set(ctx context.Context, e *SkillEnablement) error

	// Delete removes the row. Returns ErrNotFound when no row matches.
	Delete(ctx context.Context, tenantID, sessionID, skillID string) error

	// Resolve answers "should this session see this skill?" using the
	// per-session > per-tenant > manifest-default precedence. found is
	// false when no row matches; the caller falls back to the manifest
	// default.
	Resolve(ctx context.Context, tenantID, sessionID, skillID string) (enabled, found bool, err error)

	// ListForSession returns every per-session row for the given
	// (tenant, session). Used by the Console session detail view.
	ListForSession(ctx context.Context, tenantID, sessionID string) ([]*SkillEnablement, error)

	// ListForTenant returns every tenant-wide row. Used by the Console
	// tenant view + the loader's bootstrap (Phase 5).
	ListForTenant(ctx context.Context, tenantID string) ([]*SkillEnablement, error)
}
