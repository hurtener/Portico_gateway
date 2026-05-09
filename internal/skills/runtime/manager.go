package runtime

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/skills/loader"
	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

// Manager owns the live skills runtime: load → catalog → watch loop.
// It is started once at boot and stopped on graceful shutdown. The
// Manager owns no goroutines until Start is called.
type Manager struct {
	loader     *loader.Loader
	catalog    *Catalog
	enablement *Enablement
	indexGen   *IndexGenerator
	provider   *SkillProvider
	log        *slog.Logger

	mu   sync.Mutex
	stop func()
	wg   sync.WaitGroup
}

// PlanResolver answers (plan, entitlements) for a tenant. Phase 4
// ships a stub returning ("", ["*"]) — the runtime never queries
// per-tenant overrides until Phase 5.
type PlanResolver func(tenantID string) (plan string, entitlements []string)

// MissingToolsAnnotator returns (missing, warnings) for a (tenant,
// skillID) pair. The Loader's AnnotateMissingTools satisfies it.
type MissingToolsAnnotator func(ctx context.Context, tenantID, skillID string) ([]string, []string)

// NewManager wires every runtime component. The Loader provides the
// list of Sources via its construction; the Manager calls back into
// it for each event. plans and annotate may be nil; the manager
// substitutes safe defaults.
func NewManager(l *loader.Loader, e *Enablement, plans PlanResolver, annotate MissingToolsAnnotator, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	cat := NewCatalog()
	if plans == nil {
		plans = func(string) (string, []string) { return "", []string{"*"} }
	}
	idxGen := NewIndexGenerator(cat, e,
		func(tenantID string) (string, []string) { return plans(tenantID) },
		func(ctx context.Context, tenantID, skillID string) ([]string, []string) {
			if annotate == nil {
				return nil, nil
			}
			return annotate(ctx, tenantID, skillID)
		},
	)
	prov := NewSkillProvider(cat, e, idxGen)
	return &Manager{
		loader:     l,
		catalog:    cat,
		enablement: e,
		indexGen:   idxGen,
		provider:   prov,
		log:        log,
	}
}

// Catalog exposes the catalog so REST handlers and the Console can
// query it directly.
func (m *Manager) Catalog() *Catalog { return m.catalog }

// Enablement exposes the enablement registry.
func (m *Manager) Enablement() *Enablement { return m.enablement }

// IndexGenerator exposes the index generator.
func (m *Manager) IndexGenerator() *IndexGenerator { return m.indexGen }

// Provider exposes the SkillProvider for the resource + prompt
// aggregators to consume.
func (m *Manager) Provider() *SkillProvider { return m.provider }

// Start performs the initial LoadAll, populates the catalog, and
// kicks off a Watch goroutine per source. Returns an error only on
// catastrophic load failure (per-pack errors are logged + skipped).
func (m *Manager) Start(ctx context.Context, sources []source.Source) error {
	if m.loader == nil {
		return errors.New("manager: loader is nil")
	}
	results := m.loader.LoadAll(ctx)
	for _, r := range results {
		if len(r.Errors) > 0 {
			m.log.Warn("skill load failed",
				"skill_id", r.Ref.ID,
				"errors", errSliceToString(r.Errors),
				"warnings", r.Warnings)
			continue
		}
		m.catalog.Set(&Skill{
			Manifest: r.Manifest,
			Source:   r.Source,
			Ref:      r.Ref,
			Warnings: r.Warnings,
			LoadedAt: time.Now().UTC(),
		})
		m.log.Info("skill loaded", "skill_id", r.Manifest.ID, "version", r.Manifest.Version, "warnings", r.Warnings)
	}
	m.indexGen.InvalidateAll()

	// The watch loop must outlive the caller's start ctx (which may be
	// the request context, not the gateway's lifetime). Spawn a fresh
	// context cancelled only by Stop().
	wctx, cancel := context.WithCancel(context.Background()) //nolint:contextcheck
	m.mu.Lock()
	m.stop = cancel
	m.mu.Unlock()

	for _, src := range sources {
		ch, err := src.Watch(wctx) //nolint:contextcheck
		if err != nil || ch == nil {
			m.log.Info("source does not support watching; skipping", "source", src.Name(), "err", err)
			continue
		}
		m.wg.Add(1)
		go m.watchLoop(wctx, src, ch) //nolint:contextcheck
	}
	return nil
}

// Stop cancels the watch goroutines and joins them.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
	m.mu.Unlock()
	m.wg.Wait()
}

