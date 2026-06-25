# Multi-tenancy

Multi-tenancy is not a feature that was added to Portico — it is the substrate the entire system is built on. From the first database migration to the outermost HTTP handler, every data path is keyed by tenant. There is no single-tenant fast path, no global mutable state that crosses tenant boundaries, and no place in the code that assumes only one tenant exists.

This document explains what that means in practice: how tenants are identified, how identity flows through the system, how the process supervisor enforces isolation at the OS level, and what guarantees you can rely on when operating a multi-tenant deployment.

---

## Why agentic platforms need hard tenancy

Traditional SaaS products can often defer multi-tenancy to the application layer — a `WHERE tenant_id = ?` added after the fact to a table that otherwise works fine. Agentic platforms do not have this luxury for three reasons.

**Credentials are high-value targets.** When Portico fetches a downstream credential on behalf of a tool call, that credential belongs to one tenant. A cross-tenant read is not a data leak — it is a lateral-movement path from one customer's agent into another's infrastructure. Portico enforces tenant scope at the `Vault` interface boundary: every `Get`, `Put`, `Delete`, and `List` call carries an explicit `tenantID` argument, and there is no API surface that omits it.

**Audit trails must be legally attributable.** Regulated industries require that audit records can be queried per tenant without co-mingling. Portico's audit store indexes on `(tenant_id, occurred_at)` and returns only the calling tenant's events by default. An operator with the `admin` JWT scope can request cross-tenant aggregates; no other path exists.

**Process isolation is a security boundary, not a performance feature.** A misbehaving MCP server running inside one tenant's stdio subprocess must not be able to observe or interfere with another tenant's session. Portico's process supervisor tracks each subprocess by an `InstanceKey` that carries `TenantID` as a primary dimension; the kernel sees two separate processes.

---

## Tenant identification

### Production: Bearer JWT

Every northbound HTTP request must carry a Bearer JWT in the `Authorization` header. Portico's auth middleware (`internal/auth/tenant`) validates the token and extracts a tenant identity before any handler runs.

The JWT must include:

| Claim | Type | Description |
|-------|------|-------------|
| `tenant` | string | The tenant identifier. Claim name is configurable via `auth.jwt.tenant_claim` (default: `tenant`). |
| `sub` | string | The user or agent identifier within the tenant. |
| `exp` | NumericDate | Expiry. Tokens without expiry are rejected. |

Optional claims:

| Claim | Type | Description |
|-------|------|-------------|
| `scope` | string | Space-delimited scope list. Populates `Identity.Scopes`. |
| `plan` | string | Overrides the tenant's configured plan for this request. |

Portico validates asymmetric signatures only. HS256 and the `none` algorithm are not in the allowlist. Issuer URL and signing keys (JWKS endpoint or static JWKS file) are configured per deployment in `portico.yaml`:

```yaml
auth:
  jwt:
    issuer: https://auth.example.com/
    audiences: [portico]
    jwks_url: https://auth.example.com/.well-known/jwks.json
    tenant_claim: tenant   # default; omit if your IdP already uses "tenant"
    scope_claim: scope
```

After a successful validation, the middleware looks up the tenant in the tenant store. A JWT for an unregistered tenant receives `401 unknown_tenant` — knowing the signing key is not sufficient to access a tenant that has not been provisioned.

### Virtual Keys

A Virtual Key (`pk-portico-<id>.<secret>`) is a programmatic credential that resolves to a tenant at the auth boundary without requiring a full JWT. Virtual Keys carry their own scope set, optional model and server allowlists, and an optional bound Agent Profile. The middleware checks for the `pk-portico-` prefix before the JWT path, so Virtual Keys work in any context that accepts a Bearer token.

For more detail, see [Virtual Keys](/concepts/virtual-keys).

### Dev mode

Running `portico dev`, or binding to `127.0.0.1` with no `auth:` block configured, activates dev mode. In dev mode the JWT validator is bypassed and a synthetic `dev` tenant is injected into every request context. The dev tenant is upserted automatically on first request. You can rename it with the `PORTICO_DEV_TENANT` environment variable.

::: warning
Dev mode is for local development only. The binary refuses to activate it when bound to a non-loopback address unless you explicitly pass `--dev` on a non-loopback bind, which requires a separate acknowledgment flag.
:::

---

## Identity propagation

Once the middleware has validated the credential, it constructs a `tenant.Identity` value and attaches it to the request `context.Context` via `tenant.With(ctx, id)`. Every handler that needs tenant-scoped data calls `tenant.MustFrom(ctx)` to retrieve it:

