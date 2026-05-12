# Phase 15 — HTTP and gRPC Reverse Proxy

> Self-contained implementation plan. Builds on Phase 14. Adds `http_proxy` and `grpc_proxy` backend drivers so Portico can put REST microservice traffic and gRPC service traffic behind the same gateway as MCP/A2A/LLM, sharing the same tenancy, policy, audit, vault, and observability envelopes.

## Goal

After Phase 15, an operator can declare a route on any listener that targets an arbitrary HTTP or gRPC upstream:

```yaml
backends:
  - name: billing-svc
    driver: http_proxy
    config:
      upstreams:
        - { url: http://billing-1.svc:8080, weight: 1 }
        - { url: http://billing-2.svc:8080, weight: 1 }
      health: { path: /healthz, interval: 5s, timeout: 1s, unhealthy_threshold: 3 }
      retry:  { attempts: 3, per_try_timeout: 2s, retry_on: ["5xx", "connect-failure"] }
      circuit_breaker: { max_concurrent: 200, max_pending: 100 }
      egress_auth: { kind: oauth_token_exchange, vault_ref: secrets/billing-oauth }
      transform:
        request:
          add_headers:    { X-Tenant-Id: "{{ .Tenant.ID }}" }
          remove_headers: [Authorization]
        response:
          add_headers: { Cache-Control: "no-store" }

  - name: search-grpc
    driver: grpc_proxy
    config:
      upstreams: [{ url: dns://search.svc:50051 }]
      tls:      { ca_file: /etc/portico/upstream-ca.pem, server_name: search.svc }
      retry:    { attempts: 2, retry_on: ["UNAVAILABLE","DEADLINE_EXCEEDED"] }

listeners:
  - name: api
    bind: default
    protocol: http
    routes:
      - { match: { path_prefix: /billing/ }, backend: billing-svc }
      - { match: { path_prefix: /search/grpc/ }, backend: search-grpc }
```

Every request through these routes goes through the V2 envelope (tenant → JWT → policy → audit → tracing → backend) defined in Phase 14 §5.2.

## Why this phase exists

The single biggest gap between Portico and `agentgateway` after Phase 14 is *protocol breadth*. A buyer consolidating gateways needs one gateway to handle their REST microservices and their MCP/A2A traffic. Phase 15 closes that gap.

It is built on Phase 14's substrate so the substrate's design is validated by a real second protocol family. If routes/backends/middleware needed to bend in surprising ways to host HTTP proxying, Phase 14 got the abstractions wrong; the cost of finding out is paid here, where one phase's worth of refactor is cheap, rather than later when A2A and dynamic config are also riding on it.

This phase is also where Portico picks up the **CVE surface** of being an HTTP proxy. That is non-trivial: HTTP request smuggling, header injection, ambiguous transfer encodings, oversized headers, slowloris. The acceptance criteria explicitly enumerate the smuggling shapes that integration tests must cover.

## Prerequisites

- Phase 14 substrate landed and stable. Specifically:
  - `internal/dataplane/backends/ifaces.Backend` interface.
  - `internal/dataplane/middleware` chain.
  - `internal/dataplane/server.DataPlane` lifecycle.
  - The §4.4 seam pattern is the rule for adding new drivers.
- Phase 5 vault and OAuth token-exchange machinery (egress auth strategies reuse them).
- Phase 6 telemetry surface (HTTP-proxy spans use the same exporter).

## Out of scope (explicit)

