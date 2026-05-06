package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
)

// LocalDir loads Skill Packs from a directory tree:
//
//	{Root}/{namespace}/{name}/manifest.yaml
//	                          SKILL.md
//	                          prompts/*.md
//	                          resources/*.{md,json,yaml,html}
//
// Pack id is derived as `{namespace}.{name}`.
type LocalDir struct {
	root string
	log  *slog.Logger

	mu      sync.RWMutex
	watcher *fsnotify.Watcher
}

// NewLocalDir constructs a LocalDir source rooted at root. The
// directory must exist.
func NewLocalDir(root string, log *slog.Logger) (*LocalDir, error) {
	if log == nil {
		log = slog.Default()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("localdir: abs path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("localdir: stat %q: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("localdir: %q is not a directory", abs)
	}
	return &LocalDir{root: abs, log: log.With("source", "local", "root", abs)}, nil
}

// Name returns "local".
func (d *LocalDir) Name() string { return "local" }

// Root is the absolute path the source was constructed against. Useful
// for tests and the validate-skills CLI.
func (d *LocalDir) Root() string { return d.root }

// List walks {root}/<namespace>/<name>/ and returns one Ref per
// directory containing a manifest.yaml. Order is not specified; the
// loader sorts the result for deterministic output.
func (d *LocalDir) List(_ context.Context) ([]Ref, error) {
	out := make([]Ref, 0)
	entries, err := os.ReadDir(d.root)
	if err != nil {
		return nil, fmt.Errorf("localdir: read root %q: %w", d.root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nsPath := filepath.Join(d.root, e.Name())
		nsEntries, err := os.ReadDir(nsPath)
		if err != nil {
			d.log.Warn("localdir: skip namespace", "namespace", e.Name(), "err", err)
			continue
		}
		for _, ne := range nsEntries {
			if !ne.IsDir() {
				continue
			}
			packPath := filepath.Join(nsPath, ne.Name())
			manifestPath := filepath.Join(packPath, "manifest.yaml")
			body, err := os.ReadFile(manifestPath)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					d.log.Warn("localdir: skip pack", "pack", packPath, "err", err)
				}
				continue
			}
			m, _, perr := manifest.Parse(body)
			id := e.Name() + "." + ne.Name()
			version := ""
			if perr == nil && m != nil {
				if m.ID != "" {
					id = m.ID
				}
				version = m.Version
			}
			out = append(out, Ref{
				ID:      id,
				Version: version,
				Source:  d.Name(),
				Loc:     packPath,
			})
		}
	}
	return out, nil
}

// Open parses the manifest for ref. ref.Loc must point at a directory
// inside the LocalDir root.
func (d *LocalDir) Open(_ context.Context, ref Ref) (manifest.Manifest, error) {
	if err := d.guardLoc(ref.Loc); err != nil {
		return manifest.Manifest{}, err
	}
	body, err := os.ReadFile(filepath.Join(ref.Loc, "manifest.yaml"))
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("localdir: read manifest %q: %w", ref.ID, err)
	}
	m, _, err := manifest.Parse(body)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("localdir: parse manifest %q: %w", ref.ID, err)
	}
	return *m, nil
}

// ReadFile returns the bytes of relpath inside the pack at ref.Loc.
// Rejects absolute paths and traversal attempts that escape the pack
// root (security-critical: a manifest could ask for any path on the
// host filesystem otherwise).
func (d *LocalDir) ReadFile(_ context.Context, ref Ref, relpath string) (io.ReadCloser, ContentInfo, error) {
	if err := d.guardLoc(ref.Loc); err != nil {
		return nil, ContentInfo{}, err
	}
	if filepath.IsAbs(relpath) {
		return nil, ContentInfo{}, fmt.Errorf("localdir: relpath must be relative")
	}
	abs := filepath.Clean(filepath.Join(ref.Loc, relpath))
	if !strings.HasPrefix(abs, ref.Loc+string(os.PathSeparator)) && abs != ref.Loc {
		return nil, ContentInfo{}, fmt.Errorf("localdir: relpath escapes pack root")
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, ContentInfo{}, fmt.Errorf("localdir: open %q: %w", relpath, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, ContentInfo{}, fmt.Errorf("localdir: stat %q: %w", relpath, err)
	}
	return f, ContentInfo{
		MIMEType: detectMIME(abs),
		Size:     info.Size(),
		ModTime:  info.ModTime().UTC(),
	}, nil
}

// Watch wires fsnotify against every directory under root and emits a
// debounced Updated event per pack on any descendant change. Phase 4
// uses the Updated kind regardless of whether the underlying event was
// CREATE/REMOVE — the loader treats every event as a re-validation
// trigger, which is simpler and robust against editor-driven rename
// dances.
func (d *LocalDir) Watch(ctx context.Context) (<-chan Event, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("localdir: fsnotify: %w", err)
	}
	if err := addRecursive(w, d.root); err != nil {
		_ = w.Close()
		return nil, err
	}
	d.mu.Lock()
	d.watcher = w
	d.mu.Unlock()

	out := make(chan Event, 32)
	go d.watchLoop(ctx, w, out)
	return out, nil
}

func (d *LocalDir) watchLoop(ctx context.Context, w *fsnotify.Watcher, out chan<- Event) {
	defer close(out)
	defer w.Close()
	const debounce = 200 * time.Millisecond
	pending := make(map[string]*time.Timer)
	var mu sync.Mutex

	flush := func(packLoc string) {
		mu.Lock()
		delete(pending, packLoc)
		mu.Unlock()
		ref := d.refForLoc(packLoc)
		select {
		case out <- Event{Kind: EventUpdated, Ref: ref}:
		default:
			d.log.Warn("localdir watch: dropping event (channel full)", "pack", packLoc)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			d.log.Warn("localdir watch error", "err", err)
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			packLoc := d.packForPath(ev.Name)
			if packLoc == "" {
				continue
			}
			// New directory added under root: register it for watching.
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = w.Add(ev.Name)
				}
			}
			mu.Lock()
			if t, exists := pending[packLoc]; exists {
				t.Stop()
			}
			loc := packLoc
			pending[packLoc] = time.AfterFunc(debounce, func() { flush(loc) })
			mu.Unlock()
		}
	}
}

func (d *LocalDir) refForLoc(loc string) Ref {
	rel, _ := filepath.Rel(d.root, loc)
	parts := strings.Split(filepath.ToSlash(rel), "/")
	id := loc
	if len(parts) >= 2 {
		id = parts[0] + "." + parts[1]
	}
	return Ref{ID: id, Source: d.Name(), Loc: loc}
}

// packForPath returns the pack root (directly under {root}/<ns>/<name>)
// containing path, or "" when path is outside any known pack.
func (d *LocalDir) packForPath(path string) string {
	rel, err := filepath.Rel(d.root, path)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return ""
	}
	return filepath.Join(d.root, parts[0], parts[1])
}

func (d *LocalDir) guardLoc(loc string) error {
	if loc == "" {
		return fmt.Errorf("localdir: empty loc")
	}
	if !strings.HasPrefix(loc, d.root+string(os.PathSeparator)) && loc != d.root {
		return fmt.Errorf("localdir: loc %q outside root %q", loc, d.root)
	}
	return nil
}

func detectMIME(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md":
		return "text/markdown"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".json":
		return "application/json"
	case ".html", ".htm":
		return "text/html"
	case ".txt":
		return "text/plain"
	}
	if mt := mime.TypeByExtension(ext); mt != "" {
		return mt
	}
	return "application/octet-stream"
}

func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}
