package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/hurtener/Portico_gateway/internal/apps"
	auditpkg "github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/jwt"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/config"
	porticohttp "github.com/hurtener/Portico_gateway/internal/mcp/northbound/http"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/runtime/process"
	"github.com/hurtener/Portico_gateway/internal/secrets"
	"github.com/hurtener/Portico_gateway/internal/secrets/inject"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	skillloader "github.com/hurtener/Portico_gateway/internal/skills/loader"
	skillruntime "github.com/hurtener/Portico_gateway/internal/skills/runtime"
	skillsource "github.com/hurtener/Portico_gateway/internal/skills/source"
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

	// Phase 5: audit store, policy engine, approval flow, credential
	// injectors, server-initiated requester. Wired BEFORE the skills
	// manager builds so the engine can reference the catalog when it
	// lands.
	rawDB, err := rawSQLFromBackend(backend)
	if err != nil {
		return fmt.Errorf("phase-5 audit store: %w", err)
	}
	auditStore := auditpkg.NewStore(rawDB, logger.With("component", "audit"))
	auditStore.Start()

	auditEmitter := auditpkg.NewFanoutEmitter(auditpkg.SlogEmitter{Log: logger.With("component", "audit.fanout")}, auditStore)

	approvalStorage := approval.NewStorageAdapter(backend.Approvals())
	serverInit := porticohttp.NewServerInitiatedRequester(sessions)
	approvalFlow := approval.New(approvalStorage,
		serverInitSenderAdapter{r: serverInit},
		sessionLookupAdapter{sessions: sessions},
		auditEmitter,
		logger.With("component", "approval"))

	injectorRegistry := inject.NewRegistry()
	if vault != nil {
		injectorRegistry.Register(inject.NewEnvInjector(vault))
		injectorRegistry.Register(inject.NewHTTPHeaderInjector(vault))
		injectorRegistry.Register(inject.NewSecretRefInjector(vault))
	}
	injectorRegistry.Register(inject.NewShimInjector())
	// OAuth injectors are constructed per-server (each has its own IdP).
	// Wire them via the registry's seedRegistry hook below — for now we
	// register a default single-exchanger injector that the dispatcher
	// consults; a richer per-server lookup ships in a follow-up.

	// Phase 4: skills runtime. Sources are constructed from cfg.Skills.
	// The provider plugs into the resource + prompt aggregators; the
	// index generator surfaces the per-tenant catalog.
	skillsMgr, skillSources, err := buildSkillsManager(ctx, cfg, reg, logger, backend)
	if err != nil {
		logger.Warn("skills runtime disabled", "err", err)
	}
	if skillsMgr != nil {
		resourceAgg.SetSkillProvider(skillsMgr.Provider(), nil)
		// Drop the index cache when the catalog mutates so the next
		// _index render reflects new state. The skills Manager already
		// invalidates on its own change paths; this hook is defensive.
		go func() {
			ch := skillsMgr.Catalog().Subscribe()
			for range ch {
				skillsMgr.IndexGenerator().InvalidateAll()
			}
		}()
		if err := skillsMgr.Start(ctx, skillSources); err != nil {
			logger.Warn("skills manager start failed", "err", err)
		}
	}

	// Phase 5: now the catalog is alive, build the policy engine and
	// install the pipeline + audit emitter on the dispatcher.
	var policyCatalog *skillruntime.Catalog
	var policyEnable *skillruntime.Enablement
	if skillsMgr != nil {
		policyCatalog = skillsMgr.Catalog()
		policyEnable = skillsMgr.Enablement()
	}
	policyEngine := policy.New(manager, policyCatalog, policyEnable, nil, policy.EngineConfig{
		DefaultRiskClass:       policy.RiskWrite,
		DefaultApprovalTimeout: 5 * time.Minute,
		Logger:                 logger.With("component", "policy"),
	})
	pipeline := mcpgw.NewPolicyPipeline(mcpgw.PipelineConfig{
		Engine:    policyEngine,
		Approvals: approvalFlow,
		Injectors: injectorRegistry,
		Emitter:   auditEmitter,
		Registry:  reg,
		Logger:    logger.With("component", "policy_pipeline"),
	})
	dispatcher.SetPolicyPipeline(pipeline)
	dispatcher.SetAuditEmitter(auditEmitter)

	// Phase 6: snapshot service + lazy session→snapshot binder + drift
	// detector. The probe reads from the same southbound manager the
	// dispatcher already holds, so snapshots reflect exactly what the
	// session would see live.
	snapshotProbe := mcpgw.NewSnapshotProbe(manager, reg, skillsMgr,
		snapshotEnablement(skillsMgr),
		policyEngine,
		nil, // tenant policy resolver — Phase 5 wires this when configured.
	)
	snapshotService := snapshots.NewService(
		snapshots.NewStorageAdapter(backend.Snapshots()),
		snapshotProbe,
		auditEmitter,
		logger.With("component", "snapshots"),
	)
	snapshotBinder := mcpgw.NewSnapshotBinder(snapshotService)
	dispatcher.SetSnapshotBinder(snapshotBinder)
	sessions.OnClose(snapshotBinder.Forget)
	sessions.OnClose(func(sid string) {
		_ = backend.Snapshots().CloseSession(context.Background(), sid)
	})

	driftProbe := newSnapshotsDriftAdapter(snapshotProbe)
	driftDetector := snapshots.NewDetector(snapshotService, driftProbe,
		logger.With("component", "snapshots.drift"),
		cfg.Telemetry.DriftInterval.Duration())
	driftDetector.Start(ctx)

	otelShutdown, err := telemetry.Init(ctx, telemetry.Config{
		Enabled:       cfg.Telemetry.Enabled,
		ServiceName:   cfg.Telemetry.ServiceName,
		Exporter:      cfg.Telemetry.Exporter,
		OTLPEndpoint:  cfg.Telemetry.OTLPEndpoint,
		OTLPHeaders:   cfg.Telemetry.OTLPHeaders,
		SampleRate:    cfg.Telemetry.SampleRate,
		ResourceAttrs: cfg.Telemetry.ResourceAttrs,
	}, logger.With("component", "telemetry"))
	if err != nil {
		logger.Warn("telemetry init failed; tracing disabled", "err", err)
		otelShutdown = telemetry.NopShutdown
	}

	// Sweep expired approvals every minute. The flow keeps the in-memory
	// row consistent; the sweep flips persisted rows that were never
	// resolved manually after a fallback.
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := approvalFlow.Sweep(context.Background()); err != nil {
					logger.Warn("approval sweep failed", "err", err)
				}
			}
		}
	}()

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
		Skills:         skillsMgr,
		Approvals:      backend.Approvals(),
		ApprovalFlow:   api.NewApprovalFlowAdapter(approvalFlowResolverFor(approvalFlow)),
		Vault:          vault,
		ServerInit:     serverInit,
		Snapshots:      snapshotService,
		SnapshotBinder: snapshotBinder,
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
	serverInit.Stop()
	driftDetector.Stop()
	auditStore.Stop()
	if otelShutdown != nil {
		_ = otelShutdown(shutdownCtx)
	}
	return nil
}

