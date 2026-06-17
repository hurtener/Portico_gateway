# Code Mode — token-savings estimator

Every `executeToolCode` run records an estimate of the tokens Code Mode saved versus a
plain-mode turn over the same catalog. The figure is **deterministic** (a fixed function of
its inputs) and surfaced per execution (`tokens_saved_est`) and rolled up per tenant on the
observability dashboard. The implementation is
`internal/mcp/codemode/savings.go::EstimateTokensSaved`.

## The formula

```
saved = (catalog_render_tokens - meta_tools_render_tokens)   // the catalog never shipped
      + num_tool_calls * per_call_overhead_tokens             // round trips collapsed
      - (executed_code_tokens + executed_results_tokens)      // the cost Code Mode adds
```

clamped at zero (a tiny catalog where the executed code costs more than it replaced yields
no savings, never a negative number).

### Terms

- **`catalog_render_tokens`** — the token cost of rendering the snapshot's full tool catalog
  as an OpenAI tool-definitions blob, the spend a plain session pays *every turn*. Computed
  from each tool's `name + description + JSON schema` length plus a fixed per-tool framing
  allowance.
- **`meta_tools_render_tokens`** — the (small, fixed) cost of the four meta-tool definitions
  a Code Mode session sees instead.
- **`per_call_overhead_tokens`** — the per-round-trip "model framing" overhead a plain
  session pays for each *separate* tool call (the assistant turn that emits the call plus the
  tool-result turn). Code Mode captures this by batching calls inside one execution.
- **`executed_code_tokens` / `executed_results_tokens`** — the cost Code Mode adds: the
  snippet the model wrote and the result that crossed back.

### Tokenization approximation

Tokens are approximated at **≈4 characters/token** (a BPE-shaped constant for English text +
JSON). This is intentionally simple and deterministic; it is an *estimate* for operator ROI
reporting, not a billing figure. The constants live in `savings.go`:

| constant                | value | meaning                                            |
|-------------------------|------:|----------------------------------------------------|
| `charsPerToken`         | 4     | chars per token                                    |
| `perToolFramingChars`   | 40    | JSON envelope around each tool definition          |
| `perCallOverheadChars`  | 320   | per-round-trip model framing a plain call pays      |
| `metaToolsRenderChars`  | 900   | rendered size of the four meta-tool definitions     |

## Worked example

A session that talks to a snapshot of 50 tools (each ~250 chars of name+description+schema):

```
catalog_render_tokens ≈ 50 * (250 + 40) / 4            ≈ 3625
meta_tools_render_tokens = 900 / 4                      =  225
per execution with 3 tool calls, 200 chars of code, 600 chars of results:
  saved ≈ (3625 - 225) + 3 * (320/4) - (200 + 600)/4
        ≈ 3400 + 240 - 200
        ≈ 3440 tokens
```

The catalog term dominates: the more tools a session *could* see, the more Code Mode saves by
not shipping them.

## Determinism and testing

`EstimateTokensSaved` is a pure function. `savings_test.go` asserts byte-stable output for
fixed snapshots, that a larger catalog yields more savings, that more tool calls yield more
savings, and that the result clamps at zero. A nil snapshot yields zero.

## Caveats

- This is a *modeled* estimate, not a measured token count. It does not call a tokenizer; it
  approximates one. Treat it as a relative ROI signal, not an exact ledger.
- It models the savings of *not shipping the catalog* and *collapsing round trips*. It does
  not model second-order effects (e.g. the model reasoning more efficiently over batched
  results), so it is conservative for genuinely multi-tool sessions.
