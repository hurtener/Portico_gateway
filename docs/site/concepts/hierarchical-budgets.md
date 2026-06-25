# Hierarchical Budgets

Portico's budget system caps spend or usage at multiple scopes simultaneously and
enforces those caps on every LLM call. Budgets nest: a Virtual Key sits inside a
Team, which sits inside a Customer, which sits inside the Tenant. Every level can
carry its own independent budget; all applicable levels are checked before a call
is allowed, and all are debited atomically after the call completes.

The result is that a finance operator can say "the marketing department gets
$5 000 per month across all the Virtual Keys it owns" and have that limit enforced
uniformly, without having to coordinate with the teams that create individual
Virtual Keys.

## The budget hierarchy

```
Tenant
└── Customer
    └── Team
        └── Virtual Key
```

A Virtual Key belongs to at most one direct parent — either a Team or a Customer,
never both. A Team always belongs to exactly one Customer. Every VK has the
Tenant as its ultimate ancestor. When Portico processes an LLM request, it walks
the scope chain from most-specific to least-specific:

```
vk → team (if VK's parent is a team) → customer → tenant
```

This chain is resolved from the authenticated principal on each request. A caller
using a plain JWT (no Virtual Key) participates only at the tenant level.

## Budget definition

A budget is identified by the tuple `(scope_kind, scope_id, metric, period,
alignment, limit_val)`. Every field is required at creation time except
`alignment`, which defaults to `rolling`.

| Field | Accepted values | Notes |
|---|---|---|
| `scope_kind` | `vk` `team` `customer` `tenant` | Identifies which level this budget governs |
| `scope_id` | Any valid ID at that scope | The actual VK id, team id, customer id, or tenant id |
| `metric` | `requests` `tokens` `cost_usd` | Units for `limit_val`: count, token count, or US dollars |
| `period` | `1m` `1h` `1d` `1w` `1M` `1Y` | Reset cadence |
| `alignment` | `rolling` `calendar` | How window boundaries are computed (see below) |
| `limit_val` | Positive float | Threshold that triggers enforcement |
| `enabled` | `true` / `false` | Disabled budgets are skipped by the enforcer |

One scope + metric combination can hold at most one budget per period. The
database enforces a unique constraint on `(tenant_id, scope_kind, scope_id,
metric, period)`.

## Window alignment

Period windows can be computed in two modes.

**`calendar`** aligns to civil UTC calendar boundaries:

- `1m` — start of the current UTC minute
- `1h` — start of the current UTC hour
- `1d` — 00:00 UTC today
- `1w` — Monday 00:00 UTC of the current ISO week
- `1M` — first day of the current UTC calendar month
- `1Y` — January 1, 00:00 UTC of the current year

**`rolling`** uses fixed-duration buckets anchored to the Unix epoch. The bucket
containing the current instant is computed by integer division of
`unix_seconds / bucket_seconds`. For `1M` this is a fixed 30-day bucket; for `1Y`
it is a fixed 365-day bucket. These are documented approximations — `calendar`
alignment is the civil-correct option for monthly and yearly reporting.

Window math in `internal/budgets/window.go` is pure and deterministic: the
enforcer never calls `time.Now()` directly. The caller passes the current instant
explicitly, which makes the behaviour fully testable without sleeps or clock mocks.

## Creating a budget

Use the REST API:

```http
POST /api/governance/budgets
Content-Type: application/json

{
  "scope_kind": "team",
  "scope_id":   "tm_01j9zxexample",
  "metric":     "cost_usd",
  "period":     "1M",
  "alignment":  "calendar",
  "limit_val":  5000.00,
  "enabled":    true
}
```

Response (`201 Created`):

```json
{
  "id":         "bdg_a3f1c9e2b8d40721",
  "scope_kind": "team",
  "scope_id":   "tm_01j9zxexample",
  "metric":     "cost_usd",
  "period":     "1M",
  "alignment":  "calendar",
  "limit_val":  5000.00,
  "enabled":    true,
  "created_at": "2026-06-01T09:00:00Z",
  "updated_at": "2026-06-01T09:00:00Z"
}
```

::: info Required fields
`scope_kind`, `scope_id`, `metric`, and `period` are required. `alignment`
defaults to `rolling` when omitted. `enabled` defaults to `true`.
:::

All budget CRUD operations require the caller to hold the `governance:admin`
scope. Tenant-scoped isolation is enforced at the storage layer; no query crosses
tenant boundaries.

## How enforcement works

### Pre-call check

Before every LLM call, the enforcer walks the scope chain most-specific to
least-specific. For each scope, it loads the enabled budgets that apply to the
call's metric, computes the current window for each budget, reads the ledger row
for that window, and tests whether `used + estimated_amount > limit_val`.

