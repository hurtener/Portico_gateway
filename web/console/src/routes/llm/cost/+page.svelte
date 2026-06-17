<script lang="ts">
  /**
   * LLM cost dashboard (Phase 13). Two halves: usage telemetry (per-tenant
   * daily rollups summarised over a date range) and the global price book
   * (unit costs per provider driver+model that turn token counts into
   * dollars). Reads GET /api/llm/costs and /api/llm/costs/prices; the price
   * book is edited via PUT (admin scope).
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    Input,
    MetricStrip,
    PageHeader,
    SegmentedControl,
    Skeleton,
    Table,
    toast
  } from '$lib/components';
  import type { Metric } from '$lib/components/MetricStrip.svelte';

  type CostDaily = {
    day: string;
    alias: string;
    requests: number;
    input_tokens: number;
    output_tokens: number;
    cost_usd: number;
  };
  type CostSummary = {
    requests: number;
    input_tokens: number;
    output_tokens: number;
    cost_usd: number;
  };
  type UnitPrice = {
    provider_driver: string;
    provider_model: string;
    input_per_1k: number;
    output_per_1k: number;
  };

  let loading = true;
  let unavailable = false;
  let error = '';

  let summary: CostSummary = { requests: 0, input_tokens: 0, output_tokens: 0, cost_usd: 0 };
  let daily: CostDaily[] = [];
  let prices: UnitPrice[] = [];

  let range: '7d' | '30d' | 'all' = '30d';
  const rangeOptions = [
    { value: '7d', label: 'Last 7 days' },
    { value: '30d', label: 'Last 30 days' },
    { value: 'all', label: 'All time' }
  ];

  // Price-book inline add form.
  let pDriver = '';
  let pModel = '';
  let pIn = '';
  let pOut = '';
  let savingPrice = false;

  const dailyColumns = [
    { key: 'day', label: 'Day' },
    { key: 'alias', label: 'Model' },
    { key: 'requests', label: 'Requests', align: 'right' as const },
    { key: 'input_tokens', label: 'Input tok', align: 'right' as const },
    { key: 'output_tokens', label: 'Output tok', align: 'right' as const },
    { key: 'cost_usd', label: 'Cost', align: 'right' as const }
  ];
  const priceColumns = [
    { key: 'provider_driver', label: 'Driver' },
    { key: 'provider_model', label: 'Model' },
    { key: 'input_per_1k', label: 'Input / 1k', align: 'right' as const },
    { key: 'output_per_1k', label: 'Output / 1k', align: 'right' as const }
  ];

  function dayString(offsetDays: number): string {
    const d = new Date();
    d.setUTCDate(d.getUTCDate() - offsetDays);
    return d.toISOString().slice(0, 10);
  }

  function rangeParams(): { from?: string; to?: string } {
    if (range === 'all') return {};
    return { from: dayString(range === '7d' ? 6 : 29), to: dayString(0) };
  }

  function setRange(v: string) {
    range = v as '7d' | '30d' | 'all';
    void loadCosts();
  }

  onMount(async () => {
    await Promise.all([loadCosts(), loadPrices()]);
    loading = false;
  });

  async function loadCosts() {
    error = '';
    try {
      const { from, to } = rangeParams();
      const res = await api.listLLMCosts(from, to);
      summary = res.summary;
      daily = res.daily ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        unavailable = true;
      } else {
        error = e instanceof Error ? e.message : 'Failed to load costs';
      }
    }
  }

  async function loadPrices() {
    try {
      const res = await api.listLLMPrices();
      prices = res.prices ?? [];
    } catch {
      prices = [];
    }
  }

  function fmtUSD(n: number): string {
    return `$${n.toFixed(n < 1 ? 4 : 2)}`;
  }
  function fmtInt(n: number): string {
    return n.toLocaleString();
  }

  $: metrics = [
    { id: 'cost', label: 'Total cost', value: fmtUSD(summary.cost_usd), tone: 'brand' },
    { id: 'req', label: 'Requests', value: fmtInt(summary.requests) },
    { id: 'in', label: 'Input tokens', value: fmtInt(summary.input_tokens) },
    { id: 'out', label: 'Output tokens', value: fmtInt(summary.output_tokens) }
  ] satisfies Metric[];

  async function addPrice() {
    if (!pDriver.trim() || !pModel.trim()) {
      toast.danger('Driver and model are required');
      return;
    }
    const inN = parseFloat(pIn);
    const outN = parseFloat(pOut);
    if (Number.isNaN(inN) || Number.isNaN(outN) || inN < 0 || outN < 0) {
      toast.danger('Prices must be non-negative numbers');
      return;
    }
    savingPrice = true;
    try {
      await api.updateLLMPrice({
        provider_driver: pDriver.trim(),
        provider_model: pModel.trim(),
        input_per_1k: inN,
        output_per_1k: outN
      });
      pDriver = '';
      pModel = '';
      pIn = '';
      pOut = '';
      await loadPrices();
      toast.success('Price saved');
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Save failed');
    } finally {
      savingPrice = false;
    }
  }
</script>

<PageHeader
  title="LLM Cost"
  description="Spend and token usage across the gateway, priced from the global price book."
  compact
/>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="LLM gateway not configured"
    description="The LLM cost store is not wired in this build."
  />
{:else if loading}
  <div class="stack">
    <Skeleton height="96px" />
    <Skeleton height="240px" />
  </div>
{:else}
  <div class="stack">
    <div class="toolbar">
      <SegmentedControl
        options={rangeOptions}
        value={range}
        ariaLabel="Cost date range"
        onChange={setRange}
      />
    </div>

    <MetricStrip {metrics} label="LLM cost summary" />

    <section>
      <h3 class="section-title">Daily usage</h3>
      {#if daily.length === 0}
        <EmptyState
          title="No usage in this range"
          description="Cost rollups appear here once requests flow through the gateway."
        />
      {:else}
        <Table columns={dailyColumns} rows={daily} rowKeyField="day">
          <svelte:fragment slot="cell" let:row let:column>
            {#if column.key === 'alias'}
              <Badge tone="neutral" mono>{row.alias}</Badge>
            {:else if column.key === 'requests'}
              {fmtInt(row.requests)}
            {:else if column.key === 'input_tokens'}
              {fmtInt(row.input_tokens)}
            {:else if column.key === 'output_tokens'}
              {fmtInt(row.output_tokens)}
            {:else if column.key === 'cost_usd'}
              <span class="mono">{fmtUSD(row.cost_usd)}</span>
            {:else}
              {row[column.key]}
            {/if}
          </svelte:fragment>
        </Table>
      {/if}
    </section>

    <section>
      <h3 class="section-title">Price book</h3>
      <p class="muted">
        Global per-model unit costs. Token counts are priced from these rates; an unpriced model
        still records usage at $0.
      </p>
      {#if prices.length > 0}
        <Table columns={priceColumns} rows={prices} rowKeyField="provider_model">
          <svelte:fragment slot="cell" let:row let:column>
            {#if column.key === 'provider_driver'}
              <Badge tone="neutral" mono>{row.provider_driver}</Badge>
            {:else if column.key === 'provider_model'}
              <span class="mono">{row.provider_model}</span>
            {:else if column.key === 'input_per_1k'}
              <span class="mono">{fmtUSD(row.input_per_1k)}</span>
            {:else if column.key === 'output_per_1k'}
              <span class="mono">{fmtUSD(row.output_per_1k)}</span>
            {:else}
              {row[column.key]}
            {/if}
          </svelte:fragment>
        </Table>
      {/if}
      <div class="addprice">
        <Input bind:value={pDriver} placeholder="driver (e.g. openai)" />
        <Input bind:value={pModel} placeholder="provider model (e.g. gpt-4o)" />
        <Input bind:value={pIn} type="number" placeholder="input / 1k" />
        <Input bind:value={pOut} type="number" placeholder="output / 1k" />
        <Button variant="secondary" size="sm" on:click={addPrice} disabled={savingPrice}>
          {savingPrice ? 'Saving…' : 'Save price'}
        </Button>
      </div>
    </section>
  </div>
{/if}

<style>
  .error {
    color: var(--color-danger-fg, var(--color-text));
    margin: var(--space-2) 0;
  }
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .toolbar {
    display: flex;
  }
  .section-title {
    margin: 0 0 var(--space-3);
    font-size: var(--font-size-body-lg);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
  }
  .muted {
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
    margin: 0 0 var(--space-3);
    max-width: 60ch;
  }
  .mono {
    font-family: var(--font-mono);
  }
  .addprice {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
  .addprice :global(.field) {
    width: auto;
    min-width: 140px;
  }
</style>
