<script lang="ts">
  /**
   * Resources — Phase 10.7a redesign.
   *
   * Composes the design vocabulary established in Phase 10.6:
   * PageHeader (compact) + (KPI strip + filter chip bar + table) inside
   * a left main-col, sticky Inspector right rail when a row is
   * selected. URL-state for filter + selection.
   *
   * The substrate is derived from the resources list itself: MIME
   * type buckets the row into a category (App / Text / JSON / Image
   * / Binary), and the source server is extracted from the namespace
   * prefix (`mcp+server://...` or `ui://...`) or the `_meta.serverID`
   * hint when the rewriter set it.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type Resource } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    FilterChipBar,
    IdentityCell,
    Inspector,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    Table
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconLayers from 'lucide-svelte/icons/layers';
  import IconBoxes from 'lucide-svelte/icons/boxes';
  import IconFileText from 'lucide-svelte/icons/file-text';
  import IconFileBinary from 'lucide-svelte/icons/binary';
  import type { ComponentType } from 'svelte';

  // === Loading state ===================================================

  let resources: Resource[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      const r = await api.listResources();
      resources = r.resources ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }
  onMount(refresh);

  // === URL-bound filter state =========================================

  let chip = '';
  let serverFilter = '';
  let search = '';
  let selectedUri: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('category') || 'all';
    serverFilter = u.get('server') || '';
    search = u.get('q') || '';
    selectedUri = u.get('selected');
  }

  function pushUrl(updates: Record<string, string | null>) {
    if (typeof window === 'undefined') return;
    const params = new URLSearchParams($page.url.searchParams);
    for (const [k, v] of Object.entries(updates)) {
      if (v === null || v === '' || v === 'all') params.delete(k);
      else params.set(k, v);
    }
    const qs = params.toString();
    goto(qs ? `?${qs}` : '?', { replaceState: true, keepFocus: true, noScroll: true });
  }

  function onChipChange(e: CustomEvent<string>) {
    chip = e.detail;
    pushUrl({ category: chip });
  }
  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'server') {
      serverFilter = e.detail.value;
      pushUrl({ server: e.detail.value });
    }
  }
  function onSearchChange(e: CustomEvent<string>) {
    search = e.detail;
    pushUrl({ q: search });
  }
  function selectRow(row: ResourceRow) {
    selectedUri = row.uri;
    pushUrl({ selected: row.uri });
  }
  function closeInspector() {
    selectedUri = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    serverFilter = '';
    search = '';
    pushUrl({ category: null, server: null, q: null });
  }

  // === Substrate derivations ==========================================

  type Category = 'app' | 'text' | 'json' | 'image' | 'binary' | 'unknown';

  function categoryOf(r: Resource): Category {
    if (r.uri.startsWith('ui://')) return 'app';
    const m = (r.mimeType ?? '').toLowerCase();
    if (!m) return 'unknown';
    if (m === 'application/json' || m.endsWith('+json')) return 'json';
    if (m.startsWith('text/')) return 'text';
    if (m.startsWith('image/')) return 'image';
    if (
      m.startsWith('application/octet-stream') ||
      m.startsWith('audio/') ||
      m.startsWith('video/')
    )
      return 'binary';
    return 'unknown';
  }

  function serverIDOf(r: Resource): string {
    const meta = r._meta as { serverID?: string } | undefined;
    if (meta && typeof meta.serverID === 'string') return meta.serverID;
    if (r.uri.startsWith('mcp+server://'))
      return r.uri.slice('mcp+server://'.length).split('/')[0] ?? '';
    if (r.uri.startsWith('ui://')) return r.uri.slice('ui://'.length).split('/')[0] ?? '';
    return '';
  }

  function categoryLabel(c: Category): string {
    switch (c) {
      case 'app':
        return $t('resources.category.app');
      case 'text':
        return $t('resources.category.text');
      case 'json':
        return $t('resources.category.json');
      case 'image':
        return $t('resources.category.image');
      case 'binary':
        return $t('resources.category.binary');
      default:
        return $t('resources.category.unknown');
    }
  }

  type Tone = 'success' | 'danger' | 'warning' | 'neutral' | 'info' | 'accent';
  function categoryTone(c: Category): Tone {
    switch (c) {
      case 'app':
        return 'accent';
      case 'json':
        return 'info';
      case 'text':
        return 'neutral';
      case 'image':
        return 'warning';
      case 'binary':
        return 'neutral';
      default:
        return 'neutral';
    }
  }

  type ResourceRow = Resource & { server: string; category: Category };

  $: rows = resources.map<ResourceRow>((r) => ({
    ...r,
    server: serverIDOf(r),
    category: categoryOf(r)
  }));

  $: filtered = rows.filter((r) => {
    if (chip === 'apps' && r.category !== 'app') return false;
    if (chip === 'text' && r.category !== 'text') return false;
    if (chip === 'json' && r.category !== 'json') return false;
    if (chip === 'binary' && r.category !== 'binary' && r.category !== 'image') return false;
    if (serverFilter && r.server !== serverFilter) return false;
    if (search) {
      const needle = search.toLowerCase();
      const hay = `${r.uri} ${r.name ?? ''} ${r.mimeType ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let apps = 0;
    let text = 0;
    let json = 0;
    let binary = 0;
    const servers = new Set<string>();
    for (const r of rows) {
      total++;
      switch (r.category) {
        case 'app':
          apps++;
          break;
        case 'text':
          text++;
          break;
        case 'json':
          json++;
          break;
        case 'binary':
        case 'image':
          binary++;
          break;
      }
      if (r.server) servers.add(r.server);
    }
    return { total, apps, text, json, binary, servers: servers.size };
  })();

  $: serverOptions = (() => {
    const uniq = new Set<string>();
    for (const r of rows) if (r.server) uniq.add(r.server);
    return [
      { value: '', label: $t('resources.filter.any') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  // === Inspector wiring ===============================================

  $: selected = filtered.find((r) => r.uri === selectedUri) ?? null;

  // Surface the upstream URI when the rewriter set it. Reading
  // selected._meta inline forces a TS cast that Svelte's parser
  // doesn't accept, so we extract it here.
  $: upstreamURI = ((): string => {
    if (!selected || !selected._meta) return '';
    const meta = selected._meta as Record<string, unknown>;
    const v = meta.upstreamURI;
    return typeof v === 'string' ? v : '';
  })();

  $: inspectorTabs = [
    { id: 'overview', label: $t('resources.inspector.tab.overview') },
    { id: 'source', label: $t('resources.inspector.tab.source') }
  ];

  // === Composition ====================================================

  $: pageActions = [
    { label: $t('common.refresh'), icon: IconRefreshCw, onClick: refresh, loading }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('resources.metric.total'),
      value: counts.total.toString(),
      helper: $t('resources.metric.total.helper', { n: counts.servers }),
      icon: IconLayers as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'apps',
      label: $t('resources.metric.apps'),
      value: counts.apps.toString(),
      helper: $t('resources.metric.apps.helper', { n: counts.apps }),
      icon: IconBoxes as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'text',
      label: $t('resources.metric.text'),
      value: (counts.text + counts.json).toString(),
      helper: $t('resources.metric.text.helper'),
      icon: IconFileText as ComponentType<any>
    },
    {
      id: 'binary',
      label: $t('resources.metric.binary'),
      value: counts.binary.toString(),
      helper: $t('resources.metric.binary.helper'),
      icon: IconFileBinary as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('resources.filter.all'), count: counts.total },
    { id: 'apps', label: $t('resources.filter.apps'), count: counts.apps },
    { id: 'text', label: $t('resources.filter.text'), count: counts.text },
    { id: 'json', label: $t('resources.filter.json'), count: counts.json },
    { id: 'binary', label: $t('resources.filter.binary'), count: counts.binary }
  ];

  $: dropdowns = [
    {
      id: 'server',
      label: $t('resources.filter.server'),
      value: serverFilter,
      options: serverOptions
    }
  ];

  $: columns = [
    { key: 'uri', label: $t('resources.col.resource'), width: '300px' },
    { key: 'category', label: $t('resources.col.category'), width: '110px' },
    { key: 'mime', label: $t('resources.col.mime'), width: '180px', mono: true },
    ...(selected ? [] : [{ key: 'server', label: $t('resources.col.server'), width: '140px' }])
  ];
</script>

<PageHeader title={$t('resources.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('resources.title')} />
    <FilterChipBar
      searchValue={search}
      searchPlaceholder={$t('resources.filter.search')}
      {chips}
      activeChip={chip}
      {dropdowns}
      on:chipChange={onChipChange}
      on:dropdownChange={onDropdownChange}
      on:searchChange={onSearchChange}
    />

    <Table
      {columns}
      rows={filtered}
      empty={$t('resources.filter.empty.title')}
      onRowClick={selectRow}
      selectedKey={selectedUri}
      rowKeyField="uri"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const r = row}
        {#if column.key === 'uri'}
          <IdentityCell
            primary={r.name || r.uri}
            secondary={r.name && r.name !== r.uri ? r.uri : undefined}
            mono
            size="md"
            glyphSeed={r.server || r.uri}
          />
        {:else if column.key === 'category'}
          <Badge tone={categoryTone(r.category)}>{categoryLabel(r.category)}</Badge>
        {:else if column.key === 'mime'}
          {#if r.mimeType}
            <Badge tone="neutral" mono>{r.mimeType}</Badge>
          {:else}
            <span class="muted">—</span>
          {/if}
        {:else if column.key === 'server'}
          {#if r.server}
            <Badge tone="neutral" mono>{r.server}</Badge>
          {:else}
            <span class="muted">—</span>
          {/if}
        {:else}
          {r[column.key] ?? '—'}
        {/if}
      </svelte:fragment>
      <svelte:fragment slot="empty">
        {#if rows.length === 0}
          <EmptyState
            title={$t('resources.empty.title')}
            description={$t('resources.empty.description')}
            compact
          />
        {:else}
          <EmptyState
            title={$t('resources.filter.empty.title')}
            description={$t('resources.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('resources.filter.empty.action')}
              </Button>
            </svelte:fragment>
          </EmptyState>
        {/if}
      </svelte:fragment>
    </Table>
  </div>

  <Inspector
    open={selected !== null}
    tabs={inspectorTabs}
    bind:activeTab={inspectorTab}
    emptyTitle={$t('resources.inspector.empty.title')}
    emptyDescription={$t('resources.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell
          primary={selected.name || selected.uri}
          secondary={selected.name && selected.name !== selected.uri ? selected.uri : undefined}
          mono
          size="lg"
          glyphSeed={selected.server || selected.uri}
        />
      {/if}
    </svelte:fragment>

    <svelte:fragment slot="actions">
      {#if selected && selected.server}
        <Button
          variant="secondary"
          size="sm"
          href={`/servers/${encodeURIComponent(selected.server)}`}
        >
          {$t('resources.inspector.action.openServer')}
        </Button>
      {/if}
    </svelte:fragment>

    {#if selected}
      {#if inspectorTab === 'overview'}
        {#if selected.description}
          <section class="card">
            <p class="prose">{selected.description}</p>
          </section>
        {/if}
        <section class="card">
          <h4>{$t('resources.inspector.section.identity')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('resources.col.category'), value: categoryLabel(selected.category) },
              {
                label: $t('resources.col.mime'),
                value: selected.mimeType || '—'
              }
            ]}
            columns={2}
          />
        </section>
        <section class="card">
          <h4>{$t('resources.inspector.section.server')}</h4>
          {#if selected.server}
            <a class="server-link" href={`/servers/${encodeURIComponent(selected.server)}`}>
              <Badge tone="neutral" mono>{selected.server}</Badge>
            </a>
          {:else}
            <span class="muted">—</span>
          {/if}
        </section>
      {:else if inspectorTab === 'source'}
        <section class="card">
          <h4>{$t('resources.inspector.section.upstream')}</h4>
          <code class="uri">{selected.uri}</code>
          {#if upstreamURI}
            <code class="uri muted-uri">{upstreamURI}</code>
          {/if}
        </section>
      {/if}
    {/if}
  </Inspector>
</div>

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-6);
    align-items: start;
  }
  .layout.has-selection {
    grid-template-columns: minmax(0, 1fr) 304px;
  }
  @media (max-width: 1279px) {
    .layout.has-selection {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .main-col {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .muted {
    color: var(--color-text-tertiary);
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
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
  .prose {
    margin: 0;
    color: var(--color-text-primary);
    font-size: var(--font-size-body-sm);
    line-height: 1.55;
  }
  .server-link {
    text-decoration: none;
  }
  .uri {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
    word-break: break-all;
  }
  .muted-uri {
    color: var(--color-text-tertiary);
  }
</style>
