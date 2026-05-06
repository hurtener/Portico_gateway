package config_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/config"
)

const baseYAML = `
server:
  bind: 127.0.0.1:8080
storage:
  driver: sqlite
  dsn: ":memory:"
logging:
  level: info
  format: json
servers:
%s
`

func writeYAML(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWatcher_TriggersHandlerOnEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portico.yaml")
	initial := []byte(`server:
  bind: 127.0.0.1:8080
storage:
  driver: sqlite
  dsn: ":memory:"
logging:
  level: info
  format: json
`)
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	calls := atomic.Int32{}
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	w, err := config.NewWatcher(path, cfg, func(_, newCfg *config.Config) error {
		if newCfg == nil {
			t.Errorf("nil new config in handler")
		}
		calls.Add(1)
		return nil
	}, logger)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = w.Start(ctx) }()

	// Give the watcher a moment to register the directory.
	time.Sleep(120 * time.Millisecond)

	// Edit: change logging level.
	updated := []byte(`server:
  bind: 127.0.0.1:8080
storage:
  driver: sqlite
  dsn: ":memory:"
logging:
  level: debug
  format: json
`)
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatalf("handler never invoked after edit")
	}
	if got := w.Current().Logging.Level; got != "debug" {
		t.Errorf("Current().Logging.Level = %q, want debug", got)
	}
}

func TestWatcher_KeepsPreviousOnInvalidEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portico.yaml")
	good := []byte(`server:
  bind: 127.0.0.1:8080
storage:
  driver: sqlite
  dsn: ":memory:"
logging:
  level: info
  format: json
`)
	if err := os.WriteFile(path, good, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	w, err := config.NewWatcher(path, cfg, func(_, _ *config.Config) error { return nil }, logger)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = w.Start(ctx) }()
	time.Sleep(120 * time.Millisecond)

	// Write an invalid YAML.
	if err := os.WriteFile(path, []byte("not: [valid yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)

	// Current should still match the original config.
	if w.Current().Logging.Level != "info" {
		t.Errorf("Current() should still hold info-level config after parse failure")
	}
}
