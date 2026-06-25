# OAuth token exchange

Portico never forwards an agent's incoming token to a downstream MCP server. Instead, it performs an RFC 8693 token exchange on the agent's behalf, obtaining a narrowly-scoped access token whose audience and permission set are limited to exactly one downstream service. The agent's broad identity credential stays behind the gateway; the downstream service receives only what it needs.

This page explains the mechanics of that exchange, how to configure it per server, how the resulting token reaches the downstream service, and when the passthrough escape hatch applies.

## Why token exchange instead of passthrough

An agentic workflow can accumulate significant privilege. An agent might hold a JWT with scopes spanning several services, personal data APIs, or administrative surfaces. Forwarding that token to each MCP server it calls would mean every server in a multi-server workflow gets access to the full token surface — far broader than any individual server needs.

Token exchange solves this by delegation without broadening:

1. Portico's identity (its OAuth client ID and secret) vouches to a central IdP that it is acting on behalf of the authenticated agent.
2. The IdP issues a downstream-specific token scoped to the requested `audience` and `scope`, bounded by the policies the IdP enforces for that client.
3. The downstream server receives only that audience-bound token. It has no visibility into the agent's original JWT or any other tokens Portico may have obtained for other servers in the same session.

This is the credential architecture that satisfies Portico's core guarantee: agents never see broad downstream tokens, and no server in the mesh can escalate privilege by examining what was sent to it.

## RFC 8693 wire protocol

Portico's exchanger (`internal/secrets/oauth/`) POSTs to the IdP's token endpoint with the following form body:

```http
POST /oauth/token HTTP/1.1
Content-Type: application/x-www-form-urlencoded

grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Atoken-exchange
&subject_token=<agent-jwt>
&subject_token_type=urn%3Aietf%3Aparams%3Aoauth%3Atoken-type%3Ajwt
&requested_token_type=urn%3Aietf%3Aparams%3Aoauth%3Atoken-type%3Aaccess_token
&audience=<configured-audience>
&scope=<configured-scope>
```

| Field | Value | Notes |
|-------|-------|-------|
| `grant_type` | `urn:ietf:params:oauth:grant-type:token-exchange` | RFC 8693 constant |
| `subject_token` | The agent's raw incoming JWT | Portico extracts this from the `Authorization: Bearer` header |
| `subject_token_type` | `urn:ietf:params:oauth:token-type:jwt` | RFC 8693 constant |
| `requested_token_type` | `urn:ietf:params:oauth:token-type:access_token` | Access token requested |
| `audience` | Configured per server | e.g. `github-mcp`, `postgres-api` |
| `scope` | Configured per server (optional) | Space-separated; IdP may narrow further |

The client authenticates to the token endpoint using HTTP Basic Auth (`client_id` + `client_secret`). When no `client_secret` is configured (public client pattern), only `client_id` is sent in the form body.

::: tip IdP validation responsibility
The IdP must validate the `subject_token`'s issuer and audience before issuing a downstream token. Portico forwards the raw JWT and trusts the IdP to enforce its own policies. Document this expectation when configuring IdP policies for the Portico client ID.
:::

## Token cache

Exchanged tokens are cached in memory for the duration of their TTL minus a 30-second safety window. The cache key is `(tenantID, userID, audience)`, which means:

- Two users on the same tenant receive distinct cached tokens.
- The same user calling two servers with different audiences receives distinct tokens.
- Cached tokens are never shared across tenant boundaries — the tenant component of the key makes cross-tenant cache hits impossible.

When a cached entry's `ExpiresAt` is in the past the entry is evicted at the next lookup, and a fresh exchange is performed. There is no background reaper; eviction is lazy and safe under concurrent access.

### Error handling

| IdP response | Retry behavior | Audit event |
|---|---|---|
| 2xx with `access_token` | Cached | `credential.exchange.success` |
| 4xx (e.g. `invalid_client`) | Not retried — configuration is wrong | `credential.exchange.failed` |
| 5xx | Retried once with 200–300ms jitter | `credential.exchange.failed` on second failure |
| Transport error | Propagated immediately | None |

4xx responses are not retried because they indicate an operator configuration problem. A retry would just add latency before the same error. 5xx responses are retried once to absorb transient IdP outages without requiring the upstream agent to retry the tool call.

