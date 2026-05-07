// registry.go is the per-tenant Source orchestrator. The loader and
// runtime depend on it instead of holding a single concrete Source.

package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// AuditEmitter is the slim audit surface used by the registry.
type AuditEmitter interface {
	Emit(ctx context.Context, eventType, tenantID, userID string, payload map[string]any)
}

// Registry materialises the active per-tenant set of Source values.
// External callers ask the Registry for a tenant's sources; the
// Registry caches per-tenant materialised Source instances and
// invalidates the cache on CRUD writes.
type Registry struct {
	store        ifaces.SkillSourceStore
	authoredRepo AuthoredRepo
	vault        VaultLookup
	dataDir      string
	log          *slog.Logger
	emitter      AuditEmitter

	mu     sync.RWMutex
	cached map[string]*tenantBundle // tenantID -> active sources

	// closeFns holds per-source teardown funcs so DELETE /api/skill-sources/{name}
	// joins the watcher cleanly.
	closeFnsMu sync.Mutex
	closeFns   map[string]map[string]func()
}

type tenantBundle struct {
	sources []Source
	byName  map[string]Source
}

// NewRegistry constructs a Registry. authoredRepo may be nil — in that
// case the authored driver factory will refuse to construct a Source.
func NewRegistry(store ifaces.SkillSourceStore, authoredRepo AuthoredRepo, vault VaultLookup, dataDir string, log *slog.Logger, emitter AuditEmitter) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{
		store:        store,
		authoredRepo: authoredRepo,
		vault:        vault,
		dataDir:      dataDir,
		log:          log.With("component", "skills.registry"),
		emitter:      emitter,
		cached:       make(map[string]*tenantBundle),
		closeFns:     make(map[string]map[string]func()),
	}
}

// Sources returns the active Source list for the tenant, ordered by
// priority ASC. Cached per tenant; invalidated on CRUD writes.
func (r *Registry) Sources(ctx context.Context, tenantID string) ([]Source, error) {
	r.mu.RLock()
	if b, ok := r.cached[tenantID]; ok {
		r.mu.RUnlock()
		return b.sources, nil
	}
	r.mu.RUnlock()
	return r.materialise(ctx, tenantID)
}

// SourceByName returns one Source for the tenant. Convenience helper
// the REST handlers use to scope refresh + diagnostic calls.
func (r *Registry) SourceByName(ctx context.Context, tenantID, name string) (Source, error) {
	if _, err := r.Sources(ctx, tenantID); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.cached[tenantID]
	if !ok {
		return nil, fmt.Errorf("registry: tenant %q has no sources", tenantID)
	}
	src, ok := b.byName[name]
	if !ok {
		return nil, fmt.Errorf("registry: source %q not found for tenant", name)
	}
	return src, nil
}

// Invalidate drops the per-tenant cache. Called after every CRUD
// write; the next Sources call rebuilds.
func (r *Registry) Invalidate(tenantID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unregisterTenantClosersLocked(tenantID)
	delete(r.cached, tenantID)
}

// Refresh forces a List on every active source for the tenant and
// updates last_refresh_at / last_error per source. Used by the
// /api/skill-sources/{name}/refresh handler and the post-publish
// re-evaluation path.
func (r *Registry) Refresh(ctx context.Context, tenantID string) error {
	rows, err := r.store.List(ctx, tenantID)
	if err != nil {
		return err
	}
	bundle, err := r.Sources(ctx, tenantID)
	if err != nil {
		return err
	}
	byName := make(map[string]Source, len(bundle))
	for _, s := range bundle {
		byName[s.Name()] = s
	}
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		src, ok := byName[row.Name]
		if !ok {
			continue
		}
		var errStr string
		if _, err := src.List(ctx); err != nil {
			errStr = err.Error()
		}
		if mErr := r.store.MarkRefreshed(ctx, tenantID, row.Name, time.Now().UTC(), errStr); mErr != nil {
			r.log.Warn("registry: mark refreshed failed", "name", row.Name, "err", mErr)
		}
	}
	return nil
}

