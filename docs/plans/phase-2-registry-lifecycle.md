# Phase 2 — Registry & Process Lifecycle

> Self-contained implementation plan. Builds on Phase 0 + Phase 1.

## Goal

Replace Phase 1's static, config-driven server list with a tenant-aware dynamic registry, and put a real process supervisor behind the southbound stdio clients. After Phase 2, Portico can:

- Add/remove/reload MCP servers per tenant at runtime via API.
- Hot-reload `servers:` from `portico.yaml`.
- Run all five V1 runtime modes (`shared_global`, `per_tenant`, `per_user`, `per_session`, `remote_static`).
- Supervise stdio processes: lazy spawn, idle timeout, crash recovery with exponential backoff, graceful shutdown, resource limits where supported, env var interpolation with secret references (placeholder), per-process log capture.
- Surface registry state (health, last error, tool count, schema hash) in the Console.

## Why this phase exists

Phase 1 proved the protocol works. Phase 2 makes the gateway operable: an admin can register a server for a tenant, see its health, restart it if needed, and the supervisor handles transient failures without operator attention. Multi-tenancy starts to bite here — `per_tenant` and `per_user` modes mean Portico can run *N* copies of the same server image across tenants with no cross-talk.

## Prerequisites

Phase 0 + Phase 1 complete. Specifically:
- `internal/mcp/southbound/{stdio,http}` clients work.
- `internal/mcp/southbound/manager.go` exists with `connKey` keyed by (server, tenant, user, session).
- `servers` SQLite table exists.
- `internal/server/api/router.go` is mountable for new routes.

## Deliverables

1. Server registry under `internal/registry/`: in-memory + SQLite-backed, tenant-scoped.
2. Hot-reload: changes to `portico.yaml` `servers:` block apply atomically (drain old connections, start new ones).
3. Process supervisor under `internal/runtime/process/` with a pluggable `Spawner` interface.
4. Health checker: process-alive probe + MCP `ping` probe with configurable interval.
5. Crash recovery: exponential backoff, max attempts cap, "circuit-open" state.
6. Idle timeout: shut down a process if no calls in N seconds, restart on next call.
7. Resource limits: stdlib `syscall.Setrlimit` for memory + CPU; Linux-specific cgroups path optional behind a build tag.
8. Env interpolation: `{{secret:name}}` placeholder resolution from a stub vault (real vault arrives in Phase 5; Phase 2 supplies a file-based stub for tests).
9. Per-process log capture: stderr → rotating file at `${data_dir}/logs/{server_id}.{tenant_id}.{instance}.log`.
10. Registry CRUD API (`/v1/servers/*`) — handlers replace Phase 0 stubs.
11. Console page `/servers` showing live state.
12. Tests: registry CRUD, hot reload, supervisor lifecycle (spawn/restart/idle/crash), per-tenant isolation, env interpolation.

## Acceptance criteria

1. `POST /v1/servers` creates a server for the caller's tenant and returns 201 with the canonical record.
2. `POST /v1/servers/{id}/reload` triggers a clean restart of all running connections for that server (across modes).
3. Hot reload: editing `servers:` in `portico.yaml` (adding, removing, modifying a server) applies within 500ms of the file change debounce, with no requests in flight failing.
4. Two tenants configured with the same logical server (id `github`) get **two independent stdio processes** when runtime mode is `per_tenant` or `per_user`. Verified by integration test.
5. Crash test: SIGKILL the downstream process; the supervisor restarts it within 1s (first attempt) with exponential backoff up to 30s; after 5 failures, the connection enters `circuit_open` state and refuses calls for 5 minutes; the registry record reflects this state.
6. Idle test: with `idle_timeout_seconds: 5`, after no calls for 6s, the process is killed; the next call lazily restarts it; total round-trip on the cold call is < 3s for a fast mock.
7. `GET /v1/servers/{id}` returns a record including: transport, runtime mode, current health, last schema hash, last error, list of running instances (with start time, idle time, last call time).
8. Env interpolation: a server config with `env: ["GITHUB_TOKEN={{secret:github_token}}"]` and a stub vault entry for `github_token` results in the downstream process receiving the resolved value; an unresolvable placeholder fails server start with a clear error.
9. Resource limits: a process configured with `memory_max: 100MB` exceeds the limit and is OOM-killed (Linux only; macOS test is skipped); supervisor records `oom_killed: true` and applies normal backoff.
10. Console `/servers` page lists each server, tenant, runtime mode, instance count, status pill (green/amber/red), and links to instance details.

