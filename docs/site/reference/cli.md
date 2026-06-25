# CLI

`portico` is a single static binary. Every capability — gateway, dev server, vault operations, offline diagnostics, conformance testing — ships in one artifact. There are no side-car daemons or helper tools to install separately.

This page is a complete reference for every subcommand. All flags are sourced directly from the Go source under `cmd/portico/`. If a flag is not listed here it does not exist.

::: tip Installation
See [Installation](/getting-started/installation) for how to obtain the binary. See [Deployment](/guides/deployment) for production recommendations.
:::

---

## Quick reference

| Command | What it does |
|---|---|
| `serve` | Run the gateway with a YAML config file (production). |
| `dev` | Run in dev mode: localhost only, synthetic tenant, no JWT required. |
| `validate` | Parse and validate a config file; exit with a summary. |
| `validate-skills` | Validate one or more Skill Pack manifests. |
| `vault` | Manage the credential vault (put / get / delete / list / rotate-key). |
| `inspect-session` | Dump a session's snapshot, audit events, and approvals offline. |
| `agents` | Manage Agent Profiles offline against the SQLite data dir. |
| `governance` | Manage governance customers and teams offline. |
| `code-mode` | Render stub files or execute Starlark snippets offline. |
| `conformance` | Run the OpenAI API conformance suite against a live gateway. |
| `version` | Print the binary version and build commit. |

---

## `serve`

```
portico serve --config <path>
```

Start the gateway in production mode. Loads the YAML config file at `<path>`, opens (and creates if necessary) the SQLite data directory, seeds tenants and Agent Profiles, starts the process supervisor, and begins listening on the configured bind address.

A hot-reload watcher fires whenever the config file changes on disk. Only the registry (server specs and tenant list) is reloaded live; changes to auth, storage, or bind address require a restart.

**Required flag**

| Flag | Description |
|---|---|
| `--config <path>` | Path to `portico.yaml`. No default; the flag is mandatory. |

**Environment variables**

| Variable | Effect |
|---|---|
| `PORTICO_VAULT_KEY` | Base64-encoded 32-byte AES-256 key. When set, activates the credential vault and enables <span v-pre>`{{secret:…}}`</span> interpolation in server environment blocks. When absent, the vault is disabled and secret references cause downstream server start failures. |
| `PORTICO_DEV_TENANT` | Overrides the synthetic tenant ID used in dev mode; has no effect on `serve`. |

**Exit codes**: 0 on clean shutdown (SIGINT / SIGTERM); 1 on configuration errors or listener failures.

See [Configuration reference](/reference/configuration) for the full YAML schema.

---

## `dev`

```
portico dev [--bind <addr>] [--data-dir <path>]
```

Start the gateway in developer mode. The config is synthesised in memory — no YAML file is needed. Auth middleware is disabled; every request runs under the synthetic tenant `dev` (overridable via `PORTICO_DEV_TENANT`). The bind address must resolve to localhost; non-localhost addresses are rejected at startup.

Skill Packs are loaded automatically: `./skills` is checked first, then `./examples/skills`. When neither directory exists, the skills runtime starts in a disabled state.

**Flags**

| Flag | Default | Description |
|---|---|---|
| `--bind` | `127.0.0.1:8080` | `host:port` to listen on. Must be `127.0.0.1`, `::1`, or `localhost`. |
| `--data-dir` | current working directory | Directory for the SQLite database and logs. The database file is named `portico.db` inside this directory. |

::: warning Dev mode is not for production
Dev mode disables JWT validation entirely. It is not appropriate for any environment reachable by untrusted callers.
:::

---

## `validate`

```
portico validate --config <path>
```

Load and validate a config file, then exit. Does not start the server and does not touch the database. Prints a single summary line on success:

```
config OK: bind=127.0.0.1:8080 tenants=2 storage=sqlite dev_mode=false
```

Exits 1 and prints a structured error if validation fails.

**Required flag**

| Flag | Description |
|---|---|
| `--config <path>` | Path to `portico.yaml`. |

---

## `validate-skills`

```
portico validate-skills <path>...
```

Validate one or more Skill Pack manifests without starting the gateway. Each argument can be:

- A directory containing a `manifest.yaml` (a single pack root).
- A directory tree in the standard two-level namespace layout (`<namespace>/<pack>/manifest.yaml`). All packs discovered under the tree are validated.
- A path directly to a `manifest.yaml` file.

Output is one line per pack:

```
OK     my-pack/search 1.2.0  (/data/skills/acme/search)
WARN   my-pack/ingest 0.3.0  (/data/skills/acme/ingest)
        WARN:  optional tool github.search not found in registry
ERROR  bad-pack/? ?  (/data/skills/broken/bad)
        ERROR: manifest.yaml: required field id is missing
```