- **No layer-7 WAF.** No SQL-injection scanning, no rate-by-pattern body inspection. Phase 17 adds tool-poisoning content scanning for *agentic* surfaces; generic WAF is post-V2.
- **No service mesh features.** No mTLS sidecar, no traffic mirroring, no shadow traffic. Portico stays north-south.
- **No HTTP/3.** Phase 15 supports HTTP/1.1 and HTTP/2 (cleartext + TLS). HTTP/3 / QUIC is post-V2.
- **No WebSocket proxying as a first-class feature.** The `http_proxy` driver supports the standard Connection: Upgrade dance for WebSocket on a single upstream, but does not introduce session affinity or fan-out for WebSocket. MCP's WebSocket transport (if it ever lands) goes through the MCP listener, not http_proxy.
- **No gRPC-Web translation.** gRPC-Web is post-V2. gRPC over HTTP/2 only.
- **No service discovery beyond DNS and static.** Consul / etcd / Kubernetes-EndpointSlice integrations are post-V2 (Phase 19's K8s operator may surface EndpointSlices automatically).
- **No advanced load-balancing policies.** Round-robin and weighted random in Phase 15. Least-request, ring-hash, etc. are post-V2.

## Deliverables

1. **`internal/dataplane/backends/http_proxy/`** — driver implementing the `Backend` interface, wrapping `net/http` with strict header validation, retry, circuit breaker, weighted-random load balancing, health checks, and request/response transformation.
2. **`internal/dataplane/backends/grpc_proxy/`** — driver wrapping `google.golang.org/grpc` (CGo-free), proxying gRPC requests with retry and TLS.
3. **`internal/dataplane/listeners/http/`** — generic HTTP listener kind. Owns the wire (TLS termination if attached to a TLS bind), the route table, and the standard middleware chain. The Phase 14 `rest_api` listener kind continues to host the control-plane REST API; the new `http` kind hosts arbitrary HTTP routes.
4. **`internal/dataplane/middleware/auth_egress.go`** — middleware that, given a route's `egress_auth` config, fetches credentials from the vault (or completes a token exchange) and attaches them to the outbound request. Strategies: `static_header`, `bearer_from_vault`, `basic_from_vault`, `mtls_client_cert`, `oauth_token_exchange`.
5. **`internal/dataplane/middleware/transform.go`** — request/response header add/remove/rewrite, path rewrite, body templating (Go `text/template` over a tightly typed context — no arbitrary code execution).
6. **Health checking** — `internal/dataplane/health/` defines a passive (response-driven) and active (probe-driven) health checker. Backends mark upstreams unhealthy and exclude them from load-balancing for a configurable window.
7. **Retry + circuit breaker** — `internal/dataplane/resilience/` houses retry budgets, per-try timeouts, and a Hystrix-style circuit breaker. Defaults are conservative; per-route overrides via `config.retry` / `config.circuit_breaker`.
8. **Mock HTTP and gRPC servers** — `examples/servers/mock/cmd/mockhttp/` and `examples/servers/mock/cmd/mockgrpc/` for integration tests; both deterministic and inspectable.
9. **REST CRUD over backends and routes** — `internal/server/api/handlers_routes.go` and `handlers_backends.go` extend Phase 14's read-only surface to support POST/PUT/DELETE for `http_proxy` and `grpc_proxy` backends and for routes that target them. Per-tenant ownership; admin scope for cross-tenant.
10. **Console screens** — `/dataplane/routes`, `/dataplane/routes/[id]`, `/dataplane/routes/new`, `/dataplane/backends/new` (with HTTP and gRPC variant cards). The §4.5.1 operator UX gates apply: every list page has a `+ Add` CTA; forms cover the full plan-defined surface; Playwright covers the create flows.
11. **Smoke** — `scripts/smoke/phase-15.sh` exercises a full `/billing/*` HTTP-proxy round trip through the standard middleware chain plus a gRPC unary call through `grpc_proxy`.
12. **Security tests** — `test/integration/http_smuggling_test.go` covers the canonical request-smuggling shapes (CL.TE, TE.CL, TE.TE) and asserts Portico rejects all of them; `test/integration/header_injection_test.go` asserts CR/LF injection in headers is sanitised.

## Acceptance criteria

1. **HTTP round trip.** A GET to `/billing/invoices` proxies to the mock HTTP server, returns the upstream body verbatim, and produces a span with `route_id`, `backend_id`, `upstream_url`, `upstream_status` attributes.
2. **gRPC round trip.** A unary call to `search.Search/Query` through the `grpc_proxy` backend reaches the mock gRPC server and returns the response. Streaming RPCs (server-streaming, client-streaming, bidi) work.
3. **Per-route policy applies.** A `policy.deny` rule on `route_id=billing-svc` causes Portico to return 403 without contacting the upstream; an audit event records the decision.
4. **Per-route auth-egress applies.** A route configured with `egress_auth: bearer_from_vault` attaches a `Authorization: Bearer <vault>` header to the upstream request that was *not* present in the inbound request. The inbound `Authorization` (the JWT) is not forwarded unless explicitly opted in.
5. **Token exchange end-to-end.** A route configured with `egress_auth: oauth_token_exchange` performs RFC 8693 against the configured token endpoint, attaches the exchanged token to the upstream request, and audits the exchange. Exchange tokens are cached per (tenant, route, subject) for the documented lifetime.
6. **Retry budget honoured.** Configured `retry.attempts: 3` retries 5xx and connect-failures up to twice (3 total tries); does not retry 4xx; respects `per_try_timeout`. A test asserts the retry count via the mock's request counter.
7. **Circuit breaker trips and recovers.** With `max_concurrent: 1`, two simultaneous slow requests result in the second being rejected with 503. After the slow request completes and the half-open probe succeeds, traffic resumes.
8. **Health checks exclude unhealthy upstreams.** Two upstreams; the mock kills one mid-test; subsequent requests bypass the dead upstream. When the dead upstream returns to health, traffic resumes within the configured window.
9. **Weighted load balancing.** Two upstreams with `weight: 3` and `weight: 1` produce ~75/25 split over 1000 requests (±3% tolerance).
10. **Request transformation.** `add_headers` renders the template against the request envelope (`Tenant`, `User`, `Request`); the rendered headers reach the upstream. `remove_headers` strips inbound headers before they reach the upstream.
11. **Response transformation.** `add_headers` on the response side reaches the client; `remove_headers` strips upstream headers before they reach the client.
12. **Smuggling defence.** Every test case in `test/integration/http_smuggling_test.go` is rejected with 400 and an audit event of type `http.smuggling_attempt`. No upstream contact is made.
13. **Header injection defence.** CR / LF in header names or values is rejected with 400. Inbound `Host` header beyond RFC 7230 grammar is rejected.
14. **Slowloris defence.** A client that sends 1 byte every 5 s of a 1 MB body is closed with a `read_timeout` after the configured `client_body_read_timeout` (default 30 s). The connection is closed at the TCP level; the upstream is not contacted.
15. **Console screens.** `+ Add` on `/dataplane/routes` opens a form covering all of: match (path/method/header/host), backend reference, middleware override, optional notes. The form persists, the route appears in the table, and a smoke check confirms it resolves traffic.
16. **Multi-tenant isolation.** Route `R-A` belongs to tenant A. A request from tenant B's JWT to a path matching `R-A` does not reach the route. An integration test asserts this with two tenants and one shared listener.
17. **No CGo introduced.** `gRPC` is `google.golang.org/grpc` pure-Go. CI build with `CGO_ENABLED=0` succeeds. Binary size delta vs. Phase 14 ≤ +25 MB.
18. **Smoke gate.** `scripts/smoke/phase-15.sh` shows OK ≥ 18, FAIL = 0; prior phases' smokes still pass.
19. **Coverage.** `internal/dataplane/backends/http_proxy/` ≥ 85%, `internal/dataplane/backends/grpc_proxy/` ≥ 80%, `internal/dataplane/middleware/auth_egress.go` ≥ 85%, `internal/dataplane/resilience/` ≥ 85%.

## Architecture

### 6.1 Package layout

```
internal/dataplane/
├── backends/
│   ├── http_proxy/
│   │   ├── backend.go
│   │   ├── upstream.go
│   │   ├── transport.go     # http.RoundTripper with strict validation
│   │   └── backend_test.go
│   └── grpc_proxy/
│       ├── backend.go
│       ├── stream.go
│       └── backend_test.go
├── listeners/
│   └── http/
│       ├── listener.go
│       └── listener_test.go
├── middleware/
│   ├── auth_egress.go
│   └── transform.go
├── health/
│   ├── checker.go
│   └── checker_test.go
└── resilience/
    ├── retry.go
    ├── circuit.go
    └── circuit_test.go
```

### 6.2 HTTP request flow (proxy route)

```
inbound TCP → bind → listener(http) → TLS termination (if any)
            → wire decode (net/http parse, strict)
            → route.Match()
            → middleware chain (tenant → jwt → policy → audit-open → tracing-open
                                 → auth_egress (egress credentials) → transform.req)
            → http_proxy.Backend.Dispatch
                → load-balancer pick upstream
                → retry.Do(per-try-timeout, retry-on rules)
                    → circuit_breaker.Acquire
                        → http.RoundTripper to upstream
                    → circuit_breaker.Release
            → middleware tail (transform.resp → audit-close → tracing-close)
            → response
```

### 6.3 gRPC request flow

`grpc_proxy` uses `grpc.ClientConn` per upstream, multiplexes through it. For unary RPCs, the same retry / circuit-breaker shape as HTTP. For streaming RPCs, retries are limited to "before any frames have been sent" (idempotency); after the first frame, errors propagate verbatim.

The gRPC backend exposes the same neutral `Request`/`Response` shape as HTTP; the listener encodes/decodes the gRPC framing. The middleware chain is identical.

### 6.4 Egress auth strategies

Each strategy lives in its own file under `internal/dataplane/middleware/auth_egress/`:

| Strategy             | What it does                                                                                                        |
|----------------------|---------------------------------------------------------------------------------------------------------------------|
| `static_header`      | Attach a fixed header to the upstream request. Value may reference a vault entry.                                  |
| `bearer_from_vault`  | Read a bearer token from the vault, attach as `Authorization: Bearer ...`.                                         |
| `basic_from_vault`   | Read user/password from the vault, attach as `Authorization: Basic ...`.                                           |
| `mtls_client_cert`   | Attach a client certificate (from the vault) to the TLS handshake with the upstream.                              |
| `oauth_token_exchange` | RFC 8693 against a configured token endpoint, with vault-stored client credentials. Tokens cached per (tenant, route, sub). |

The inbound `Authorization` header is **not** forwarded by default. Operators must explicitly opt in via `transform.request.add_headers.Authorization: "{{ .Inbound.Headers.Authorization }}"`. This default mirrors the V1 credential-handling rule from `AGENTS.md` §7.3 ("no credential passthrough").

### 6.5 Resilience

- **Retry budget**: `attempts` total tries; `per_try_timeout` per try; `retry_on` is a list of conditions (`5xx`, `connect-failure`, `timeout`, `gRPC: UNAVAILABLE`, etc.). Retries respect the parent `ctx`'s deadline.
- **Circuit breaker**: per-backend, per-upstream concurrent-request and pending-request caps. Breaker states: closed → open (after N consecutive failures or > X% error rate) → half-open (probe) → closed.
- **Health checks**: passive (response status drives the failure counter) and active (HTTP GET / gRPC health-check call on a schedule). Unhealthy upstreams are excluded from load-balancing for `unhealthy_window` (default 30 s) before re-probing.

### 6.6 Smuggling defence (binding)

`internal/dataplane/listeners/http/listener.go` rejects, with 400 and an `http.smuggling_attempt` audit event:

- A request with both `Content-Length` and `Transfer-Encoding`.
- A request with `Transfer-Encoding: chunked` and a body that is not actually chunked.
- A request with multiple `Content-Length` values that disagree.
- A request with `Transfer-Encoding` containing values other than `chunked` or `identity` (case-insensitive).
- A request with non-ASCII or control characters in header names or values.

These are enforced by a custom `http.Transport`-side parser that runs before the standard library hands the request to the handler. The parser is small and is integration-tested against a curated list of smuggling test cases.

## Configuration extensions

Adds to Phase 14's shape:

```yaml
backends:
  - name: <string>
    driver: http_proxy | grpc_proxy
    config:
      upstreams: [...]
      health:    {...}
      retry:     {...}
      circuit_breaker: {...}
      egress_auth:     {...}
      transform:       {...}
      tls:             {...}    # for grpc_proxy or for HTTPS upstreams
```

Hot reload: yes. New routes/backends apply on the next request; in-flight requests against a mutated route complete on the previous version.

## REST APIs

Per-tenant unless `admin` scope present.

| Method | Path                                       | Body / params                              | Returns                          |
|--------|--------------------------------------------|--------------------------------------------|----------------------------------|
| GET    | `/api/dataplane/routes`                    | filter by listener, backend, path-prefix   | array                            |
| POST   | `/api/dataplane/routes`                    | route spec                                 | 201 + route                      |
| PUT    | `/api/dataplane/routes/{id}`               | route spec                                 | 200 + route                      |
| DELETE | `/api/dataplane/routes/{id}`               | -                                          | 204                              |
| GET    | `/api/dataplane/backends`                  | filter by driver                           | array                            |
| POST   | `/api/dataplane/backends`                  | backend spec (driver-specific config)      | 201 + backend                    |
| PUT    | `/api/dataplane/backends/{id}`             | backend spec                               | 200 + backend                    |
| DELETE | `/api/dataplane/backends/{id}`             | -                                          | 204                              |
| GET    | `/api/dataplane/upstreams/{backend_id}/health` | -                                      | per-upstream health snapshot     |

Validation errors return `422` with `{"error":"invalid_route", "details":{"path":"/match.path_prefix","message":"required"}}` (JSON-Pointer-shaped per Phase 8 convention).

## Implementation walkthrough

1. **`http_proxy` skeleton + `mockhttp` server.** Driver compiles, registers, can dispatch a single request to a single upstream with no retry / health / transform. Smoke proves a round trip.
2. **Strict request parsing + smuggling defence.** Add the parser; integration tests for every smuggling shape.
3. **Multi-upstream + weighted random.** Add load-balancer; tests cover distribution.
4. **Health checking.** Active + passive; tests use the mock's `kill` endpoint.
5. **Retry + circuit breaker.** Resilience package; tests cover each retry case and breaker state.
6. **Egress auth strategies.** Implement each; reuse vault and OAuth code from Phase 5; tests for each strategy.
7. **Transform middleware.** Header add/remove/rewrite, path rewrite, body templating.
8. **`grpc_proxy`.** Reuse the resilience and health packages; gRPC-specific stream handling.
9. **CRUD endpoints.** REST API with validation.
10. **Console screens.** Routes list, route create/edit, backend create/edit (HTTP variant + gRPC variant). Playwright specs.
11. **Smoke + microbench.** `phase-15.sh`, perf gate against Phase 14 baseline.

## Test plan

Unit:

- `TestHTTPProxy_DispatchSingleUpstream`
- `TestHTTPProxy_WeightedRandom_Distribution`
- `TestHTTPProxy_RetryOn5xx`
- `TestHTTPProxy_RetryRespectsParentDeadline`
- `TestHTTPProxy_NoRetryOn4xx`
- `TestHTTPProxy_CircuitBreaker_TripsOnConsecutiveErrors`
- `TestHTTPProxy_CircuitBreaker_HalfOpenProbeRecovers`
- `TestHTTPProxy_HealthCheck_ExcludesUnhealthy`
- `TestHTTPProxy_Transform_AddRemoveHeaders`
- `TestHTTPProxy_Transform_PathRewrite`
- `TestHTTPProxy_Transform_BodyTemplate_NoCodeExecution`
- `TestEgressAuth_StaticHeader`
- `TestEgressAuth_BearerFromVault`
- `TestEgressAuth_OAuthTokenExchange_Caches`
- `TestEgressAuth_NeverForwardsInboundAuthByDefault`
- `TestGRPCProxy_UnaryDispatch`
- `TestGRPCProxy_ServerStream`
- `TestGRPCProxy_RetryOnUnavailable`
- `TestHTTPListener_RejectsSmuggling_CL_TE`
- `TestHTTPListener_RejectsSmuggling_TE_CL`
- `TestHTTPListener_RejectsSmuggling_TE_TE`
- `TestHTTPListener_RejectsHeaderCRLF`
- `TestHTTPListener_SlowlorisTimeout`

Integration (`test/integration/`):

- `TestE2E_HTTPProxy_BillingRoute_Roundtrip`
- `TestE2E_HTTPProxy_PolicyDeny_BlocksUpstream`
- `TestE2E_HTTPProxy_OAuthExchange_Endtoend`
- `TestE2E_GRPCProxy_SearchRoute_Roundtrip`
- `TestE2E_HTTPProxy_MultiTenantIsolation`
- `TestE2E_HTTPProxy_HotReload_AddBackend`

## Common pitfalls

1. **Forwarding the inbound JWT to upstreams.** Default-off; opt-in via explicit `transform.add_headers`. The "no credential passthrough" rule from `AGENTS.md` §7.3 applies here.
2. **Blanket retry of POST.** Idempotency is the operator's concern. Default `retry_on` does not include `5xx` for non-idempotent methods unless the operator opts in by writing `retry_on: ["5xx"]` explicitly. The driver does not infer.
3. **Streaming with retry.** gRPC streaming retries only before any frames flow. After that, errors propagate.
4. **Body templating that allows arbitrary code.** Use `text/template` with a tightly typed context (`Tenant`, `User`, `Request.Headers`, `Request.Path`). No template functions that touch the filesystem, network, or environment.
5. **Health-check loops.** Each backend owns its checker goroutines; `Close` joins them. The leak test catches forgotten ones.
6. **Logging the upstream `Authorization` on error.** Redactor must scrub it. Test fixture asserts.
7. **Per-tenant routes leaking.** Route ownership is `(tenant, route)`. A request whose `tenant.MustFrom(ctx)` differs from `route.Tenant` does not match — even if the path matches. The multi-tenant isolation test catches this.

## Hand-off to Phase 16

Phase 16 inherits the `http_proxy` and `grpc_proxy` drivers as templates for the `a2a_peer` driver: same shape (upstreams, health, retry, transform), different protocol semantics. Phase 16 also reuses the egress-auth machinery for A2A peer authentication.

The Console `+ Add` story for Phase 16 follows the pattern Phase 15 establishes: a route create form with backend variants per driver kind, with the §4.5.1 gates intact.
