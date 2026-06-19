# Semantic Cache

Portico can put a cache in front of the LLM gateway so repeated or near-repeated
requests skip the upstream provider call entirely — cutting cost and latency.
The cache is a **§4.4 seam** (`internal/llm/cache`): the driver is chosen by
config, and swapping drivers is a config-only change.

## Drivers

| Driver | Mode | Use |
| --- | --- | --- |
| `none` | — | Default. No caching; the gateway behaves exactly as before. |
| `inmem` | exact-hash | Development + tests. Bounded LRU with TTL, in-process. |
| `redis` | exact-hash | Production. Pure-Go `go-redis`; TTL via Redis `EX`. |
| `weaviate` | exact + semantic | *(fast-follow — see "Semantic mode" below)* |
| `qdrant` | semantic | *(fast-follow)* |

Configure it under `cache:` in `portico.yaml`:

```yaml
cache:
  driver: redis          # none | inmem | redis | weaviate | qdrant
  scope: tenant          # tenant | customer | team | vk
  ttl: 5m
  threshold: 0.85        # semantic similarity floor (semantic drivers)
  options:               # driver-specific
    addr: 127.0.0.1:6379
```

## How a request flows

1. The gateway resolves the alias → provider + model.
2. **Cache lookup** (before quota/budget — a hit is free):
   - On a **hit**, the cached OpenAI response is returned with `x-portico-cache: hit`
     and an `llm.cache_hit` audit event. No upstream call, no quota/budget debit.
   - On a **miss**, the request dispatches upstream.
3. After a successful dispatch, the result is **stored** for future hits.

## Key composition & tenant isolation

The cache key is **tenant-first** by construction:

```
<tenant_id>\x1f<scope>\x1f<scope_id>\x1f<alias>\x1f<sha256(normalized_request)>
```

Two tenants asking the identical prompt of the identical model get **separate**
entries — cross-tenant sharing is impossible, and the driver re-checks the
stored entry's tenant on lookup as defence in depth. Within a tenant, `scope`
narrows sharing: `scope: vk` gives each Virtual Key its own cache partition;
`scope: tenant` shares across the whole tenant.

## Bypass headers

Clients opt out per request:

- `Cache-Control: no-store` — neither read nor write (the response carries no cache).
- `Cache-Control: no-cache` — skip the read (force upstream) but still write the result.
- Bifrost's `x-bf-cache-no-store` / `x-bf-cache-no-cache` are recognised for compatibility.

Operators can deny bypass via the policy DSL (`force_cache_bypass`,
`deny_on_cache_miss`).

## What is *not* cached

- **Streaming** responses (`stream: true`) — they would have to be materialised.
- **Tool-use** responses — they are conversation-state-dependent and unsafe to replay.
- Direct MCP `tools/call` from a non-LLM client (the cost is in the tool itself).

## Invalidation

`POST /api/llm/cache/invalidate` (admin scope) drops entries for the caller's
tenant by `alias`, `scope_id`, or `all`, emitting an `llm.cache_invalidated`
audit event. Never crosses tenants. `GET /api/llm/cache/config` and
`/api/llm/cache/stats` expose the active driver + per-tenant hit rate; the
Console surfaces all of this at **Governance → Semantic Cache**.

## Semantic mode (fast-follow)

Exact-hash caching (above) is the shipped, production-validated path. Semantic
(embedding-similarity) caching — where a *paraphrased* prompt hits a prior
answer — is delivered by the `weaviate`/`qdrant` drivers plus the embedding
generator (`internal/llm/cache/embeddings/llm_gateway`, which routes embeddings
through the gateway so Portico-managed credentials apply). The embedding seam is
shipped; the vector-store drivers are a config-only addition via the cache seam,
validated against a live Weaviate/Qdrant.
