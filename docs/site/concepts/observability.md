# Telemetry

Portico ships two complementary observability primitives in a single binary: structured logging via Go's `log/slog` and distributed tracing via the OpenTelemetry SDK. Both are active from the first inbound request; the operator controls verbosity, exporter, and sampling rate through `portico.yaml`. Neither primitive requires a sidecar or an external agent to deliver value — `stdout` mode is a fully self-contained starting point.

---

## Structured logging

Every component writes to a single `*slog.Logger` created at startup by `internal/telemetry.NewLogger`. There are no per-package log globals, no `fmt.Println`, and no `log.Print*` calls in production code paths.

### Formats and levels

The logger is configured via the top-level `logging` block:

```yaml
logging:
  level: info      # debug | info | warn | error
  format: json     # json | text
```

`format: json` is the default and the expected value in production deployments. It produces one JSON object per log line suitable for ingestion by any structured log aggregator. `format: text` produces a human-readable key=value format useful during local development. Unknown level strings fall through to `info`.

### Severity conventions

| Level | When to use |
|-------|-------------|
| `debug` | Details useful only during active debugging: southbound round-trip timing, policy decision inputs, snapshot comparison results. |
| `info` | Lifecycle events an operator should know about: process started, server registered, session opened, snapshot created. |
| `warn` | Unexpected but recovered: a downstream timeout that was retried, a list_changed notification suppressed in stable mode, a spanstore overflow. |
| `error` | A request or operation failed: policy denial (if surfaced as an error path), credential exchange failure, session dropped. |

### Standard attributes

Every request-scoped log child (built once per request via `logger.With(...)`) carries a consistent attribute set. Components add the subset that is meaningful at their level:

| Attribute key | Where set |
|---|---|
| `tenant_id` | All tenant-scoped handlers |
| `request_id` | HTTP middleware, from the chi request-id header |
| `trace_id` | Wherever a span is active |
| `span_id` | Wherever a span is active |
| `session_id` | MCP session lifetime |
| `server_id` | Southbound calls, registry operations |
| `tool` | Tool dispatch, policy evaluation |

These keys are consistent between logs and traces — a `trace_id` in a log line is the same value carried by the active OTel span, making log-to-trace correlation straightforward in any observability stack.

### What is never logged

Tool arguments and tool results are never written to the log. They routinely carry credentials, personal data, and other secrets. Any payload that must be recorded for auditability goes through the audit redactor before being persisted to the audit store — not the log stream. Raw secret values, vault contents, and injected credential material are also excluded. See [Audit](/concepts/audit) for the persistence path.

---

## OpenTelemetry tracing

Portico uses the [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/) (v1.44.0) with the W3C TraceContext propagator. Tracing is disabled by default; enabling it requires setting `telemetry.enabled: true` and choosing an exporter. When tracing is disabled, the W3C propagator is still registered in propagator-only mode so inbound `traceparent` headers continue to round-trip correctly — a tracing-disabled Portico placed mid-chain does not break an upstream trace.

### TelemetryConfig reference

```yaml
telemetry:
  enabled: true

  # Human-readable name attached to every span as service.name.
  # Defaults to "portico" when omitted.
  service_name: portico

  # Exporter: otlp_grpc | otlp_http | stdout | none
  exporter: otlp_grpc

  # Collector endpoint.
  # otlp_grpc default: localhost:4317
  # otlp_http default: localhost:4318
  otlp_endpoint: "otel-collector:4317"

  # Optional headers forwarded with every OTLP export request.
  # If "authorization" or "api-key" / "x-api-key" are present,
  # TLS is enabled automatically; otherwise the exporter uses
  # an insecure connection.
  otlp_headers:
    authorization: "Bearer <token>"

  # Fraction of traces to sample. Range: 0.0–1.0.
  # Default: 1.0 (record every trace).
  sample_rate: 0.25

  # Extra key=value pairs merged into the OTel Resource.
  # service.name is always set; these extend it.
  resource_attrs:
    deployment.environment: production
    service.version: "0.9.0"

  # How often the drift detector re-fingerprints active sessions'
  # downstream servers. Accepts Go duration strings ("60s", "5m").
  # Default: 60s.
  drift_interval: 60s
```

When `exporter: stdout` is set, finished spans are printed to stderr with pretty-printing — useful for local development without a collector. When `exporter: none` (or `enabled: false`), no spans are recorded and the binary behaves as if OTel were not compiled in.

::: tip Propagator-only mode
Even with tracing fully disabled, Portico registers the W3C TraceContext + Baggage propagator globally. An inbound `traceparent` header is extracted, preserved in the request context, and injected into every downstream MCP call. The span context flows through the chain even though no spans are produced locally.
:::

### Exporter TLS

