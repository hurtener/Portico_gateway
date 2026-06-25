# Code Mode — token savings

Every `executeToolCode` run produces a deterministic estimate of the tokens Code
Mode saved compared to a plain-mode turn over the same tool catalog. The figure is
recorded against the execution, emitted as an OpenTelemetry span attribute, and
rolled up per tenant through a REST API designed for the observability dashboard.

This page explains the model behind the estimate, the constants that calibrate it,
and how the numbers propagate from a single execution all the way to an operator
summary.

## Why the estimate matters

In a plain MCP session the model receives the full tool catalog — every tool's
name, description, and JSON Schema — on every turn. For a catalog of tens or
hundreds of tools, this framing cost is substantial and repeats with every request.

Code Mode replaces the catalog with four fixed meta-tool definitions. Each
`executeToolCode` run substitutes one per-turn catalog render with a single Starlark
snippet. The savings estimator quantifies that substitution so platform operators
can measure the ROI of enabling Code Mode without relying on approximate provider
billing statements.

The estimate is intentionally **modeled**, not measured. It does not invoke a
tokenizer; it applies a fixed character-to-token ratio calibrated against
BPE-encoded English text and JSON payloads. Use it as a relative ROI signal and
operational planning input, not as a ledger figure for cost allocation.

## The formula

```
saved = (catalog_render_tokens - meta_tools_render_tokens)   // catalog never shipped
      + num_tool_calls × per_call_overhead_tokens            // round trips collapsed
      - (executed_code_tokens + executed_results_tokens)     // cost Code Mode adds
```

The result is clamped at zero. A very small catalog where the executed snippet and
its results cost more than the catalog they replaced yields no savings — but never
a negative number. The implementation is `EstimateTokensSaved` in
`internal/mcp/codemode/savings.go`.

## Terms

### `catalog_render_tokens`

The estimated token cost of rendering the snapshot's complete tool catalog as an
OpenAI tool-definitions blob — the spend a plain session pays on every turn.

Computed by iterating over each `ToolInfo` in the snapshot and summing:

```
chars = len(NamespacedName) + len(Description) + len(InputSchema) + perToolFramingChars
```

The `perToolFramingChars` constant (40) accounts for the JSON envelope Portico
wraps around each tool definition: object braces, property keys, quoting, and list
separators, beyond the tool's own content bytes.

The sum is divided by `charsPerToken` (4) to obtain tokens. This function is also
exported as `CatalogRenderTokens` for use in tests and future tooling.

### `meta_tools_render_tokens`

Code Mode sessions see four fixed meta-tool definitions instead of the full catalog.
Their combined rendered size is approximated at `metaToolsRenderChars` (900 chars),
giving a constant of 225 tokens regardless of catalog size. This term is subtracted
from the catalog savings to reflect the non-zero framing Code Mode itself contributes.

### `per_call_overhead_tokens`

Each plain-mode tool call requires two additional turns in the model context: the
assistant turn that emits the call, and the tool-result turn. Code Mode collapses
multiple tool calls into one execution, eliminating these turns entirely.

The per-round-trip overhead is approximated at `perCallOverheadChars` (320 chars,
80 tokens), calibrated against the Phase 11 replay corpus of realistic MCP sessions.
The savings scale linearly with `numToolCalls`.

### `executed_code_tokens` and `executed_results_tokens`

Code Mode is not free. The Starlark snippet and the execution result both consume
context. These two terms, derived from `len(code)` and `len(result)` in bytes,
represent the net cost Code Mode adds and are subtracted from the total savings.

## Constants

All four tuneable constants live in `internal/mcp/codemode/savings.go`:

| Constant               | Value | Meaning                                                |
|------------------------|------:|--------------------------------------------------------|
| `charsPerToken`        |     4 | BPE-shaped approximation (English text + JSON)         |
| `perToolFramingChars`  |    40 | JSON envelope around each tool definition              |
| `perCallOverheadChars` |   320 | Per-round-trip model framing a plain call pays          |
| `metaToolsRenderChars` |   900 | Rendered size of the four meta-tool definitions         |

