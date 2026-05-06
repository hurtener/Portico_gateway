package runtime

import (
	"context"
	"errors"
	"log/slog"
	"sync"

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
		m.catalog.Remove(ev.Ref.ID)
		m.indexGen.InvalidateAll()
		m.log.Info("skill removed", "skill_id", ev.Ref.ID)
	case source.EventAdded, source.EventUpdated:
		m.reloadSkill(ctx, src, ev.Ref)
	default:
		m.log.Debug("unknown skill event; ignoring", "kind", ev.Kind, "skill_id", ev.Ref.ID)
	}
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
	m.catalog.Set(&Skill{
		Manifest: res.Manifest,
		Source:   src,
		Ref:      res.Ref,
		Warnings: res.Warnings,
	})
	m.indexGen.InvalidateAll()
	m.log.Info("skill reloaded", "skill_id", res.Manifest.ID, "version", res.Manifest.Version)
}

func errSliceToString(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}