The `otlp_grpc` and `otlp_http` exporters infer TLS from the `otlp_headers` map. If the map contains a key matching `authorization`, `api-key`, or `x-api-key` (case-insensitive), the exporter upgrades to a secure connection automatically. Deployments without authentication headers use an insecure (plaintext) connection — suitable for a local collector on the same host or an in-cluster sidecar.

---

## Span catalog

Every span name and attribute key is defined as a constant in `internal/telemetry/attrs.go` and `internal/telemetry/spans.go`. Nothing in the codebase hardcodes a span name as a raw string; the constants are the single source of truth.

### Span names

| Span | Triggered by |
|---|---|
| `mcp.session` | Session open/close lifecycle |
| `mcp.request` | Every inbound JSON-RPC method dispatch |
| `mcp.tool_call` | `tools/call` — parent for the full call tree |
| `policy.evaluate` | Policy engine decision (child of `mcp.tool_call`) |
| `approval.flow` | Human-in-the-loop elicitation (child of `mcp.tool_call`) |
| `credential.resolve` | Credential injection / OAuth exchange (child of `mcp.tool_call`) |
| `southbound.call` | Outbound request to a downstream MCP server |
| `audit.emit` | Audit event write to the store |
| `snapshot.create` | Catalog snapshot created at session start |
| `snapshot.drift_check` | Periodic schema fingerprint comparison |

A typical `tools/call` trace looks like:

```
mcp.request
└── mcp.tool_call          tenant.id, session.id, mcp.tool, mcp.server_id
    ├── policy.evaluate    policy.allow, policy.reason, policy.risk_class
    ├── approval.flow      approval.id, approval.outcome   (if required)
    ├── credential.resolve credential.strategy, credential.cache_hit
    ├── southbound.call    mcp.server_id, mcp.transport, peer.url
    └── audit.emit         audit.type
```

### Span attributes

All attribute keys follow `<namespace>.<field>` naming:

| Attribute | Type | Set on |
|---|---|---|
| `tenant.id` | string | All session and tool spans |
| `user.id` | string | Session span |
| `session.id` | string | Session, tool call |
| `mcp.request_id` | string | Request span |
| `mcp.method` | string | Request span |
| `mcp.tool` | string | Tool call |
| `mcp.server_id` | string | Tool call, southbound |
| `mcp.skill_id` | string | Tool call (when routed via a Skill) |
| `mcp.transport` | string | Southbound: `stdio` or `http` |
| `peer.url` | string | Southbound HTTP calls |
| `snapshot.id` | string | Snapshot create and drift check |
| `snapshot.servers` | string | Snapshot create |
| `snapshot.tools_count` | int | Snapshot create |
| `drift.detected` | bool | Drift check |
| `policy.allow` | bool | Policy evaluate |
| `policy.reason` | string | Policy evaluate |
| `policy.requires_approval` | bool | Policy evaluate |
| `policy.risk_class` | string | Policy evaluate |
| `approval.id` | string | Approval flow |
| `approval.elicit` | bool | Approval flow |
| `approval.outcome` | string | Approval flow |
| `credential.strategy` | string | Credential resolve |
| `credential.cache_hit` | bool | Credential resolve |
| `audit.type` | string | Audit emit |

---

## Trace context propagation

Portico propagates W3C TraceContext across every transport boundary so that a trace started by a client continues unbroken through the gateway, the policy engine, the credential exchange, the downstream MCP server, and back.

### HTTP northbound and southbound

On inbound requests, the `traceparent` (and optional `tracestate`) HTTP headers are extracted from the request and loaded into the Go `context.Context`. On outbound HTTP southbound calls, `telemetry.InjectIntoHTTP` writes the active span's `traceparent` header into the request before it leaves Portico. Every downstream HTTP MCP server receives the same trace ID as the originating client request.

### Stdio southbound

For stdio-transport MCP servers, trace context is injected in two ways:

1. **Process environment**: when the process supervisor spawns a stdio server, it sets `MCP_TRACEPARENT` and `TRACEPARENT` environment variables to the current `traceparent` value. This covers servers that read environment-based trace context at startup.

2. **`_meta.traceparent`**: for per-request propagation, `telemetry.InjectIntoMCPMeta` writes `traceparent` (and `tracestate` if present) into the `_meta` block of every outbound MCP JSON-RPC request. `telemetry.ExtractFromMCPMeta` reads it back on the inbound side for servers that reflect or forward `_meta`.

See [MCP Southbound](/concepts/mcp-southbound) for how the southbound client layer uses these helpers.

---

## Sampling

The `sample_rate` field controls a `TraceIDRatioBased` sampler applied globally. The default of `1.0` records every trace. Lower values reduce export volume proportionally:

```yaml
telemetry:
  sample_rate: 0.1   # 10% of traces
```

A zero value is treated as `1.0` (full sampling). There is no minimum — setting `0.001` is valid for very high-volume deployments.

