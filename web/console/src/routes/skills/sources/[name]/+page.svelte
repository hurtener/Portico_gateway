<script lang="ts">
  /**
   * Skill source detail — Phase 10.8 detail-page sub-vocabulary.
   *
   * Adopts: Breadcrumbs slot, meta badges, PageActionGroup, compact
   * MetricStrip mini-KPI (Packs / Last refresh / Errors), Tabs
   * (Overview / Config / Packs), and .card sections with the <h4>
   * SECTION-LABEL header. The legacy stacked-sections layout retires.
   * `last_error` lifts into a Badge tone="danger" so it's visible
   * before scrolling.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type SkillSource, type SourcePack } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    CodeBlock,
    EmptyState,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    Table,
    Tabs,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconLayers from 'lucide-svelte/icons/layers';
  import IconClock from 'lucide-svelte/icons/clock';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import type { ComponentType } from 'svelte';

  let source: SkillSource | null = null;
  let packs: SourcePack[] = [];
  let loading = true;
  let error = '';
  let activeTab = 'overview';

  $: name = $page.params.name ?? '';

  async function refresh() {
    if (!name) return;
    loading = true;
    error = '';
    try {
      source = await api.getSkillSource(name);
      try {
        const out = await api.listSkillSourcePacks(name);
        packs = out.items ?? [];
      } catch {
        packs = [];
      }
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function trigger() {
    try {
      await api.refreshSkillSource(name);
      toast.success($t('sources.action.refresh'));
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);

  function fmt(iso?: string): string {
    if (!iso) return '—';
    try {
      return new Date(iso).toLocaleString();
    } catch {
      return iso;
    }
  }

  function relativeFromNow(iso?: string): string {
    if (!iso) return $t('common.dash');
    try {
      const target = new Date(iso).getTime();
      const diffSec = Math.round((Date.now() - target) / 1000);
      const abs = Math.abs(diffSec);
      let value: string;
      if (abs < 60) value = `${abs}s`;
      else if (abs < 3600) value = `${Math.round(abs / 60)}m`;
      else if (abs < 86400) value = `${Math.round(abs / 3600)}h`;
      else value = `${Math.round(abs / 86400)}d`;
      return $t('sources.relative.ago', { value });
    } catch {
      return $t('common.dash');
    }
  }

  $: tabs = [
    { id: 'overview', label: $t('sourceDetail.tab.overview') },
    { id: 'config', label: $t('sourceDetail.tab.config') },
    { id: 'packs', label: $t('sourceDetail.tab.packs') }
  ];

  $: identityKV = source
    ? [
        { label: $t('sources.col.driver'), value: source.driver, mono: true },
        { label: $t('sources.col.priority'), value: String(source.priority ?? 100) },
        {
          label: $t('sources.col.enabled'),
          value: source.enabled ? $t('common.yes') : $t('common.no')
        }
      ]
    : [];

  $: timingKV = source
    ? [
        {
          label: $t('sources.detail.lastRefresh'),
          value: fmt(source.last_refresh_at)
        },
        {
          label: $t('sources.col.createdAt'),
          value: fmt(source.created_at)
        }
      ]
    : [];

  $: pageActions = [
    {
      label: $t('sources.action.refresh'),
      icon: IconRefreshCw,
      onClick: trigger,
      loading
    }
  ];

  $: metrics = source
    ? [
        {
          id: 'packs',
          label: $t('sourceDetail.metric.packs'),
          value: packs.length.toString(),
          icon: IconLayers as ComponentType<any>,
          tone: 'brand' as const
        },
        {
          id: 'lastRefresh',
          label: $t('sourceDetail.metric.lastRefresh'),
          value: source.last_refresh_at ? relativeFromNow(source.last_refresh_at) : $t('common.dash'),
          icon: IconClock as ComponentType<any>
        },
        {
          id: 'error',
          label: $t('sourceDetail.metric.errors'),
          value: source.last_error ? '1' : '0',
          icon: IconAlertTriangle as ComponentType<any>,
          tone: source.last_error ? ('danger' as const) : ('default' as const),
          attention: !!source.last_error
        }
      ]
    : [];

  const packCols = [
    { key: 'id', label: 'ID', mono: true },
    { key: 'version', label: 'Version', mono: true }
  ];
</script>

<PageHeader title={name} description={source?.driver}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[
      { label: $t('nav.skills'), href: '/skills' },
      { label: $t('nav.sources'), href: '/skills/sources' },
      { label: name }
    ]}
  />
  <div slot="meta">
    {#if source}
      <Badge tone="neutral" mono>{source.driver}</Badge>
      {#if source.last_error}
        <Badge tone="danger">{$t('sources.status.failing')}</Badge>
      {:else if source.enabled}
        <Badge tone="success">{$t('sources.status.enabled')}</Badge>
      {:else}
        <Badge tone="neutral">{$t('sources.status.disabled')}</Badge>
      {/if}
    {/if}
  </div>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if !source && !loading}
  <EmptyState title={$t('sources.detail.notFound.title')} />
{:else if source}
  <MetricStrip {metrics} compact label={$t('sourceDetail.metric.aria')} />
  <Tabs {tabs} bind:active={activeTab} />

  {#if activeTab === 'overview'}
    <section class="card">
      <h4>{$t('sourceDetail.section.identity')}</h4>
      <KeyValueGrid items={identityKV} columns={3} />
    </section>
    <section class="card">
      <h4>{$t('sourceDetail.section.timing')}</h4>
      <KeyValueGrid items={timingKV} columns={1} />
    </section>
    {#if source.last_error}
      <section class="card">
        <h4>{$t('sourceDetail.section.lastError')}</h4>
        <p class="err-block">{source.last_error}</p>
      </section>
    {/if}
  {:else if activeTab === 'config'}
    <section class="card">
      <h4>{$t('sourceDetail.section.config')}</h4>
      <CodeBlock language="json" code={JSON.stringify(source.config ?? {}, null, 2)} />
    </section>
    {#if source.credential_ref}
      <section class="card">
        <h4>{$t('sourceDetail.section.credential')}</h4>
        <code class="mono">{source.credential_ref}</code>
      </section>
    {/if}
  {:else if activeTab === 'packs'}
    <section class="card">
      <h4>{$t('sources.detail.packs')}</h4>
      {#if packs.length === 0}
        <EmptyState title={$t('common.empty')} compact />
      {:else}
        <Table columns={packCols} rows={packs} />
      {/if}
    </section>
  {/if}
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .err-block {
    margin: 0;
    color: var(--color-danger);
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    background: var(--color-bg-canvas);
    border-radius: var(--radius-sm);
    padding: var(--space-3);
    border: 1px solid var(--color-border-soft);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    margin-top: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .card h4 {
    margin: 0;
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
</style>