## Architecture

```
+-----------------------------+
| internal/registry           |
|  Store (SQLite + memory)    |   <-- API CRUD + config hot reload
|  Tenant-scoped queries      |
+--------+--------------------+
         |
         v
+-----------------------------+
| internal/runtime/process    |
|  Supervisor                 |
|   ├── Spawner (subprocess)  |   build-tagged: + sandboxedSpawner
|   ├── HealthChecker         |
|   ├── BackoffPolicy         |
|   ├── IdleTimer             |
|   ├── LogCapture            |
|   └── Limits (rlimit/cgrp)  |
+--------+--------------------+
         |
         v
+-----------------------------+
| internal/mcp/southbound     |
|  Manager (Phase 1, extended) |
|   - GetOrStart routes via   |
|     Supervisor for stdio    |
|   - HTTP clients are static |
+-----------------------------+
```

## Package layout (added in this phase)

```
internal/registry/
  registry.go             # tenant-scoped CRUD
  store.go                # SQLite-backed
  watcher.go              # config sync (consumes config.Watcher events)
  registry_test.go
internal/runtime/process/
  supervisor.go
  spawner.go              # Spawner interface + subprocessSpawner
  spawner_linux.go        # build-tagged: rlimit + (optional) cgroups
  spawner_darwin.go       # build-tagged: rlimit only
  health.go               # health checker
  backoff.go              # exponential backoff state machine
  idle.go                 # idle timer
  limits.go               # ResourceLimits parsing
  logcapture.go           # rotating log writer
  env.go                  # env interpolation
  supervisor_test.go
internal/secrets/         # stub vault for Phase 2 (real one in Phase 5)
  stubvault.go            # file-backed key→value lookup
  ifaces.go               # Vault interface
  stubvault_test.go
internal/server/api/
  handlers_servers.go     # /v1/servers CRUD
  handlers_servers_test.go
web/console/templates/
  servers.templ           # filled in
  server_detail.templ
test/integration/
  registry_e2e_test.go
  supervisor_e2e_test.go
```

## Data model additions

The `servers` table from Phase 0 already has `(tenant_id, id)` PK. Phase 2 adds:

```sql
-- 0002_servers_extended.sql

ALTER TABLE servers ADD COLUMN runtime_mode TEXT;
ALTER TABLE servers ADD COLUMN transport TEXT;
ALTER TABLE servers ADD COLUMN status TEXT NOT NULL DEFAULT 'unknown';  -- unknown|healthy|unhealthy|circuit_open|disabled
ALTER TABLE servers ADD COLUMN status_detail TEXT;

CREATE TABLE IF NOT EXISTS server_instances (
    id            TEXT PRIMARY KEY,                    -- ULID
    tenant_id     TEXT NOT NULL,
    server_id     TEXT NOT NULL,
    user_id       TEXT,                                -- for per_user
    session_id    TEXT,                                -- for per_session
    pid           INTEGER,
    started_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_call_at  TEXT,
    state         TEXT NOT NULL,                       -- starting|running|idle|stopping|crashed|backoff|circuit_open
    restart_count INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT,
    schema_hash   TEXT,
    FOREIGN KEY (tenant_id, server_id) REFERENCES servers(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_instances_tenant_server ON server_instances(tenant_id, server_id);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (2);
```

## Public types and interfaces

### Registry