// RefreshOne forces a List on a single source for the tenant.
func (r *Registry) RefreshOne(ctx context.Context, tenantID, name string) error {
	src, err := r.SourceByName(ctx, tenantID, name)
	if err != nil {
		return err
	}
	var errStr string
	if _, err := src.List(ctx); err != nil {
		errStr = err.Error()
	}
	if mErr := r.store.MarkRefreshed(ctx, tenantID, name, time.Now().UTC(), errStr); mErr != nil {
		r.log.Warn("registry: mark refreshed failed", "name", name, "err", mErr)
	}
	if errStr != "" {
		return errors.New(errStr)
	}
	return nil
}

// Subscribe fans in Watch events from every active source for a
// tenant. The returned channel closes on ctx end. Drop-oldest on
// backpressure with an audit event on first drop in a window.
func (r *Registry) Subscribe(ctx context.Context, tenantID string) (<-chan Event, error) {
	srcs, err := r.Sources(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make(chan Event, 32)
	var dropOnce sync.Once
	for _, src := range srcs {
		ch, err := src.Watch(ctx)
		if err != nil || ch == nil {
			continue
		}
		go func(c <-chan Event, name string) {
			for ev := range c {
				select {
				case out <- ev:
				default:
					dropOnce.Do(func() {
						if r.emitter != nil {
							r.emitter.Emit(ctx, "skill_source.dropped", tenantID, "", map[string]any{"source": name})
						}
						r.log.Warn("registry: subscribe dropped event", "tenant", tenantID, "source", name)
					})
				}
			}
		}(ch, src.Name())
	}
	go func() {
		<-ctx.Done()
		close(out)
	}()
	return out, nil
}

// --- materialisation -------------------------------------------------

func (r *Registry) materialise(ctx context.Context, tenantID string) ([]Source, error) {
	rows, err := r.store.List(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("registry: list rows: %w", err)
	}
	// Order by priority ASC (lower wins on collision); break ties by
	// name to keep the boot-time order deterministic across runs.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Priority != rows[j].Priority {
			return rows[i].Priority < rows[j].Priority
		}
		return rows[i].Name < rows[j].Name
	})
	srcs := make([]Source, 0, len(rows)+1)
	byName := make(map[string]Source, len(rows)+1)
	closers := make(map[string]func(), len(rows)+1)

	// Always synthesise the in-Portico authored source for the tenant
	// even when no row exists. Operators don't (and shouldn't) author
	// their own "authored" rows.
	if r.authoredRepo != nil {
		deps := FactoryDeps{
			Vault: r.vault, TenantID: tenantID, DataDir: r.dataDir,
			Logger: r.log.With("source", "authored"), AuthoredRepo: r.authoredRepo,
		}
		if src, err := Build(ctx, "authored", []byte(`{}`), deps); err == nil {
			srcs = append(srcs, src)
			byName[src.Name()] = src
		} else {
			r.log.Warn("registry: authored materialise failed", "err", err)
		}
	}

	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		deps := FactoryDeps{
			Vault: r.vault, TenantID: tenantID, DataDir: r.dataDir,
			Logger:     r.log.With("source", row.Name, "driver", row.Driver),
			SourceName: row.Name,
		}
		src, err := Build(ctx, row.Driver, row.ConfigJSON, deps)
		if err != nil {
			r.log.Warn("registry: source materialise failed", "name", row.Name, "driver", row.Driver, "err", err)
			now := time.Now().UTC()
			_ = r.store.MarkRefreshed(ctx, tenantID, row.Name, now, err.Error())
			continue
		}
		srcs = append(srcs, src)
		byName[row.Name] = src
		if closer, ok := src.(interface{ Stop() }); ok {
			closers[row.Name] = closer.Stop
		}
	}

	r.mu.Lock()
	r.cached[tenantID] = &tenantBundle{sources: srcs, byName: byName}
	r.mu.Unlock()

	r.closeFnsMu.Lock()
	r.closeFns[tenantID] = closers
	r.closeFnsMu.Unlock()

	return srcs, nil
}

func (r *Registry) unregisterTenantClosersLocked(tenantID string) {
	r.closeFnsMu.Lock()
	closers := r.closeFns[tenantID]
	delete(r.closeFns, tenantID)
	r.closeFnsMu.Unlock()
	for _, fn := range closers {
		if fn != nil {
			fn()
		}
	}
}
