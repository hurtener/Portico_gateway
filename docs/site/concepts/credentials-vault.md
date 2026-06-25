# Credentials & Vault

Portico enforces a non-negotiable design constraint: agents never receive broad downstream tokens. Every credential used to reach a downstream MCP server or API lives inside Portico's encrypted vault and is resolved at request time, injected transparently into outbound connections, and never returned to the agent or any MCP client.

## How the vault works

The vault is a file-backed credential store (`vault.yaml`) encrypted at the value level using AES-256-GCM. The master key is the only input required to decrypt the file; by design, that key is never stored in configuration files, source code, or environment files — it is loaded exclusively from the `PORTICO_VAULT_KEY` environment variable.

### Encryption scheme (v1)

Each secret is encrypted independently. A full copy of the vault file yields no information about individual values without the master key, and a leak of one derived key yields no information about others. The scheme applied to every new write (tagged `scheme: v1`):

1. **Per-value key derivation.** An AES-256 key is derived from the master key using HKDF-SHA256 with the info string `portico/v1/<tenant_id>/<name>`. Two entries with the same name under different tenants — or two different names under the same tenant — always derive distinct keys.

2. **Authentication binding.** The AES-256-GCM `Seal` call binds the string `<tenant_id>/<name>` as Additional Authenticated Data (AAD). If a ciphertext block is physically moved to any other position in the vault file — even within the same tenant — decryption fails because the AAD no longer matches the stored tag.

3. **Random nonce.** A fresh 12-byte nonce is generated from `crypto/rand` for every write. Nonces are never reused.

4. **Atomic file writes.** The vault file is never partially written. Every `Put`, `Delete`, and `RotateKey` operation serializes a temporary file, renames it over the current vault file, and returns only after the rename succeeds. A crash mid-write leaves the previous file intact.

The on-disk format is a YAML file readable only with the master key:

```yaml
version: 1
entries:
  acme:
    github_token:
      ct: <base64 ciphertext+GCM tag>
      nonce: <base64 12-byte nonce>
      scheme: v1
  beta:
    github_token:
      ct: <base64 ciphertext+GCM tag>
      nonce: <base64 12-byte nonce>
      scheme: v1
```

`acme` and `beta` both carry a `github_token` entry. Their ciphertexts are derived from different per-value keys and carry different AAD, so they decrypt independently and cannot be swapped by an attacker who has the raw YAML file.

### Master key management

Set `PORTICO_VAULT_KEY` before starting Portico or running any `vault` CLI subcommand:

```bash
# Generate a suitable 32-byte master key and base64-encode it.
openssl rand -base64 32

# Inject it through your deployment platform's secret mechanism.
export PORTICO_VAULT_KEY="<base64-encoded 32 bytes>"
```

The value must decode to exactly 32 bytes. A shorter or longer value is rejected at startup with a clear error message.

Portico's startup behavior when `PORTICO_VAULT_KEY` is absent depends on the vault file's state:

- **File absent or empty:** Portico starts normally. Any vault lookup returns a `vault_not_configured` error, which surfaces to the caller as a policy failure.
- **File present with data:** Portico exits immediately with a descriptive error. Running a server that silently cannot resolve credentials is not acceptable.

::: warning Never hardcode the vault key
The master key must not appear in `portico.yaml`, `.env` files, Dockerfiles, test fixtures, or source code. Supply it through your deployment platform's secret injection (environment variables, mounted secrets, external secret manager). Portico's contributor normatives treat a hardcoded key as a security bug.
:::

## Tenant isolation

The vault is keyed by `(tenant_id, name)`. Every read and write method takes both fields as required parameters:

```go
Get(ctx context.Context, tenantID, name string) (string, error)
Put(ctx context.Context, tenantID, name, value string) error
Delete(ctx context.Context, tenantID, name string) error
List(ctx context.Context, tenantID string) ([]string, error)
```

