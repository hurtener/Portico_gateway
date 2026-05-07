// Phase 8 wiring — bridges between the REST API contracts the api
// package declares (SkillSourcesController, AuthoredSkillsController,
// SkillValidator) and the concrete implementations under
// internal/skills/source/* + internal/skills/loader.
//
// Lives in cmd/portico (binary entry point) so the api package stays
// free of imports on driver packages — see CLAUDE.md §4.4.

package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	auditpkg "github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/skills/loader"
	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	skillruntime "github.com/hurtener/Portico_gateway/internal/skills/runtime"
	skillsource "github.com/hurtener/Portico_gateway/internal/skills/source"
	"github.com/hurtener/Portico_gateway/internal/skills/source/authored"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// skillSourcesAdapter implements api.SkillSourcesController by
// delegating to the SQLite store + the per-tenant skill source
// registry. CRUD writes invalidate the registry's per-tenant cache
// and re-attach sources to the running skills Manager.
type skillSourcesAdapter struct {
	store    ifaces.SkillSourceStore
	registry *skillsource.Registry
	manager  *skillruntime.Manager
	emitter  *auditpkg.FanoutEmitter
	log      *slog.Logger
}

func newSkillSourcesAdapter(store ifaces.SkillSourceStore, registry *skillsource.Registry, mgr *skillruntime.Manager, emitter *auditpkg.FanoutEmitter, log *slog.Logger) *skillSourcesAdapter {
	return &skillSourcesAdapter{store: store, registry: registry, manager: mgr, emitter: emitter, log: log}
}

func (a *skillSourcesAdapter) List(ctx context.Context, tenantID string) ([]*ifaces.SkillSourceRecord, error) {
	return a.store.List(ctx, tenantID)
}

func (a *skillSourcesAdapter) Get(ctx context.Context, tenantID, name string) (*ifaces.SkillSourceRecord, error) {
	return a.store.Get(ctx, tenantID, name)
}

func (a *skillSourcesAdapter) Upsert(ctx context.Context, rec *ifaces.SkillSourceRecord) error {
	if err := a.store.Upsert(ctx, rec); err != nil {
		return err
	}
	// Invalidate the per-tenant cache, then re-attach sources to the
	// running manager so the catalog reflects the new state.
	if a.registry != nil {
		a.registry.Invalidate(rec.TenantID)
	}
	if a.manager != nil {
		a.attachSources(ctx, rec.TenantID)
	}
	if a.emitter != nil {
		a.emitter.Emit(ctx, auditpkg.Event{
			Type:     "skill_source.added",
			TenantID: rec.TenantID,
			Payload:  map[string]any{"name": rec.Name, "driver": rec.Driver},
		})
	}
	return nil
}

func (a *skillSourcesAdapter) Delete(ctx context.Context, tenantID, name string) error {
	if err := a.store.Delete(ctx, tenantID, name); err != nil {
		return err
	}
	if a.manager != nil {
		a.manager.RemoveSource(tenantID, name)
	}
	if a.registry != nil {
		a.registry.Invalidate(tenantID)
	}
	if a.emitter != nil {
		a.emitter.Emit(ctx, auditpkg.Event{
			Type:     "skill_source.removed",
			TenantID: tenantID,
			Payload:  map[string]any{"name": name},
		})
	}
	return nil
}

func (a *skillSourcesAdapter) Refresh(ctx context.Context, tenantID, name string) error {
	if a.registry == nil {
		return errors.New("skill_sources: registry not configured")
	}
	if err := a.registry.RefreshOne(ctx, tenantID, name); err != nil {
		return err
	}
	// Re-attach so any new packs land in the catalog.
	if a.manager != nil {
		a.attachSources(ctx, tenantID)
	}
	if a.emitter != nil {
		a.emitter.Emit(ctx, auditpkg.Event{
			Type:     "skill_source.refreshed",
			TenantID: tenantID,
			Payload:  map[string]any{"name": name},
		})
	}
	return nil
}

