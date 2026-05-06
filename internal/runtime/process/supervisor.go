package process

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	httpclient "github.com/hurtener/Portico_gateway/internal/mcp/southbound/http"
	stdiocli "github.com/hurtener/Portico_gateway/internal/mcp/southbound/stdio"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/secrets"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Supervisor lifecycle states. Persisted on InstanceRecord.State.
const (
	StateStarting    = "starting"
	StateRunning     = "running"
	StateIdle        = "idle"
	StateStopping    = "stopping"
	StateCrashed     = "crashed"
	StateBackoff     = "backoff"
	StateCircuitOpen = "circuit_open"
)

// InstanceKey uniquely identifies a supervised instance. Resolved per
// runtime mode at Acquire time.
type InstanceKey struct {
	TenantID  string
	ServerID  string
	UserID    string
	SessionID string
}

func (k InstanceKey) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", k.TenantID, k.ServerID, k.UserID, k.SessionID)
}

// AcquireOpts carries the per-call identity needed to compute the
// InstanceKey. UserID + SessionID are optional; the supervisor zeroes them
// when the runtime mode does not require them.
type AcquireOpts struct {
	TenantID  string
	UserID    string
	SessionID string
}

// KeyForMode returns the InstanceKey for the given mode + identity. Errors
// when a required identity field is missing (e.g. per_session without
// SessionID).
func KeyForMode(mode string, serverID string, opts AcquireOpts) (InstanceKey, error) {
	switch mode {
	case registry.ModeSharedGlobal, registry.ModeRemoteStatic:
		return InstanceKey{ServerID: serverID, TenantID: "_global"}, nil
	case registry.ModePerTenant:
		if opts.TenantID == "" {
			return InstanceKey{}, errors.New("supervisor: per_tenant requires tenant id")
		}
		return InstanceKey{ServerID: serverID, TenantID: opts.TenantID}, nil
	case registry.ModePerUser:
		if opts.TenantID == "" || opts.UserID == "" {
			return InstanceKey{}, errors.New("supervisor: per_user requires tenant and user id")
		}
		return InstanceKey{ServerID: serverID, TenantID: opts.TenantID, UserID: opts.UserID}, nil
	case registry.ModePerSession:
		if opts.TenantID == "" || opts.SessionID == "" {
			return InstanceKey{}, errors.New("supervisor: per_session requires tenant and session id")
		}
		return InstanceKey{ServerID: serverID, TenantID: opts.TenantID, UserID: opts.UserID, SessionID: opts.SessionID}, nil
	default:
		return InstanceKey{}, fmt.Errorf("supervisor: unsupported runtime mode %q", mode)
	}
}

// instance holds the live state for one supervised entry.
type instance struct {
	id       string
	key      InstanceKey
	spec     *registry.ServerSpec
	tenantID string // the *real* tenant id used for env interpolation, even when key.TenantID == "_global"

	mu sync.Mutex

	state        string
	client       southbound.Client
	startedAt    time.Time
	lastCallAt   time.Time
	lastError    string
	restartCount int

	circuitUntil time.Time

	idle *IdleTimer
}

// Supervisor owns the southbound client lifecycle for stdio/http servers.
// It serializes Acquire operations per InstanceKey so concurrent callers
// share a single startup attempt instead of spawning duplicates.
type Supervisor struct {
	log      *slog.Logger
	resolver *Resolver
	registry *registry.Registry

	mu        sync.Mutex
	instances map[InstanceKey]*instance
	starting  map[InstanceKey]chan struct{}
}

// NewSupervisor constructs a Supervisor. resolver must be non-nil — pass a
// resolver with a nil vault to disable secret interpolation.
func NewSupervisor(log *slog.Logger, resolver *Resolver, reg *registry.Registry) *Supervisor {
	if log == nil {
		log = slog.Default()
	}
	if resolver == nil {
		resolver = NewResolver(nil)
	}
	return &Supervisor{
		log:       log,
		resolver:  resolver,
		registry:  reg,
		instances: make(map[InstanceKey]*instance),
		starting:  make(map[InstanceKey]chan struct{}),
	}
}

