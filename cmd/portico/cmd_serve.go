package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/hurtener/Portico_gateway/internal/auth/jwt"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	// Side-effect: register the sqlite driver. Future drivers register here.
	_ "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
	"github.com/hurtener/Portico_gateway/internal/telemetry"
)

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to portico.yaml (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return errors.New("serve: --config is required")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	return runWithConfig(ctx, cfg)
}

// runWithConfig is shared by serve/dev. Owns the full boot sequence:
// logger, storage, auth validator, HTTP server, graceful shutdown.
func runWithConfig(ctx context.Context, cfg *config.Config) error {
	logger := telemetry.NewLogger(telemetry.LoggerConfig{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
	}, os.Stderr)

	logger.Info("portico boot",
		"version", version,
		"commit", buildCommit,
		"bind", cfg.Server.Bind,
		"dev_mode", cfg.IsDevMode(),
		"storage_driver", cfg.Storage.Driver,
		"tenants_configured", len(cfg.Tenants),
	)

	if err := ensureDataDir(cfg.Storage.DSN, logger); err != nil {
		return err
	}

	backend, err := storage.Open(ctx, cfg.Storage, logger)
	if err != nil {
		return err
	}
	defer func() { _ = backend.Close() }()

	tenants := backend.Tenants()
	audit := backend.Audit()

	// Materialize tenants from config so the auth middleware can find them.
	for _, t := range cfg.Tenants {
		err := tenants.Upsert(ctx, &ifaces.Tenant{
			ID:          t.ID,
			DisplayName: firstNonEmpty(t.DisplayName, t.ID),
			Plan:        firstNonEmpty(t.Plan, "free"),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		})
		if err != nil {
			return fmt.Errorf("seed tenant %q: %w", t.ID, err)
		}
	}

	var validator *jwt.Validator
	if !cfg.IsDevMode() {
		validator, err = jwt.NewValidator(ctx, cfg.Auth.JWT)
		if err != nil {
			return err
		}
	}

	deps := api.Deps{
		Logger:      logger,
		Validator:   validator,
		DevMode:     cfg.IsDevMode(),
		DevTenant:   devTenantOrEnv(cfg.IsDevMode()),
		Tenants:     tenants,
		Audit:       audit,
		Version:     version,
		BuildCommit: buildCommit,
	}

	handler := api.NewRouter(deps)
	srv := &http.Server{
		Addr:              cfg.Server.Bind,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "bind", cfg.Server.Bind, "tenant_id", deps.DevTenant)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown timeout", "err", err)
	}
	return nil
}

// ensureDataDir extracts a directory from a SQLite DSN like
// "file:/var/lib/portico/portico.db?cache=shared" and creates it if absent.
func ensureDataDir(dsn string, logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}) error {
	path, ok := sqlitePathFromDSN(dsn)
	if !ok || path == ":memory:" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." || dir == "/" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create data dir %q: %w", dir, err)
	}
	logger.Info("data dir ready", "dir", dir)
	return nil
}

func sqlitePathFromDSN(dsn string) (string, bool) {
	const prefix = "file:"
	s := dsn
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		s = s[len(prefix):]
	}
	for i, r := range s {
		if r == '?' {
			return s[:i], true
		}
	}
	return s, true
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func devTenantOrEnv(devMode bool) string {
	if !devMode {
		return ""
	}
	if v := os.Getenv("PORTICO_DEV_TENANT"); v != "" {
		return v
	}
	return "dev"
}