The **first scope where this condition is true** causes an immediate rejection:

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json

{
  "error": "budget_exceeded",
  "message": "budget exceeded at vk level for cost_usd",
  "details": {
    "level":  "vk",
    "metric": "cost_usd",
    "limit":  10.00,
    "used":    9.87
  }
}
```

The `details.level` field tells operators exactly which level fired — `vk`,
`team`, `customer`, or `tenant`. When a Virtual Key and its parent Team would
both trip on the same call, the response names the Virtual Key, because the
Virtual Key is the more specific scope. This makes "which budget do I need to
raise?" immediately legible without cross-referencing parent relationships.

The estimated usage is conservative: `max_tokens` from the request is treated as
worst-case output token consumption.

### Budget store errors fail open

If the budget store returns an error during the pre-call check, the request is
allowed. Portico does not block production traffic on a budget-infrastructure
hiccup. Operators should monitor `llm.budget_warning` and `llm.budget_critical`
audit events to catch cases where the store is degraded.

### Post-call reconcile

After a call completes successfully, the actual usage (real token counts and
resolved cost in USD) is applied to every applicable budget level in a single
transaction. The `BudgetStore.ReconcileUsage` contract is explicit:

```go
// ReconcileUsage applies ALL updates in ONE transaction (atomic post-call
// reconcile across budget levels). Each update upserts the ledger row, adding
// Delta to used. If ANY update fails the whole tx rolls back (no partial).
ReconcileUsage(ctx context.Context, tenantID string, updates []LedgerUpdate) error
```

A fault anywhere in the transaction rolls back all ledger updates for that call.
No level is ever debited without every other applicable level being debited at the
same time. This prevents budget-hierarchy inconsistency: if the team ledger update
failed while the VK ledger succeeded, the team's reported spend would be
understated.

Post-call reconciliation is best-effort with respect to the HTTP response — the
response is already sent to the caller before reconcile runs, so a reconcile error
does not cause the caller to see a failure.

## Usage example: layered governance

An organisation has one Portico tenant. Within it:

- Customer `acme-corp` represents a top-level business unit with a $10 000/month
  cap.
- Team `acme-marketing` belongs to `acme-corp` and has a $2 000/month cap.
- Virtual Key `pk-portico-abc` belongs to `acme-marketing` and has a $500/month
  cap and a 100 000 token/day cap.

Four budget records in storage:

```json
[
  {
    "scope_kind": "tenant",
    "scope_id":   "tenant-xyz",
    "metric":     "cost_usd",
    "period":     "1M",
    "alignment":  "calendar",
    "limit_val":  50000.00
  },
  {
    "scope_kind": "customer",
    "scope_id":   "cust_acme",
    "metric":     "cost_usd",
    "period":     "1M",
    "alignment":  "calendar",
    "limit_val":  10000.00
  },
  {
    "scope_kind": "team",
    "scope_id":   "tm_marketing",
    "metric":     "cost_usd",
    "period":     "1M",
    "alignment":  "calendar",
    "limit_val":  2000.00
  },
  {
    "scope_kind": "vk",
    "scope_id":   "vk_abc",
    "metric":     "tokens",
    "period":     "1d",
    "alignment":  "calendar",
    "limit_val":  100000
  }
]
```

A call using `pk-portico-abc` is checked in this order:

1. VK `vk_abc` — daily token budget. If `used + estimated_tokens > 100 000`, deny
   with `level: vk, metric: tokens`.
2. Team `tm_marketing` — monthly cost budget. If the team's cost aggregate would
   exceed $2 000, deny with `level: team, metric: cost_usd`.
3. Customer `cust_acme` — monthly cost budget. If the customer aggregate would
   exceed $10 000, deny with `level: customer, metric: cost_usd`.
4. Tenant — monthly cost budget. If the tenant aggregate would exceed $50 000,
   deny with `level: tenant, metric: cost_usd`.

If the call succeeds, all four applicable ledgers are updated atomically.

## Budget warnings

As a ledger accumulates usage within its current window, Portico emits audit
events when it crosses three thresholds:

| Threshold | Audit event type | Notes |
|---|---|---|
| 80% | `llm.budget_warning` | Emitted once per threshold per window |
| 95% | `llm.budget_critical` | Emitted once per window |
| 100% | `llm.budget_critical` | Budget also starts enforcing (next pre-check denies) |

Warnings are debounced: once a threshold fires for a given `(budget, window)`,
it does not fire again even if further calls accumulate usage. The debounce state
is stored in `last_warning_level` on the ledger row and is reset automatically
when the window rolls over.

The audit payload for a warning includes `level`, `metric`, `budget_id`, `pct`,
`used`, and `limit`. Customers can carry a webhook URL for budget alerts;
`llm.budget_critical` events at the customer scope trigger the webhook.

## Reading headroom

`GET /api/governance/virtual-keys/{id}/budget` returns the live status of every
budget in the VK's scope chain:

```http
GET /api/governance/virtual-keys/vk_abc/budget
Authorization: Bearer <jwt>
```

```json
{
  "vk_id": "vk_abc",
  "levels": [
    {
      "level":       "vk",
      "metric":      "tokens",
      "budget_id":   "bdg_tokenday",
      "period":      "1d",
      "used":        72000,
      "limit":       100000,
      "resets_at":   "2026-06-02T00:00:00Z",
      "headroom_pct": 28.0
    },
    {
      "level":       "team",
      "metric":      "cost_usd",
      "budget_id":   "bdg_mktgmonth",
      "period":      "1M",
      "used":        1430.50,
      "limit":       2000.00,
      "resets_at":   "2026-07-01T00:00:00Z",
      "headroom_pct": 28.475
    }
  ]
}
```

`headroom_pct` is 0–100, clamped (never negative even when `used > limit`).
The Console renders these as stacked headroom bars on the Virtual Key detail page.

## Disabling a budget

Set `"enabled": false` via `PUT /api/governance/budgets/{id}`. A disabled budget
is skipped by the pre-call enforcer and excluded from reconcile updates. The
ledger rows are preserved; re-enabling the budget resumes enforcement against the
existing accumulated usage in the current window.

## Storage schema

Budgets and their ledgers are stored in two tables introduced by migration `0022`:

```sql
CREATE TABLE governance_budgets (
    tenant_id   TEXT NOT NULL,
    id          TEXT NOT NULL,
    scope_kind  TEXT NOT NULL CHECK (scope_kind IN ('vk','team','customer','tenant')),
    scope_id    TEXT NOT NULL,
    metric      TEXT NOT NULL CHECK (metric IN ('requests','tokens','cost_usd')),
    period      TEXT NOT NULL CHECK (period IN ('1m','1h','1d','1w','1M','1Y')),
    alignment   TEXT NOT NULL CHECK (alignment IN ('rolling','calendar')),
    limit_val   REAL NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, scope_kind, scope_id, metric, period)
);