```go
// internal/auth/tenant/context.go

type Identity struct {
    TenantID string
    UserID   string
    Plan     string
    Scopes   []string
    Issuer   string
    Subject  string
    DevMode  bool
    RawToken string // treat as sensitive; never log
}

func MustFrom(ctx context.Context) Identity { ... }
```

`MustFrom` panics if called in a handler that somehow bypassed the auth middleware — this is intentional. A handler that reaches tenant-scoped storage without an authenticated identity is a programming error, not a recoverable condition.

The `Identity.TenantID` value is then passed explicitly to every storage and vault call. There is no "current tenant" global; the tenant ID travels down the call stack as a plain argument.

---

## Tenant provisioning

Tenants are declared in `portico.yaml` under the `tenants:` key. The server upserts them into the tenant store at boot (idempotent by ID):

```yaml
tenants:
  - id: acme
    display_name: Acme Corp
    plan: enterprise
    entitlements:
      skills: ["*"]          # all skill packs
      max_sessions: 200

  - id: beta
    display_name: Beta Industries
    plan: pro
    entitlements:
      skills: ["github.*"]   # only GitHub skill packs
      max_sessions: 10
```

Fields:

| Key | Type | Description |
|-----|------|-------------|
| `id` | string | Unique tenant identifier. Must match the `tenant` JWT claim. |
| `display_name` | string | Human-readable name shown in the Console. |
| `plan` | string | Plan tier (`free`, `pro`, `enterprise`, or a custom string). Used in entitlement evaluation. |
| `entitlements.skills` | []string | Glob patterns controlling which Skill Packs the tenant may enable. |
| `entitlements.max_sessions` | int | Maximum concurrent MCP sessions. |
| `credentials_ref` | string | Path to a per-tenant credential file (optional; vault-backed secrets are preferred). |

The REST API (`/v1/admin/tenants`) provides dynamic tenant CRUD for operators holding the `admin` JWT scope.

---

## Storage isolation

Every tenant-scoped table in the SQLite schema carries a `tenant_id NOT NULL` column. The primary keys of tenant-scoped tables are composite `(tenant_id, id)` pairs, and foreign key constraints cascade on tenant deletion:

```sql
CREATE TABLE IF NOT EXISTS servers (
    tenant_id   TEXT NOT NULL,
    id          TEXT NOT NULL,
    spec_json   TEXT NOT NULL,
    -- ...
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
```

This pattern applies to every persistent entity: servers, sessions, catalog snapshots, skill enablement, approvals, and audit events. Every read path uses a `WHERE tenant_id = ?` predicate bound to the tenant from the request context — no in-process filtering after a full-table scan.

The tables where tenant isolation is enforced at the schema level include:

- `servers` — registered MCP server specs
- `sessions` — active MCP sessions
- `catalog_snapshots` — per-session tool catalog snapshots
- `skill_enablement` — which skills are enabled per tenant/session
- `approvals` — pending and decided tool-call approvals
- `audit_events` — the full event log
- `tenant_skill_sources` — skill source registrations
- `tenant_authored_skills` — Skills authored or pinned by the tenant

---

## Runtime process isolation

The process supervisor (`internal/runtime/process`) manages the lifecycle of stdio and HTTP downstream MCP servers. Each running server is indexed by an `InstanceKey`:

```go
type InstanceKey struct {
    TenantID  string
    ServerID  string
    UserID    string
    SessionID string
}
```

The `TenantID` field in the key determines whether two requests share a process or get separate ones. The mapping is controlled per server by the `runtime_mode` field in the server's spec.

### Runtime modes

| Mode | Instance key | Use case |
|------|--------------|----------|
| `shared_global` | `(server_id)` — tenant field set to `_global` | Stateless, read-only tools with no per-tenant state |
| `per_tenant` | `(tenant_id, server_id)` | Enterprise isolation; one OS process per tenant |
| `per_user` | `(tenant_id, server_id, user_id)` | User-scoped credentials or state |
| `per_session` | `(tenant_id, server_id, user_id, session_id)` | Maximum isolation; statefulness across tool calls within one session |
| `remote_static` | `(server_id)` — HTTP only, no managed process | Hosted MCP services; no local process |

The default when `runtime_mode` is omitted depends on transport: `http` transports default to `remote_static`; `stdio` transports default to `shared_global`. Operators should set `per_tenant` or stricter for any server that handles tenant-specific data or holds tenant credentials.