There is no cross-tenant read path. The HKDF derivation and the GCM AAD both independently enforce this: a ciphertext produced for tenant `acme` cannot be decrypted under the derivation for tenant `beta`, even by an operator who physically copies the ciphertext bytes. The vault's test suite includes a dedicated test (`TestFileVault_HKDF_TenantNameBinding`) that verifies decryption fails when entries are swapped between tenants or between names within the same tenant.

For a deeper treatment of how tenant identity propagates through every layer of Portico, see [Multi-Tenancy](/concepts/multi-tenancy).

## CLI reference

The `portico vault` subcommand manages secrets without starting the gateway process. It requires `PORTICO_VAULT_KEY` and targets `./vault.yaml` by default; override the file path with `--path` on any subcommand.

### Storing a secret

```bash
# Literal value inline (avoid for highly sensitive values; may appear in shell history).
portico vault put --tenant acme --name github_token --value ghp_xxx

# From a file (recommended for multi-line values such as PEM private keys).
portico vault put --tenant acme --name tls_key --from-file ./tls.key

# From stdin (value never appears in shell history or process list).
cat ~/.config/my-service/token | \
  portico vault put --tenant acme --name service_token --from-stdin
```

Exactly one of `--value`, `--from-file`, or `--from-stdin` must be provided; providing more than one is an error.

### Reading a secret

```bash
portico vault get --tenant acme --name github_token
```

::: warning Cleartext output
`vault get` writes the secret value in plaintext to stdout. The command simultaneously emits a warning to stderr:

```
warning: printing secret value to stdout in cleartext
```

Do not use this in pipelines where stdout is captured to a log.
:::

### Listing and deleting

```bash
# List all secret names for a tenant (alphabetical order; no values returned).
portico vault list --tenant acme

# Remove a single secret.
portico vault delete --tenant acme --name github_token
```

### Key rotation

When a master key must be cycled (routine security policy, suspected exposure), `rotate-key` re-encrypts every entry in place. The operation derives fresh per-value keys from the new master and rewrites the vault file atomically. If any single entry fails to decrypt under the current key — for example, because a ciphertext was corrupted — the rotation aborts and the vault file is left byte-for-byte unchanged.

```bash
# Generate the replacement master key.
NEW_KEY=$(openssl rand -base64 32)

# Re-encrypt all entries.
portico vault rotate-key --new-key "$NEW_KEY"
# vault: key rotated. update PORTICO_VAULT_KEY before next start.

# Update your deployment's secret store, then restart the process.
export PORTICO_VAULT_KEY="$NEW_KEY"
```

::: tip Rotation is atomic or does nothing
A failed rotation (wrong current key, corrupted entry, filesystem error) leaves the vault file exactly as it was. No partial re-encryption is possible because the rotation builds the entire new layout in memory before touching the filesystem.
:::

## Credential injectors

Storing a secret in the vault is only half the picture. Portico's credential injector layer resolves vault values at tool-call time and writes them into outbound connections — as environment variables for stdio servers, or as HTTP headers for remote servers. The agent never sees a resolved value.

Each downstream server declares its credential strategy under `auth.strategy` in the server specification. Portico constructs the corresponding injector at startup and uses it for every request to that server.

### Available strategies

| Strategy | Transport | Effect |
|---|---|---|
| `env_inject` | stdio | Resolves <span v-pre>`{{secret:name}}`</span> placeholders in `auth.env` entries; writes the resulting `KEY=value` pairs into the subprocess environment at spawn time. |
| `http_header_inject` | HTTP | Resolves <span v-pre>`{{secret:name}}`</span> placeholders in `auth.headers` entries; adds the resolved headers to every southbound HTTP request. |
| `secret_reference` | HTTP | Looks up the single secret named by `auth.secret_ref`; writes it as `Authorization: Bearer <value>` on every outbound request. |
| `oauth2_token_exchange` | HTTP | Exchanges the incoming user JWT for a narrowly-scoped downstream token via RFC 8693; injects the result as `Authorization: Bearer <token>`. Tokens are cached by `(tenant, user, audience)` and evicted 30 seconds before their stated expiry. |
| `credential_shim` | stdio | Reserved for a future per-call credential injection path over a secondary control channel. Returns `not_yet_implemented` in V1. |