## Worked example

Consider a session backed by a snapshot of 50 tools, each contributing approximately
250 bytes of namespaced name, description, and input schema.

**Catalog render cost**

```
chars_per_tool  = 250 + 40 (framing)   = 290
catalog_chars   = 50 × 290             = 14 500
catalog_tokens  = 14 500 / 4           ≈ 3 625
```

**Meta-tools cost**

```
meta_tokens     = 900 / 4              = 225
```

**One execution: 3 tool calls, 200 bytes of code, 600 bytes of results**

```
call_tokens     = 3 × (320 / 4)        = 240
exec_tokens     = (200 + 600) / 4      = 200

saved = (3 625 − 225) + 240 − 200
      = 3 400 + 240 − 200
      = 3 440 tokens
```

The catalog term dominates. As catalogs grow, the savings grow proportionally; the
call-collapse term adds a secondary benefit that scales with how many tools a single
execution batches together.

::: tip Savings scale with catalog size

The break-even point — where Code Mode's added cost exactly offsets the catalog
savings — occurs only with extremely small catalogs (one or two tools with very
short schemas) running large snippets with voluminous output. For any production
deployment with a meaningful set of downstream tools, Code Mode saves tokens on
every completed execution.

:::

::: info Conservative estimate

The model captures two effects: eliminating the per-turn catalog render and
collapsing per-call round trips. It does not model second-order gains such as the
model reasoning more efficiently over batched results or reduced session context
growth. Real savings may exceed the estimate for multi-tool sessions.

:::

## How the estimate flows through Portico

### Per-execution recording

After a successful execution completes, `runCodeMode` in
`internal/server/mcpgw/codemode_continuation.go` calls `EstimateTokensSaved` with
the live snapshot, the number of tool calls issued by the Starlark runtime, and the
byte lengths of the submitted code and the execution result. The returned value is:

1. Set as the `code_mode.tokens_saved_est` attribute on the active OpenTelemetry
   span, alongside `code_mode.tool_calls`.
2. Emitted as the `tokens_saved_est` field in the `code_mode.exec.completed` audit
   event payload.
3. Stored as `tokens_saved_est` in the `code_mode_executions` table (keyed by
   tenant and execution ID) via `CodeModeStore.PutExecution`.
4. Returned to the MCP client in the structured tool result alongside the execution
   output.

The client-visible structured result has this shape:

```json
{
  "result":           "<structured output from the snippet>",
  "output":           "<print() buffer>",
  "output_truncated": false,
  "tool_calls":       3,
  "tokens_saved_est": 3440,
  "duration_ms":      142
}
```

Failed executions do not record a savings estimate (the field is omitted or zero);
executions suspended awaiting approval record zero until they resume and complete.

### Tenant-level rollup

The REST API exposes two Code Mode observability endpoints scoped to the
authenticated tenant (requires `admin` scope):

**`GET /api/code-mode/executions`**

Returns a paginated list of execution records. Each item carries
`tokens_saved_est` for the individual run:

```http
GET /api/code-mode/executions?session=sess_abc&limit=50
Authorization: Bearer <admin-token>
```

```json
[
  {
    "execution_id":    "cm_01j9...",
    "session_id":      "sess_abc",
    "status":          "completed",
    "started_at":      "2026-06-24T11:00:00Z",
    "finished_at":     "2026-06-24T11:00:00.142Z",
    "snippet_sha":     "a3f2...",
    "tool_calls":      3,
    "tokens_saved_est": 3440
  }
]
```

Omit `session` to list across the whole tenant. The default page cap is 100 rows;
pass `limit` to override.

**`GET /api/code-mode/savings`**

Returns the aggregate ROI rollup for the tenant, optionally bounded to a time
window:

```http
GET /api/code-mode/savings?since=2026-06-01T00:00:00Z
Authorization: Bearer <admin-token>
```

