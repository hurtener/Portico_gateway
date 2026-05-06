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

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/auth/jwt"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/runtime/process"
	"github.com/hurtener/Portico_gateway/internal/secrets"
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

	return runWithConfig(ctx, cfg, *configPath)
}

// runWithConfig is shared by serve/dev. Owns the full boot sequence:
// logger, storage, auth validator, HTTP server, graceful shutdown.
//
// configPath is non-empty only for `serve` (where the file is the source
// of truth and warrants a hot-reload watcher). `dev` synthesises a
// config in-memory and passes "".
//
// Linter note: this is a deliberate flat boot sequence — pulling the
// branches into helpers obscures the order without removing real
// complexity, so the function carries a gocyclo waiver.
//
//nolint:gocyclo
func runWithConfig(ctx context.Context, cfg *config.Config, configPath string) error {
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

	// Phase 2: registry over the storage backend. Persists ServerSpecs
	// per-tenant and broadcasts change events that the supervisor consumes.
	reg := registry.New(backend.Registry(), logger)
	seedRegistryFromConfig(ctx, reg, cfg, logger)

	// Phase 2: stub vault for {{secret:...}} env interpolation. Loaded
	// from PORTICO_VAULT_KEY (base64 32-byte AES-256 key) + a YAML file
	// at <data_dir>/vault.yaml. Missing key disables the vault — secret
	// references in stdio env then surface as start failures.
	var vault secrets.Vault
	if key, err := secrets.LoadKeyFromEnv(); err != nil {
		return err
	} else if key != nil {
		vaultPath := filepath.Join(filepath.Dir(deriveDataDir(cfg.Storage.DSN)), "vault.yaml")
		fv, err := secrets.NewFileVault(vaultPath, key)
		if err != nil {
			return fmt.Errorf("vault: %w", err)
		}
		vault = fv
	}

	// Phase 2: process supervisor. Holds southbound stdio + http clients
	// keyed by InstanceKey (tenant/user/session, per runtime mode).
	resolver := process.NewResolver(vault)
	supervisor := process.NewSupervisor(logger, resolver, reg)

	// Phase 1+2: MCP gateway components. The Manager is now a thin
	// coordinator over Registry + Supervisor — clients are constructed
	// lazily on first use via supervisor.Acquire.
	manager := southboundmgr.NewManager(reg, supervisor, logger)
	dispatcher := mcpgw.NewDispatcher(manager, logger)
	sessions := mcpgw.NewSessionRegistry()

	// Phase 3: resource + prompt aggregators, MCP Apps registry, list-
	// changed mux. The mux subscribes to every supervised client's
	// notifications stream via supervisor.SetNotifSink.
	appsReg := apps.New(apps.DefaultCSP())
	resourceAgg := mcpgw.NewResourceAggregator(manager, appsReg, mcpgw.DefaultResourceLimits(), logger)
	promptAgg := mcpgw.NewPromptAggregator(manager, resourceAgg, logger)
	listChangedMux := mcpgw.NewListChangedMux(sessions, resourceAgg, mcpgw.ModeStable, logger)
	dispatcher.SetAggregators(resourceAgg, promptAgg, listChangedMux)
	supervisor.SetNotifSink(func(ctx context.Context, serverID string, n protocol.Notification) {
		listChangedMux.OnDownstream(ctx, serverID, n)
	})
	sessions.OnClose(listChangedMux.ForgetSession)
	// Drop per-session caches on session termination so long-running
	// gateways don't leak memory in proportion to session churn.
	sessions.OnClose(dispatcher.InvalidateSession)

	// Subscribe the supervisor to registry change events so spec edits
	// (via /v1/servers/{id}/reload, hot-reload, or admin POST) drain the
	// affected instances.
	reactor := registry.NewReactor(reg, supervisor, logger)

	// Phase 2: optional config hot-reload. Only `serve --config` exposes
	// a file path; `dev` synthesises the config in memory.
	var watcher *config.Watcher
	if configPath != "" {
		w, err := config.NewWatcher(configPath, cfg, func(_, newCfg *config.Config) error {
			seedRegistryFromConfig(ctx, reg, newCfg, logger)
			return nil
		}, logger)
		if err != nil {
			logger.Warn("config watcher disabled", "err", err)
		} else {
			watcher = w
			go func() {
				if err := w.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
					logger.Warn("config watcher exited", "err", err)
				}
			}()
		}
	}
	_ = watcher // kept for future inspection; lifecycle owned by ctx

	deps := api.Deps{
		Logger:         logger,
		Validator:      validator,
		DevMode:        cfg.IsDevMode(),
		DevTenant:      devTenantOrEnv(cfg.IsDevMode()),
		Tenants:        tenants,
		Audit:          audit,
		Version:        version,
		BuildCommit:    buildCommit,
		Sessions:       sessions,
		Dispatcher:     dispatcher,
		Manager:        manager,
		Registry:       reg,
		Apps:           appsReg,
		AllowedOrigins: cfg.Server.AllowedOrigins,
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
	reactor.Stop()
	reg.CloseAll()
	return nil
}

// seedRegistryFromConfig materialises every server in the YAML config into
// the registry under each known tenant. Phase 2 ships a per-tenant view
// from a single global config block; per-tenant overrides arrive in a
// follow-up. Skips servers that fail validation with a warn (no fatal
// error path — operators expect dev-mode config quirks not to block
// boot).
func seedRegistryFromConfig(ctx context.Context, reg *registry.Registry, cfg *config.Config, log interface {
	Warn(msg string, args ...any)
}) {
	if len(cfg.Servers) == 0 {
		return
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
		return
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

// deriveDataDir extracts the data directory from a SQLite DSN. If the DSN
// is ":memory:" or otherwise lacks a path, returns the cwd.
func deriveDataDir(dsn string) string {
	path := sqlitePathFromDSN(dsn)
	if path == ":memory:" || path == "" {
		return "."
	}
	return path
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
