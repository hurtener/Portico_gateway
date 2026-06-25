# Security model

Portico is built around a small number of non-negotiable security properties.
This page catalogues each guarantee, explains how it is enforced in the runtime,
and points to the concept pages that expand on the individual subsystems.

---

## Core guarantees at a glance

| Guarantee | Short description |
|---|---|
| Asymmetric-only JWT | HS\* and `none` algorithms are statically rejected |
| Vault key from environment | Master key never lives in config or on disk |
| Token exchange by default | Raw caller tokens are not forwarded to downstreams |
| Approval flow non-bypassable | `requires_approval` tools always block for a decision |
| Path-traversal containment | Every manifest-sourced path is cleaned and prefix-checked |
| CSP for MCP Apps | `ui://` HTML is wrapped with a configurable Content Security Policy |
| Secret redaction on audit emit | Every payload is scrubbed before it reaches persistence |
| No untyped tool arguments in audit | Payloads carry summaries and counts, never raw tool args |
| Process command allowlist | Operator-configured allowlist gates which commands stdio servers may run |
| Argv-form exec only | No shell-string expansion in process launch |
| HMAC-only Virtual Key storage | VK secrets are never persisted; only `salt + HMAC-SHA256(salt, secret)` |

---

## JWT validation — asymmetric algorithms only

Every request to Portico's northbound surface (MCP over HTTP+SSE and the REST
API) must carry a Bearer token or a Virtual Key. Bearer tokens are validated by
`internal/auth/jwt`. The algorithm allowlist is hard-coded:

```go
// internal/auth/jwt/validator.go
var allowedAlgs = []string{
    "RS256", "RS384", "RS512",   // RSA-PKCS1v15 / PSS
    "ES256", "ES384", "ES512",   // ECDSA
}
```

Symmetric algorithms (HS256, HS384, HS512) and `none` are unconditionally
rejected — the allowlist is enforced both at the `jwt/v5` parser level
(`WithValidMethods`) and in the key-lookup callback, providing defence in depth.
A token that presents an unlisted algorithm receives a `401` before any claim
decoding takes place.

The validator requires:
- A `kid` header pointing to a key in the configured JWKS (static file or
  remote URL).
- The `tenant` claim (or the field named by `jwt.tenant_claim`) to be non-empty
  — tenant identity is extracted here and injected into the request context.
- Audience and scope checks when configured.

See [Authentication](/concepts/authentication) for the full validator
configuration reference.

---

## Vault master key from environment only

Portico's credential vault encrypts every secret entry at rest with AES-256-GCM.
The master key is derived from `PORTICO_VAULT_KEY` — a base64-encoded 32-byte
value read at startup by `secrets.LoadKeyFromEnv()`. The function refuses to
start if the variable is absent (vault disabled) or if the decoded value is not
exactly 32 bytes.

The on-disk scheme (v1) derives a **per-value key** from the master key using
HKDF-SHA256 with the domain-separation tag `portico/v1/<tenant>/<name>`, and
binds the `(tenant, name)` tuple as Additional Authenticated Data. A fresh
12-byte nonce is generated for each write. This means:

- Reusing the same master key with a different `(tenant, name)` tuple produces
  a different derived key.
- An entry ciphertext cannot be replayed under a different name — the AAD check
  will fail.
- Rotating the master key re-derives and re-encrypts every entry atomically
  (`vault rotate-key`).

The key is **never** written to config files, logs, or the database. Hardcoding
it anywhere (including in tests) is an unconditional project violation; tests use
fixture keys from `internal/secrets/testdata/`.

See [Credentials vault](/concepts/credentials-vault) for the vault CLI and
per-tenant key scoping.

---

## No credential passthrough — token exchange by default

Portico never forwards the caller's raw Bearer token to a downstream MCP server
by default. The credential injection pipeline (in `internal/secrets/inject`)
supports five strategies:

| Strategy | Description |
|---|---|
| `env_inject` | Write resolved secrets into the child process environment |
| `http_header_inject` | Attach resolved secrets as HTTP headers |
| `secret_reference` | Pull a single named vault entry as the auth credential |
| `oauth2_token_exchange` | Perform RFC 8693 token exchange before each call |
| `credential_shim` | Reserved for future pipeline composition |

The `oauth2_token_exchange` strategy is the recommended default for any
downstream that requires delegated identity. It implements
`urn:ietf:params:oauth:grant-type:token-exchange` (RFC 8693): the caller's JWT
is sent to a configured IdP as the `subject_token`, and the downstream receives
only the exchanged access token scoped to that specific server. The caller's
original credential is never exposed to the downstream.

Direct passthrough — forwarding the caller's raw token without exchange — is
an opt-in behaviour that requires explicit configuration (`auth.passthrough:
true` in the server spec) and causes the gateway to emit `credential.passthrough`
audit events for every call. Enabling passthrough without the audit scaffolding
is a project-level violation.

```yaml
# Recommended: token exchange
servers:
  - id: github
    transport: http
    http:
      url: https://api.github.com/mcp
    auth:
      strategy: oauth2_token_exchange
      exchange:
        token_url: https://idp.example.com/token
        client_id: portico-gateway
        client_secret_ref: github_client_secret
        audience: https://api.github.com
```

See [OAuth token exchange](/concepts/oauth-token-exchange) and
[Credentials vault](/concepts/credentials-vault).

---

## Approval flow is non-bypassable

Any tool whose policy decision carries `requires_approval: true` is blocked
until an explicit grant or denial is received. This is enforced in
`internal/server/mcpgw/policy_pipeline.go`: the dispatcher pauses the tool call
and emits an `approval.pending` audit event. The approval gate checks for an
existing grant within the configured replay window (same tenant, tool, and
arguments) before issuing a new prompt.

Three things are guaranteed by construction:

1. **No unconditional bypass path exists.** The pipeline always reaches the
   approval gate when `RequiresApproval` is true.
2. **Approval grants are scoped.** A cached grant from one tenant or one skill
   does not apply to another.
3. **Every approval decision is audited.** `approval.pending`, `approval.decided`,
   `approval.expired`, and `approval.replayed` events all carry `tenant_id` and
   the full tool identity.

Approval prompts are surfaced headlessly: the gateway emits them as MCP
`elicitation/create` requests or as structured JSON-RPC errors, and the host
(Claude Desktop, a custom agent shell) renders the UX. Portico does not display
its own approval UI.

See [Approvals](/concepts/approvals) and [Policy](/concepts/policy).

---

## Path-traversal containment

Wherever Portico accepts a relative path from an external source — a Skill Pack
manifest, a config file, or an API input — it normalizes and bounds-checks the
path before any filesystem access. The canonical implementation lives in
`internal/skills/source/localdir.go`:

```go
// Reject absolute paths outright.
if filepath.IsAbs(relpath) {
    return nil, ContentInfo{}, fmt.Errorf("localdir: relpath must be relative")
}
// Normalize: resolve "..", symlink-free.
abs := filepath.Clean(filepath.Join(ref.Loc, relpath))
// Verify the result is still inside the pack root.
if !strings.HasPrefix(abs, ref.Loc+string(os.PathSeparator)) && abs != ref.Loc {
    return nil, ContentInfo{}, fmt.Errorf("localdir: relpath escapes pack root")
}
```

The same pattern — `filepath.Clean` followed by a `strings.HasPrefix` against
the allowed root — is required in any future code path that reads from a
manifest-supplied or user-supplied path. The helper in `localdir.go` is the
canonical reference; code that re-implements traversal protection without
following the same two-step is rejected.

---

## CSP for MCP Apps

MCP Apps are `ui://` resources that MCP servers publish as `text/html`. Because
these fragments render inside the host's chat surface, arbitrary inline scripts
or cross-origin connections would be a significant XSS risk.

Portico's `internal/apps/csp.go` implements `CSPConfig.Compose()`, which:

1. Parses the HTML body using a tolerant HTML5 parser.
2. Injects a `<meta http-equiv="Content-Security-Policy">` as the first child of
   `<head>`, creating the element if it is absent.
3. Returns the modified HTML alongside a `_meta.portico` block carrying the CSP
   string and sandbox value, so the host can also apply out-of-band protections.

The default policy is conservative:

```go
// internal/apps/csp.go — DefaultCSP()
CSPConfig{
    DefaultSrc: []string{"'self'"},
    ScriptSrc:  []string{"'self'"},
    StyleSrc:   []string{"'self'"},
    ImgSrc:     []string{"'self'", "data:"},
    ConnectSrc: []string{"'self'"},
    FrameSrc:   []string{"'self'"},
    Sandbox:    "allow-scripts",
}
```

Which produces:

```
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:;
connect-src 'self'; frame-src 'self'
```

Operators can relax individual directives per server in `portico.yaml` under the
`apps.csp` key. Each field (`default_src`, `script_src`, `style_src`, `img_src`,
`connect_src`, `frame_src`, `sandbox`) is a list of source expressions. Empty
fields inherit from `DefaultCSP`. The `Compose` function is best-effort: if
HTML parsing fails, the original body is returned unchanged but the CSP `_meta`
block is still emitted so the host can apply its own protections.

::: warning Bypassing CSP
Returning `text/html` content from a `ui://` resource without passing it through
`CSPConfig.Compose` bypasses this protection. This is a forbidden practice in
Portico's contribution rules.
:::

---

## Secret redaction on every audit emit

The audit subsystem (`internal/audit`) inserts a `Redactor` in front of the
persistence path. The default redactor (`NewDefaultRedactor`) applies two
complementary strategies:

**Pattern-based redaction** — regex rules that replace known credential shapes
in any string value within the event payload:

| Label | Pattern family |
|---|---|
| `bearer` | `Bearer <token>` (≥ 20-char token) |
| `basic_auth` | `Basic <base64>` (≥ 16-char encoded value) |
| `github_pat` | `gh[pousr]_...` |
| `aws_access_key` | `AKIA...` (16 uppercase alphanumerics) |
| `slack_token` | `xox[abprs]-...` |
| `jwt_generic` | Three-part `eyJ...` structure |
| `private_key_block` | PEM `-----BEGIN ... PRIVATE KEY-----` blocks |

**Structural redaction** — map keys that match a sensitive-name set (lowercase
comparison) have their values replaced wholesale with `[REDACTED:key=<keyname>]`
regardless of value shape. The default set includes: `token`, `secret`,
`password`, `passwd`, `api_key`, `apikey`, `authorization`, `auth`,
`access_token`, `refresh_token`, `session_token`, `client_secret`, `secret_key`,
`private_key`, `credential`, `credentials`.

The replacement strings include a label so operators can distinguish *which* rule
fired without the log entry leaking the value.

Operators can extend the default redactor with additional patterns by registering
custom `UserPattern` entries. The `Store.WithRedactor` option allows a
fully-custom `Redactor` for specialized deployments. The replacement format
(`[REDACTED:<label>]`) is fixed and non-configurable to keep audit reads
machine-parseable.

::: info Redaction does not substitute for pre-summarization
The redactor is a safety net, not a substitute for callers pre-summarizing
sensitive data. Code Mode execution events, for example, carry execution counts
only — never code text, tool arguments, or results — because the redactor cannot
be expected to recognize every possible secret shape in unstructured natural
language.
:::

See [Audit](/concepts/audit).

---

## No untyped tool arguments in audit

Tool call audit events (`tool_call.start`, `tool_call.complete`,
`tool_call.failed`) are prohibited from carrying raw tool arguments or raw
results. Tool arguments routinely contain PII, credentials, and business-secret
data; persisting them unredacted would turn the audit log into a liability.

The rule in practice:

- Callers pass a `Payload map[string]any` to the emitter. Payloads must contain
  summaries (e.g. argument count, schema hash, truncated output length), not
  verbatim argument values.