::: tip Choosing a mode
- **Stateless tools** (time lookup, unit conversion): `shared_global` is efficient.
- **Tools using tenant credentials** (GitHub, Postgres, Slack): `per_tenant` or `per_user` ensures credentials are injected into a process that belongs to one tenant and cannot leak to another.
- **Stateful tools** (a web browser, a long-running computation): `per_session` keeps state bound to the conversation.
:::

The supervisor enforces mode constraints at `Acquire` time: calling `Acquire` with `per_tenant` and an empty `TenantID` returns an error rather than silently falling back to a shared process.

```yaml
# portico.yaml — server registration with explicit isolation mode
servers:
  - id: github
    transport: stdio
    runtime_mode: per_tenant
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
```

---

## Credential vault isolation

The `secrets.Vault` interface is the single entry point for all credential operations. Every method signature carries `tenantID` as an explicit first parameter:

```go
type Vault interface {
    Get(ctx context.Context, tenantID, name string) (string, error)
    Put(ctx context.Context, tenantID, name, value string) error
    Delete(ctx context.Context, tenantID, name string) error
    List(ctx context.Context, tenantID string) ([]string, error)
    RotateKey(ctx context.Context, newKey []byte) error
    Close() error
}
```

There is no `GetGlobal` or `ListAll` method. A caller that omits `tenantID` cannot construct a valid call — the compiler enforces the isolation. The vault implementation encrypts every secret individually with a HKDF-derived per-value key that encodes the `(tenantID, name)` pair into the additional authenticated data (AAD), so a ciphertext from one tenant cannot be decrypted even if moved to another tenant's namespace.

The vault master key is read from the `PORTICO_VAULT_KEY` environment variable and never from config files or code. The `vault` CLI subcommand (`portico vault put|get|delete|list|rotate-key`) operates against the same interface and requires an explicit `--tenant` flag when more than one tenant is configured.

---

## Audit isolation

Every audit event carries a mandatory `tenant_id` field:

```go
type Event struct {
    Type      string
    TenantID  string         // required; never empty
    SessionID string
    UserID    string
    // ...
}
```

The audit store's `Query` API filters by `tenant_id` by default. A caller cannot omit the tenant filter unless their `Identity` carries the `admin` scope:

```go
// Querying audit events — the store's WHERE clause always leads with tenant_id:
//   WHERE tenant_id = ? [AND type = ?] [AND occurred_at >= ?]
```

The Console's audit view is scoped to the operator's tenant. A cross-tenant audit view requires the `admin` scope and is only accessible through the admin section of the Console.

---

## Isolation guarantees summary

| Layer | Guarantee |
|-------|-----------|
| **JWT validation** | A token for tenant `acme` cannot access tenant `beta`'s endpoints; the middleware rejects unknown tenants at the wire. |
| **Storage** | Every tenant-scoped table has a `tenant_id NOT NULL` column and a `WHERE tenant_id = ?` filter on every read. |
| **Process supervisor** | `per_tenant` and stricter modes give each tenant a dedicated OS subprocess. Two tenants never share a stdio process unless the operator explicitly chose `shared_global`. |
| **Credential vault** | The `Vault` interface takes `tenantID` as a required argument on every method. AAD-bound encryption prevents cross-tenant ciphertext reuse. |
| **Audit log** | Events are queryable per tenant by default. Cross-tenant queries require the `admin` scope. |
| **Catalog snapshots** | Snapshots are tenant-scoped. A session cannot read another tenant's snapshot. |
| **Agent Profiles** | Profiles are stored and resolved per tenant. The default profile (no explicit binding) is a code construct scoped to the requesting tenant, never a row that crosses tenants. |

An integration test (`test/integration/`) asserts that cross-tenant data access attempts fail at the storage layer. This test is part of the CI gate and must pass on every commit.

---

## Related

- [Architecture](/concepts/architecture) — how the northbound, southbound, and runtime layers compose
- [Authentication](/concepts/authentication) — JWT validation, Virtual Keys, and dev mode in detail
- [Credentials Vault](/concepts/credentials-vault) — the vault interface, injection strategies, and key rotation
- [Agent Profiles](/concepts/agent-profiles) — per-tenant consumer entitlement bindings
- [Audit](/concepts/audit) — the structured audit event model and query API
- [Getting Started: Dev Mode](/getting-started/dev-mode) — running Portico locally with the synthetic dev tenant