```go
// internal/registry/registry.go
package registry

type Registry struct {
    store    Store
    log      *slog.Logger
    onChange chan ChangeEvent
}

type Store interface {
    Get(ctx context.Context, tenantID, id string) (*ServerRecord, error)
    List(ctx context.Context, tenantID string) ([]*ServerRecord, error)
    Upsert(ctx context.Context, r *ServerRecord) error
    Delete(ctx context.Context, tenantID, id string) error
    UpdateStatus(ctx context.Context, tenantID, id, status, detail string) error
    UpsertInstance(ctx context.Context, i *InstanceRecord) error
    UpdateInstance(ctx context.Context, i *InstanceRecord) error
    DeleteInstance(ctx context.Context, id string) error
    ListInstances(ctx context.Context, tenantID, serverID string) ([]*InstanceRecord, error)
}

type ServerRecord struct {
    TenantID    string
    ID          string
    DisplayName string
    Transport   string
    RuntimeMode string
    Spec        json.RawMessage  // canonical YAML re-encoded as JSON
    Enabled     bool
    Status      string
    StatusDetail string
    SchemaHash  string
    LastError   string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type InstanceRecord struct {
    ID         string
    TenantID   string
    ServerID   string
    UserID     string
    SessionID  string
    PID        int
    StartedAt  time.Time
    LastCallAt time.Time
    State      string
    RestartCount int
    LastError  string
    SchemaHash string
}

type ChangeEvent struct {
    Kind ChangeKind  // Added | Updated | Removed
    TenantID string
    ServerID string
    Old *ServerRecord
    New *ServerRecord
}

func (r *Registry) Get(ctx, tenantID, id string) (*ServerRecord, error)
func (r *Registry) List(ctx, tenantID string) ([]*ServerRecord, error)
func (r *Registry) Upsert(ctx, *ServerRecord) error
func (r *Registry) Delete(ctx, tenantID, id string) error
func (r *Registry) Subscribe() <-chan ChangeEvent  // unbounded fan-out via internal mux
```

### Spec validation

```go
// internal/registry/spec.go
package registry

type ServerSpec struct {
    ID            string             `yaml:"id" json:"id"`
    DisplayName   string             `yaml:"display_name" json:"display_name"`
    Transport     string             `yaml:"transport" json:"transport"`     // stdio|http
    RuntimeMode   string             `yaml:"runtime_mode" json:"runtime_mode"`
    Stdio         *StdioSpec         `yaml:"stdio,omitempty" json:"stdio,omitempty"`
    HTTP          *HTTPSpec          `yaml:"http,omitempty" json:"http,omitempty"`
    Health        HealthSpec         `yaml:"health" json:"health"`
    Lifecycle     LifecycleSpec      `yaml:"lifecycle" json:"lifecycle"`
    Limits        ResourceLimits     `yaml:"limits,omitempty" json:"limits,omitempty"`
    Auth          AuthSpec           `yaml:"auth,omitempty" json:"auth,omitempty"`  // Phase 5 fully implements; Phase 2 parses
    Namespace     NamespaceSpec      `yaml:"namespace,omitempty" json:"namespace,omitempty"`
    Policy        PolicySpec         `yaml:"policy,omitempty" json:"policy,omitempty"`     // Phase 5
}

type StdioSpec struct {
    Command      string   `yaml:"command"`
    Args         []string `yaml:"args"`
    Env          []string `yaml:"env"`             // KEY=VALUE; supports {{secret:name}}
    Cwd          string   `yaml:"cwd"`
    StartTimeout Duration `yaml:"start_timeout"`   // default 10s
}

type HTTPSpec struct {
    URL        string   `yaml:"url"`
    AuthHeader string   `yaml:"auth_header,omitempty"`  // Phase 5 wires from vault
    Timeout    Duration `yaml:"timeout"`
}

type HealthSpec struct {
    PingInterval Duration `yaml:"ping_interval"`   // default 30s; 0 disables
    PingTimeout  Duration `yaml:"ping_timeout"`    // default 5s
    StartupGrace Duration `yaml:"startup_grace"`   // default 5s
}

type LifecycleSpec struct {
    IdleTimeout       Duration `yaml:"idle_timeout"`         // 0 disables; default 0
    BackoffInitial    Duration `yaml:"backoff_initial"`      // default 500ms
    BackoffMax        Duration `yaml:"backoff_max"`          // default 30s
    MaxRestartAttempts int      `yaml:"max_restart_attempts"` // default 5; 0 = unlimited
    CircuitOpenDuration Duration `yaml:"circuit_open_duration"` // default 5m
    ShutdownGrace     Duration `yaml:"shutdown_grace"`       // default 5s
}

type ResourceLimits struct {
    MemoryMax  string `yaml:"memory_max"`   // e.g. "256MB"
    CPUMillicores int  `yaml:"cpu_millicores"` // Linux only; ignored elsewhere
    OpenFiles  int    `yaml:"open_files"`     // RLIMIT_NOFILE
    Processes  int    `yaml:"processes"`      // RLIMIT_NPROC
}

func (s *ServerSpec) Validate() error
```

