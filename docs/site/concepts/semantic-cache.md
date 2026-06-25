# Semantic Cache

Portico can place a cache in front of the LLM gateway so repeated or near-repeated
requests skip the upstream provider call entirely — cutting both cost and latency.
The cache is a first-class extensibility seam (`internal/llm/cache`): the driver is
chosen by configuration, and swapping drivers is a config-file-only change with no
code required.

## Drivers

Portico ships four drivers and registers two more as fast-follow targets:

| Driver | Mode | Status | Notes |
|--------|------|--------|-------|
| `none` | — | Default | No-op. Every request reaches upstream. Selecting `none` is a safe zero-configuration starting point. |
| `inmem` | exact-hash | Shipped | Bounded LRU with per-entry TTL, in-process. No external dependency. Suited for development, single-node testing, and low-traffic deployments. |
| `redis` | exact-hash | Shipped | Production-grade. Pure-Go `go-redis` client; TTL is delegated to Redis native `EX`. Compatible with Redis and Valkey. |
| `weaviate` | exact + semantic | Fast-follow | Adds embedding-similarity matching. A paraphrased prompt can hit a prior answer if cosine similarity exceeds the configured threshold. |
| `qdrant` | semantic | Fast-follow | Vector-store-only semantic driver; same embedding seam, different store. |

The exact-hash path (`inmem`, `redis`) is the production-validated default. Semantic
drivers are additive: the interface, the embedding seam
(`internal/llm/cache/embeddings/ifaces`), and the LLM-gateway-backed embedding
generator are all shipped; the vector-store driver implementations arrive as a
config-only extension.

## Configuration

The cache is configured under the `cache:` top-level key in `portico.yaml`.
All fields are optional; an absent or empty block selects the `none` driver and
leaves gateway behaviour unchanged.

```yaml
cache:
  driver: redis            # none | inmem | redis | weaviate | qdrant  (default: none)
  scope: tenant            # tenant | customer | team | vk             (default: tenant)
  ttl: 5m                  # entry lifetime; driver default used when absent
  threshold: 0.85          # semantic similarity floor (0–1); semantic drivers only
  options:                 # driver-specific block; merged as-is into the driver factory
    addr: 127.0.0.1:6379
    password: ""
    db: 0
    key_prefix: portico:cache
```

The `CacheConfig` struct in `internal/config/config.go` is the authoritative schema:

| YAML key | Go field | Description |
|----------|----------|-------------|
| `driver` | `Driver string` | Driver name. Empty string selects `none`. |
| `scope` | `Scope string` | Cache-key partition level within a tenant (see [Key Composition](#key-composition-and-tenant-isolation)). |
| `ttl` | `TTL string` | Default entry lifetime as a Go duration string (`"5m"`, `"1h"`). Falls back to the driver's built-in default (5 minutes for both `inmem` and `redis`). |
| `threshold` | `Threshold float32` | Similarity floor for semantic drivers. `0` delegates to the driver's default. |
| `options` | `Options map[string]any` | Opaque block forwarded verbatim to the driver factory. |

### inmem options

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `max_entries` | int | 1024 | Maximum number of entries before LRU eviction triggers. |
| `default_ttl` | string or int | `"5m"` | Entry lifetime when the request does not specify one. String accepted as a Go duration; integer as seconds. |

### redis options

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `addr` | string | `"127.0.0.1:6379"` | Redis server address. |
| `password` | string | `""` | Redis auth password. Leave empty for unauthenticated servers. |
| `db` | int | `0` | Redis database index. |
| `key_prefix` | string | `"portico:cache"` | Prefix applied to every key. Allows sharing a Redis instance across multiple Portico deployments or other applications. |
| `default_ttl` | string or int | `"5m"` | Entry lifetime when not otherwise set. |

::: tip Redis is lazy on startup
The Redis driver does not `PING` the server at startup. A misconfigured or
unreachable Redis surfaces errors on the first `Lookup` or `Store` call, so a bad
cache address does not block Portico from booting.
:::

## How a request flows

On each non-streaming LLM chat request the gateway executes this sequence:

```
Request arrives
    │
    ├─ [bypass check] Cache-Control: no-store → skip read AND write
    │
    ▼
Cache Lookup  ←─── key = Compose(tenant, scope, scopeID, alias, sha256(normalized_request))
    │
    ├─ HIT  → return cached response; emit llm.cache_hit audit event;
    │          set x-portico-cache: hit response header;
    │          no upstream call, no quota/budget debit
    │
    └─ MISS → dispatch to upstream provider
                  │
                  └─ on success: Cache Store (best-effort; errors do not fail the request)
```

A cache hit is completely free from the budget's perspective: the hierarchical budget
pre-check and post-call reconcile run only on actual upstream calls. This means a
well-tuned cache directly reduces spend at every budget level simultaneously.

## Key composition and tenant isolation

The cache key is **tenant-first by construction**. Two tenants submitting an
identical prompt to an identical model receive completely separate cache entries.
Cross-tenant sharing is impossible.

The composed key for the generic (`inmem`, etc.) drivers follows this layout, with
`\x1f` (ASCII unit-separator, U+001F) as the field delimiter:

```
<tenant_id>\x1f<scope>\x1f<scope_id>\x1f<alias>\x1f<hex(sha256(normalized_input || extra_salt))>
```

For the Redis driver the same fields are separated by `:` after a configurable prefix:

```
portico:cache:<tenant_id>:<scope>:<scope_id>:<alias>:<hex(sha256(normalized_input || extra_salt))>
```

The `normalized_input` is a canonical JSON encoding of the request's `model`,
`messages`, `temperature`, and `max_tokens` fields — a fixed-field struct, so two
logically identical requests always produce identical bytes and thus an identical
hash.

The `extra_salt` field (`ifaces.Key.ExtraSalt`) is an optional operator-supplied
per-route salt, for example a fingerprint of a system prompt that is injected
outside the messages array. Including it in the hash prevents stale responses from
being served when a system prompt changes.

### Tenant re-check as defence in depth

The composed key is tenant-first, which makes cross-tenant collisions structurally
impossible. As a second layer of protection, every driver also stores the tenant id
in the cached value and re-checks it on every lookup:

- The `inmem` driver stores `tenantID` in each LRU node and returns a miss if the
  stored tenant does not match `key.TenantID`.
- The Redis driver stores the tenant id as field `"t"` in the JSON payload and
  returns a miss if `w.TenantID != key.TenantID`.

A mismatch is always treated as a miss, never as an error and never as a hit.

### Scope narrowing within a tenant

The `scope` config key sub-partitions cache entries within a single tenant. This
trades cache hit rate for sharing precision:

| Scope | What shares a cache entry |
|-------|--------------------------|
| `tenant` | All requests within the tenant, regardless of which Virtual Key or team they come from. Maximum sharing. |
| `customer` | All requests from the same Customer. |
| `team` | All requests from the same Team. |
| `vk` | Each Virtual Key gets its own partition. Minimum sharing; maximum isolation per consumer. |

When `scope: vk` is set but the caller presents a plain JWT (no Virtual Key),
Portico falls back to `scope: tenant` automatically so the caller still gets a
stable cache key.

## Bypass headers

Clients can opt out of caching on a per-request basis using standard
`Cache-Control` semantics:

| Header value | Behaviour |
|---|---|
| `Cache-Control: no-store` | Skip both the cache read and the cache write. The response is not cached. |
| `Cache-Control: no-cache` | Skip the cache read (force an upstream call) but write the result into the cache on success. |

Two additional bypass headers are recognised for interoperability with upstream
gateway conventions:

| Header | Equivalent to |
|--------|--------------|
| `x-bf-cache-no-store: true` | `Cache-Control: no-store` |
| `x-bf-cache-no-cache: true` | `Cache-Control: no-cache` |

Either `"true"` or `"1"` is accepted for the `x-bf-*` headers.

::: info Policy override
Operators can enforce or deny bypass via the policy DSL. Rules can set
`force_cache_bypass` to prevent any request from being served from cache, or
`deny_on_cache_miss` to reject requests that would otherwise reach the upstream.
See [Policy](/concepts/policy) for the rule language.
:::

## What is not cached

The gateway never caches the following response types:

- **Streaming responses** (`"stream": true`). A streaming response would have to
  be fully materialised before it could be stored; the semantics change
  significantly, and streaming is excluded unconditionally.
- **Tool-use responses**. Tool calls are conversation-state-dependent: replaying
  an earlier tool-call response into a later turn produces incorrect context.
  The gate (`responseHasToolCalls`) in `internal/server/api/cache_integration.go`
  is the single place to update when the tool-use wire format lands.
- **MCP `tools/call` requests** that arrive directly from a non-LLM client. The
  cost of an MCP tool call is in the downstream tool itself, not in an LLM
  inference, so the LLM cache is not the right layer.

## Invalidation

`POST /api/llm/cache/invalidate` drops entries for the caller's tenant. The caller
must hold the `admin` scope. The request body selects which entries to drop; exactly
one selector is honoured (`all` wins over the others):

```http
POST /api/llm/cache/invalidate
Authorization: Bearer <jwt-with-admin-scope>
Content-Type: application/json

{
  "all": true
}
```

```json
{ "removed": 42 }
```

Invalidation never crosses tenant boundaries — the tenant is always taken from the
authenticated principal, not from the request body.

### Selector reference

| Field | Effect |
|-------|--------|
| `"all": true` | Drop every entry in the caller's tenant. Wins over the other selectors. |
| `"alias": "<model-alias>"` | Drop all entries for a specific model alias across all scopes. |
| `"scope_id": "<id>"` | Drop all entries for a specific Virtual Key, Team, or Customer scope id. |

The Redis driver implements invalidation via `SCAN + MATCH` (never `KEYS`), so the
operation is safe on production clusters with large key sets. Each successful
invalidation emits an `llm.cache_invalidated` audit event recording `alias`,
`scope_id`, `all`, and `removed`.

## Stats and configuration endpoints

`GET /api/llm/cache/stats` returns a per-tenant snapshot of the active cache.

```http
GET /api/llm/cache/stats
Authorization: Bearer <jwt>
```

```json
{
  "entries":  1847,
  "hit_rate": 0.63,
  "driver":   "redis"
}
```

`hit_rate` is a float from 0 to 1 computed over the driver's in-process counters
(not a Redis INFO stat). It represents the ratio of hits to total lookups since the
last server boot.

`GET /api/llm/cache/config` returns the active cache configuration visible to the
operator:

```json
{
  "driver":      "redis",
  "scope":       "vk",
  "ttl_seconds": 300,
  "threshold":   0.85,
  "enabled":     true
}
```

`enabled` is `true` for any driver other than `none`.

The Console surfaces both responses at **Governance → Semantic Cache**: a hit-rate
sparkline, an entry count, the active driver and scope, and the invalidation form.

## Semantic mode (fast-follow)

Exact-hash caching hits only when the request is byte-for-byte identical after
normalization. Semantic mode extends this: if a paraphrased or near-identical prompt
has a prior cached response, and the cosine similarity between the two prompts'
embedding vectors exceeds `threshold`, the cached response is returned.

The architecture for semantic mode is already present:

- `ifaces.Key.SimilarityVector` carries the embedding vector for the current
  request.
- `ifaces.LookupOpts.Threshold` is passed to every `Lookup` call.
- `ifaces.Mode` distinguishes `"exact"` from `"semantic"` in stored entries and
  in audit events.
- `internal/llm/cache/embeddings/ifaces.EmbeddingGenerator` is the embedding seam.
  The default production implementation (`internal/llm/cache/embeddings/llm_gateway`)
  routes embedding requests through Portico's own LLM gateway, so the operator's
  configured provider credentials apply and a dedicated cheap embedding model alias
  can be pointed at any provider.

The `inmem` and `redis` drivers are exact-only: they return a miss when
`key.Mode == ModeSemantic` rather than erroring. This means the `weaviate` and
`qdrant` drivers are drop-in additions via the cache seam; the rest of the system
requires no changes.

::: info Embedding cost
Embedding calls for semantic cache lookups are routed through the LLM gateway and
therefore subject to the same budget and policy enforcement as regular requests.
Operators should configure a low-cost embedding alias to keep per-request overhead
small.
:::

## Driver seam

Portico's cache follows the same extensibility pattern as storage, skill sources,
and credential vaults. Adding a new driver requires only:

1. A package under `internal/llm/cache/<driver>/` that implements `ifaces.Cache`.
2. A call to `cache.Register("<name>", factory)` from the package's `init()`.
3. A blank import of the new package in `cmd/portico/`.

No other code changes. The factory error message lists registered drivers by name so
a typo in `portico.yaml` produces an actionable diagnostic.

```go
// cache: unknown driver "memcached" (registered: [none inmem redis weaviate qdrant])
```

## Example: Redis cache with VK-level scope

```yaml
cache:
  driver: redis
  scope: vk          # each Virtual Key has its own partition
  ttl: 10m
  options:
    addr: redis.internal:6379
    password: ""
    key_prefix: portico:prod:cache
```

With this configuration:

- Two different Virtual Keys sending identical prompts get separate entries.
- Each entry lives for 10 minutes.
- A cache hit skips the upstream call and does not debit any budget level.
- The audit log records `llm.cache_hit` with `alias`, `mode: "exact"`, and the
  entry's age in seconds.

## Related

- [LLM Gateway](/concepts/llm-gateway) — the request path the cache sits in front
  of; describes the full dispatch, routing, and budget flow.
- [Multi-tenancy](/concepts/multi-tenancy) — how tenant identity flows through every
  layer, including why the cache key is always tenant-first.
- [Hierarchical Budgets](/concepts/hierarchical-budgets) — a cache hit skips both
  the pre-call budget check and the post-call reconcile, directly reducing spend at
  every budget scope.
- [Virtual Keys](/concepts/virtual-keys) — the `vk` scope partitions the cache by
  Virtual Key id, so consumers with different policies or cost allocations do not
  share entries.
- [Audit](/concepts/audit) — `llm.cache_hit` and `llm.cache_invalidated` events are
  queryable through the audit API.
- [Policy](/concepts/policy) — rules can enforce or deny cache bypass on a
  per-route or per-consumer basis.
- [Observability](/concepts/observability) — cache hit/miss counters appear in
  Portico's span attributes and the Console stats card.
