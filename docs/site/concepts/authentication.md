# Authentication & scopes

Every request to Portico — REST, MCP, LLM, or A2A — passes through a single authentication layer before it reaches any business logic. That layer resolves the caller to a `tenant.Identity`, which carries the tenant ID, user ID, plan, and scope set for the remainder of the request. This page describes how that resolution works, how to configure it, and what each scope grants.

## Two credential paths

Portico supports two ways for a caller to authenticate:

| Path | Token form | Best for |
|------|-----------|----------|
| Bearer JWT | Any signed JWT issued by a trusted IdP | Human operators, CI pipelines, agent frameworks with their own identity provider |
| Virtual Key | `pk-portico-<id>.<secret>` | Programmatic agents, automated pipelines, per-customer API keys |

Both paths produce a `tenant.Identity` the rest of the system treats identically, with one difference: a Virtual Key can carry narrower allowlists (specific providers, models, or MCP servers) on top of its scope set.

## Bearer JWT authentication

### Algorithm allowlist

Portico enforces asymmetric-only signing. The allowed algorithms are:

- **RSA**: RS256, RS384, RS512
- **ECDSA**: ES256, ES384, ES512

Symmetric algorithms (HS256, HS384, HS512) and the `none` algorithm are permanently rejected. The validator double-checks the algorithm at the key-lookup stage — even if a future library version relaxed `WithValidMethods`, the inner guard catches it.

Every JWT must carry a `kid` header. The validator uses it to look up the matching public key from the key set. A token without `kid` is rejected before any signature check.

### Key distribution — JWKS or static file

Configure one of the two key-set backends under `auth.jwt`:

```yaml
auth:
  jwt:
    issuer: "https://auth.example.com"
    audiences:
      - "portico-api"
    jwks_url: "https://auth.example.com/.well-known/jwks.json"
    # Or for a static file on disk:
    # static_jwks: "/etc/portico/keys/jwks.json"
```

**Remote JWKS** (`jwks_url`): Portico fetches the URL at startup and refreshes every 10 minutes in the background. If a refresh fails, the last good key set continues serving requests (fail-open on key set, fail-closed on signature). The initial fetch failing at startup is fatal — Portico will not serve unauthenticated traffic because key load failed.

**Static JWKS** (`static_jwks`): The file is read once at startup. Use this in air-gapped environments or when you manage key rotation out-of-band. Both RSA (`kty: RSA`) and EC (`kty: EC`) keys are parsed; symmetric (`kty: oct`) keys are silently dropped, never loaded.

::: warning
Exactly one of `jwks_url` or `static_jwks` must be present. Providing neither causes startup to fail with a clear error.
:::

### Full JWTConfig reference

```yaml
auth:
  jwt:
    # Required: the expected `iss` claim value.
    issuer: "https://auth.example.com"

    # Required: at least one audience string that must appear in the `aud` claim.
    audiences:
      - "portico-api"
      - "portico-mcp"  # multiple values are OR-checked

    # One of jwks_url or static_jwks is required.
    jwks_url: "https://auth.example.com/.well-known/jwks.json"
    # static_jwks: "/etc/portico/keys/jwks.json"

    # Name of the claim that carries the tenant identifier.
    # Default: "tenant"
    tenant_claim: "tenant"

    # Name of the claim that carries the scope list.
    # Accepts a space-separated string ("read write") or a JSON array.
    # Default: "scope"
    scope_claim: "scope"

    # If set, every token must include this scope or validation fails.
    # Useful for a gateway-wide portal scope, e.g. "portico:access".
    required_scope: ""

    # Leeway applied to nbf/exp comparisons to tolerate clock drift.
    # Default: 60s
    clock_skew: "60s"
```

### Required claims

After signature and audience verification, the validator extracts:

| Claim | Notes |
|-------|-------|
| `iss` | Must match `issuer` exactly |
| `sub` | Becomes `Identity.UserID` and `Identity.Subject` |
| `aud` | At least one value must match an entry in `audiences` |
| `exp` | Mandatory; checked with clock-skew leeway |
| `iat` | Checked as well; future-issued tokens are rejected |
| `<tenant_claim>` | Maps to `Identity.TenantID`; missing → 401 |
| `<scope_claim>` | Space-delimited string or JSON array |
| `plan` | Optional; carried into `Identity.Plan` (tenant plan wins when both are set) |