Validation rules:
- `id` matches `^[a-z0-9][a-z0-9_-]{0,31}$`.
- `transport` ∈ {`stdio`, `http`}; conditional config block must match.
- `runtime_mode` ∈ V1 modes.
- `stdio.command` non-empty if transport=stdio.
- `http.url` valid URL if transport=http.
- For stdio + `remote_static`: error.
- For http + non-`remote_static` mode: error.
- Defaults applied if zero.

### Supervisor

```go
// internal/runtime/process/supervisor.go
package process

type Supervisor struct {
    spawner    Spawner
    health     *HealthChecker
    log        *slog.Logger
    metrics    *Metrics  // wired by Phase 6
    instances  sync.Map  // key -> *Instance
    onState    chan StateEvent
}

type Instance struct {
    Key        InstanceKey
    Spec       *registry.ServerSpec
    Resolved   ResolvedEnv
    State      atomic.Value  // string
    Process    *os.Process
    StartedAt  time.Time
    LastCallAt atomic.Int64  // unix nanos
    RestartCount int
    Backoff      time.Duration
    NextAttempt  time.Time
    LastError    error
    Cancel       context.CancelFunc
    LogFile      *RotatingFile
    Client       southbound.Client
}

type InstanceKey struct {
    TenantID  string
    ServerID  string
    UserID    string
    SessionID string
}

type StateEvent struct {
    Key   InstanceKey
    State string
    Err   error
}

func New(spawner Spawner, log *slog.Logger) *Supervisor

// Acquire ensures an instance exists and is in `running` state, returns its client.
// Creates an instance lazily according to runtime mode.
func (s *Supervisor) Acquire(ctx context.Context, key InstanceKey, spec *registry.ServerSpec) (southbound.Client, error)

// Release marks the instance idle (per-session-mode subroutines may close on session end).
func (s *Supervisor) Release(ctx context.Context, key InstanceKey)

// Stop kills an instance now.
func (s *Supervisor) Stop(ctx context.Context, key InstanceKey) error

// StopAll on shutdown.
func (s *Supervisor) StopAll(ctx context.Context) error

func (s *Supervisor) Subscribe() <-chan StateEvent
```

`InstanceKey` resolution by runtime mode:
- `shared_global`: `{ServerID, TenantID:"_global", "", ""}`
- `per_tenant`: `{ServerID, TenantID, "", ""}`
- `per_user`: `{ServerID, TenantID, UserID, ""}`
- `per_session`: `{ServerID, TenantID, UserID, SessionID}`
- `remote_static`: `{ServerID, "_global", "", ""}` (no process, just an HTTP client; supervisor still holds the record for status reporting)

The southbound `Manager` from Phase 1 delegates `GetOrStart` to `Supervisor.Acquire`.

### Spawner

```go
// internal/runtime/process/spawner.go
package process

type Spawner interface {
    Spawn(ctx context.Context, spec SpawnSpec) (*Spawned, error)
}

type SpawnSpec struct {
    Command   string
    Args      []string
    Env       []string
    Cwd       string
    Stdin     io.Reader
    Stdout    io.Writer
    Stderr    io.Writer
    Limits    ResourceLimits
    Sandbox   SandboxSpec   // future: seccomp/landlock/container
}

type Spawned struct {
    Process *os.Process
    Cleanup func(ctx context.Context) error  // graceful shutdown
}

type SandboxSpec struct {
    Mode string  // "none" | "seccomp" | "container" — Phase 2 only "none"
    // Future: SeccompProfile, ContainerImage, etc.
}

// subprocessSpawner is the default impl: plain os/exec with rlimit.
type subprocessSpawner struct {
    log *slog.Logger
}

func NewSubprocessSpawner(log *slog.Logger) Spawner
```

`subprocessSpawner.Spawn`:
- Builds `*exec.Cmd` with the given env, command, args, cwd.
- Sets `Stdout`/`Stderr` to the supplied writers (typically a `MultiWriter` of the southbound stdio reader and the rotating log file).
- On Linux/Darwin, sets `SysProcAttr.Setpgid = true` so we can kill the whole process group.
- Applies rlimits via `syscall.Setrlimit` after fork (Linux/Darwin); `CPUMillicores` is honored on Linux via cgroups when the build tag `cgroups` is set, otherwise logged as ignored.
- Returns a `Spawned` with `Cleanup` that attempts `SIGTERM` → wait `ShutdownGrace` → `SIGKILL`.