When an exchange fails, the tool call is rejected with a `policy_denied` error (`reason: credential_lookup_failed`) before any downstream request is made.

## Credential injectors

Once the exchanger returns a token, a credential injector writes it to the outbound request. Portico ships five injection strategies, selectable per server. Only `oauth2_token_exchange` involves the exchanger; the others cover static-secret patterns.

| Strategy | How it obtains the credential | Where it writes the credential |
|---|---|---|
| `oauth2_token_exchange` | RFC 8693 exchange against configured IdP | `Authorization: Bearer <token>` header on southbound HTTP requests |
| `http_header_inject` | Vault lookup; resolves <span v-pre>`{{secret:name}}`</span> in header values | Named headers on southbound HTTP requests |
| `env_inject` | Vault lookup; resolves <span v-pre>`{{secret:name}}`</span> in env declarations | Environment variables passed to stdio server processes at spawn |
| `secret_reference` | Single vault lookup by name | `Authorization: Bearer <value>` header on southbound HTTP requests |
| `credential_shim` | Reserved for future per-request stdio injection | (Not implemented in V1) |

The injector writes into a `PrepTarget` struct — a set of `Env` and `Headers` maps — that the runtime hands to the process supervisor or the southbound HTTP client. The target maps are freshly allocated per request; a previous tenant's resolved credentials cannot leak into a subsequent request.

## Server configuration

Configure token exchange under `auth` for the relevant MCP server:

```yaml
servers:
  - id: github
    transport: http
    runtime_mode: remote_static
    http:
      url: https://mcp.github.example.com/mcp
    auth:
      strategy: oauth2_token_exchange
      default_risk_class: read
      exchange:
        token_url: https://auth.example.com/oauth/token
        client_id: portico-gateway
        client_secret_ref: oauth_client_secret   # vault key name; never plain text
        audience: github-mcp
        scope: "repo read:org"
```

| Key | Type | Required | Description |
|---|---|---|---|
| `auth.strategy` | string | yes | Must be `oauth2_token_exchange` to use the exchanger |
| `auth.exchange.token_url` | string | yes | Full URL of the IdP's token endpoint |
| `auth.exchange.client_id` | string | yes | OAuth client ID for Portico's gateway credential |
| `auth.exchange.client_secret_ref` | string | recommended | Vault key name from which the client secret is resolved at runtime. Do not put the secret in plain config. |
| `auth.exchange.audience` | string | recommended | Restricts the issued token to one downstream system |
| `auth.exchange.scope` | string | no | Space-separated scope list; IdP may grant a subset |
| `auth.exchange.grant_type` | string | no | Defaults to the RFC 8693 `urn:ietf:params:oauth:grant-type:token-exchange` IRI |
| `auth.default_risk_class` | string | no | Risk class applied to tools on this server when no Skill Pack override is present |

The `client_secret_ref` value names a key in the encrypted vault for the server's tenant. Store the client secret via the CLI before starting the gateway:

```bash
portico vault put --tenant acme --name oauth_client_secret --value <secret>
```

::: warning Never store OAuth client secrets in plain configuration
`client_secret_ref` must be a vault key name. Putting a raw secret in `auth.exchange.client_secret` is rejected at config validation. The vault is the only permitted storage path.
:::

## Token delivery on southbound requests

For servers with `transport: http`, the `oauthInjector` writes the exchanged token into the HTTP RoundTripper's request headers before the request leaves Portico:

```
Authorization: Bearer <exchanged-access-token>
```

The agent's original JWT is never forwarded. The downstream MCP server sees only the audience-bound token Portico obtained from the IdP.

For servers with `transport: stdio`, the `oauth2_token_exchange` strategy is not applicable — stdio processes receive credentials via environment variables at spawn time using `env_inject`. If a stdio server needs per-request credentials, the `credential_shim` strategy (post-V1) will handle that case.

## Dev mode behavior

In dev mode (`portico dev`), Portico boots without a vault key and without a configured IdP. Tool calls to servers that declare `auth.strategy: oauth2_token_exchange` return `ErrNoSubjectToken` because there is no incoming JWT to use as the subject token. The error surfaces as `policy_denied: missing_subject_token`.