func (a *skillSourcesAdapter) ListPacks(ctx context.Context, tenantID, name string) ([]api.SourcePack, error) {
	if a.registry == nil {
		return nil, errors.New("skill_sources: registry not configured")
	}
	src, err := a.registry.SourceByName(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}
	refs, err := src.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.SourcePack, 0, len(refs))
	for _, r := range refs {
		out = append(out, api.SourcePack{ID: r.ID, Version: r.Version, Loc: r.Loc})
	}
	return out, nil
}

// attachSources re-materialises the per-tenant source list and adds
// every source to the running manager. Idempotent — AddSource calls
// catalog.Set which is upsert.
func (a *skillSourcesAdapter) attachSources(ctx context.Context, tenantID string) {
	if a.registry == nil || a.manager == nil {
		return
	}
	srcs, err := a.registry.Sources(ctx, tenantID)
	if err != nil {
		a.log.Warn("phase8: registry sources failed", "tenant_id", tenantID, "err", err)
		return
	}
	for _, src := range srcs {
		if src == nil {
			continue
		}
		if err := a.manager.AddSource(ctx, src, tenantID); err != nil {
			a.log.Warn("phase8: AddSource failed", "tenant_id", tenantID, "source", src.Name(), "err", err)
		}
	}
}

// ------------------------------------------------------------------
// authoredSkillsAdapter implements api.AuthoredSkillsController.

type authoredSkillsAdapter struct {
	store       *authored.Store
	manager     *skillruntime.Manager
	registryRef *skillsource.Registry
	emitter     *auditpkg.FanoutEmitter
	log         *slog.Logger
}

func newAuthoredSkillsAdapter(store *authored.Store, mgr *skillruntime.Manager, registry *skillsource.Registry, emitter *auditpkg.FanoutEmitter, log *slog.Logger) *authoredSkillsAdapter {
	return &authoredSkillsAdapter{store: store, manager: mgr, registryRef: registry, emitter: emitter, log: log}
}

func (a *authoredSkillsAdapter) ListAuthored(ctx context.Context, tenantID string) ([]authored.Authored, error) {
	return a.store.ListAuthored(ctx, tenantID)
}

func (a *authoredSkillsAdapter) GetAuthored(ctx context.Context, tenantID, skillID, version string) (*authored.Authored, error) {
	return a.store.GetAuthored(ctx, tenantID, skillID, version)
}

func (a *authoredSkillsAdapter) History(ctx context.Context, tenantID, skillID string) ([]authored.Authored, error) {
	return a.store.History(ctx, tenantID, skillID)
}

func (a *authoredSkillsAdapter) GetActive(ctx context.Context, tenantID, skillID string) (*authored.Authored, error) {
	return a.store.GetActive(ctx, tenantID, skillID)
}

func (a *authoredSkillsAdapter) CreateDraft(ctx context.Context, tenantID, userID string, m manifest.Manifest, files []authored.File) (*authored.Authored, error) {
	rec, err := a.store.CreateDraft(ctx, tenantID, userID, m, files)
	if err != nil {
		return nil, err
	}
	if a.emitter != nil {
		a.emitter.Emit(ctx, auditpkg.Event{
			Type:     "skill.authored.draft_created",
			TenantID: tenantID,
			UserID:   userID,
			Payload:  map[string]any{"skill_id": rec.SkillID, "version": rec.Version, "checksum": rec.Checksum},
		})
	}
	return rec, nil
}

func (a *authoredSkillsAdapter) UpdateDraft(ctx context.Context, tenantID, skillID, version, userID string, m manifest.Manifest, files []authored.File) (*authored.Authored, error) {
	rec, err := a.store.UpdateDraft(ctx, tenantID, skillID, version, userID, m, files)
	if err != nil {
		return nil, err
	}
	if a.emitter != nil {
		a.emitter.Emit(ctx, auditpkg.Event{
			Type:     "skill.authored.draft_updated",
			TenantID: tenantID,
			UserID:   userID,
			Payload:  map[string]any{"skill_id": rec.SkillID, "version": rec.Version, "checksum": rec.Checksum},
		})
	}
	return rec, nil
}

