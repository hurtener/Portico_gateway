// Package source defines the SkillSource abstraction — the seam
// between the on-disk (or remote) representation of Skill Packs and
// the loader/runtime that consume them. V1 ships LocalDir; future
// drivers (Git, OCI, HTTP) plug in additively.
package source

import (
	"context"
	"io"
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
