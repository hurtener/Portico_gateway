<script lang="ts">
  /**
   * Skills — Phase 10.6 redesign.
   *
   * Composition mirrors /servers (PageHeader + MetricStrip +
   * FilterChipBar + Table + Inspector) plus two skill-specific affordances:
   *   - Bulk-select column with a sticky action bar that surfaces when ≥1
   *     row is selected (Enable / Disable / Cancel).
   *   - Pagination footer (default 25/page) — the catalog can grow into
   *     the dozens or hundreds, unlike the server count.
   *
   * URL state:
   *   ?selected=<skill-id>       inspector selection
   *   ?status=<chip>             chip filter (all|enabled|disabled|missing|withUI)
   *   ?server=<server-id>        attached-server dropdown filter
   *   ?q=<search>
   *   ?page=<n> ?per=<10|25|50|100>
   *
   * Bulk-selected ids live in component state, not the URL — they
   * shouldn't survive reload (operator surprise). Pagination is the
   * only place ids are remembered across navigation.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type SkillIndexEntry, type SkillStatus } from '$lib/api';
  import {
    Badge,
    Button,
    Checkbox,
    EmptyState,
    FilterChipBar,
    IdentityCell,
    Inspector,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    Table,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconBlocks from 'lucide-svelte/icons/blocks';
  import IconNetwork from 'lucide-svelte/icons/network';
  import IconFileText from 'lucide-svelte/icons/file-text';
  import IconPanelTop from 'lucide-svelte/icons/panel-top';
  import IconShieldAlert from 'lucide-svelte/icons/shield-alert';
  import IconExternalLink from 'lucide-svelte/icons/external-link';
  import type { ComponentType } from 'svelte';

  // === Loading + data state ============================================

  let entries: SkillIndexEntry[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      const idx = await api.listSkills();
      entries = idx?.skills ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  // === Filter / pagination / selection state — sourced from URL =======

  let chip = '';
  let serverFilter = '';
  let search = '';
  let pageNum = 1;
  let perPage = 25;
  let selectedId: string | null = null;
  let inspectorTab = 'overview';

  // Bulk-select ids — component state only, cleared on filter change.
  let bulkSelected = new Set<string>();

  $: {
    const u = $page.url.searchParams;
    chip = u.get('status') || 'all';
    serverFilter = u.get('server') || '';
    search = u.get('q') || '';
    pageNum = Math.max(1, Number(u.get('page') || 1));
    const per = Number(u.get('per') || 25);
    perPage = [10, 25, 50, 100].includes(per) ? per : 25;
    selectedId = u.get('selected');
  }

  function pushUrl(updates: Record<string, string | number | null>) {
    if (typeof window === 'undefined') return;
    const params = new URLSearchParams($page.url.searchParams);
    for (const [k, v] of Object.entries(updates)) {
      if (
        v === null ||
        v === '' ||
        v === 'all' ||
        (typeof v === 'number' && v <= 1 && k === 'page')
      ) {
        params.delete(k);
      } else {
        params.set(k, String(v));
      }
    }
    const qs = params.toString();
    goto(qs ? `?${qs}` : '?', { replaceState: true, keepFocus: true, noScroll: true });
  }

  function onChipChange(e: CustomEvent<string>) {
    chip = e.detail;
    bulkSelected = new Set();
    pushUrl({ status: chip, page: 1 });
  }
  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'server') {
      serverFilter = e.detail.value;
      bulkSelected = new Set();
      pushUrl({ server: e.detail.value, page: 1 });
    }
  }
  function onSearchChange(e: CustomEvent<string>) {
    search = e.detail;
    pushUrl({ q: search, page: 1 });
  }
  function selectRow(row: SkillIndexEntry) {
    selectedId = row.id;
    pushUrl({ selected: row.id });
  }
  function closeInspector() {
    selectedId = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    serverFilter = '';
    search = '';
    bulkSelected = new Set();
    pushUrl({ status: null, server: null, q: null, page: 1 });
  }
  function nextPage() {
    pageNum += 1;
    pushUrl({ page: pageNum });
  }
  function prevPage() {
    pageNum = Math.max(1, pageNum - 1);
    pushUrl({ page: pageNum });
  }

  // === Filtering + pagination ==========================================

  function statusOf(s: SkillIndexEntry): SkillStatus {
    if (s.status) return s.status;
    if (s.missing_tools && s.missing_tools.length > 0) return 'missing_tools';
    if (s.enabled_for_tenant) return 'enabled';
    return 'disabled';
  }

  $: filtered = entries.filter((s) => {
    const st = statusOf(s);
    if (chip === 'enabled' && st !== 'enabled') return false;
    if (chip === 'disabled' && st !== 'disabled') return false;
    if (chip === 'missing' && st !== 'missing_tools') return false;
    if (chip === 'withUI' && !s.ui_resource_uri) return false;
    if (serverFilter) {
      const attached = s.attached_server || s.required_servers?.[0] || '';
      if (attached !== serverFilter) return false;
    }
    if (search) {
      const needle = search.toLowerCase();
      const hay = `${s.id} ${s.title ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: pageTotal = filtered.length;
  $: pageStart = (pageNum - 1) * perPage;
  $: pageEnd = Math.min(pageStart + perPage, pageTotal);
  $: pageRows = filtered.slice(pageStart, pageEnd);

  // === Aggregates for the KPI strip ====================================

  $: counts = (() => {
    let enabled = 0;
    let disabled = 0;
    let missing = 0;
    let withUI = 0;
    let prompts = 0;
    const servers = new Set<string>();
    for (const s of entries) {
      const st = statusOf(s);
      if (st === 'enabled') enabled++;
      else if (st === 'missing_tools') {
        missing++;
        disabled++; // missing-tools also counts as not-effectively-enabled
      } else disabled++;
      if (s.ui_resource_uri) withUI++;
      prompts += s.assets?.prompts ?? 0;
      const att = s.attached_server || s.required_servers?.[0];
      if (att) servers.add(att);
    }
    return {
      total: entries.length,
      enabled,
      disabled,
      missing,
      withUI,
      prompts,
      servers: servers.size
    };
  })();

  // Distinct attached servers — feeds the dropdown.
  $: serverOptions = (() => {
    const uniq = new Set<string>();
    for (const s of entries) {
      const att = s.attached_server || s.required_servers?.[0];
      if (att) uniq.add(att);
    }
    return [
      { value: '', label: $t('skills.filter.any') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  // === UI helpers ======================================================

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

  type Tone = 'success' | 'danger' | 'warning' | 'neutral' | 'info' | 'accent';
  function statusTone(s: SkillStatus): Tone {
    switch (s) {
      case 'enabled':
        return 'success';
      case 'missing_tools':
        return 'danger';
      case 'review':
        return 'warning';
      case 'draft':
        return 'neutral';
      case 'disabled':
      default:
        return 'neutral';
    }
  }
  function statusLabel(s: SkillStatus): string {
    switch (s) {
      case 'enabled':
        return $t('skills.status.enabled');
      case 'missing_tools':
        return $t('skills.status.missingTools');
      case 'draft':
        return $t('skills.status.draft');
      case 'review':
        return $t('skills.status.review');
      case 'disabled':
      default:
        return $t('skills.status.disabled');
    }
  }
  function fmtAssets(a: SkillIndexEntry['assets']): string {
    if (!a) return '—';
    const parts: string[] = [];
    parts.push($t('skills.assets.format', { prompts: a.prompts, resources: a.resources }));
    if (a.apps > 0) {
      parts.push(
        a.apps === 1
          ? $t('skills.assets.app', { n: a.apps })
          : $t('skills.assets.apps', { n: a.apps })
      );
    }
    return parts.join(' · ');
  }

  // === Single-row toggle (also reused by inspector) ====================

  async function toggle(s: SkillIndexEntry) {
    try {
      if (s.enabled_for_tenant) await api.disableSkill(s.id);
      else await api.enableSkill(s.id);
      await refresh();
    } catch (e) {
      toast.danger((e as Error).message);
    }
  }

  // === Bulk operations =================================================

  function toggleBulk(id: string) {
    const next = new Set(bulkSelected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    bulkSelected = next;
  }
  function toggleAllOnPage(checked: boolean) {
    const next = new Set(bulkSelected);
    for (const r of pageRows) {
      if (checked) next.add(r.id);
      else next.delete(r.id);
    }
    bulkSelected = next;
  }
  $: allOnPageSelected = pageRows.length > 0 && pageRows.every((r) => bulkSelected.has(r.id));

  async function bulkApply(enable: boolean) {
    const ids = Array.from(bulkSelected);
    if (ids.length === 0) return;
    let failures = 0;
    for (const id of ids) {
      try {
        if (enable) await api.enableSkill(id);
        else await api.disableSkill(id);
      } catch {
        failures++;
      }
    }
    if (failures > 0) toast.danger(`${failures}/${ids.length} bulk operations failed`);
    else toast.success(`${ids.length} skills updated`);
    bulkSelected = new Set();
    await refresh();
  }
  function clearBulk() {
    bulkSelected = new Set();
  }

  // === Selected row + inspector tabs ===================================

  $: selected = entries.find((s) => s.id === selectedId) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('skills.inspector.tab.overview') },
    { id: 'assets', label: $t('skills.inspector.tab.assets') },
    { id: 'policy', label: $t('skills.inspector.tab.policy') },
    { id: 'more', label: $t('skills.inspector.tab.more') }
  ];

  // Page actions
  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: refresh,
      loading
    },
    {
      label: $t('nav.sources'),
      onClick: () => goto('/skills/sources')
    },
    {
      label: $t('skills.action.create'),
      variant: 'primary' as const,
      icon: IconExternalLink,
      onClick: () => goto('/skills/authored/new')
    }
  ];

  // KPI strip
  $: metrics = [
    {
      id: 'skills',
      label: $t('skills.metric.skills'),
      value: counts.total.toString(),
      helper: $t('skills.metric.skills.helper', {
        enabled: counts.enabled,
        disabled: counts.disabled
      }),
      icon: IconBlocks as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'servers',
      label: $t('skills.metric.attachedServers'),
      value: counts.servers.toString(),
      helper: $t('skills.metric.attachedServers.helper', { n: counts.servers }),
      icon: IconNetwork as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'prompts',
      label: $t('skills.metric.prompts'),
      value: counts.prompts.toString(),
      helper: $t('skills.metric.prompts.helper', { n: counts.prompts }),
      icon: IconFileText as ComponentType<any>
    },
    {
      id: 'apps',
      label: $t('skills.metric.uiApps'),
      value: counts.withUI.toString(),
      helper: $t('skills.metric.uiApps.helper'),
      icon: IconPanelTop as ComponentType<any>
    },
    {
      id: 'missing',
      label: $t('skills.metric.missing'),
      value: counts.missing.toString(),
      helper:
        counts.missing > 0 ? $t('skills.metric.missing.helper') : $t('skills.metric.missing.none'),
      icon: IconShieldAlert as ComponentType<any>,
      attention: counts.missing > 0
    }
  ];

  // Filter chips with live counts.
  $: chips = [
    { id: 'all', label: $t('skills.filter.all'), count: counts.total },
    { id: 'enabled', label: $t('skills.filter.enabled'), count: counts.enabled },
    { id: 'disabled', label: $t('skills.filter.disabled'), count: counts.disabled },
    { id: 'missing', label: $t('skills.filter.missing'), count: counts.missing },
    { id: 'withUI', label: $t('skills.filter.withUI'), count: counts.withUI }
  ];
  $: dropdowns = [
    {
      id: 'server',
      label: $t('skills.filter.server'),
      value: serverFilter,
      options: serverOptions
    }
  ];

  // Table columns. The select column appears even with no selection
  // (per mockup); the inspector-open shrink hides `lastUpdated` only.
  $: columns = [
    { key: '_select', label: '', width: '36px', align: 'center' as const },
    { key: 'name', label: $t('skills.col.skill'), width: '240px' },
    { key: 'status', label: $t('skills.col.status'), width: '110px' },
    { key: 'attachedServer', label: $t('skills.col.attachedServer'), width: '130px' },
    { key: 'assets', label: $t('skills.col.assets'), width: '180px' },
    { key: 'version', label: $t('skills.col.version'), width: '80px', mono: true },
    ...(selected
      ? []
      : [{ key: 'lastUpdated', label: $t('skills.col.lastUpdated'), width: '110px' }])
  ];
</script>

<PageHeader title={$t('skills.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<!-- Two-column shell — see /servers/+page.svelte for the rationale.
     KPI strip + filter bar + bulk action bar all live INSIDE the left
     column so they shrink with the table when the inspector opens. -->
<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('skills.title')} />

    <FilterChipBar
      searchValue={search}
      searchPlaceholder={$t('skills.filter.search')}
      {chips}
      activeChip={chip}
      {dropdowns}
      on:chipChange={onChipChange}
      on:dropdownChange={onDropdownChange}
      on:searchChange={onSearchChange}
    />

    {#if bulkSelected.size > 0}
      <div class="bulk" data-region="bulk-actions" role="region" aria-label="Bulk actions">
        <span class="bulk-count">{$t('skills.bulk.selected', { n: bulkSelected.size })}</span>
        <span class="bulk-spacer"></span>
        <Button variant="primary" size="sm" on:click={() => bulkApply(true)}>
          {$t('skills.bulk.enable')}
        </Button>
        <Button variant="secondary" size="sm" on:click={() => bulkApply(false)}>
          {$t('skills.bulk.disable')}
        </Button>
        <Button variant="ghost" size="sm" on:click={clearBulk}>
          {$t('skills.bulk.cancel')}
        </Button>
      </div>
    {/if}
    <Table
      {columns}
      rows={pageRows}
      empty={$t('skills.filter.empty.title')}
      onRowClick={selectRow}
      selectedKey={selectedId}
      rowKeyField="id"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const r = row}
        {#if column.key === '_select'}
          <!-- The span swallows the row click so the checkbox toggles
               selection without also opening the inspector. role=
               presentation keeps the a11y tree clean. -->
          <span
            class="checkbox-cell"
            role="presentation"
            on:click|stopPropagation
            on:keydown|stopPropagation
          >
            <Checkbox
              checked={bulkSelected.has(r.id)}
              on:change={() => toggleBulk(r.id)}
              ariaLabel={`Select ${r.id}`}
            />
          </span>
        {:else if column.key === 'name'}
          <IdentityCell primary={r.id} secondary={r.title} mono size="md" />
        {:else if column.key === 'status'}
          <Badge tone={statusTone(statusOf(r))}>{statusLabel(statusOf(r))}</Badge>
        {:else if column.key === 'attachedServer'}
          {#if r.attached_server || r.required_servers?.[0]}
            <Badge tone="neutral" mono>{r.attached_server || r.required_servers[0]}</Badge>
          {:else}
            <span class="muted">—</span>
          {/if}
        {:else if column.key === 'assets'}
          <span class="assets">{fmtAssets(r.assets)}</span>
        {:else if column.key === 'version'}
          <Badge tone="neutral" mono>{r.version}</Badge>
        {:else if column.key === 'lastUpdated'}
          <span class="muted">{fmtRelative(r.last_updated)}</span>
        {:else}
          {r[column.key] ?? '—'}
        {/if}
      </svelte:fragment>
      <svelte:fragment slot="empty">
        {#if entries.length === 0}
          <EmptyState
            title={$t('skills.empty.title')}
            description={$t('skills.empty.description')}
            compact
          />
        {:else}
          <EmptyState
            title={$t('skills.filter.empty.title')}
            description={$t('skills.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('skills.filter.empty.action')}
              </Button>
            </svelte:fragment>
          </EmptyState>
        {/if}
      </svelte:fragment>
    </Table>

    {#if pageTotal > 0}
      <div class="pagination" data-region="pagination">
        <span class="page-summary">
          {$t('skills.pagination.showing', {
            from: pageStart + 1,
            to: pageEnd,
            total: pageTotal
          })}
        </span>
        <span class="page-controls">
          <Button variant="ghost" size="sm" disabled={pageNum <= 1} on:click={prevPage}>
            {$t('skills.pagination.prev')}
          </Button>
          <span class="page-num">{pageNum}</span>
          <Button variant="ghost" size="sm" disabled={pageEnd >= pageTotal} on:click={nextPage}>
            {$t('skills.pagination.next')}
          </Button>
        </span>
      </div>
    {/if}
  </div>

  <Inspector
    open={selected !== null}
    tabs={inspectorTabs}
    bind:activeTab={inspectorTab}
    emptyTitle={$t('skills.inspector.empty.title')}
    emptyDescription={$t('skills.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell primary={selected.id} secondary={selected.title} mono size="lg" />
      {/if}
    </svelte:fragment>

    <svelte:fragment slot="actions">
      {#if selected}
        <Button variant="secondary" size="sm" href={`/skills/${encodeURIComponent(selected.id)}`}>
          {$t('skills.inspector.action.viewDetails')}
        </Button>
        <Button variant="ghost" size="sm" on:click={() => toggle(selected)}>
          {selected.enabled_for_tenant
            ? $t('skills.inspector.action.toggle.disable')
            : $t('skills.inspector.action.toggle.enable')}
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
          <h4>{$t('skills.inspector.section.identity')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('skills.col.version'), value: selected.version },
              { label: $t('skills.col.status'), value: statusLabel(statusOf(selected)) },
              { label: $t('skills.col.lastUpdated'), value: fmtRelative(selected.last_updated) }
            ]}
            columns={2}
          />
        </section>
        <section class="card">
          <h4>{$t('skills.inspector.section.attachedServer')}</h4>
          {#if selected.attached_server || selected.required_servers?.[0]}
            <a
              class="server-link"
              href={`/servers/${encodeURIComponent(selected.attached_server || selected.required_servers[0])}`}
            >
              <Badge tone="neutral" mono>
                {selected.attached_server || selected.required_servers[0]}
              </Badge>
            </a>
          {:else}
            <span class="muted">—</span>
          {/if}
        </section>
        {#if selected.required_tools && selected.required_tools.length > 0}
          <section class="card">
            <h4>{$t('skills.inspector.section.requiredTools')}</h4>
            <ul class="tool-list">
              {#each selected.required_tools as tool (tool)}
                <li>
                  <Badge tone={selected.missing_tools?.includes(tool) ? 'danger' : 'neutral'} mono>
                    {tool}
                  </Badge>
                </li>
              {/each}
            </ul>
          </section>
        {/if}
      {:else if inspectorTab === 'assets'}
        <section class="card">
          <h4>{$t('skills.inspector.section.assets')}</h4>
          {#if selected.assets}
            <KeyValueGrid
              items={[
                { label: 'prompts', value: String(selected.assets.prompts) },
                { label: 'resources', value: String(selected.assets.resources) },
                { label: 'apps', value: String(selected.assets.apps) }
              ]}
              columns={3}
            />
          {:else}
            <span class="muted">—</span>
          {/if}
        </section>
        {#if selected.ui_resource_uri}
          <section class="card">
            <h4>{$t('skills.inspector.section.uiApp')}</h4>
            <code class="uri">{selected.ui_resource_uri}</code>
          </section>
        {/if}
      {:else if inspectorTab === 'policy'}
        <section class="card">
          <h4>{$t('skills.inspector.section.policy')}</h4>
          {#if selected.missing_tools && selected.missing_tools.length > 0}
            <p class="warn">
              <Badge tone="danger">{$t('skills.status.missingTools')}</Badge>
              {selected.missing_tools.length} required tools are not registered.
            </p>
          {:else if selected.enabled_for_tenant}
            <p>
              <Badge tone="success">{$t('skills.status.enabled')}</Badge>
            </p>
          {:else}
            <p>
              <Badge tone="neutral">{$t('skills.status.disabled')}</Badge>
            </p>
          {/if}
        </section>
      {:else if inspectorTab === 'more'}
        <details class="raw">
          <summary>manifest</summary>
          <pre><code>{JSON.stringify(selected, null, 2)}</code></pre>
        </details>
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
  .bulk {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    background: var(--color-accent-primary-subtle);
    border: 1px solid var(--color-accent-primary);
    border-radius: var(--radius-md);
    padding: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
  }
  .bulk-count {
    color: var(--color-accent-primary);
    font-weight: var(--font-weight-semibold);
    font-size: var(--font-size-body-sm);
  }
  .bulk-spacer {
    flex: 1;
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
  .checkbox-cell {
    display: inline-flex;
  }
  .assets {
    color: var(--color-text-secondary);
    font-size: var(--font-size-label);
  }
  .pagination {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: var(--space-3);
    padding: var(--space-3) var(--space-4);
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
  }
  .page-summary {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .page-controls {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }
  .page-num {
    font-family: var(--font-mono);
    color: var(--color-text-secondary);
    font-size: var(--font-size-label);
    padding: 2px var(--space-3);
    background: var(--color-bg-subtle);
    border-radius: var(--radius-pill);
    border: 1px solid var(--color-border-soft);
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
  .tool-list {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
  .uri {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
    word-break: break-all;
  }
  .warn {
    margin: 0;
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-sm);
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .raw summary {
    cursor: pointer;
    font-size: var(--font-size-body-sm);
    color: var(--color-text-secondary);
    margin-bottom: var(--space-2);
  }
  .raw pre {
    max-height: 320px;
    overflow: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: var(--space-3);
    color: var(--color-text-secondary);
  }
</style>