The placeholder syntax <span v-pre>`{{secret:name}}`</span> is recognized across `auth.env` entries, `auth.headers` values, and (future) URL templates. An <span v-pre>`{{env:name}}`</span> variant expands a host environment variable rather than a vault lookup; both are handled by the same resolver.

### env_inject example

For a stdio MCP server that needs a database connection string passed at spawn:

```yaml
servers:
  - id: postgres
    transport: stdio
    runtime_mode: per_user
    stdio:
      command: postgres-mcp
    auth:
      strategy: env_inject
      env:
        - "PG_DSN={{secret:pg_dsn}}"
      default_risk_class: write
```

When a tool call arrives and a process must be spawned, Portico resolves <span v-pre>`{{secret:pg_dsn}}`</span> by calling `vault.Get(tenantID, "pg_dsn")` and passes `PG_DSN=<value>` in the child process environment. The agent's JWT never touches the database credential.

### oauth2_token_exchange example

For a remote HTTP MCP server behind an OAuth IdP (see [OAuth Token Exchange](/concepts/oauth-token-exchange) for the full protocol detail):

```yaml
servers:
  - id: github
    transport: http
    runtime_mode: remote_static
    http:
      url: https://api.githubmcp.example.com/mcp
    auth:
      strategy: oauth2_token_exchange
      default_risk_class: read
      exchange:
        token_url: https://auth.example.com/oauth/token
        client_id: portico-gateway
        client_secret_ref: oauth_client_secret   # resolved from vault at startup
        audience: github-mcp
        scope: "repo read:org"
        grant_type: urn:ietf:params:oauth:grant-type:token-exchange
```

On each tool call, Portico presents the user's JWT as the RFC 8693 `subject_token`, receives a narrowly-scoped access token for `github-mcp`, caches it keyed by `(tenant, user, audience)` for the token's TTL minus a 30-second safety window, and injects it as `Authorization: Bearer ...` on the outbound HTTP request. A 4xx from the IdP is not retried (treat it as a configuration error); a 5xx triggers a single retry with jitter.

`client_secret_ref` names a secret stored in the vault, not a plaintext value. Store the OAuth client secret before deploying:

```bash
portico vault put --tenant acme --name oauth_client_secret --value <client-secret>
```

## Two-step reveal flow

Vault secrets are write-only from the Console and REST API perspective: list and metadata endpoints return `(tenant_id, name)` pairs but never the stored value. When an operator needs to inspect or copy a stored secret, Portico implements a two-step reveal:

**Step 1 — Issue.** The Console calls `POST /api/admin/secrets/{name}/reveal`. Portico confirms the secret exists, generates a 256-bit random token using `crypto/rand`, and holds it in memory (never written to disk or the audit log) with a 60-second TTL and an `(tenant, name, actor)` binding. The response body carries the token and its expiry timestamp:

```json
{
  "token": "<base64url-encoded 32-byte random>",
  "expires_at": "2026-06-25T14:03:00Z"
}
```

**Step 2 — Consume.** The Console immediately fetches `GET /api/admin/secrets/reveal/{token}`. Portico validates the token using a constant-time comparison (to resist timing-based probing), removes it from the in-memory map regardless of whether the lookup succeeds (single-use is non-negotiable), then reads and returns the plaintext:

```json
{
  "tenant_id": "acme",
  "name":      "github_token",
  "value":     "ghp_xxx"
}
```

Both steps emit audit events (`secret.reveal.issued`, `secret.reveal.consumed`). If the 60-second window expires before the consume request arrives, the token is gone; the operator must issue a new one. A server restart also drops all pending reveal tokens — in-memory storage is intentional, not a limitation.

::: info Reveal tokens survive only in memory
The `RevealManager` holds tokens in a process-local map. There is no database row, no log entry containing the token, and no mechanism to retrieve a token after it has been consumed or after the process restarts.
:::

## REST API

Portico exposes vault management through two API surfaces: a primary surface under `/api/admin/secrets` used by the Console, and a legacy surface under `/v1/admin/secrets` retained for backward compatibility.