// snapshotEnablement returns the skills runtime's enablement when the
// skills manager is configured; nil otherwise.
func snapshotEnablement(m *skillruntime.Manager) *skillruntime.Enablement {
	if m == nil {
		return nil
	}
	return m.Enablement()
}

// newSnapshotsDriftAdapter wraps the SnapshotProbe so it satisfies the
// drift detector's narrower LiveProbe interface (which doesn't carry
// session context). The drift detector probes per-tenant, not
// per-session, so we collapse the session axis.
func newSnapshotsDriftAdapter(p *mcpgw.SnapshotProbe) snapshots.LiveProbe {
	return &driftAdapter{p: p}
}

type driftAdapter struct {
	p *mcpgw.SnapshotProbe
}

func (a *driftAdapter) ListTools(ctx context.Context, tenantID string) (map[string][]protocol.Tool, error) {
	tools, err := a.p.ListTools(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}
	out := make(map[string][]protocol.Tool)
	for _, t := range tools {
		out[t.ServerID] = append(out[t.ServerID], t.Tool)
	}
	return out, nil
}

// buildSkillsManager wires the skills runtime from cfg.Skills.
// Returns (nil, nil, nil) when no sources are configured — the gateway
// runs with skills disabled in that case.
func buildSkillsManager(_ context.Context, cfg *config.Config, reg *registry.Registry, logger *slog.Logger, backend ifaces.Backend) (*skillruntime.Manager, []skillsource.Source, error) {
	if len(cfg.Skills.Sources) == 0 {
		return nil, nil, nil
	}
	srcs := make([]skillsource.Source, 0, len(cfg.Skills.Sources))
	for _, s := range cfg.Skills.Sources {
		switch s.Type {
		case "local":
			lo, err := skillsource.NewLocalDir(s.Path, logger.With("component", "skills.localdir"))
			if err != nil {
				return nil, nil, fmt.Errorf("skill source local %q: %w", s.Path, err)
			}
			srcs = append(srcs, lo)
		default:
			return nil, nil, fmt.Errorf("skill source type %q not supported in V1", s.Type)
		}
	}
	loaderInst, err := skillloader.New(srcs, reg, logger.With("component", "skills.loader"))
	if err != nil {
		return nil, nil, fmt.Errorf("skill loader: %w", err)
	}
	mode := skillruntime.ModeOptIn
	if cfg.Skills.EnablementDefault == string(skillruntime.ModeAuto) {
		mode = skillruntime.ModeAuto
	}
	enablement := skillruntime.NewEnablement(backend.Skills(), mode)

	// The annotator closure consults the manager's catalog (created
	// inside NewManager). We declare a holder, build the manager, then
	// the closure resolves through the holder safely after construction.
	var mgrHolder *skillruntime.Manager
	annotate := func(ctx context.Context, tenantID, skillID string) ([]string, []string) {
		if mgrHolder == nil {
			return nil, nil
		}
		s, ok := mgrHolder.Catalog().Get(skillID)
		if !ok {
			return nil, nil
		}
		missing := loaderInst.AnnotateMissingTools(ctx, tenantID, s.Manifest)
		return missing, nil
	}
	mgr := skillruntime.NewManager(loaderInst, enablement,
		func(string) (string, []string) { return "", []string{"*"} },
		annotate,
		logger.With("component", "skills.manager"))
	mgrHolder = mgr
	return mgr, srcs, nil
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
	if c.Auth != nil {
		out.Auth = &registry.AuthSpec{
			Strategy:         c.Auth.Strategy,
			DefaultRiskClass: c.Auth.DefaultRiskClass,
			Env:              append([]string(nil), c.Auth.Env...),
			Headers:          copyStringMap(c.Auth.Headers),
			SecretRef:        c.Auth.SecretRef,
		}
		if c.Auth.Exchange != nil {
			out.Auth.Exchange = &registry.OAuthExchangeSpec{
				TokenURL:        c.Auth.Exchange.TokenURL,
				ClientID:        c.Auth.Exchange.ClientID,
				ClientSecretRef: c.Auth.Exchange.ClientSecretRef,
				Audience:        c.Auth.Exchange.Audience,
				Scope:           c.Auth.Exchange.Scope,
				GrantType:       c.Auth.Exchange.GrantType,
				SubjectTokenSrc: c.Auth.Exchange.SubjectTokenSrc,
			}
		}
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
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
