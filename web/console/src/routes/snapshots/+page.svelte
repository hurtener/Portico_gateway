<script lang="ts">
  /**
   * Snapshots — Phase 10.7b redesign.
   *
   * The Phase 10.5 page already had the right column set; Phase 10.7b
   * folds it into the design vocabulary: KPI strip + filter chip bar
   * + sticky Inspector when a row is selected. Identity is the
   * snapshot id (mono) with the relative-time subline. The Inspector
   * fetches the full snapshot on demand so the Servers / Tools tabs
   * can render the per-server breakdown without bloating the list
   * payload.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type Snapshot } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    FilterChipBar,
    IdBadge,
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
  import IconDatabase from 'lucide-svelte/icons/database';
  import IconClock from 'lucide-svelte/icons/clock';
  import IconPlay from 'lucide-svelte/icons/play';
  import IconBox from 'lucide-svelte/icons/box';
  import type { ComponentType } from 'svelte';

  // === Loading ========================================================

  let snapshots: Snapshot[] = [];
  let cursor = '';
  let loading = true;
  let listError = '';

  async function refresh(append = false) {
    loading = true;
    listError = '';
    try {
      const res = await api.listSnapshots({
        cursor: append ? cursor : undefined,
        limit: 50
      });
      cursor = res.next_cursor || '';
      snapshots = append ? [...snapshots, ...res.snapshots] : res.snapshots;
    } catch (e) {
      listError = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(() => refresh(false));

  // === URL state ======================================================

  let chip = '';
  let search = '';
  let selectedId: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('source') || 'all';
    search = u.get('q') || '';
    selectedId = u.get('selected');
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
    pushUrl({ source: chip });
  }
  function onSearchChange(e: CustomEvent<string>) {
    search = e.detail;
    pushUrl({ q: search });
  }
  function selectRow(row: Snapshot) {
    selectedId = row.id;
    pushUrl({ selected: row.id });
    loadDetail(row.id);
  }
  function closeInspector() {
    selectedId = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    search = '';
    pushUrl({ source: null, q: null });
  }

  // === Substrate ======================================================

  function isPlayground(s: Snapshot): boolean {
    return typeof s.session_id === 'string' && s.session_id.startsWith('psn_');
  }
  function todayUtc(d: Date): string {
    return d.toISOString().slice(0, 10);
  }
  $: today = todayUtc(new Date());

  $: filtered = snapshots.filter((s) => {
    if (chip === 'today') {
      const created = (s.created_at || '').slice(0, 10);
      if (created !== today) return false;
    }
    if (chip === 'playground' && !isPlayground(s)) return false;
    if (chip === 'mcp' && isPlayground(s)) return false;
    if (search) {
      const needle = search.toLowerCase();
      const hay = `${s.id} ${s.session_id ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let todayCount = 0;
    let playground = 0;
    let mcp = 0;
    let toolSum = 0;
    for (const s of snapshots) {
      total++;
      if ((s.created_at || '').slice(0, 10) === today) todayCount++;
      if (isPlayground(s)) playground++;
      else mcp++;
      toolSum += s.tools?.length ?? 0;
    }
    return {
      total,
      today: todayCount,
      playground,
      mcp,
      avgTools: total > 0 ? Math.round(toolSum / total) : 0
    };
  })();

  // === Detail fetch on selection ======================================

  let detail: Snapshot | null = null;
  let detailError = '';
  let detailLoading = false;
  let detailFor = '';

  async function loadDetail(id: string) {
    if (detailFor === id) return;
    detailFor = id;
    detailLoading = true;
    detailError = '';
    detail = null;
    try {
      detail = await api.getSnapshot(id);
    } catch (e) {
      detailError = (e as Error).message;
    } finally {
      detailLoading = false;
    }
  }

  // Reload detail when URL-driven selection changes (e.g. on first
  // mount with `?selected=...` already in the URL).
  $: if (selectedId && selectedId !== detailFor) loadDetail(selectedId);

  $: selectedHeader = filtered.find((s) => s.id === selectedId) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('snapshots.inspector.tab.overview') },
    {
      id: 'servers',
      label: $t('snapshots.inspector.tab.servers'),
      disabled: !detail
    },
    {
      id: 'tools',
      label: $t('snapshots.inspector.tab.tools'),
      disabled: !detail
    }
  ];

  // === Helpers ========================================================

  function fmt(t: string): string {
    try {
      return new Date(t).toLocaleString();
    } catch {
      return t;
    }
  }
  function fmtRelative(iso?: string | null): string {
    if (!iso) return '—';
    const ts = new Date(iso).getTime();
    if (isNaN(ts)) return '—';
    const delta = Date.now() - ts;
    const m = Math.floor(delta / 60_000);
    if (m < 1) return $t('landing.relTime.justNow');
    if (m < 60) return $t('landing.relTime.minutes', { n: m });
    const h = Math.floor(m / 60);
    if (h < 24) return $t('landing.relTime.hours', { n: h });
    const d = Math.floor(h / 24);
    return $t('landing.relTime.days', { n: d });
  }

  // === Composition ====================================================

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh(false),
      loading
    }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('snapshots.metric.total'),
      value: counts.total.toString(),
      helper: $t('snapshots.metric.total.helper'),
      icon: IconDatabase as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'today',
      label: $t('snapshots.metric.today'),
      value: counts.today.toString(),
      helper: $t('snapshots.metric.today.helper'),
      icon: IconClock as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'playground',
      label: $t('snapshots.metric.playground'),
      value: counts.playground.toString(),
      helper: $t('snapshots.metric.playground.helper'),
      icon: IconPlay as ComponentType<any>
    },
    {
      id: 'tools',
      label: $t('snapshots.metric.tools'),
      value: counts.avgTools.toString(),
      helper: $t('snapshots.metric.tools.helper'),
      icon: IconBox as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('snapshots.filter.all'), count: counts.total },
    { id: 'today', label: $t('snapshots.filter.today'), count: counts.today },
    { id: 'playground', label: $t('snapshots.filter.playground'), count: counts.playground },
    { id: 'mcp', label: $t('snapshots.filter.mcp'), count: counts.mcp }
  ];

  $: columns = [
    { key: 'snapshot', label: $t('snapshots.col.snapshot'), width: '220px' },
    { key: 'source', label: $t('snapshots.col.source'), width: '120px' },
    { key: 'tools', label: $t('snapshots.col.tools'), align: 'right' as const, width: '90px' },
    ...(selected
      ? []
      : [
          { key: 'tenant_id', label: $t('snapshots.col.tenant'), width: '120px' },
          { key: 'session_id', label: $t('snapshots.col.session'), width: '160px' },
          { key: 'overall_hash', label: 'Fingerprint', width: '140px' }
        ])
  ];

  // alias the selectedHeader as `selected` for the layout class binding
  $: selected = selectedHeader;
</script>

<PageHeader title={$t('snapshots.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if listError}<p class="error">{listError}</p>{/if}

<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('snapshots.title')} />
    <FilterChipBar
      searchValue={search}
      searchPlaceholder={$t('snapshots.filter.search')}
      {chips}
      activeChip={chip}
      on:chipChange={onChipChange}
      on:searchChange={onSearchChange}
    />

    <Table
      {columns}
      rows={filtered}
      empty={$t('snapshots.filter.empty.title')}
      onRowClick={selectRow}
      selectedKey={selectedId}
      rowKeyField="id"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const s = row}
        {#if column.key === 'snapshot'}
          <IdentityCell primary={s.id} secondary={fmtRelative(s.created_at)} mono size="md" />
        {:else if column.key === 'source'}
          <Badge tone={isPlayground(s) ? 'info' : 'neutral'}>
            {isPlayground(s) ? $t('snapshots.source.playground') : $t('snapshots.source.mcp')}
          </Badge>
        {:else if column.key === 'tools'}
          <Badge tone="neutral">{s.tools?.length ?? 0}</Badge>
        {:else if column.key === 'tenant_id'}
          <span class="muted">{s.tenant_id}</span>
        {:else if column.key === 'session_id'}
          {#if s.session_id}
            <IdBadge value={s.session_id} />
          {:else}
            <span class="muted">—</span>
          {/if}
        {:else if column.key === 'overall_hash'}
          <IdBadge value={s.overall_hash} chars={8} />
        {:else}
          {s[column.key] ?? '—'}
        {/if}
      </svelte:fragment>
      <svelte:fragment slot="empty">
        {#if snapshots.length === 0}
          <EmptyState
            title={$t('snapshots.empty.title')}
            description={$t('snapshots.empty.description')}
            compact
          />
        {:else}
          <EmptyState
            title={$t('snapshots.filter.empty.title')}
            description={$t('snapshots.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('snapshots.filter.empty.action')}
              </Button>
            </svelte:fragment>
          </EmptyState>
        {/if}
      </svelte:fragment>
    </Table>

    {#if cursor}
      <div class="more">
        <Button variant="secondary" {loading} on:click={() => refresh(true)}>
          {$t('common.loadMore')}
        </Button>
      </div>
    {/if}
  </div>

  <Inspector
    open={selected !== null}
    tabs={inspectorTabs}
    bind:activeTab={inspectorTab}
    emptyTitle={$t('snapshots.inspector.empty.title')}
    emptyDescription={$t('snapshots.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell primary={selected.id} secondary={fmt(selected.created_at)} mono size="lg" />
      {/if}
    </svelte:fragment>

    <svelte:fragment slot="actions">
      {#if selected}
        <Button
          variant="secondary"
          size="sm"
          href={`/snapshots/${encodeURIComponent(selected.id)}`}
        >
          {$t('snapshots.inspector.action.viewDetails')}
        </Button>
      {/if}
    </svelte:fragment>

    {#if selected}
      {#if inspectorTab === 'overview'}
        <section class="card">
          <h4>{$t('snapshots.inspector.section.identity')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('snapshots.col.tenant'), value: selected.tenant_id },
              { label: $t('snapshots.col.created'), value: fmt(selected.created_at) },
              {
                label: $t('snapshots.col.source'),
                value: isPlayground(selected)
                  ? $t('snapshots.source.playground')
                  : $t('snapshots.source.mcp')
              }
            ]}
            columns={2}
          />
        </section>
        <section class="card">
          <h4>Fingerprint</h4>
          <IdBadge value={selected.overall_hash} chars={8} />
        </section>
        {#if selected.session_id}
          <section class="card">
            <h4>{$t('snapshots.col.session')}</h4>
            <IdBadge value={selected.session_id} />
          </section>
        {/if}
      {:else if inspectorTab === 'servers'}
        {#if detailLoading}
          <p class="muted">…</p>
        {:else if detailError}
          <p class="error">{detailError}</p>
        {:else if detail && detail.servers.length > 0}
          <ul class="ent-list">
            {#each detail.servers as srv (srv.id)}
              <li>
                <a class="ent-link" href={`/servers/${encodeURIComponent(srv.id)}`}>
                  <Badge tone="neutral" mono>{srv.display_name || srv.id}</Badge>
                </a>
                <span class="ent-meta">
                  {srv.transport} · {srv.runtime_mode ?? '—'}
                </span>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="muted">No servers in this snapshot.</p>
        {/if}
      {:else if inspectorTab === 'tools'}
        {#if detailLoading}
          <p class="muted">…</p>
        {:else if detail}
          <p class="muted">
            {detail.tools.length} tools · {detail.resources.length} resources · {detail.prompts
              .length} prompts
          </p>
          {#if detail.warnings && detail.warnings.length > 0}
            <section class="card">
              <h4>{$t('snapshots.inspector.section.warnings')}</h4>
              <ul class="warns">
                {#each detail.warnings as w (w)}
                  <li>{w}</li>
                {/each}
              </ul>
            </section>
          {/if}
        {/if}
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
    font-size: var(--font-size-label);
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
  .ent-list {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .ent-list li {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .ent-link {
    text-decoration: none;
  }
  .ent-meta {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .warns {
    margin: 0;
    padding-left: var(--space-5);
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-sm);
  }
  .more {
    margin-top: var(--space-4);
    display: flex;
    justify-content: center;
  }
</style>