// AddSource attaches a previously-unknown Source to the running
// manager: lists its packs, populates the catalog, and starts a
// Watch goroutine. Used by the Phase 8 source.Registry after a
// POST /api/skill-sources write.
//
// tenantID, when non-empty, scopes every loaded pack to that tenant
// (Skill.TenantID is set so ForTenant won't leak the pack across
// tenants). Pass "" for global / cross-tenant sources.
func (m *Manager) AddSource(ctx context.Context, src source.Source, tenantID string) error {
	if m.loader == nil {
		return errors.New("manager: loader is nil")
	}
	results := []loader.LoadResult{}
	refs, err := src.List(ctx)
	if err != nil {
		m.log.Warn("manager: source list failed", "source", src.Name(), "err", err)
		return err
	}
	for _, ref := range refs {
		results = append(results, m.loader.LoadOne(ctx, src, ref))
	}
	for _, r := range results {
		if len(r.Errors) > 0 {
			m.log.Warn("skill load failed",
				"skill_id", r.Ref.ID,
				"errors", errSliceToString(r.Errors),
				"warnings", r.Warnings)
			continue
		}
		key := r.Manifest.ID
		if tenantID != "" {
			key = tenantID + ":" + r.Manifest.ID
		}
		_ = key // catalog keys today are skill ids; tenant scoping is via Skill.TenantID
		m.catalog.Set(&Skill{
			Manifest: r.Manifest,
			Source:   r.Source,
			Ref:      r.Ref,
			Warnings: r.Warnings,
			TenantID: tenantID,
		})
		m.log.Info("skill loaded",
			"skill_id", r.Manifest.ID,
			"version", r.Manifest.Version,
			"source", src.Name(),
			"tenant_id", tenantID)
	}
	m.indexGen.InvalidateAll()

	m.mu.Lock()
	stopCtx := m.stop
	m.mu.Unlock()
	_ = stopCtx
	// Use the manager's lifetime ctx (background) so the watch survives
	// the request that triggered the add. The ctx is intentionally a
	// fresh background — the watch outlives the request scope.
	wctx := context.Background()
	ch, err := src.Watch(wctx) //nolint:contextcheck // wctx is the manager's lifetime, not the request's
	if err != nil || ch == nil {
		return nil
	}
	m.wg.Add(1)
	go m.watchLoop(wctx, src, ch) //nolint:contextcheck // see above
	return nil
}

// RemoveSource drops every catalog entry that came from the named
// source within the given tenant scope. Pass tenantID="" to remove
// across every tenant (used when an operator deletes a global source).
// Watch goroutines are joined when the source's ctx ends or when the
// source's own Stop is called by the caller.
func (m *Manager) RemoveSource(tenantID, name string) {
	m.catalog.RemoveBySource(tenantID, name)
	m.indexGen.InvalidateAll()
	m.log.Info("source removed", "source", name, "tenant_id", tenantID)
}

// RemoveForTenant drops the (tenantID, skillID) entry. Used by the
// authored-skill archive flow.
func (m *Manager) RemoveForTenant(tenantID, skillID string) {
	m.catalog.RemoveForTenant(tenantID, skillID)
	m.indexGen.InvalidateAll()
	m.log.Info("authored skill archived", "skill_id", skillID, "tenant_id", tenantID)
}

func (m *Manager) watchLoop(ctx context.Context, src source.Source, ch <-chan source.Event) {
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			m.handleEvent(ctx, src, ev)
		}
	}
}

func (m *Manager) handleEvent(ctx context.Context, src source.Source, ev source.Event) {
	switch ev.Kind {
	case source.EventRemoved:
		// Determine tenant context from a previously-loaded entry:
		// look up by source name and id; when the prior catalog entry
		// is tenant-scoped, route the removal accordingly.
		tenantID := lookupTenantForSourceID(m.catalog, src.Name(), ev.Ref.ID)
		if tenantID != "" {
			m.catalog.RemoveForTenant(tenantID, ev.Ref.ID)
		} else {
			m.catalog.Remove(ev.Ref.ID)
		}
		m.indexGen.InvalidateAll()
		m.log.Info("skill removed", "skill_id", ev.Ref.ID, "tenant_id", tenantID)
	case source.EventAdded, source.EventUpdated:
		m.reloadSkill(ctx, src, ev.Ref)
	default:
		m.log.Debug("unknown skill event; ignoring", "kind", ev.Kind, "skill_id", ev.Ref.ID)
	}
}

// lookupTenantForSourceID returns the TenantID of a catalog entry that
// matches (sourceName, skillID), or "" if absent or global.
func lookupTenantForSourceID(c *Catalog, sourceName, id string) string {
	for _, s := range c.List() {
		if s == nil || s.Source == nil || s.Manifest == nil {
			continue
		}
		if s.Source.Name() == sourceName && s.Manifest.ID == id {
			return s.TenantID
		}
	}
	return ""
}

// reloadSkill re-validates a single pack and atomically swaps it in;
// on validation failure the previous version stays active (the spec's
// "typo shouldn't take down a tenant" guarantee).
func (m *Manager) reloadSkill(ctx context.Context, src source.Source, ref source.Ref) {
	res := m.loader.LoadOne(ctx, src, ref)
	if len(res.Errors) > 0 {
		m.log.Warn("skill reload failed; keeping previous version",
			"skill_id", ref.ID,
			"errors", errSliceToString(res.Errors),
			"warnings", res.Warnings)
		return
	}
	tenantID := lookupTenantForSourceID(m.catalog, src.Name(), ref.ID)
	m.catalog.Set(&Skill{
		Manifest: res.Manifest,
		Source:   src,
		Ref:      res.Ref,
		Warnings: res.Warnings,
		TenantID: tenantID,
	})
	m.indexGen.InvalidateAll()
	m.log.Info("skill reloaded", "skill_id", res.Manifest.ID, "version", res.Manifest.Version, "tenant_id", tenantID)
}

func errSliceToString(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}
