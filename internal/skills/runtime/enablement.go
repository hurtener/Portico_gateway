package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// EnablementMode is the operator-chosen default for whether skills are
// active without explicit opt-in.
type EnablementMode string

const (
	// ModeOptIn (default) requires an Enable call (per-tenant or
	// per-session) before a skill is visible to a session.
	ModeOptIn EnablementMode = "opt-in"
	// ModeAuto enables every entitled skill by default. Disable
	// flips it off explicitly.
	ModeAuto EnablementMode = "auto"
)

// Enablement is the runtime wrapper around the SkillEnablementStore. It
// adds the manifest-default fallback that the bare store cannot
// express on its own.
type Enablement struct {
	store ifaces.SkillEnablementStore
	mode  EnablementMode
}

// NewEnablement wires the store + the default mode.
func NewEnablement(store ifaces.SkillEnablementStore, mode EnablementMode) *Enablement {
	if mode != ModeAuto {
		mode = ModeOptIn
	}
	return &Enablement{store: store, mode: mode}
}

// Mode reports the configured default.
func (e *Enablement) Mode() EnablementMode { return e.mode }

// IsEnabled resolves the per-session > per-tenant > manifest-default
// precedence. tenantID and skillID are required; sessionID is optional
// (empty means tenant-wide query).
func (e *Enablement) IsEnabled(ctx context.Context, tenantID, sessionID, skillID string) (bool, error) {
	if e == nil || e.store == nil {
		// Without a store the mode determines the answer.
		return e.mode == ModeAuto, nil
	}
	if tenantID == "" || skillID == "" {
		return false, errors.New("enablement: tenant_id and skill_id required")
	}
	enabled, found, err := e.store.Resolve(ctx, tenantID, sessionID, skillID)
	if err != nil {
		return false, err
	}
	if found {
		return enabled, nil
	}
	return e.mode == ModeAuto, nil
}

// Set toggles enablement at the requested scope. Pass sessionID == ""
// for tenant-wide rules.
func (e *Enablement) Set(ctx context.Context, tenantID, sessionID, skillID string, enabled bool) error {
	if e == nil || e.store == nil {
		return errors.New("enablement: no store configured")
	}
	return e.store.Set(ctx, &ifaces.SkillEnablement{
		TenantID:  tenantID,
		SessionID: sessionID,
		SkillID:   skillID,
		Enabled:   enabled,
		EnabledAt: time.Now().UTC(),
	})
}

// Delete removes the rule, falling resolution back to the next layer.
func (e *Enablement) Delete(ctx context.Context, tenantID, sessionID, skillID string) error {
	if e == nil || e.store == nil {
		return errors.New("enablement: no store configured")
	}
	if err := e.store.Delete(ctx, tenantID, sessionID, skillID); err != nil && !errors.Is(err, ifaces.ErrNotFound) {
		return err
	}
	return nil
}

// ListForSession returns explicit per-session rules. Useful for the
// Console session detail view; not the source of truth for "which
// skills the session sees" (that requires combining with tenant + mode).
func (e *Enablement) ListForSession(ctx context.Context, tenantID, sessionID string) ([]*ifaces.SkillEnablement, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}
	return e.store.ListForSession(ctx, tenantID, sessionID)
}

// ListForTenant returns explicit tenant-wide rules.
func (e *Enablement) ListForTenant(ctx context.Context, tenantID string) ([]*ifaces.SkillEnablement, error) {
	if e == nil || e.store == nil {
		return nil, nil
	}
	return e.store.ListForTenant(ctx, tenantID)
}
