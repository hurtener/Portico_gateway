<script lang="ts">
  /**
   * LLM health (Phase 13). Per-provider live status: each provider the tenant
   * has configured, cross-referenced with the engine's view of its driver.
   * A disabled provider reads "disabled" (not routable); an enabled provider
   * on a driver the engine doesn't know reads unhealthy. Read-only; a manual
   * Refresh re-polls GET /api/llm/health.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable, type LLMProviderHealth } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    IdentityCell,
    MetricStrip,
    PageHeader,
    Skeleton,
    StatusDot,
    Table,
    toast
  } from '$lib/components';
  import type { Metric } from '$lib/components/MetricStrip.svelte';
  import IconRefresh from 'lucide-svelte/icons/refresh-cw';

  let providers: LLMProviderHealth[] = [];
  let loading = true;
  let refreshing = false;
  let unavailable = false;
  let error = '';

  const columns = [
    { key: 'name', label: 'Provider' },
    { key: 'driver', label: 'Driver' },
    { key: 'status', label: 'Status' },
    { key: 'detail', label: 'Detail' }
  ];

  onMount(load);

  async function load() {
    error = '';
    try {
      const res = await api.listLLMHealth();
      providers = res.providers ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        unavailable = true;
      } else {
        error = e instanceof Error ? e.message : 'Failed to load health';
      }
    } finally {
      loading = false;
      refreshing = false;
    }
  }

  async function refresh() {
    refreshing = true;
    await load();
    if (!error && !unavailable) toast.success('Health refreshed');
  }

  function tone(p: LLMProviderHealth): 'success' | 'warning' | 'danger' {
    if (!p.enabled) return 'warning';
    return p.healthy ? 'success' : 'danger';
  }
  function statusLabel(p: LLMProviderHealth): string {
    if (!p.enabled) return 'Disabled';
    return p.healthy ? 'Healthy' : 'Unhealthy';
  }

  $: healthyCount = providers.filter((p) => p.enabled && p.healthy).length;
  $: unhealthyCount = providers.filter((p) => p.enabled && !p.healthy).length;
  $: disabledCount = providers.filter((p) => !p.enabled).length;
  $: metrics = [
    { id: 'total', label: 'Providers', value: providers.length },
    { id: 'healthy', label: 'Healthy', value: healthyCount, tone: 'success' },
    {
      id: 'unhealthy',
      label: 'Unhealthy',
      value: unhealthyCount,
      tone: 'danger',
      attention: unhealthyCount > 0
    },
    { id: 'disabled', label: 'Disabled', value: disabledCount }
  ] satisfies Metric[];
</script>

<PageHeader
  title="LLM Health"
  description="Live status of each configured provider, as seen by the routing engine."
  compact
>
  <div slot="actions">
    <Button variant="ghost" on:click={refresh} disabled={refreshing || loading || unavailable}>
      <IconRefresh slot="leading" size={14} />{refreshing ? 'Refreshing…' : 'Refresh'}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="LLM gateway not configured"
    description="The LLM engine is not wired in this build."
  />
{:else if loading}
  <div class="stack">
    <Skeleton height="96px" />
    <Skeleton height="200px" />
  </div>
{:else if providers.length === 0}
  <EmptyState
    title="No providers configured"
    description="Add an LLM provider to see its health here."
  >
    <svelte:fragment slot="actions">
      <Button href="/llm/providers" variant="secondary">Go to Providers</Button>
    </svelte:fragment>
  </EmptyState>
{:else}
  <div class="stack">
    <MetricStrip {metrics} label="LLM provider health" />
    <Table {columns} rows={providers} rowKeyField="name">
      <svelte:fragment slot="cell" let:row let:column>
        {#if column.key === 'name'}
          <IdentityCell primary={row.name} size="md" />
        {:else if column.key === 'driver'}
          <Badge tone="neutral" mono>{row.driver}</Badge>
        {:else if column.key === 'status'}
          <span class="status">
            <StatusDot tone={tone(row)} pulse={row.enabled && row.healthy} />
            {statusLabel(row)}
          </span>
        {:else if column.key === 'detail'}
          <span class="muted">{row.detail}</span>
        {/if}
      </svelte:fragment>
    </Table>
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
    gap: var(--space-4);
  }
  .status {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }
  .muted {
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
  }
</style>