The `kid` header is required on every token and is used to select the verification key from the key set.

### Tenant registration check

After the JWT passes all signature and claim checks, the middleware looks up the tenant from the `tenant` claim in the datastore. If no matching tenant record exists, the request is rejected with `unknown_tenant`. This means provisioning a tenant in Portico (via the REST API or config-based boot seeding) is a prerequisite for any JWT issued for that tenant to authenticate successfully.

See [Multi-tenancy](/concepts/multi-tenancy) for details on tenant provisioning.

## Dev mode — no JWT required

When the `auth` block is **absent** from `portico.yaml`, Portico starts in dev mode. Every request receives a synthetic identity with no authentication check:

```go
Identity{
    TenantID: "dev",   // or $PORTICO_DEV_TENANT
    UserID:   "dev",
    Plan:     "enterprise",
    Scopes:   []string{"admin"},
    DevMode:  true,
}
```

The dev tenant is upserted into the datastore on first request. The tenant ID defaults to `"dev"` and can be overridden with the `PORTICO_DEV_TENANT` environment variable.

::: warning
Dev mode is intended for local development only. It must never be combined with a non-localhost bind address. There is no authentication — any caller gets full `admin` access.
:::

::: tip
`./bin/portico dev` enables dev mode automatically and binds to `127.0.0.1:18080`. See [Dev mode](/getting-started/dev-mode) for the full workflow.
:::

## Virtual Keys

Virtual Keys are a programmatic alternative to JWTs designed for agents, automated pipelines, and per-customer isolation scenarios where issuing a full JWT per consumer is impractical.

### Token format

A Virtual Key bearer token looks like:

```
pk-portico-vk_<24-hex-chars>.<30-char-base62-secret>
```

The `pk-portico-` prefix is how the auth middleware routes the bearer to the VK resolver rather than the JWT validator. Malformed tokens (wrong prefix, missing separator, empty segments) are rejected before any database lookup.

### Security model

Portico stores only a salt and `HMAC-SHA256(salt, secret)` — never the secret itself. The full token is returned to the operator exactly once when the key is created or rotated, and is never retrievable again. A compromised database cannot reconstruct a usable token: the stored HMAC for VK A cannot be used to authenticate as VK B.

Verification uses `crypto/subtle.ConstantTimeCompare` to prevent timing oracles.

Resolved VKs are cached in-process for 60 seconds (keyed on `SHA-256(token)`). On revocation or rotation, the instance immediately drops its cache entry so the change takes effect without waiting for TTL expiry.

### What a Virtual Key carries

Beyond the tenant ID and scope set, a VK can restrict the surface a caller can reach:

| Field | Effect |
|-------|--------|
| `scopes` | The scope set granted to this key (same vocabulary as JWT scopes) |
| `provider_allowlist` | Restrict to specific LLM provider drivers; empty = all |
| `model_allowlist` | Restrict to specific model aliases; empty = all |
| `mcp_server_allowlist` | Restrict to specific MCP server IDs; empty = all |
| `profile_id` | Pin the key to an Agent Profile |
| `parent_kind` / `parent_id` | Attach to a budget hierarchy node (`team` or `customer`) |

See [Virtual Keys](/concepts/virtual-keys) for the full lifecycle API (create, rotate, revoke).

### Request flow

The auth middleware checks for the VK prefix before the dev-mode bypass, so Virtual Keys work in both dev and production:

```
Authorization: Bearer pk-portico-vk_...
       │
       ▼
LooksLikeVK?  ──yes──► VKResolver.Resolve()
                              │
                     cache hit?  ──yes──► Identity
                              │no
                        DB lookup + HMAC verify
                              │
                        cache + return Identity
```

## Named scopes

Portico uses a fixed vocabulary of named scopes. The middleware enforces them via the `scope.Require(s)` handler wrapper on protected routes.