// Acquire returns a started client for the given (server spec, identity).
// Lazily spawns the underlying stdio/http client on first call.
//
// Per-server initialisation is serialised: if two callers race to start
// the same instance, the second waits on the first via a per-key channel
// instead of spawning a duplicate.
func (s *Supervisor) Acquire(ctx context.Context, spec *registry.ServerSpec, opts AcquireOpts) (southbound.Client, error) {
	if spec == nil {
		return nil, errors.New("supervisor: nil spec")
	}
	key, err := KeyForMode(spec.RuntimeMode, spec.ID, opts)
	if err != nil {
		return nil, err
	}

	for {
		s.mu.Lock()
		inst, exists := s.instances[key]
		if exists {
			s.mu.Unlock()
			ready, err := s.ensureRunning(ctx, inst, opts.TenantID)
			if err != nil {
				return nil, err
			}
			return ready, nil
		}
		// Not in the map: check whether another caller is already starting it.
		if waitCh, starting := s.starting[key]; starting {
			s.mu.Unlock()
			select {
			case <-waitCh:
				// retry the loop — by now the other caller has either
				// installed an instance or failed.
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		// Claim the slot.
		waitCh := make(chan struct{})
		s.starting[key] = waitCh
		s.mu.Unlock()

		client, startErr := s.startNew(ctx, key, spec, opts.TenantID)

		s.mu.Lock()
		delete(s.starting, key)
		close(waitCh)
		s.mu.Unlock()
		if startErr != nil {
			return nil, startErr
		}
		return client, nil
	}
}

// ensureRunning returns the client for a known instance, lazily restarting
// it if it has fallen idle or crashed (subject to circuit breaker / backoff).
func (s *Supervisor) ensureRunning(ctx context.Context, inst *instance, callerTenantID string) (southbound.Client, error) {
	inst.mu.Lock()
	state := inst.state
	circuitUntil := inst.circuitUntil
	client := inst.client
	inst.mu.Unlock()

	switch state {
	case StateRunning:
		s.tickIdle(inst)
		return client, nil
	case StateCircuitOpen:
		if time.Now().Before(circuitUntil) {
			return nil, fmt.Errorf("supervisor: %s: circuit open until %s", inst.spec.ID, circuitUntil.Format(time.RFC3339))
		}
		// circuit window elapsed; allow a fresh attempt.
		fallthrough
	case StateIdle, StateCrashed, StateBackoff, StateStopping:
		// Lazy restart.
		client, err := s.restart(ctx, inst, callerTenantID)
		if err != nil {
			return nil, err
		}
		return client, nil
	default:
		return nil, fmt.Errorf("supervisor: %s: unexpected state %q", inst.spec.ID, state)
	}
}

func (s *Supervisor) startNew(ctx context.Context, key InstanceKey, spec *registry.ServerSpec, tenantID string) (southbound.Client, error) {
	inst := &instance{
		id:       newInstanceID(),
		key:      key,
		spec:     spec,
		tenantID: tenantID,
		state:    StateStarting,
	}
	if spec.Lifecycle.IdleTimeout > 0 {
		// IdleTimer fires from its own goroutine after a deadline, with no
		// caller context to inherit; markIdle uses context.Background.
		inst.idle = NewIdleTimer(spec.Lifecycle.IdleTimeout.Std(), func() { s.markIdle(inst) }) //nolint:contextcheck
	}

	client, err := s.spawnClient(ctx, inst)
	if err != nil {
		inst.state = StateCrashed
		inst.lastError = err.Error()
		s.persistInstance(ctx, inst, err)
		return nil, err
	}

	inst.mu.Lock()
	inst.client = client
	inst.state = StateRunning
	inst.startedAt = time.Now().UTC()
	inst.mu.Unlock()
	if inst.idle != nil {
		inst.idle.Start()
	}

	s.mu.Lock()
	s.instances[key] = inst
	s.mu.Unlock()
	s.persistInstance(ctx, inst, nil)
	s.publishStatus(ctx, spec, registry.StatusHealthy, "")
	return client, nil
}

func (s *Supervisor) restart(ctx context.Context, inst *instance, callerTenantID string) (southbound.Client, error) {
	inst.mu.Lock()
	if inst.client != nil {
		_ = inst.client.Close(ctx)
		inst.client = nil
	}
	inst.state = StateStarting
	if callerTenantID != "" {
		inst.tenantID = callerTenantID
	}
	inst.mu.Unlock()

	client, err := s.spawnClient(ctx, inst)
	if err != nil {
		inst.mu.Lock()
		inst.restartCount++
		inst.lastError = err.Error()
		max := inst.spec.Lifecycle.MaxRestartAttempts
		if max > 0 && inst.restartCount >= max {
			inst.state = StateCircuitOpen
			inst.circuitUntil = time.Now().Add(inst.spec.Lifecycle.CircuitOpenDuration.Std())
			s.publishStatus(ctx, inst.spec, registry.StatusCircuitOpen, err.Error())
		} else {
			inst.state = StateBackoff
			s.publishStatus(ctx, inst.spec, registry.StatusUnhealthy, err.Error())
		}
		inst.mu.Unlock()
		s.persistInstance(ctx, inst, err)
		return nil, err
	}

	inst.mu.Lock()
	inst.client = client
	inst.state = StateRunning
	inst.startedAt = time.Now().UTC()
	inst.restartCount = 0
	inst.lastError = ""
	inst.mu.Unlock()
	if inst.idle != nil {
		inst.idle.Start()
	}
	s.persistInstance(ctx, inst, nil)
	s.publishStatus(ctx, inst.spec, registry.StatusHealthy, "")
	return client, nil
}

func (s *Supervisor) spawnClient(ctx context.Context, inst *instance) (southbound.Client, error) {
	switch inst.spec.Transport {
	case "stdio":
		return s.spawnStdio(ctx, inst)
	case "http":
		return s.spawnHTTP(ctx, inst)
	default:
		return nil, fmt.Errorf("supervisor: unsupported transport %q", inst.spec.Transport)
	}
}

func (s *Supervisor) spawnStdio(ctx context.Context, inst *instance) (southbound.Client, error) {
	if inst.spec.Stdio == nil || inst.spec.Stdio.Command == "" {
		return nil, fmt.Errorf("supervisor: %s: stdio.command required", inst.spec.ID)
	}
	env := append([]string(nil), inst.spec.Stdio.Env...)
	if len(env) > 0 {
		resolved, err := s.resolver.Resolve(ctx, inst.tenantID, env)
		if err != nil {
			return nil, fmt.Errorf("supervisor: %s: %w", inst.spec.ID, err)
		}
		env = resolved
	}
	c := stdiocli.New(stdiocli.Config{
		ServerID:     inst.spec.ID,
		Command:      inst.spec.Stdio.Command,
		Args:         append([]string(nil), inst.spec.Stdio.Args...),
		Env:          env,
		Cwd:          inst.spec.Stdio.Cwd,
		StartTimeout: inst.spec.Stdio.StartTimeout.Std(),
		Logger:       s.log,
	})
	if err := c.Start(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Supervisor) spawnHTTP(ctx context.Context, inst *instance) (southbound.Client, error) {
	if inst.spec.HTTP == nil || inst.spec.HTTP.URL == "" {
		return nil, fmt.Errorf("supervisor: %s: http.url required", inst.spec.ID)
	}
	c := httpclient.New(httpclient.Config{
		ServerID:   inst.spec.ID,
		URL:        inst.spec.HTTP.URL,
		AuthHeader: inst.spec.HTTP.AuthHeader,
		Timeout:    inst.spec.HTTP.Timeout.Std(),
		Logger:     s.log,
	})
	if err := c.Start(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// markIdle handles the IdleTimer firing: closes the client, transitions the
// instance to idle. Next Acquire restarts it lazily.
func (s *Supervisor) markIdle(inst *instance) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.state != StateRunning {
		return
	}
	inst.state = StateIdle
	if inst.client != nil {
		_ = inst.client.Close(context.Background())
		inst.client = nil
	}
	s.publishStatus(context.Background(), inst.spec, registry.StatusUnknown, "idle")
}

// tickIdle pings the idle timer when the instance services a real call.
func (s *Supervisor) tickIdle(inst *instance) {
	inst.mu.Lock()
	last := time.Now().UTC()
	inst.lastCallAt = last
	timer := inst.idle
	inst.mu.Unlock()
	if timer != nil {
		timer.Tick()
	}
}

// Stop terminates a single instance.
func (s *Supervisor) Stop(ctx context.Context, key InstanceKey) error {
	s.mu.Lock()
	inst, ok := s.instances[key]
	if ok {
		delete(s.instances, key)
	}
	s.mu.Unlock()
	if !ok {
		return ifaces.ErrNotFound
	}
	inst.mu.Lock()
	if inst.idle != nil {
		inst.idle.Stop()
	}
	inst.state = StateStopping
	client := inst.client
	inst.client = nil
	inst.mu.Unlock()
	if client != nil {
		_ = client.Close(ctx)
	}
	if s.registry != nil {
		_ = s.registry.DeleteInstance(ctx, inst.id)
	}
	return nil
}

// StopAll terminates every supervised instance. Used on server shutdown.
func (s *Supervisor) StopAll(ctx context.Context) error {
	s.mu.Lock()
	all := s.instances
	s.instances = make(map[InstanceKey]*instance)
	s.mu.Unlock()
	var errs []error
	for _, inst := range all {
		inst.mu.Lock()
		if inst.idle != nil {
			inst.idle.Stop()
		}
		client := inst.client
		inst.client = nil
		inst.mu.Unlock()
		if client != nil {
			if err := client.Close(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if s.registry != nil {
			_ = s.registry.DeleteInstance(ctx, inst.id)
		}
	}
	return errors.Join(errs...)
}

// Tick reports a successful call against the supervisor — used by the
// Manager so the supervisor can update LastCallAt on the registry record.
func (s *Supervisor) Tick(ctx context.Context, key InstanceKey) {
	s.mu.Lock()
	inst, ok := s.instances[key]
	s.mu.Unlock()
	if !ok {
		return
	}
	s.tickIdle(inst)
	s.persistInstance(ctx, inst, nil)
}

// persistInstance writes the instance's bookkeeping to the registry store.
// Best-effort: errors are logged but do not propagate to the caller.
func (s *Supervisor) persistInstance(ctx context.Context, inst *instance, lastErr error) {
	if s.registry == nil {
		return
	}
	inst.mu.Lock()
	rec := &ifaces.InstanceRecord{
		ID:           inst.id,
		TenantID:     inst.key.TenantID,
		ServerID:     inst.key.ServerID,
		UserID:       inst.key.UserID,
		SessionID:    inst.key.SessionID,
		StartedAt:    inst.startedAt,
		LastCallAt:   inst.lastCallAt,
		State:        inst.state,
		RestartCount: inst.restartCount,
	}
	if lastErr != nil {
		rec.LastError = lastErr.Error()
	} else {
		rec.LastError = inst.lastError
	}
	inst.mu.Unlock()
	if err := s.registry.UpsertInstance(ctx, rec); err != nil {
		s.log.Warn("supervisor: persist instance", "err", err)
	}
}

func (s *Supervisor) publishStatus(ctx context.Context, spec *registry.ServerSpec, status, detail string) {
	if s.registry == nil {
		return
	}
	// We need a tenant id; for shared_global / remote_static we record
	// status under the synthetic "_global" key. Phase 2 doesn't yet track
	// tenant-scoped status separately when modes alias.
	tenantID := "_global"
	if err := s.registry.UpdateStatus(ctx, tenantID, spec.ID, status, detail); err != nil {
		// Tenant-scoped registries (per_tenant) need a different update
		// path; Phase 2 plan defers this to a follow-up. Log and move on.
		s.log.Debug("supervisor: status update", "server_id", spec.ID, "err", err)
	}
}

func newInstanceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("inst_%d", time.Now().UnixNano())
	}
	return "inst_" + base64.RawURLEncoding.EncodeToString(b)
}

// Compile-time guard: Supervisor depends on the secrets.Vault interface
// only via Resolver, so this import keeps the link explicit.
var _ secrets.Vault = (secrets.Vault)(nil)
