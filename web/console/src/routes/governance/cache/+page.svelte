<script lang="ts">
  /**
   * Governance · Semantic Cache (Phase 15.5). A single-page admin: the active
   * cache config (driver/scope/TTL/threshold), live per-tenant stats, and a
   * prefix-invalidation action. Reads /api/llm/cache/config + /stats, posts to
   * /api/llm/cache/invalidate.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable } from '$lib/api';
  import type { CacheConfig, CacheStats } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    Input,
    KeyValueGrid,
    PageHeader,
    Select,
    Skeleton,
    toast
  } from '$lib/components';

  let loading = true;
  let unavailable = false;
  let error = '';
  let config: CacheConfig | null = null;
  let stats: CacheStats | null = null;

  // Invalidation form.
  let invalKind = 'all';
  let invalValue = '';
  let invalidating = false;

  const invalKindOptions = [
    { value: 'all', label: 'All entries (this tenant)' },
    { value: 'alias', label: 'By model alias' },
    { value: 'scope_id', label: 'By scope id (e.g. a VK)' }
  ];

  onMount(load);

  async function load() {
    error = '';
    try {
      config = await api.getCacheConfig();
      stats = await api.getCacheStats();
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) unavailable = true;
      else error = e instanceof Error ? e.message : 'Failed to load cache config';
    } finally {
      loading = false;
    }
  }

  $: configRows = config
    ? [
        { label: 'Driver', value: config.driver },
        { label: 'Enabled', value: config.enabled ? 'yes' : 'no' },
        { label: 'Scope', value: config.scope },
        { label: 'TTL', value: `${config.ttl_seconds}s` },
        { label: 'Similarity threshold', value: String(config.threshold) }
      ]
    : [];

  async function invalidate() {
    const body: { alias?: string; scope_id?: string; all?: boolean } = {};
    if (invalKind === 'all') body.all = true;
    else if (invalKind === 'alias') body.alias = invalValue.trim();
    else body.scope_id = invalValue.trim();
    if (invalKind !== 'all' && !invalValue.trim()) {
      toast.danger('Enter a value to invalidate by');
      return;
    }
    invalidating = true;
    try {
      const r = await api.invalidateCache(body);
      toast.success(`Removed ${r.removed} cache ${r.removed === 1 ? 'entry' : 'entries'}`);
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Invalidate failed');
    } finally {
      invalidating = false;
    }
  }
</script>

<PageHeader
  title="Semantic Cache"
  description="The LLM response cache in front of the gateway: config, hit rate, and invalidation."
  compact
/>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="Cache not available"
    description="The cache config endpoint is not wired in this build."
  />
{:else if loading}
  <Skeleton height="240px" />
{:else}
  <section class="grid">
    <div class="card">
      <h4>Configuration</h4>
      {#if config && config.driver !== 'none'}
        <KeyValueGrid items={configRows} />
      {:else}
        <p class="hint">
          Caching is <Badge tone="neutral">off</Badge> — set <code>cache.driver</code> in portico.yaml
          (inmem / redis / weaviate / qdrant) to enable it.
        </p>
      {/if}
    </div>

    <div class="card">
      <h4>Live stats</h4>
      {#if stats}
        <div class="metrics">
          <div class="metric">
            <span class="metric-num">{stats.entries}</span>
            <span class="metric-label">entries</span>
          </div>
          <div class="metric">
            <span class="metric-num">{(stats.hit_rate * 100).toFixed(1)}%</span>
            <span class="metric-label">hit rate</span>
          </div>
          <div class="metric">
            <span class="metric-num">{stats.driver}</span>
            <span class="metric-label">driver</span>
          </div>
        </div>
      {/if}
    </div>
  </section>

  <section class="card">
    <h4>Invalidate</h4>
    <p class="hint">Remove cached entries for this tenant. Invalidation never crosses tenants.</p>
    <div class="inval-row">
      <Select bind:value={invalKind} label="Invalidate" options={invalKindOptions} />
      {#if invalKind !== 'all'}
        <Input
          bind:value={invalValue}
          label={invalKind === 'alias' ? 'Model alias' : 'Scope id'}
          block
        />
      {/if}
    </div>
    <div class="actions">
      <Button
        variant="primary"
        on:click={invalidate}
        disabled={invalidating || config?.driver === 'none'}
      >
        {invalidating ? 'Invalidating…' : 'Invalidate'}
      </Button>
    </div>
  </section>
{/if}

<style>
  .error {
    color: var(--color-danger-fg, var(--color-text));
    margin: var(--space-2) 0;
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-4);
    margin-bottom: var(--space-4);
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-bottom: var(--space-4);
  }
  .card h4 {
    margin: 0 0 var(--space-1);
    font-size: var(--font-size-body-lg);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
  }
  .hint {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
  }
  .metrics {
    display: flex;
    gap: var(--space-5);
  }
  .metric {
    display: flex;
    flex-direction: column;
  }
  .metric-num {
    font-size: var(--font-size-display-sm, 1.5rem);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
  }
  .metric-label {
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
  }
  .inval-row {
    display: flex;
    gap: var(--space-3);
    align-items: flex-end;
  }
  .actions {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
</style>