```json
{
  "executions":      1247,
  "tool_calls":      4890,
  "tokens_saved_est": 8321640,
  "by_status": {
    "completed": 1198,
    "failed":      49
  },
  "since": "2026-06-01T00:00:00Z"
}
```

The `since` parameter is an RFC3339 UTC timestamp. Omit it to aggregate all-time.
The `by_status` map shows execution counts per terminal status, letting operators
distinguish failed runs (which contribute zero to `tokens_saved_est`) from
successful ones.

The underlying query is `SummarizeExecutions` on the `CodeModeStore` interface,
implemented in `internal/storage/sqlite/code_mode_store.go` as a single
tenant-scoped `SELECT … GROUP BY status` query.

### OTel span attributes

Every completed execution emits two span attributes on the `code_mode.execution`
span:

| Attribute                    | Type    | Value                              |
|------------------------------|---------|------------------------------------|
| `code_mode.tool_calls`       | integer | Number of downstream calls issued  |
| `code_mode.tokens_saved_est` | integer | Estimated tokens saved             |

These flow into any connected OTel backend (Prometheus, Tempo, Grafana) and can be
aggregated with standard metric queries — for example, to chart `tokens_saved_est`
per tenant over time or correlate savings with catalog size.

## Determinism and testing

`EstimateTokensSaved` is a pure function: same inputs, same output, no randomness,
no I/O. `savings_test.go` asserts:

- **Byte-stable output** — calling the function twice with the same snapshot and
  parameters returns identical results (`TestSavings_DeterministicForFixedSnapshot`).
- **Positive savings for realistic catalogs** — a 50-tool snapshot with a modest
  execution produces a positive estimate that matches the analytical formula exactly
  (`TestSavings_LargeCatalogSavesTokens`).
- **Zero clamping** — a single-tool snapshot with a very large snippet clamps to
  zero, never negative (`TestSavings_ClampedAtZero`).
- **Nil safety** — a nil snapshot returns zero without panicking
  (`TestSavings_NilSnapshot`).
- **Monotonicity** — more tool calls yield a strictly larger savings estimate
  (`TestSavings_MoreToolCallsMoreSavings`).

The byte-stable property (acceptance criterion #11) is what makes the estimate safe
to use as a reproducible metric: re-running the estimator against stored execution
data always returns the same number, so historical rollups are stable.

## Caveats

**It is a model, not a measurement.** The estimate does not call a tokenizer; it
approximates one using a fixed character-to-token ratio. Different models with
different vocabularies will tokenize the same JSON differently. The estimate is
designed to be systematically comparable across executions and tenants at the same
Portico deployment, not to match any specific provider's billing statement.

**It is conservative.** Second-order savings — reduced context growth across a
session, improved model focus over batched results — are real but unmeasured. The
reported number is a lower bound on the benefit Code Mode provides.

**Failed runs contribute nothing.** Executions that fail (sandbox error, budget
exceeded, approval denied) are counted in `by_status["failed"]` but add zero to
`tokens_saved_est`. Only `completed` executions contribute to the ROI figure.

**Catalog size is measured at execution time.** The snapshot bound to a session is
fixed at session creation; if the catalog changes between sessions, each execution
reports savings against its own snapshot. The rollup correctly aggregates across
different snapshot generations.

---

## Related

- [Code Mode](/concepts/code-mode) — the `executeToolCode` meta-tool, Starlark
  sandbox, and the full execution lifecycle.
- [Catalog and Sessions](/concepts/catalog-and-sessions) — how snapshots are built
  and what goes into `catalog_render_tokens`.
- [Observability](/concepts/observability) — OTel spans, metrics, and how to wire
  the savings attribute into an aggregation pipeline.
- [Audit](/concepts/audit) — the `code_mode.exec.completed` event that carries
  `tokens_saved_est` in its payload.
- [REST API reference](/reference/rest-api) — full specification for
  `/api/code-mode/executions` and `/api/code-mode/savings`.
- [Agent Profiles](/concepts/agent-profiles) — per-consumer control over which
  tools a Code Mode session can call, directly affecting `num_tool_calls` and
  therefore the savings estimate.
