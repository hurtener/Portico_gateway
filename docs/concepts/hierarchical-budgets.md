# Hierarchical Budgets

A budget caps spend or usage on a **scope** over a **period**. Budgets nest:
**Virtual Key → Team → Customer → Tenant**. A finance-ops user can say "the
marketing department gets $5k/month across all its VKs" and have it enforced
uniformly; the lowest-level overage trips first, and the audit captures which
level fired.

## Defining a budget

A budget is a `(scope_kind, scope_id, metric, period, alignment, limit)` tuple:

- **scope_kind**: `vk` | `team` | `customer` | `tenant`.
- **metric**: `requests` | `tokens` | `cost_usd`.
- **period**: `1m` | `1h` | `1d` | `1w` | `1M` (calendar month) | `1Y`.
- **alignment**: `calendar` (civil UTC boundaries — start of hour/day/week-Monday/
  month/year) or `rolling` (fixed-duration epoch-anchored buckets).

Manage them at `POST /api/governance/budgets` or **Console → Governance →
Budgets**.

## Enforcement

A VK's budget parent (Team or Customer) plus the Team's Customer plus the Tenant
form the **scope chain** for every request. On each LLM call:

1. **Pre-call check** walks the chain most-specific → least. For each enabled
   budget whose metric the call consumes, it compares `used + estimate` against
   the limit. The **first (most-specific) level that would exceed** wins:
   `429 budget_exceeded` with `details.level` (`vk`/`team`/`customer`/`tenant`)
   and `details.metric`. (When the VK and Team would both trip, the response
   says `vk` — the most specific — so operators know exactly what to raise.)
2. **Post-call reconcile** debits the actual usage to **every** applicable level
   in a single transaction — a fault on any level rolls the whole reconcile back,
   so no level is ever updated without the others.

## Warnings

As a ledger crosses **80% / 95% / 100%** of its limit within a window, Portico
emits an audit event once per threshold per window (debounced): `llm.budget_warning`
at 80%, `llm.budget_critical` at 95% and 100%. At 100% the budget also enforces
(the next pre-check denies). Customers can carry a webhook URL for budget alerts.

## Reading headroom

`GET /api/governance/virtual-keys/{id}/budget` returns the live status of every
budget in the VK's chain: `{level, metric, period, used, limit, resets_at,
headroom_pct}`. The Console renders these as stacked headroom bars on the VK
detail.

## Windows

Window math is pure + deterministic (`internal/budgets/window.go`). `calendar`
aligns to civil UTC boundaries; `rolling` uses fixed-duration buckets (1M = 30d,
1Y = 365d — documented approximations). Both are covered by exhaustive table
tests.

See also: [virtual-keys](./virtual-keys.md).