### Health checker

```go
// internal/runtime/process/health.go
package process

type HealthChecker struct {
    log *slog.Logger
    sup *Supervisor
}

// Start begins periodic health probes for an instance. Cancels via ctx.
func (h *HealthChecker) Start(ctx context.Context, inst *Instance)

// Probe runs one MCP ping with PingTimeout; returns nil on success.
func (h *HealthChecker) Probe(ctx context.Context, inst *Instance) error
```

A failed probe transitions state: 1 fail → `unhealthy`; 3 consecutive fails → `crashed` (triggers backoff/restart pipeline).

### Backoff

```go
// internal/runtime/process/backoff.go
package process

type Backoff struct {
    Initial   time.Duration
    Max       time.Duration
    Multiplier float64
    Jitter    float64   // 0..1; default 0.2
}

func (b *Backoff) Next(attempt int) time.Duration
// attempt=1 → ~Initial; attempt=N → min(Max, Initial * Multiplier^(N-1)) ± jitter

func (b *Backoff) Default() // returns 500ms initial, 30s max, 2.0 mult, 0.2 jitter
```

### Idle timer

```go
// internal/runtime/process/idle.go
package process

type IdleTimer struct {
    timeout time.Duration
    onIdle  func()
    // ...
}

func (t *IdleTimer) Tick()    // call on every Use; resets the deadline
func (t *IdleTimer) Start(ctx)
func (t *IdleTimer) Stop()
```

Behavior: if no `Tick()` arrives for `timeout`, calls `onIdle()` exactly once. After idle shutdown, the next `Acquire` triggers a fresh start.

### Env interpolation

```go
// internal/runtime/process/env.go
package process

type Resolver struct {
    vault secrets.Vault
}

func (r *Resolver) Resolve(env []string) ([]string, error)
// Patterns supported in V1:
//   {{secret:name}}            -> vault.Get(tenantID, name)
//   {{secret:server.name}}     -> vault.Get(tenantID, "server.name") (dotted scoping)
//   {{env:VAR}}                -> os.Getenv(VAR) (process env passthrough)
// Unrecognized patterns are an error.
```

### Log capture

```go
// internal/runtime/process/logcapture.go
package process

type RotatingFile struct {
    Path        string
    MaxSize     int64        // bytes; default 10 MB
    MaxBackups  int          // default 3
    Permissions os.FileMode  // default 0640
}

func (f *RotatingFile) Open() error
func (f *RotatingFile) Write(p []byte) (int, error)
func (f *RotatingFile) Close() error
```

Log path: `${data_dir}/logs/{server_id}/{tenant_id}/{instance_id}.log`. `data_dir` defaults to dir of SQLite DSN, or `/var/lib/portico` if absolute.

### Limits

```go
// internal/runtime/process/limits.go
package process

func ParseSize(s string) (int64, error)  // "256MB", "2GB"

// ApplyOnFork is invoked by spawner_linux/darwin implementations.
// Linux applies all rlimits + optional cgroups; Darwin applies only rlimits available.
// Windows is unsupported in V1.
```

### Stub vault

```go
// internal/secrets/stubvault.go
package secrets

type Vault interface {
    Get(ctx context.Context, tenantID, name string) (string, error)
    Put(ctx context.Context, tenantID, name, value string) error
    Delete(ctx context.Context, tenantID, name string) error
    List(ctx context.Context, tenantID string) ([]string, error)
}

// FileVault: encrypted YAML file at ${data_dir}/vault.yaml.
// Uses AES-256-GCM with key from env PORTICO_VAULT_KEY (base64).
// Phase 5 adds OAuth + secret references, etc. Phase 2 ships only get/put for env interpolation.
type FileVault struct { /* ... */ }

func NewFileVault(path string, key []byte) (*FileVault, error)
```

If `PORTICO_VAULT_KEY` is unset and the vault file does not exist, vault is empty and any `{{secret:...}}` reference fails the server's start with a clear error message that mentions setting `PORTICO_VAULT_KEY`.

## Configuration