func (a *authoredSkillsAdapter) Publish(ctx context.Context, tenantID, skillID, version string) (*authored.Authored, error) {
	rec, err := a.store.Publish(ctx, tenantID, skillID, version)
	if err != nil {
		return nil, err
	}
	// Re-attach so the new version lands in the manager's catalog.
	if a.manager != nil && a.registryRef != nil {
		a.registryRef.Invalidate(tenantID)
		srcs, err := a.registryRef.Sources(ctx, tenantID)
		if err == nil {
			for _, src := range srcs {
				_ = a.manager.AddSource(ctx, src, tenantID)
			}
		}
	}
	if a.emitter != nil {
		a.emitter.Emit(ctx, auditpkg.Event{
			Type:     "skill.authored.published",
			TenantID: tenantID,
			Payload:  map[string]any{"skill_id": rec.SkillID, "version": rec.Version, "checksum": rec.Checksum},
		})
	}
	return rec, nil
}

func (a *authoredSkillsAdapter) Archive(ctx context.Context, tenantID, skillID, version string) error {
	if err := a.store.Archive(ctx, tenantID, skillID, version); err != nil {
		return err
	}
	if a.manager != nil {
		a.manager.RemoveForTenant(tenantID, skillID)
	}
	if a.emitter != nil {
		a.emitter.Emit(ctx, auditpkg.Event{
			Type:     "skill.authored.archived",
			TenantID: tenantID,
			Payload:  map[string]any{"skill_id": skillID, "version": version},
		})
	}
	return nil
}

func (a *authoredSkillsAdapter) DeleteDraft(ctx context.Context, tenantID, skillID, version string) error {
	if err := a.store.DeleteDraft(ctx, tenantID, skillID, version); err != nil {
		return err
	}
	if a.emitter != nil {
		a.emitter.Emit(ctx, auditpkg.Event{
			Type:     "skill.authored.draft_deleted",
			TenantID: tenantID,
			Payload:  map[string]any{"skill_id": skillID, "version": version},
		})
	}
	return nil
}

// ------------------------------------------------------------------
// skillValidatorAdapter implements api.SkillValidator using the
// loader's canonical validation pipeline.

type skillValidatorAdapter struct {
	schema *manifest.Schema
}

func newSkillValidatorAdapter() (*skillValidatorAdapter, error) {
	s, err := manifest.CompileSchema()
	if err != nil {
		return nil, err
	}
	return &skillValidatorAdapter{schema: s}, nil
}

func (v *skillValidatorAdapter) Validate(body []byte) []api.ValidatorViolation {
	res := loader.ValidateManifestBytes(body, v.schema)
	out := make([]api.ValidatorViolation, 0, len(res.Violations))
	for _, vio := range res.Violations {
		out = append(out, api.ValidatorViolation{
			Pointer: vio.Pointer,
			Line:    vio.Line,
			Col:     vio.Col,
			Reason:  vio.Reason,
			Kind:    vio.Kind,
		})
	}
	return out
}

// ------------------------------------------------------------------
// auditEmitterShim adapts the audit FanoutEmitter to the slim
// AuditEmitter interface defined in skillsource.

type auditEmitterShim struct {
	em *auditpkg.FanoutEmitter
}

func (s auditEmitterShim) Emit(ctx context.Context, eventType, tenantID, userID string, payload map[string]any) {
	if s.em == nil {
		return
	}
	s.em.Emit(ctx, auditpkg.Event{
		Type:       eventType,
		TenantID:   tenantID,
		UserID:     userID,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
}

// vaultLookupAdapter wraps the secrets.Vault to satisfy the slim
// VaultLookup interface skill source drivers depend on. Lets the
// source package keep zero secrets imports.
type vaultLookupAdapter struct {
	vault interface {
		Get(ctx context.Context, tenantID, name string) (string, error)
	}
}

func (a vaultLookupAdapter) Get(ctx context.Context, tenantID, name string) (string, error) {
	if a.vault == nil {
		return "", errors.New("vault not configured")
	}
	return a.vault.Get(ctx, tenantID, name)
}