- Code Mode execution events carry only lifecycle markers and counts — bytes
  executed, tool calls issued, approvals triggered — never the code text or its
  outputs.
- The `Redactor` is a secondary catch for any string that slips through; the
  primary control is callers not emitting raw args in the first place.

See [Audit](/concepts/audit) for the full event taxonomy.

---

## Process command allowlist for stdio servers

Stdio MCP servers run as supervised child processes. The operator controls which
executable commands are permitted via a per-server command allowlist in the server
spec. When the allowlist is configured, the process supervisor validates the
command before launching it; an unlisted command causes the server to fail
registration with a policy error rather than executing the binary.

This guards against two scenarios:

- A misconfigured (or malicious) server spec that tries to spawn an unintended
  binary.
- A future attack path where an adversary modifies a server spec in the storage
  layer to point at an arbitrary executable.

The allowlist is an optional field; when absent, any command the operator has
configured is permitted. Tightening it for production deployments is strongly
recommended.

See [MCP Southbound](/concepts/mcp-southbound) for the full stdio server
configuration reference.

---

## Argv-form exec only — no shell expansion

Portico's stdio supervisor spawns child processes using Go's `exec.Command` in
**argv form** only:

```go
// internal/mcp/southbound/stdio/client.go
cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
```

The command is the first element and the arguments are a separate slice. Shell
metacharacters, glob expansion, variable substitution, and command chaining are
inert: the `exec.Command` call hands the vector directly to the OS without
invoking a shell. A server spec with `args: ["-c", "rm -rf /"]` passes those
strings as literal arguments to the command — they are not interpreted by `sh`.

Using shell-string form (`exec.Command("sh", "-c", "...")`) is a forbidden
practice. Any PR introducing a shell-expanded command anywhere in production code
is rejected on sight.

---

## Virtual Key secret storage — HMAC only

Virtual Keys are long-lived bearer tokens issued by Portico operators for
programmatic callers. The raw secret is generated on `POST /api/virtual-keys`
(or on rotation) and returned **once** in the response body. It is never stored.

What is persisted is:

- A random `salt` (generated fresh on each create or rotate).
- `HMAC-SHA256(salt, secret)` — a one-way binding.

Verification at request time recomputes `HMAC-SHA256(stored_salt, presented_secret)`
and compares the result to the stored HMAC using constant-time comparison
(`crypto/subtle.ConstantTimeCompare`). If the presented secret does not match,
authentication fails. There is no path to recover the original secret from the
stored values.

::: warning One-time reveal
If the operator loses the secret returned at creation, the only recovery is
rotation: `POST /api/virtual-keys/{id}/rotate` issues a new secret and invalidates
the previous one.
:::

See [Virtual Keys](/concepts/virtual-keys) for scope, allowlist, and budget
binding.

---

## Multi-tenant isolation as a security property

All of the guarantees above are enforced **per tenant**. Specifically:

- The vault is keyed by `(tenant_id, name)` — cross-tenant reads are impossible
  by construction.
- Every audit event carries `tenant_id`; the audit query API filters by tenant
  by default.
- `per_tenant`, `per_user`, and `per_session` runtime modes produce isolated
  child processes; no two tenants share a stdio process unless the operator
  explicitly chooses `shared_global`.
- Virtual Keys belong to a tenant and cannot be used to authenticate requests on
  behalf of a different tenant.

See [Multi-tenancy](/concepts/multi-tenancy) for the full isolation model.

---

## Related

- [Authentication](/concepts/authentication) — JWT validator configuration, JWKS, Virtual Key auth
- [Credentials vault](/concepts/credentials-vault) — vault CLI, HKDF scheme, key rotation
- [OAuth token exchange](/concepts/oauth-token-exchange) — RFC 8693 exchange configuration
- [Approvals](/concepts/approvals) — approval gate, elicitation, replay window
- [Audit](/concepts/audit) — event types, redactor, retention
- [Multi-tenancy](/concepts/multi-tenancy) — process isolation, storage isolation, cross-tenant controls
- [Policy](/concepts/policy) — risk classification, tool allowlists and denylists