```yaml
servers:
  - id: github
    display_name: GitHub
    transport: stdio
    runtime_mode: per_user
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        - "GITHUB_TOKEN={{secret:github_token}}"
      cwd: ""
      start_timeout: 15s
    health:
      ping_interval: 30s
      ping_timeout: 5s
      startup_grace: 10s
    lifecycle:
      idle_timeout: 600s
      backoff_initial: 500ms
      backoff_max: 30s
      max_restart_attempts: 5
      circuit_open_duration: 5m
      shutdown_grace: 5s
    limits:
      memory_max: 512MB
      open_files: 256
```

Per-tenant overrides:

```yaml
tenants:
  - id: acme
    plan: enterprise
    server_overrides:
      github:
        runtime_mode: per_tenant
        stdio:
          env:
            - "GITHUB_TOKEN={{secret:github_token}}"
            - "GITHUB_BASE_URL=https://github.acme.internal"
```

The registry merges `servers` (global definition) with `tenants[].server_overrides[id]` deep-merge to produce the effective `ServerSpec` per (tenant, server).

## External APIs

```
GET    /v1/servers
       → 200 [{id, display_name, transport, runtime_mode, status, instance_count, last_error, schema_hash, ...}]

POST   /v1/servers
       Body: ServerSpec JSON
       → 201 {server_record}
       → 400 if invalid spec
       → 409 if id collision

GET    /v1/servers/{id}
       → 200 {server_record + instances}

PUT    /v1/servers/{id}
       Body: ServerSpec JSON (full replacement)
       → 200 {server_record}

POST   /v1/servers/{id}/reload
       → 202 (drain + restart all instances for this tenant+server)

POST   /v1/servers/{id}/disable
       → 200 (sets enabled=false; stops all instances)

POST   /v1/servers/{id}/enable
       → 200 (sets enabled=true; instances start lazily on first call)

DELETE /v1/servers/{id}
       → 204 (stops instances, deletes record)

GET    /v1/servers/{id}/instances
       → 200 [{instance_record}]

GET    /v1/servers/{id}/logs
       Query: ?instance_id=&tail=200
       → 200 text/plain
```

All endpoints tenant-scoped via the auth middleware. Admin scope can pass `?tenant_id=X` to `GET` endpoints to view across tenants.

## Implementation walkthrough

### Step 1: Registry store

Implement `Store` against SQLite. Each method takes a tenant ID and uses parameterized queries with `WHERE tenant_id = ?`. Add a vet-time check (test) that grep'ing the storage source for `WHERE tenant_id` returns at least one match per Store method.

### Step 2: Registry types + validation

`registry/spec.go` carries the YAML/JSON-tagged types. Validation must give errors that name the offending field (`servers[2].stdio.command: required for transport=stdio`).

### Step 3: Per-tenant effective spec

`registry/effective.go`:
```go
func ResolveEffective(global *ServerSpec, override *ServerOverride) *ServerSpec
```
Deep merge: per-field; lists are replaced; maps are merged.

### Step 4: Hot reload

`registry/watcher.go`: subscribe to `config.Watcher` change events. Diff the current `servers:` block against the new one to produce `Add/Update/Remove` events; persist via `Store.Upsert`/`Delete`; emit `Registry.onChange` events. Subscribers (the Manager from Phase 1) react.

### Step 5: Supervisor core

Implement `Supervisor.Acquire` as a state machine per `InstanceKey`:

```
   [needed]
       |
       v
   [starting] -- start success --> [running] -- pingFail x3 --> [crashed]
                                       ^                         |
                                       |  restart success        | applyBackoff
                                       |                         v
                                                              [backoff]
                                                                  |
                                                                  | attempts > max
                                                                  v
                                                            [circuit_open]
```

Spawning sequence:
1. Resolve env (interpolate secrets).
2. Open `RotatingFile` for log capture.
3. Build `SpawnSpec`.
4. `spawner.Spawn(ctx, spec)`.
5. Wrap stdout/stdin into a `southbound.stdio.Client`.
6. Run `Initialize`, `ListTools` to capture schema; compute `schema_hash`.
7. Start `HealthChecker` and `IdleTimer`.
8. Persist `InstanceRecord` with `state=running`.
9. Emit `StateEvent`.

Restart pipeline:
1. On health probe x3 fail (or process exit), set state `crashed`; close client; emit event.
2. Compute backoff via `Backoff.Next(restart_count+1)`; set `state=backoff`, `next_attempt`.
3. Sleep. If `Acquire` is called during backoff, wait until `next_attempt`.
4. Try start; on success, reset `restart_count=0`. On failure, increment, repeat.
5. After `max_restart_attempts`, set `state=circuit_open` for `CircuitOpenDuration`. Calls fail with `ErrUpstreamUnavailable` and `data: {state: "circuit_open", retry_after: ...}`.