| Scope | Grants access to |
|-------|-----------------|
| `admin` | All routes; acts as a wildcard superset of every named scope |
| `servers:write` | Register, edit, and delete MCP servers |
| `secrets:write` | Vault CRUD, secret reveal, and key rotation |
| `policy:write` | Create and edit policy rules |
| `tenants:admin` | Create, archive, and purge tenants |
| `playground:execute` | Invoke a tool, resource, or prompt from the playground |
| `playground:save` | Save a playground test case |

The `admin` scope is the umbrella: a request bearing `admin` is implicitly granted every other named scope. This keeps existing deployments and dev mode working without enumerating all scopes.

Per-resource scopes (`servers:write`, etc.) are the recommended grant for service-to-service tokens and Virtual Keys — they follow the principle of least privilege, and an audit trail that records which scopes were present is more meaningful when they are narrow.

```yaml
# Example: a JWT for a deployment pipeline that can only register servers
# and manage secrets — not touch policy or tenants.
#
# The token's scope claim would contain:
#   "servers:write secrets:write"
```

## Paths that bypass authentication

A small set of paths are always allowed without any credential:

| Path | Purpose |
|------|---------|
| `/healthz` | Liveness probe |
| `/readyz` | Readiness probe |
| `/favicon.svg`, `/favicon.ico`, `/robots.txt` | Static assets |
| `/_app/*` | SvelteKit runtime assets (Console SPA bootstrap) |

Every other path requires a valid Identity. Handlers that need a particular scope additionally call `scope.Require(...)`, returning 403 with `{"error":"permission_denied","message":"missing scope <name>"}` when the identity lacks it.

## Error responses

The auth middleware returns JSON for all authentication failures:

```jsonc
// 401 — missing or invalid bearer token
{
  "error": "unauthorized",
  "message": "missing bearer token"
}

// 401 — JWT validation failed (expired, bad signature, wrong audience, etc.)
{
  "error": "unauthorized",
  "message": "jwt: ..."
}

// 401 — tenant from JWT not registered in Portico
{
  "error": "unknown_tenant",
  "message": "tenant not registered"
}

// 401 — Virtual Key revoked
{
  "error": "vk_revoked",
  "message": "virtual key revoked"
}

// 403 — tenant is known but the scope is missing
{
  "error": "permission_denied",
  "message": "missing scope servers:write"
}
```

All 401 responses include a `WWW-Authenticate: Bearer realm="portico"` header.

## Putting it together — production YAML example

```yaml
server:
  bind: "0.0.0.0:8080"

auth:
  jwt:
    issuer: "https://auth.corp.example.com"
    audiences:
      - "portico-gateway"
    jwks_url: "https://auth.corp.example.com/.well-known/jwks.json"
    tenant_claim: "tenant"    # custom claim your IdP sets
    scope_claim: "scope"
    clock_skew: "30s"

storage:
  driver: sqlite
  dsn: "file:/var/lib/portico/portico.db?cache=shared"

tenants:
  - id: "acme"
    display_name: "Acme Corp"
    plan: "pro"
    entitlements:
      skills: ["*"]
      max_sessions: 200
```

With this config, a request must carry `Authorization: Bearer <jwt>` where the JWT is:

- Signed with RS256/384/512 or ES256/384/512
- Issued by `https://auth.corp.example.com`
- Targeting audience `portico-gateway`
- Containing a `tenant` claim whose value matches a registered tenant (here `"acme"`)

## Related

- [Multi-tenancy](/concepts/multi-tenancy) — how tenant IDs flow through every storage layer and why they are non-negotiable.
- [Virtual Keys](/concepts/virtual-keys) — full lifecycle: creating, rotating, revoking, and attaching keys to budgets and Agent Profiles.
- [Agent Profiles](/concepts/agent-profiles) — the consumer-level entitlement object that Virtual Keys can be pinned to.
- [Policy](/concepts/policy) — how scopes interact with the policy engine for tool-level approval and deny rules.
- [Security model](/concepts/security-model) — the full threat model, including why HS\* algorithms and credential passthrough are prohibited.
- [Getting started: installation](/getting-started/installation) — bootstrapping a Portico instance with a real config.
- [Dev mode](/getting-started/dev-mode) — the local development workflow with the authentication bypass.