::: warning Sampling and audit correlation
The audit store records every tool call regardless of the sample rate. If tracing is sampled at less than 100%, some audit events will have a `trace_id` that was not recorded by the tracer. This is expected behavior; the audit trail is always complete. Set `sample_rate: 1.0` when full trace-to-audit correlation is required.
:::

---

## Local span store

In addition to the external OTLP exporter, Portico maintains an embedded span store (`internal/telemetry/spanstore`). The store is backed by the same SQLite database that holds audit events and catalog snapshots. It is populated by a tee exporter that forwards every finished span to the store **after** sending it to the external collector, without ever blocking the trace hot path.

The tee uses a bounded in-memory channel (default 1,024 entries) with a background drain goroutine. When the channel is full, the oldest queued entry is dropped and a single `warn` log line is emitted at most every 10 seconds:

```
spanstore: dropped spans (overflow) count=47 buffer_size=1024
```

The store is queried by the session inspector in the Console (`GET /api/spans?session_id=...`) and by the `inspect-session` CLI command. It enables trace waterfall views and session debugging without any dependency on an external trace backend.

The span store schema is tenant-scoped: every `Put` and `Query` call is filtered by `tenant_id`, preserving the multi-tenancy invariant across the full observability surface. Spans can be queried by session or by trace ID:

```
GET /api/spans?session_id=<id>
GET /api/spans?trace_id=<id>
```

Both endpoints require a valid tenant credential; cross-tenant queries are not possible. See [Catalog and Sessions](/concepts/catalog-and-sessions) for how sessions and snapshots relate to the span store.

---

## Drift detection

The drift detector runs as a background goroutine at the interval configured by `telemetry.drift_interval` (default: 60 seconds). On each tick it re-fingerprints the tool schemas of every active session's downstream servers and compares them against the catalog snapshot recorded at session start. When a schema changes, a `schema.drift` audit event is emitted and the session inspector's drift banner activates.

Drift detection spans appear in traces as `snapshot.drift_check` with the `drift.detected` boolean attribute. See [Drift Detection](/concepts/drift-detection) for the full model.

---

## Offline session inspection

The `portico inspect-session` CLI command reads directly from the SQLite database (read-only open) without requiring a running server. It produces a structured dump of the session, its snapshot, all audit events, pending approvals, and a trace summary:

```bash
portico inspect-session <session_id> --output json
portico inspect-session <session_id> --output table
portico inspect-session <session_id> --output json --since 2025-01-15T10:00:00Z
```

The `--since` flag accepts an RFC3339 timestamp and filters audit and drift events to the specified window. The `--output table` format is suitable for quick terminal review; `--output json` is designed for piping into `jq` or other processing tools. See [CLI Reference](/reference/cli) for the full flag surface.

---

## Security properties

The telemetry subsystem enforces two hard rules that apply across both logs and traces:

**No secrets in spans or logs.** Credential values, vault secrets, OAuth tokens, and injected headers are never written to any log line or span attribute. The `credential.resolve` span records `credential.strategy` (e.g., `env`, `header`, `oauth_exchange`) and a cache-hit boolean — never the credential value itself.

**No raw tool arguments.** Tool call arguments and results are excluded from all log and trace output. They are summarized, truncated, or redacted before reaching the audit store. This applies regardless of log level — `debug` mode does not expose tool payloads.

---

## Resource attributes

The `resource_attrs` map is merged with the OpenTelemetry SDK's default resource, which contributes process, host, and runtime attributes automatically. Operator-supplied keys override SDK defaults on schema URL conflict. Common additions:

```yaml
telemetry:
  resource_attrs:
    deployment.environment: staging
    service.version: "1.0.0"
    k8s.cluster.name: prod-eu
    k8s.namespace.name: portico
```

These appear on every span exported from the process and are visible in any OTel-compatible backend as resource-level metadata.

---

## Code Mode token accounting

[Code Mode](/concepts/code-mode-savings) records per-execution token usage as structured attributes on the `mcp.tool_call` span. The token accounting surfaces through the same trace pipeline — operators with access to a trace backend can aggregate Code Mode savings across sessions using standard span queries without any separate instrumentation.

---

## Related

- [Audit](/concepts/audit) — the persistent, redacted event trail that complements trace spans
- [Drift Detection](/concepts/drift-detection) — schema fingerprinting and the `schema.drift` event lifecycle
- [Catalog and Sessions](/concepts/catalog-and-sessions) — how catalog snapshots are tied to sessions and span data
- [MCP Southbound](/concepts/mcp-southbound) — trace context injection into downstream MCP calls
- [Code Mode savings](/concepts/code-mode-savings) — token accounting surfaced through the trace pipeline
- [Configuration Reference](/reference/configuration) — full `telemetry` and `logging` block documentation
- [CLI Reference](/reference/cli) — `inspect-session` and other operational commands