### Step 6: Idle timer integration

`Supervisor.Acquire` returns a wrapped client; every `CallTool` increments `LastCallAt` and calls `IdleTimer.Tick`. The idle timer fires `Stop` after the timeout — it calls the same crash/restart machinery but skips backoff (no error). Next `Acquire` lazily restarts.

### Step 7: Per-mode keying

```go
func keyForMode(mode string, tenantID, serverID, userID, sessionID string) (InstanceKey, error)
```

Validate combinations:
- `per_user` requires `userID != ""`
- `per_session` requires `userID != "" && sessionID != ""`
- `shared_global` ignores userID/sessionID.

If a session ends (Phase 1 northbound emits a session-closed event), Manager calls `Supervisor.Stop` for any per-session instances bound to it.

### Step 8: Resource limits

`limits.go::Apply` builds `syscall.Rlimit` values from the spec and applies them in `cmd.SysProcAttr.AmbientCaps` / `Setrlimit`. On Darwin, RLIMIT_AS is unreliable; document the macOS limitation in code.

Cgroups path (Linux, behind build tag `cgroups`):
- Create a cgroup at `/sys/fs/cgroup/portico/{tenant}/{server}/{instance}/`.
- Write `memory.max`, `cpu.max`.
- Add the spawned PID to `cgroup.procs` after fork.
- Cleanup on instance stop.

If cgroup ops fail, fall back to rlimits and warn once.

### Step 9: Log capture

The southbound stdio Client gets stderr piped into a `MultiWriter` of (rotating log file, slog Warn lines for known-error patterns). The Console exposes the log file via `/v1/servers/{id}/logs?instance_id=&tail=N` — read the tail of the active or specified instance's file.

### Step 10: Console

`web/console/templates/servers.templ`:
- Table: ID, transport, mode, status pill, instance count, last error.
- Per-row: link to detail.

`web/console/templates/server_detail.templ`:
- Spec (formatted JSON).
- Instances table (id, state, started, last call, restart count).
- Tail of latest log (htmx-poll every 2s for live tail).

## Test plan

### Unit

- `internal/registry/registry_test.go`
  - `TestStore_UpsertAndGet`
  - `TestStore_DeleteCascadesInstances`
  - `TestRegistry_PublishesChangeEvents`
  - `TestEffectiveSpec_TenantOverride` — global runtime_mode shared_global, override per_tenant; effective is per_tenant.

- `internal/runtime/process/supervisor_test.go`
  - `TestSupervisor_AcquireStartsProcess`
  - `TestSupervisor_AcquireReusesRunning`
  - `TestSupervisor_HealthFailureTriggersRestart`
  - `TestSupervisor_BackoffSequence` — mock spawner that fails, observe attempt times.
  - `TestSupervisor_CircuitOpensAfterMaxAttempts`
  - `TestSupervisor_IdleTimeoutKills`
  - `TestSupervisor_StopAllOnShutdown`
  - `TestSupervisor_PerTenantKeyIsolation` — two tenants, same server id, runtime per_tenant; expect two distinct PIDs.
  - `TestSupervisor_PerSessionKeyIsolation` — same tenant, two sessions, runtime per_session; expect two PIDs.
  - `TestSupervisor_RemoteStaticNoProcess` — runtime remote_static; Acquire returns an HTTP client; instances table records state=running with PID=0.

- `internal/runtime/process/backoff_test.go`
  - `TestBackoff_Default` — expect ~500ms, 1s, 2s, 4s, 8s sequence (jitter ±20%).
  - `TestBackoff_CapsAtMax`.

- `internal/runtime/process/env_test.go`
  - `TestResolver_SecretInterpolation`
  - `TestResolver_EnvPassthrough`
  - `TestResolver_UnknownPattern_ReturnsError`
  - `TestResolver_MissingSecret_ReturnsError`

- `internal/runtime/process/limits_test.go`
  - `TestParseSize_*` — KB/MB/GB.
  - `TestApply_LinuxRlimits` (linux build tag).

- `internal/secrets/stubvault_test.go`
  - `TestFileVault_PutGetRoundtrip`
  - `TestFileVault_TenantScoping` — tenant A get on key from tenant B fails.
  - `TestFileVault_NoKey_StartFails`.

