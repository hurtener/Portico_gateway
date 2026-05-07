// Package source defines the SkillSource abstraction — the seam
// between the on-disk (or remote) representation of Skill Packs and
// the loader/runtime that consume them. V1 ships LocalDir, Git, HTTP,
// and an in-Portico Authored driver. New drivers self-register from
// their package init() via Register and are constructed through the
// per-tenant Registry (registry.go).
package source

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
)

// EventKind classifies a Watch notification.
type EventKind string

const (
	EventAdded   EventKind = "added"
	EventUpdated EventKind = "updated"
	EventRemoved EventKind = "removed"
)

// Ref identifies a Skill Pack within a particular Source. The pair
// (Source name, ID) is unique; Loc is backend-specific (an absolute
// path for LocalDir, a Git ref for Git, etc.).
type Ref struct {
	ID      string
	Version string
	Source  string
	Loc     string
}

// ContentInfo describes the bytes a Source returns from ReadFile.
type ContentInfo struct {
	MIMEType string
	Size     int64
	ModTime  time.Time
}

// Event is a Watch notification.
type Event struct {
	Kind EventKind
	Ref  Ref
}

// Driver names registered by the V1 drivers. Kept as constants so
// callers (and the Registry) avoid string literals.
const (
	DriverLocal    = "local"
	DriverLocalDir = "localdir" // alias accepted in tenant_skill_sources rows
	DriverGit      = "git"
	DriverHTTP     = "http"
	DriverAuthored = "authored"
)

// Source is the cross-driver interface for Skill Pack discovery.
type Source interface {
	// Name returns the driver identifier (e.g. "local"). Used in audit
	// records and the index generator.
	Name() string

	// List enumerates every Skill Pack the source can see right now.
	// Tolerant: malformed packs are returned as Ref + a Manifest with
	// empty fields; the loader decides what to do.
	List(ctx context.Context) ([]Ref, error)

	// Open parses the manifest for ref. The result is NOT validated —
	// validation runs in the loader package.
	Open(ctx context.Context, ref Ref) (manifest.Manifest, error)

	// ReadFile returns the bytes of relpath inside the pack. relpath
	// is relative to the pack root (no absolute paths, no traversal).
	// Implementations MUST reject attempts to escape the pack root.
	ReadFile(ctx context.Context, ref Ref, relpath string) (io.ReadCloser, ContentInfo, error)

	// Watch returns a channel that emits change events. nil when the
	// source does not support watching (callers fall back to periodic
	// List polling at the configured refresh_interval).
	Watch(ctx context.Context) (<-chan Event, error)
}

// FactoryDeps bundles the runtime services every driver factory may
// need. Drivers consume only what they require — fields may be nil.
// The Registry constructs this once per request and hands it to the
// factory; drivers MUST NOT cache it across constructions because the
// Vault, in particular, may rotate keys at runtime.
type FactoryDeps struct {
	// Vault provides credential lookup keyed by (tenantID, name).
	// Nil when the gateway boots without PORTICO_VAULT_KEY; drivers
	// that require credentials surface a typed error in that case.
	Vault VaultLookup

	// TenantID scopes every credential lookup.
	TenantID string

	// DataDir is the operator-configured persistent root. Drivers that
	// cache content on disk (Git clone tree, HTTP bundle cache) should
	// nest under DataDir + driver name + tenant + content hash.
	DataDir string

	// Logger is the slog.Logger seeded with tenant_id + driver name.
	// May be nil; drivers fall back to slog.Default.
	Logger *slog.Logger

	// AuthoredRepo is the SQLite-backed repository handed to the
	// authored driver. Nil for non-authored drivers.
	AuthoredRepo AuthoredRepo

	// SourceName is the operator-chosen name persisted in
	// tenant_skill_sources.name. Drivers return this from Source.Name()
	// so the Registry can attribute catalog entries back to the row
	// the operator created.
	SourceName string
}

// VaultLookup is the slim contract source drivers depend on. The full
// secrets.Vault satisfies it; declaring the dependency narrowly here
// keeps this package free of any direct vault import.
type VaultLookup interface {
	Get(ctx context.Context, tenantID, name string) (string, error)
}

// AuthoredRepo is the slim contract the authored driver depends on.
// The concrete *authored.Store satisfies it; the Registry hands the
// concrete repo through here without introducing an import cycle.
type AuthoredRepo interface {
	// ListPublished returns the (skillID, version) pairs of every
	// published authored skill for the tenant.
	ListPublished(ctx context.Context, tenantID string) ([]AuthoredHandle, error)

	// LoadManifest returns the canonical manifest body for an
	// authored skill version.
	LoadManifest(ctx context.Context, tenantID, skillID, version string) ([]byte, error)

	// LoadFile returns the bytes + mime of a relpath inside the
	// authored skill version.
	LoadFile(ctx context.Context, tenantID, skillID, version, relpath string) ([]byte, string, error)

	// SubscribePublishes returns a channel that receives every
	// publish/archive event for the tenant. Implementations must
	// honour ctx for shutdown and close the channel on exit.
	SubscribePublishes(ctx context.Context, tenantID string) (<-chan Event, error)
}

// AuthoredHandle is the (skillID, version) tuple plus pack id used by
// the Registry to materialise per-pack Refs.
type AuthoredHandle struct {
	SkillID string
	Version string
}

// Factory builds a Source from a driver-specific config payload + the
// shared FactoryDeps. Drivers register a Factory at init() time via
// Register; the Registry dispatches by driver name.
//
// configJSON is the verbatim driver config (the JSON encoding of the
// per-driver Config struct). Each driver decodes its own shape.
type Factory func(ctx context.Context, configJSON []byte, deps FactoryDeps) (Source, error)

var (
	factoriesMu sync.RWMutex
	factories   = map[string]Factory{}
)

// Register installs a Factory under the given driver name. Drivers
// MUST call Register from their package's init() function. Re-
// registering panics — factory conflicts are programmer errors.
func Register(name string, f Factory) {
	if name == "" || f == nil {
		panic("source: Register requires a non-empty name and non-nil factory")
	}
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	if _, exists := factories[name]; exists {
		panic(fmt.Sprintf("source: driver %q already registered", name))
	}
	factories[name] = f
}

// Drivers returns the names of every registered driver. Order is
// unspecified.
func Drivers() []string {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	return out
}

// Build dispatches to the named driver's factory. Returns a typed
// error listing registered drivers when name is unknown.
func Build(ctx context.Context, name string, configJSON []byte, deps FactoryDeps) (Source, error) {
	factoriesMu.RLock()
	f, ok := factories[name]
	factoriesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("source: unknown driver %q (registered: %v)", name, Drivers())
	}
	return f(ctx, configJSON, deps)
}
