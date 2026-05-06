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
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
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

	// Phase 1: build the MCP gateway components. The southbound Manager is
	// initialised with the configured server specs; clients are constructed
	// lazily on first use.
	manager := southboundmgr.NewManager(cfg.Servers, logger)
	dispatcher := mcpgw.NewDispatcher(manager, logger)
	sessions := mcpgw.NewSessionRegistry()

	// Phase 2: registry over the storage backend. Persists ServerSpecs
	// per-tenant and broadcasts change events that the supervisor (also
	// added in Phase 2) consumes.
	reg := registry.New(backend.Registry(), logger)
	if err := seedRegistryFromConfig(ctx, reg, cfg, logger); err != nil {
		return err
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
		Sessions:    sessions,
		Dispatcher:  dispatcher,
		Manager:     manager,
		Registry:    reg,
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
	// Drain MCP sessions and downstream clients.
	sessions.CloseAll()
	if err := manager.CloseAll(shutdownCtx); err != nil {
		logger.Warn("southbound shutdown errors", "err", err)
	}
	reg.CloseAll()
	return nil
}

// seedRegistryFromConfig materialises every server in the YAML config into
// the registry under each known tenant. Phase 2 ships a per-tenant view
// from a single global config block; per-tenant overrides arrive in a
// follow-up. Skips servers that fail validation with a warn.
func seedRegistryFromConfig(ctx context.Context, reg *registry.Registry, cfg *config.Config, log interface {
	Warn(msg string, args ...any)
}) error {
	if len(cfg.Servers) == 0 {
		return nil
	}
	tenantIDs := make([]string, 0, len(cfg.Tenants))
	for _, t := range cfg.Tenants {
		tenantIDs = append(tenantIDs, t.ID)
	}
	if cfg.IsDevMode() {
		dev := devTenantOrEnv(true)
		if dev != "" {
			tenantIDs = append(tenantIDs, dev)
		}
	}
	if len(tenantIDs) == 0 {
		return nil
	}
	for _, ts := range cfg.Servers {
		spec := configServerSpecToRegistry(ts)
		for _, tid := range tenantIDs {
			if _, err := reg.Upsert(ctx, tid, spec); err != nil {
				log.Warn("registry: failed to seed server",
					"tenant_id", tid, "server_id", spec.ID, "err", err)
			}
		}
	}
	return nil
}

func configServerSpecToRegistry(c config.ServerSpec) *registry.ServerSpec {
	out := &registry.ServerSpec{
		ID:          c.ID,
		DisplayName: c.DisplayName,
		Transport:   c.Transport,
		RuntimeMode: c.RuntimeMode,
	}
	if c.Stdio != nil {
		out.Stdio = &registry.StdioSpec{
			Command:      c.Stdio.Command,
			Args:         append([]string(nil), c.Stdio.Args...),
			Env:          append([]string(nil), c.Stdio.Env...),
			Cwd:          c.Stdio.Cwd,
			StartTimeout: registry.Duration(c.StartTimeout),
		}
	}
	if c.HTTP != nil {
		out.HTTP = &registry.HTTPSpec{
			URL:        c.HTTP.URL,
			AuthHeader: c.HTTP.AuthHeader,
			Timeout:    registry.Duration(c.HTTP.Timeout),
		}
	}
	return out
}

// ensureDataDir extracts a directory from a SQLite DSN like
// "file:/var/lib/portico/portico.db?cache=shared" and creates it if absent.
func ensureDataDir(dsn string, logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}) error {
	path := sqlitePathFromDSN(dsn)
	if path == ":memory:" {
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

func sqlitePathFromDSN(dsn string) string {
	const prefix = "file:"
	s := dsn
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		s = s[len(prefix):]
	}
	for i, r := range s {
		if r == '?' {
			return s[:i]
		}
	}
	return s
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