### Integration

- `test/integration/registry_e2e_test.go`
  - `TestE2E_AddServerViaAPI` — POST /v1/servers, GET, expect record; first call lazily starts.
  - `TestE2E_HotReloadAddServer` — write new server into yaml; tail file changes; expect server appears in registry within 1s.
  - `TestE2E_HotReloadRemoveServer` — remove server; instances drained; subsequent call gets `not_found`.

- `test/integration/supervisor_e2e_test.go`
  - `TestE2E_SIGKILLAndAutoRestart` — SIGKILL the mock; next call succeeds via fresh process.
  - `TestE2E_RepeatedFailureOpensCircuit` — make the mock exit instantly on start 6 times; 6th call returns `circuit_open`.
  - `TestE2E_IdleTimeoutCycle` — call once; wait > idle_timeout; verify process gone (PID dead); call again; verify process restarted.
  - `TestE2E_PerTenantIsolation` — two tenants, same server id, per_tenant mode; both call concurrently; assert both PIDs present and different.
  - `TestE2E_OOMKillRecorded` (linux only) — small memory limit + a tool that allocates; expect `oom_killed: true` recorded; standard backoff applies.

## Common pitfalls

- **PGID propagation**: stdio MCP servers may spawn helper subprocesses (npm, node, etc). Killing the parent PID alone leaks children. Use `Setpgid` and `Kill` the negative PID.
- **Health probe vs idle timeout interaction**: a health probe is *not* a use; don't `Tick` the idle timer on probes. Otherwise idle never fires on truly idle servers.
- **Hot-reload of running instance**: a YAML edit that changes `command` must restart the process; an edit that only changes `health.ping_interval` should reconfigure without restart. Diff the spec by *kind* of field.
- **Schema hash semantics**: hash the canonicalized JSON of the `Tools` slice (sorted by name, normalized whitespace) so that drift detection in Phase 6 is deterministic.
- **`exec.Cmd.Stderr` ordering**: setting both `Stderr` and using `cmd.Output()` will conflict. Always use `cmd.Start()` + manual pipe wiring.
- **fsnotify on macOS**: changes on the *file* (not directory) sometimes fire as a `Rename` event when an editor swaps the file atomically. Watch the directory and re-resolve the path on change.
- **rlimit on Linux fork**: `syscall.Setrlimit` must be set in the *parent* before fork, or via a tiny pre-exec hook. Stdlib `os/exec` does not directly expose this; use `cmd.SysProcAttr` and a wrapper. Tested approach: set rlimits via `prlimit` after PID known if direct setting is unavailable.
- **Vault key rotation**: V1 supports a single key; rotation arrives in Phase 5 (or post-V1). Document this in vault's package comment.
- **`circuit_open` should not block manual reload**: an admin pressing `POST /v1/servers/{id}/reload` clears the circuit and tries to start fresh.

## Out of scope (Phase 2)

- Real OAuth credential retrieval (Phase 5; Phase 2 supplies stub vault only).
- Tool allowlist/denylist enforcement (Phase 5).
- Catalog snapshots (Phase 6).
- Container/microVM isolation (post-V1).
- `per_request` and `sidecar` runtime modes (post-V1).
- Schema drift alerting (Phase 6).
- Multi-instance coordination (post-V1; Redis-backed locking).

## Done definition

1. All acceptance criteria pass.
2. All listed tests pass with `-race` on Linux and macOS (Linux-only tests skipped on macOS).
3. Coverage ≥ 75% for `internal/registry`, `internal/runtime/process`.
4. The Console `/servers` page shows live state for at least two configured servers, updated within 2s of state changes.
5. A demo flow:
   ```bash
   ./bin/portico dev --config examples/dev-two-servers.yaml &
   curl -s http://localhost:8080/v1/servers | jq
   curl -X POST http://localhost:8080/v1/servers/github/reload
   ```
   produces sensible output.

## Hand-off to Phase 3

Phase 3 inherits a fully functional registry + supervisor. Its first job: extend the dispatcher to handle `resources/list`, `resources/read`, `resources/templates/list`, `prompts/list`, `prompts/get`; add MCP Apps (`ui://`) discovery, indexing, and CSP enforcement; implement list-changed handling with stable-by-default + opt-in live updates.
