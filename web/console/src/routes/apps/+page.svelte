<script lang="ts">
  /**
   * MCP Apps — Phase 10.7a redesign.
   *
   * Same composition shell as the other Phase 10.7 list pages:
   * PageHeader (compact) + KPI strip + filter chip bar + table inside
   * the left main-col, sticky Inspector right rail when a row is
   * selected.
   *
   * The "bound to skill" substrate is derived by cross-referencing
   * the apps registry with the skills index — every skill manifest
   * with `binding.ui.resource_uri` declares an app. Apps not
   * referenced by any installed skill are "unbound" (still reachable
   * over MCP, but no curated entry point).
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type AppEntry, type SkillIndexEntry } from '$lib/api';
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
  import IconBoxes from 'lucide-svelte/icons/boxes';
  import IconLink from 'lucide-svelte/icons/link';
  import IconUnlink from 'lucide-svelte/icons/unlink';
  import IconNetwork from 'lucide-svelte/icons/network';
  import type { ComponentType } from 'svelte';

  // === Loading ========================================================

  let items: AppEntry[] = [];
  let skills: SkillIndexEntry[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      // Fire both fetches in parallel — `apps` always exists; skills may
      // 503 when the runtime is disabled, in which case we silently
      // treat every app as unbound.
      const [appsRes, skillsRes] = await Promise.all([
        api.listApps(),
        api.listSkills().catch(() => ({ skills: [] as SkillIndexEntry[] }))
      ]);
      items = appsRes.items ?? [];
      skills = skillsRes?.skills ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }
  onMount(refresh);

  // === URL state ======================================================

  let chip = '';
  let serverFilter = '';
  let search = '';
  let selectedUri: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('binding') || 'all';
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
    pushUrl({ binding: chip });
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
  function selectRow(row: AppRow) {
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
    pushUrl({ binding: null, server: null, q: null });
  }

  // === Substrate ======================================================

  type AppRow = AppEntry & { boundSkill: SkillIndexEntry | null };

  // Index of skill manifests by their declared UI resource URI. Built
  // once per refresh; bound-skill lookup is then O(1).
  $: skillByUri = (() => {
    const m = new Map<string, SkillIndexEntry>();
    for (const s of skills) {
      if (s.ui_resource_uri) m.set(s.ui_resource_uri, s);
    }
    return m;
  })();

  $: rows = items.map<AppRow>((a) => ({
    ...a,
    boundSkill: skillByUri.get(a.uri) ?? null
  }));

  $: filtered = rows.filter((r) => {
    if (chip === 'bound' && !r.boundSkill) return false;
    if (chip === 'unbound' && r.boundSkill) return false;
    if (serverFilter && r.serverId !== serverFilter) return false;
    if (search) {
      const needle = search.toLowerCase();
      const hay = `${r.uri} ${r.name ?? ''} ${r.description ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let bound = 0;
    let unbound = 0;
    const servers = new Set<string>();
    for (const r of rows) {
      total++;
      if (r.boundSkill) bound++;
      else unbound++;
      if (r.serverId) servers.add(r.serverId);
    }
    return { total, bound, unbound, servers: servers.size };
  })();

  $: serverOptions = (() => {
    const uniq = new Set<string>();
    for (const r of rows) if (r.serverId) uniq.add(r.serverId);
    return [
      { value: '', label: $t('apps.filter.any') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  $: selected = filtered.find((r) => r.uri === selectedUri) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('apps.inspector.tab.overview') },
    { id: 'binding', label: $t('apps.inspector.tab.binding') }
  ];

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
    { label: $t('common.refresh'), icon: IconRefreshCw, onClick: refresh, loading }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('apps.metric.total'),
      value: counts.total.toString(),
      helper: $t('apps.metric.total.helper', { n: counts.servers }),
      icon: IconBoxes as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'bound',
      label: $t('apps.metric.bound'),
      value: counts.bound.toString(),
      helper: $t('apps.metric.bound.helper'),
      icon: IconLink as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'unbound',
      label: $t('apps.metric.unbound'),
      value: counts.unbound.toString(),
      helper: $t('apps.metric.unbound.helper'),
      icon: IconUnlink as ComponentType<any>,
      attention: counts.unbound > 0 && counts.bound > 0
    },
    {
      id: 'servers',
      label: $t('apps.metric.servers'),
      value: counts.servers.toString(),
      helper: $t('apps.metric.servers.helper'),
      icon: IconNetwork as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('apps.filter.all'), count: counts.total },
    { id: 'bound', label: $t('apps.filter.bound'), count: counts.bound },
    { id: 'unbound', label: $t('apps.filter.unbound'), count: counts.unbound }
  ];

  $: dropdowns = [
    {
      id: 'server',
      label: $t('apps.filter.server'),
      value: serverFilter,
      options: serverOptions
    }
  ];

  $: columns = [
    { key: 'app', label: $t('apps.col.app'), width: '300px' },
    { key: 'mime', label: $t('apps.col.mime'), width: '160px' },
    { key: 'boundSkill', label: $t('apps.col.boundSkill'), width: '200px' },
    ...(selected
      ? []
      : [
          { key: 'server', label: $t('apps.col.server'), width: '130px' },
          { key: 'discovered', label: $t('apps.col.discovered'), width: '110px' }
        ])
  ];
</script>

<PageHeader title={$t('apps.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('apps.title')} />
    <FilterChipBar
      searchValue={search}
      searchPlaceholder={$t('apps.filter.search')}
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
      empty={$t('apps.filter.empty.title')}
      onRowClick={selectRow}
      selectedKey={selectedUri}
      rowKeyField="uri"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const r = row}
        {#if column.key === 'app'}
          <IdentityCell
            primary={r.name || r.uri}
            secondary={r.uri}
            mono
            size="md"
            glyphSeed={r.serverId || r.uri}
          />
        {:else if column.key === 'mime'}
          {#if r.mimeType}
            <Badge tone="accent" mono>{r.mimeType}</Badge>
          {:else}
            <span class="muted">—</span>
          {/if}
        {:else if column.key === 'boundSkill'}
          {#if r.boundSkill}
            <Badge tone="success" mono>{r.boundSkill.id}</Badge>
          {:else}
            <Badge tone="neutral">unbound</Badge>
          {/if}
        {:else if column.key === 'server'}
          {#if r.serverId}
            <Badge tone="neutral" mono>{r.serverId}</Badge>
          {:else}
            <span class="muted">—</span>
          {/if}
        {:else if column.key === 'discovered'}
          <span class="muted">{fmtRelative(r.discoveredAt)}</span>
        {:else}
          {r[column.key] ?? '—'}
        {/if}
      </svelte:fragment>
      <svelte:fragment slot="empty">
        {#if rows.length === 0}
          <EmptyState
            title={$t('apps.empty.title')}
            description={$t('apps.empty.description')}
            compact
          />
        {:else}
          <EmptyState
            title={$t('apps.filter.empty.title')}
            description={$t('apps.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('apps.filter.empty.action')}
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
    emptyTitle={$t('apps.inspector.empty.title')}
    emptyDescription={$t('apps.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell
          primary={selected.name || selected.uri}
          secondary={selected.uri}
          mono
          size="lg"
          glyphSeed={selected.serverId || selected.uri}
        />
      {/if}
    </svelte:fragment>

    <svelte:fragment slot="actions">
      {#if selected && selected.serverId}
        <Button
          variant="secondary"
          size="sm"
          href={`/servers/${encodeURIComponent(selected.serverId)}`}
        >
          {$t('apps.inspector.action.openServer')}
        </Button>
      {/if}
      {#if selected && selected.boundSkill}
        <Button
          variant="ghost"
          size="sm"
          href={`/skills/${encodeURIComponent(selected.boundSkill.id)}`}
        >
          {$t('apps.inspector.action.openSkill')}
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
          <h4>{$t('apps.inspector.section.identity')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('apps.col.mime'), value: selected.mimeType || '—' },
              { label: $t('apps.col.discovered'), value: fmtRelative(selected.discoveredAt) }
            ]}
            columns={2}
          />
        </section>
        <section class="card">
          <h4>{$t('apps.inspector.section.server')}</h4>
          {#if selected.serverId}
            <a class="server-link" href={`/servers/${encodeURIComponent(selected.serverId)}`}>
              <Badge tone="neutral" mono>{selected.serverId}</Badge>
            </a>
          {:else}
            <span class="muted">—</span>
          {/if}
        </section>
      {:else if inspectorTab === 'binding'}
        {#if selected.boundSkill}
          <section class="card">
            <h4>{$t('apps.inspector.section.boundSkill')}</h4>
            <a class="server-link" href={`/skills/${encodeURIComponent(selected.boundSkill.id)}`}>
              <Badge tone="success" mono>{selected.boundSkill.id}</Badge>
            </a>
            {#if selected.boundSkill.title}
              <p class="prose">{selected.boundSkill.title}</p>
            {/if}
          </section>
        {:else}
          <section class="card">
            <h4>{$t('apps.inspector.section.unbound')}</h4>
            <p class="prose">{$t('apps.inspector.section.unbound.description')}</p>
          </section>
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
</style>