This is intentional. Dev mode verifies MCP wiring and tool routing, not production auth flows. Use a real configuration file with a configured IdP for end-to-end credential testing.

## The passthrough escape hatch

Token exchange is the default and the recommended path. Passthrough — forwarding the incoming JWT directly to a downstream server — is available as an opt-in escape hatch for deployments where the downstream server validates the agent's identity token directly and no IdP is available for exchange.

To enable passthrough for a server:

```yaml
servers:
  - id: legacy-service
    transport: http
    http:
      url: https://legacy.example.com/mcp
    auth:
      passthrough: true   # opt-in; see audit requirements below
```

::: warning Passthrough is audited and intentionally conspicuous
Setting `auth.passthrough: true` emits a `credential.passthrough` audit event on every request to that server. This event is queryable via `GET /v1/audit/events?type=credential.passthrough`. Operators reviewing audit logs will see every forwarded token use explicitly. This visibility is deliberate — passthrough broadens the token's reach, and that decision is on the record.

Do not add passthrough without understanding the downstream server's token validation model and the resulting scope expansion.
:::

Passthrough does not disable the policy engine, approval flow, or audit trail. It only changes how the outbound credential is formed.

## Audit trail for credential operations

Every credential operation emits a structured audit event regardless of whether token exchange or passthrough is in use. No token values are ever included in event payloads — events carry metadata only.

| Event type | When emitted | Payload fields (no secrets) |
|---|---|---|
| `credential.exchange.success` | Exchange returned a valid token | `audience`, `ttl_s` |
| `credential.exchange.failed` | Exchange returned an error | `audience`, `error_code` |
| `credential.injected` | Injector successfully wrote to target | `strategy`, `server_id`, `scope` |
| `credential.passthrough` | Incoming JWT forwarded directly | `server_id` |
| `vault.get` | Vault lookup for `client_secret_ref` or `secret_reference` | `name`, hit/miss (no value) |

The audit store redacts any payload field that matches a known token or credential shape before persistence. Even if an upstream caller mistakenly constructs an event containing a raw token, the redactor strips it before the SQLite insert. Query credential events via:

```bash
# Recent exchange failures for a tenant
curl -H "Authorization: Bearer $TOKEN" \
  "https://gateway.example.com/v1/audit/events?type=credential.exchange.failed&limit=50"
```

See [Audit](/concepts/audit) for query options and the full event schema.

## Multi-tenant isolation

The token cache key includes `tenantID` as its first component. Two tenants with configurations that point to the same IdP and audience receive independently cached tokens; there is no shared cache slot between them. The vault that resolves `client_secret_ref` is keyed by `(tenantID, name)` at the storage level — Portico has no API path to read one tenant's vault entry from another tenant's request context.

For detailed isolation guarantees and the integration tests that enforce them, see [Multi-tenancy](/concepts/multi-tenancy).

## Sequence diagram

```
Agent                 Portico                   IdP             Downstream MCP
  |                     |                        |                    |
  |-- tools/call ------->|                        |                    |
  |                     |-- POST /oauth/token --->|                    |
  |                     |   subject_token=<jwt>   |                    |
  |                     |   audience=downstream   |                    |
  |                     |<-- access_token --------|                    |
  |                     |   (cached for TTL-30s)  |                    |
  |                     |                        |                    |
  |                     |-- POST /mcp -----------+-------------------->|
  |                     |   Authorization: Bearer <access_token>       |
  |                     |<-- tool result ---------+---------------------|
  |<-- tool result ------|                        |                    |
```

On a cache hit, the IdP call is skipped and Portico proceeds directly to the southbound MCP request using the cached token.

## Related

- [Credentials & Vault](/concepts/credentials-vault) — how vault stores and encrypts the secrets that underpin credential injection
- [Security model](/concepts/security-model) — the full threat model, including why token exchange is the default and passthrough is opt-in
- [Audit](/concepts/audit) — querying credential events and the redaction model
- [Policy](/concepts/policy) — how risk classes and approval requirements interact with credential resolution
- [Authentication](/concepts/authentication) — how Portico authenticates incoming agents before credential injection occurs
- [MCP southbound](/concepts/mcp-southbound) — how the injected credentials reach the downstream MCP transport