CREATE TABLE governance_budget_ledger (
    tenant_id          TEXT NOT NULL,
    budget_id          TEXT NOT NULL,
    window_key         TEXT NOT NULL,
    used               REAL NOT NULL DEFAULT 0,
    resets_at          TEXT NOT NULL,
    last_warning_level INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, budget_id, window_key),
    FOREIGN KEY (tenant_id, budget_id)
        REFERENCES governance_budgets(tenant_id, id) ON DELETE CASCADE
);
```

`window_key` is a deterministic string identifier for the window bucket (for
example, `cal:1M:2026-06` for the calendar-aligned June 2026 monthly window).
The `last_warning_level` column stores the highest threshold (0, 80, 95, or 100)
that has already been emitted for this ledger row, providing the per-window
debounce.

Ledger rows cascade-delete with their parent budget.

## Policy integration

Budgets expose matcher attributes for the policy engine:

- `budget.headroom_pct` — the remaining percentage at the most-constraining
  level; rules can deny or reroute when headroom is below a threshold.

Actions include `clamp_to_customer_budget` to cap `max_tokens` on the request to
what the customer's budget headroom would allow before a hard denial.

See [Policy](/concepts/policy) for the full rule language.

## Related

- [Virtual Keys](/concepts/virtual-keys) — the most granular scope level in the
  budget hierarchy; carry their own scope sets, provider allowlists, and budget
  allocations.
- [LLM Gateway](/concepts/llm-gateway) — the request path that triggers the
  pre-call check and post-call reconcile.
- [Audit](/concepts/audit) — `llm.budget_warning` and `llm.budget_critical` events
  are queryable through the audit API; each event records the level, metric,
  budget id, and threshold percentage that fired.
- [Agent Profiles](/concepts/agent-profiles) — the consumer-binding primitive that
  Virtual Keys attach to; controls which servers, tools, and models a caller can
  reach.
- [Observability](/concepts/observability) — budget pre-check latency and warning
  counts appear in Portico's span attributes.
- [REST API Reference](/reference/rest-api) — full endpoint signatures for
  `/api/governance/budgets`, `/api/governance/virtual-keys/{id}/budget`, and
  related governance resources.
