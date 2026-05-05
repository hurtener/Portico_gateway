package config

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ChangeHandler receives the previous and newly-validated config when the file
// changes. It returns an error if the new config cannot be applied; the watcher
// keeps the previous config in that case.
type ChangeHandler func(old, new *Config) error

// Watcher reloads portico.yaml on edit. Phase 0 supports a small set of fields
// (tenants, logging.level); other field changes are logged and require restart.
// Phase 2+ extends the list.
type Watcher struct {
	path    string
	current atomic[Config]
	handler ChangeHandler
	log     *slog.Logger

	debounceMu sync.Mutex
	debounce   *time.Timer
}

// atomic[T] is a tiny generic wrapper around sync.RWMutex for storing a
// pointer that gets swapped atomically. Avoids the import of sync/atomic.Value
// and the type assertion noise.
type atomic[T any] struct {
	mu sync.RWMutex
	v  *T
}

func (a *atomic[T]) Store(v *T) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v = v
}

func (a *atomic[T]) Load() *T {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.v
}

// NewWatcher returns a Watcher that watches `path` and calls handler on each
// successful reload. The initial cfg is stored as "current" and not handed back
// to handler.
func NewWatcher(path string, current *Config, handler ChangeHandler, log *slog.Logger) (*Watcher, error) {
	if log == nil {
		log = slog.Default()
	}
	if path == "" {
		return nil, errors.New("config watcher: path is required")
	}
	w := &Watcher{
		path:    path,
		handler: handler,
		log:     log,
	}
	w.current.Store(current)
	return w, nil
}

// Start blocks until ctx is cancelled or the underlying fsnotify watcher errors.
func (w *Watcher) Start(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	dir := filepath.Dir(w.path)
	if err := fsw.Add(dir); err != nil {
		return err
	}

	w.log.Info("config watcher started", "path", w.path, "dir", dir)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if filepath.Clean(ev.Name) != filepath.Clean(w.path) {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			w.scheduleReload()
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.log.Warn("config watcher error", "err", err)
		}
	}
}

// scheduleReload coalesces editor-burst events using a 200ms debounce.
func (w *Watcher) scheduleReload() {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.debounce = time.AfterFunc(200*time.Millisecond, w.reload)
}

func (w *Watcher) reload() {
	old := w.current.Load()
	newCfg, err := Load(w.path)
	if err != nil {
		w.log.Warn("config reload failed; keeping previous", "err", err)
		return
	}
	if w.handler != nil {
		if err := w.handler(old, newCfg); err != nil {
			w.log.Warn("config change handler rejected; keeping previous", "err", err)
			return
		}
	}
	w.current.Store(newCfg)
	w.log.Info("config reloaded", "path", w.path)
}

// Current returns the most recently applied config. Safe for concurrent use.
func (w *Watcher) Current() *Config {
	return w.current.Load()
}