### Primary API (`/api/admin/secrets`)

All endpoints require a valid JWT. Cross-tenant operations additionally require the `admin` scope.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/admin/secrets` | List `(tenant_id, name, version)` records for the requesting tenant. Admin scope may pass `?tenant=X` to query another tenant. |
| `POST` | `/api/admin/secrets` | Create a secret. Body: `{"tenant_id": "acme", "name": "k", "value": "..."}`. |
| `GET` | `/api/admin/secrets/{name}` | Return metadata for a named secret. Never returns the value. |
| `PUT` | `/api/admin/secrets/{name}` | Update a secret's value. Body: `{"value": "..."}`. |
| `DELETE` | `/api/admin/secrets/{name}` | Delete a secret. Returns `404` when not found. |
| `POST` | `/api/admin/secrets/{name}/rotate` | Re-encrypt the entry under the current master key (a Get + Put cycle). |
| `POST` | `/api/admin/secrets/{name}/reveal` | Issue a 60-second single-use reveal token. |
| `GET` | `/api/admin/secrets/reveal/{token}` | Consume a reveal token and return the plaintext once. |

### Legacy API (`/v1/admin/secrets`)

Requires the `admin` scope. Operates on explicit `(tenant, name)` path segments rather than deriving the tenant from the JWT.

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/admin/secrets` | List all `(tenant_id, name)` pairs across every registered tenant. |
| `PUT` | `/v1/admin/secrets/{tenant}/{name}` | Store or replace a secret. Body: `{"value": "..."}`. |
| `DELETE` | `/v1/admin/secrets/{tenant}/{name}` | Delete a secret. |

All write operations emit a corresponding audit event (`secret.created`, `secret.updated`, `secret.deleted`, `secret.rotated`) that records `tenant_id` and `name` but never the value.

## Security properties

A summary of the non-negotiable constraints that govern the vault:

- **Master key source.** The master key comes from `PORTICO_VAULT_KEY` only. Hardcoding keys anywhere — including test fixtures — is a security violation in Portico's contributor normatives.
- **No credential passthrough.** The default for all OAuth flows is token exchange (RFC 8693). Returning a broad downstream token to the agent requires explicit `auth.passthrough: true` in the server configuration and emits `credential.passthrough` audit events on every passthrough.
- **Audit redaction.** Every audit event containing potential secrets passes through Portico's audit redactor before persistence. The redactor strips bearer tokens, JWTs, AWS access keys, GitHub PATs, Slack tokens, and other known secret shapes using a pattern set that covers common credential formats. Tool call arguments and results are never logged raw; they are redacted and truncated to a configurable byte cap.
- **No untyped argument logging.** Full tool arguments are never written to the audit store; only a summarized, redacted representation is persisted.
- **HKDF versioning.** The info string prefix `portico/v1/` is versioned. A future scheme upgrade can derive different per-value keys from the same master key without invalidating existing entries — an upgrade increments the prefix and provides a migration path.

For the full security design including JWT algorithm constraints, approval flow enforcement, and CSP rules, see the [Security Model](/concepts/security-model).

## Related

- [OAuth Token Exchange](/concepts/oauth-token-exchange) — detailed RFC 8693 protocol flow, caching behavior, and IdP error handling for the `oauth2_token_exchange` strategy
- [Multi-Tenancy](/concepts/multi-tenancy) — how tenant identity propagates through the vault and every other layer of the gateway
- [Security Model](/concepts/security-model) — the complete security design: JWT validation, credential passthrough rules, audit redaction, and the approval flow
- [Approvals](/concepts/approvals) — the approval gate that intercepts high-risk tool calls before credential injection occurs
- [Audit](/concepts/audit) — how vault and credential lifecycle events are stored, queried, and retained
- [Configuration Reference](/reference/configuration) — full `auth.strategy`, `auth.exchange`, and `auth.env` field documentation
- [CLI Reference](/reference/cli) — complete `portico vault` subcommand syntax and flags