Status codes are `OK`, `WARN` (pack loads but references missing optional tools), and `ERROR` (structural or semantic failure). The command exits 1 if any pack has errors.

See [Skill Packs](/concepts/skill-packs) for the manifest schema.

---

## `vault`

```
portico vault <subcommand> [flags]
```

Operate on the file-backed credential vault offline (no server running required). `PORTICO_VAULT_KEY` must be exported before any vault command. All subcommands accept a `--path` flag that defaults to `./vault.yaml`.

The vault is keyed per `(tenant, name)`. Cross-tenant reads are not possible by construction.

::: info
See [Credentials Vault](/concepts/credentials-vault) for a conceptual overview of how secrets flow from the vault into downstream server processes.
:::

### `vault put`

```
portico vault put --tenant <id> --name <key>
    (--value <literal> | --from-file <path> | --from-stdin)
    [--path <vault.yaml>]
```

Store a secret. Exactly one value source must be supplied.

| Flag | Description |
|---|---|
| `--tenant` | Tenant ID that will own this secret. Required. |
| `--name` | Secret name (the key within that tenant's namespace). Required. |
| `--value` | Literal secret value. |
| `--from-file` | Read the secret value from a file at this path. |
| `--from-stdin` | Read the secret value from stdin. |
| `--path` | Path to the vault file. Default: `./vault.yaml`. |

### `vault get`

```
portico vault get --tenant <id> --name <key> [--path <vault.yaml>]
```

Print a secret value to stdout in cleartext. A warning is written to stderr before any output:

```
warning: printing secret value to stdout in cleartext
```

Do not use in CI pipelines where stdout is captured in logs.

### `vault delete`

```
portico vault delete --tenant <id> --name <key> [--path <vault.yaml>]
```

Remove a secret from the vault. The vault file is rewritten atomically.

### `vault list`

```
portico vault list --tenant <id> [--path <vault.yaml>]
```

Print all secret names for a tenant, one per line. Values are not printed.

### `vault rotate-key`

```
portico vault rotate-key --new-key <base64-32B> [--path <vault.yaml>]
```

Re-encrypt the entire vault under a new master key. `--new-key` must be a base64-encoded 32-byte value (standard encoding). After rotation succeeds, update `PORTICO_VAULT_KEY` in the server's environment before the next start. The command prints a reminder to stderr.

| Flag | Description |
|---|---|
| `--new-key` | New master key, base64-encoded 32 bytes. Required. |
| `--path` | Path to the vault file. Default: `./vault.yaml`. |

**Generating a new key**:

```bash
openssl rand 32 | base64
```

---

## `inspect-session`

```
portico inspect-session <session_id> [--output json|table] [--since <RFC3339>] [--dsn <dsn>]
```

Produce an offline structured dump of a session — useful when triaging an incident from a backup of the data directory. Opens SQLite directly without booting the full server stack. The output includes:

- Session row (tenant, user, snapshot ID, start/end timestamps).
- The catalog snapshot payload attached to this session.
- Audit events for the session, optionally filtered to a `--since` lower bound.
- Approval rows (tool name, risk class, status, timestamp).
- A trace summary: total event count, error count, and time span.

`schema.drift` events are surfaced separately under `drift_events` in JSON output.

**Positional argument**

| Argument | Description |
|---|---|
| `session_id` | The session ID to inspect. Required. |

**Flags**

| Flag | Default | Description |
|---|---|---|
| `--output` | `json` | Output format. `json` writes an indented JSON document; `table` writes a human-readable summary to stdout. |
| `--since` | (none) | RFC3339 timestamp lower bound. Only audit events at or after this time are included. |
| `--dsn` | `file:./data/portico.db?mode=ro` | SQLite DSN. Read-only mode by default; avoids write-lock contention against a running server on the same data dir. |

**Example — JSON output**:

```bash
portico inspect-session sess_abc123 \
  --output json \
  --since 2026-06-01T00:00:00Z \
  --dsn "file:/var/lib/portico/portico.db?mode=ro"
```

---

## `agents`

```
portico agents <subcommand> --tenant <id> [--dsn <dsn>] [flags]
```

Manage [Agent Profiles](/concepts/agent-profiles) offline against the SQLite data directory. No running server is required; the command opens SQLite directly. All subcommands default `--dsn` to `file:./data/portico.db`.

Agent Profiles are the single source of truth for what an agent consumer is allowed to reach — MCP servers, tools, skills, and LLM model aliases. The `agents test` subcommand uses the same `Profile.Allows*` decision methods as the live dispatcher, so the verdict matches production exactly.

### `agents list`

```
portico agents list --tenant <id> [--dsn <dsn>]
```

Print all Agent Profiles for the tenant as a JSON array.

### `agents get`

```
portico agents get --tenant <id> --id <profile_id> [--dsn <dsn>]
```

Print a single Agent Profile as a JSON object.

### `agents create`

```
portico agents create --tenant <id> --name <name> [flags] [--dsn <dsn>]
```

Create a new Agent Profile. Prints the created profile as JSON.

| Flag | Default | Description |
|---|---|---|
| `--name` | — | Profile name. Required. |
| `--description` | `""` | Human-readable description. |
| `--servers` | `""` | Comma-separated list of allowed MCP server names. |
| `--tools` | `""` | Comma-separated list of allowed namespaced tools (`server.tool`). |
| `--skills` | `""` | Comma-separated list of allowed Skill Pack IDs. |
| `--models` | `""` | Comma-separated list of allowed LLM model aliases. |
| `--scopes` | `mcp:call` | Comma-separated list of scopes. |

### `agents delete`

```
portico agents delete --tenant <id> --id <profile_id> [--dsn <dsn>]
```

Delete an Agent Profile. The command exits 1 with "agent profile not found" if the ID does not exist.

### `agents bind`

```
portico agents bind --tenant <id> --id <profile_id> --sub <jwt_subject> [--dsn <dsn>]
```

Bind a JWT subject (`sub` claim) to an Agent Profile. When an inbound JWT presents this subject, the gateway resolves the associated profile and applies its entitlements. A subject can only be bound to one profile per tenant.

### `agents unbind`

```
portico agents unbind --tenant <id> --sub <jwt_subject> [--dsn <dsn>]
```

Remove the binding between a JWT subject and whichever Agent Profile it was bound to.

### `agents test`

```
portico agents test --tenant <id> --id <profile_id>
    (--tool <server.tool> | --alias <model_alias> | --skill <skill_id>)
    [--dsn <dsn>]
```

Offline allow/deny check: "would this profile permit access to this target?" Exactly one of `--tool`, `--alias`, or `--skill` must be supplied. The output is a JSON object:

```json
{
  "profile_id": "ap_a1b2c3d4...",
  "tenant": "acme",
  "kind": "tool",
  "target": "github.search_repositories",
  "allowed": true,
  "reason": "tool_in_profile"
}
```

The check uses the identical `Profile.Allows*` methods the live dispatcher uses, so the verdict matches what would happen at runtime.

---

## `governance`

```
portico governance <resource> <verb> --tenant <id> [flags] [--dsn <dsn>]
```

Manage governance entities — customers and teams — offline against the SQLite data directory. This follows the same offline access pattern as `agents`: the command opens SQLite directly, no running server needed. Default DSN: `file:./data/portico.db`.

Governance entities underpin Virtual Key assignment and hierarchical budget structures. See [Virtual Keys](/concepts/virtual-keys) and [Hierarchical Budgets](/concepts/hierarchical-budgets).

### Customers

```
portico governance customers list   --tenant <id>
portico governance customers get    --tenant <id> --id <customer_id>
portico governance customers create --tenant <id> --name <name> [--description <s>] [--webhook-url <url>]
portico governance customers update --tenant <id> --id <customer_id> [--name <s>] [--description <s>] [--webhook-url <url>]
portico governance customers delete --tenant <id> --id <customer_id>
```

Customer IDs are generated as `cust_<random16hex>`. `list`, `get`, `create`, and `update` emit JSON to stdout. `delete` prints the deleted ID.

### Teams

```
portico governance teams list   --tenant <id>
portico governance teams get    --tenant <id> --id <team_id>
portico governance teams create --tenant <id> --name <name> [--customer-id <id>] [--description <s>]
portico governance teams update --tenant <id> --id <team_id> [--name <s>] [--customer-id <id>] [--description <s>]
portico governance teams delete --tenant <id> --id <team_id>
```

Team IDs are generated as `team_<random16hex>`. A team may optionally reference a parent customer via `--customer-id`.

---

## `code-mode`

```
portico code-mode <render|exec> [flags]
```

Offline tooling for [Code Mode](/concepts/code-mode). Both subcommands open the SQLite data dir directly and operate against a session's stored catalog snapshot. No running server is needed for either operation.

### `code-mode render`

```
portico code-mode render --session <id> [--tenant <id>] [--binding-level server|tool]
                         [--file <virtual_path>] [--dsn <dsn>]
```

Dump the projected `.pyi` stub filesystem for a session's snapshot. The output is deterministic: the same snapshot always produces byte-identical stubs.

| Flag | Default | Description |
|---|---|---|
| `--session` | — | Session ID whose snapshot to render. Required. |
| `--tenant` | `""` | Optional tenant filter when multiple tenants share a data dir. |
| `--binding-level` | `server` | Stub granularity: `server` generates one module per server; `tool` generates one function per tool. |
| `--file` | `""` | Render only this virtual path (e.g. `servers/github.pyi`) and print its contents to stdout. |
| `--dsn` | `file:./data/portico.db?mode=ro` | SQLite DSN. Read-only by default. |

**Example**:

```bash
# List all stub files for a session
portico code-mode render --session sess_abc123

# Print a single stub file
portico code-mode render --session sess_abc123 --file servers/github.pyi
```

### `code-mode exec`

```
portico code-mode exec --session <id> --code <snippet|@path> [--tenant <id>]
                       [--binding-level server|tool] [--dsn <dsn>]
```

Run a Starlark snippet through the hardened sandbox against a session's snapshot. Tool calls fail closed — a live server is required to dispatch them. This makes `exec` suitable for testing snippet safety, syntax, and pure computation, but not for testing real tool execution paths.

A `code_mode.cli_exec` audit event recording the SHA-256 digest of the snippet and the run status is inserted into the data dir's `audit_events` table.

| Flag | Default | Description |
|---|---|---|
| `--session` | — | Session ID whose snapshot to bind. Required. |
| `--code` | — | Starlark source. Pass an inline string, or prefix with `@` to read from a file (`@path/to/snippet.star`). Required. |
| `--tenant` | `""` | Optional tenant filter. |
| `--binding-level` | `server` | Stub granularity: `server` or `tool`. |
| `--dsn` | `file:./data/portico.db` | SQLite DSN. |

**Output** (JSON):

```json
{
  "result": "...",
  "output": "...",
  "tool_calls": [],
  "steps": 4,
  "output_truncated": false
}
```

::: warning Sandbox errors
If the snippet violates a sandbox policy (forbidden built-in, execution byte limit, recursion depth), the command exits 1 with a structured error code and detail message.
:::

---

## `conformance`

```
portico conformance --suite openai --target <url> [--token <jwt>] [--model <alias>]
```

Run an API conformance suite against a running Portico instance. Useful for verifying that a deployment correctly implements the expected wire protocol before routing production traffic.

The only currently supported suite is `openai`, which exercises Portico's OpenAI-compatible LLM gateway surface.

**Flags**

| Flag | Default | Description |
|---|---|---|
| `--suite` | — | Conformance suite name. Currently only `openai` is accepted. Required. |
| `--target` | — | Base URL of the running gateway (e.g. `https://gateway.example.com`). Required. |
| `--token` | `""` | JWT bearer token to include in requests. Optional; omit for dev-mode targets. |
| `--model` | `gpt-4o` | Model alias to use in chat and embedding requests. |

**Checks in the `openai` suite**

| Check | What is asserted |
|---|---|
| `models` | `GET /v1/models` returns `{"object":"list","data":[…]}`. |
| `chat` | `POST /v1/chat/completions` returns a well-formed completion envelope, or a well-formed error envelope (SKIP — no upstream configured). |
| `chat (unknown model)` | An unknown model name returns a 4xx response with an `{"error":…}` body. |
| `chat (malformed)` | A malformed request body returns a 4xx response with an `{"error":…}` body. |
| `embeddings` | `POST /v1/embeddings` returns a well-formed embeddings envelope, or a well-formed error (SKIP). |
| `vk-bearer` | A forged `pk-portico-*` bearer token is rejected with HTTP 401. SKIP when Virtual Key auth is not enforced in the target build. |

**Exit codes**: 0 when all checks pass or skip; 1 when any check fails.

**Example output**:

```
models: OK
chat: SKIP - no usable model/upstream (503)
chat (unknown model): OK
chat (malformed): OK
embeddings: SKIP - no usable model/upstream (503)
vk-bearer: OK (forged pk-portico-* rejected 401)
conformance: 3 ok, 2 skip, 0 fail
```

---

## `version`

```
portico version
portico --version
portico -v
```

Print the binary version and build commit, then exit.

```
portico 1.0.0 (a3b7c912)
```

In development builds (compiled without `-ldflags`), version reports `dev` and the commit field is omitted.

---

## Related

- [Installation](/getting-started/installation) — download and verify the binary.
- [Deployment](/guides/deployment) — systemd unit, Docker, and configuration recommendations for production.
- [Configuration reference](/reference/configuration) — full YAML schema for `portico.yaml`.
- [Credentials Vault](/concepts/credentials-vault) — how secrets are encrypted, injected, and referenced.
- [Agent Profiles](/concepts/agent-profiles) — the entitlement model behind the `agents` subcommand.
- [Skill Packs](/concepts/skill-packs) — manifest format validated by `validate-skills`.
- [Code Mode](/concepts/code-mode) — the Starlark sandbox that `code-mode render` and `code-mode exec` operate against.
