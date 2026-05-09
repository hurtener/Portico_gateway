<script lang="ts">
  /**
   * Sessions — Phase 10.7c redesign.
   *
   * There is no `/api/sessions` endpoint yet (Phase 9 left it as a
   * placeholder). The Phase 10.6 substrate-first principle says:
   * derive from what we already have. Both snapshots and audit events
   * carry `session_id`, so we aggregate them in-page into a session
   * index. The result is a real, useful page that surfaces what's
   * been observed without waiting on a server-side API.
   *
   * What we know per session_id:
   *  - firstSeen / lastSeen   (min/max created_at across snapshots + events)
   *  - snapshots              (count of catalog snapshots)
   *  - events                 (count of audit events)
   *  - tenant_id              (last-known tenant)
   *  - isPlayground           (psn_ prefix per Phase 10.7b convention)
   *
   * Selecting a row opens an Inspector with overview + linked
   * snapshots + a CTA to jump into the audit log scoped to that
   * session. Filter chips + a tenant dropdown match the rest of the
   * Phase 10.7 family.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type AuditEvent, type Snapshot } from '$lib/api';
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
  import IconActivity from 'lucide-svelte/icons/activity';
  import IconClock from 'lucide-svelte/icons/clock';
  import IconPlay from 'lucide-svelte/icons/play';
  import IconBox from 'lucide-svelte/icons/box';
  import IconExternalLink from 'lucide-svelte/icons/external-link';
  import type { ComponentType } from 'svelte';

  // === Loading ========================================================

  let snapshots: Snapshot[] = [];
  let events: AuditEvent[] = [];
  let loading = true;
  let error = '';

  /**
   * Pull both substrate sources in parallel. We intentionally page
   * each at 100 — sessions older than the most recent ~100 events or
   * snapshots aren't useful as a triage surface; if you need older
   * sessions, the audit/snapshot pages already let you scroll back.
   */
  async function refresh() {
    loading = true;
    error = '';
    try {
      const [snapsRes, auditRes] = await Promise.all([
        api.listSnapshots({ limit: 100 }).catch(() => ({ snapshots: [] as Snapshot[], next_cursor: '' })),
        api.queryAudit({ limit: 100 }).catch(() => ({ events: [] as AuditEvent[], next_cursor: '' }))
      ]);
      snapshots = snapsRes.snapshots ?? [];
      events = auditRes.events ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  // === URL state ======================================================

  let chip = '';
  let tenantFilter = '';
  let search_q = '';
  let selectedId: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('source') || 'all';
    tenantFilter = u.get('tenant') || '';
    search_q = u.get('q') || '';
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
  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'tenant') {
      tenantFilter = e.detail.value;
      pushUrl({ tenant: e.detail.value });
    }
  }
  function onSearchChange(e: CustomEvent<string>) {
    search_q = e.detail;
    pushUrl({ q: search_q });
  }
  function selectRow(row: SessionRow) {
    selectedId = row.session_id;
    pushUrl({ selected: row.session_id });
  }
  function closeInspector() {
    selectedId = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    tenantFilter = '';
    search_q = '';
    pushUrl({ source: null, tenant: null, q: null });
  }

  // === Substrate ======================================================

  type SessionRow = {
    session_id: string;
    tenant_id: string;
    firstSeen: string;
    lastSeen: string;
    snapshots: number;
    events: number;
    isPlayground: boolean;
  };

  function isPlaygroundId(sid: string): boolean {
    return sid.startsWith('psn_');
  }

  function maxIso(a: string, b: string): string {
    if (!a) return b;
    if (!b) return a;
    return a > b ? a : b;
  }
  function minIso(a: string, b: string): string {
    if (!a) return b;
    if (!b) return a;
    return a < b ? a : b;
  }

  $: rows = (() => {
    const map = new Map<string, SessionRow>();
    for (const s of snapshots) {
      const sid = s.session_id ?? '';
      if (!sid) continue;
      const existing = map.get(sid);
      if (existing) {
        existing.snapshots++;
        existing.firstSeen = minIso(existing.firstSeen, s.created_at);
        existing.lastSeen = maxIso(existing.lastSeen, s.created_at);
        if (!existing.tenant_id && s.tenant_id) existing.tenant_id = s.tenant_id;
      } else {
        map.set(sid, {
          session_id: sid,
          tenant_id: s.tenant_id,
          firstSeen: s.created_at,
          lastSeen: s.created_at,
          snapshots: 1,
          events: 0,
          isPlayground: isPlaygroundId(sid)
        });
      }
    }
    for (const e of events) {
      const sid = e.session_id ?? '';
      if (!sid) continue;
      const existing = map.get(sid);
      if (existing) {
        existing.events++;
        existing.firstSeen = minIso(existing.firstSeen, e.occurred_at);
        existing.lastSeen = maxIso(existing.lastSeen, e.occurred_at);
        if (!existing.tenant_id && e.tenant_id) existing.tenant_id = e.tenant_id;
      } else {
        map.set(sid, {
          session_id: sid,
          tenant_id: e.tenant_id,
          firstSeen: e.occurred_at,
          lastSeen: e.occurred_at,
          snapshots: 0,
          events: 1,
          isPlayground: isPlaygroundId(sid)
        });
      }
    }
    return Array.from(map.values()).sort((a, b) => (a.lastSeen < b.lastSeen ? 1 : -1));
  })();

  $: filtered = rows.filter((r) => {
    if (chip === 'active' && !isActive(r)) return false;
    if (chip === 'playground' && !r.isPlayground) return false;
    if (chip === 'mcp' && r.isPlayground) return false;
    if (tenantFilter && r.tenant_id !== tenantFilter) return false;
    if (search_q) {
      const needle = search_q.toLowerCase();
      const hay = `${r.session_id} ${r.tenant_id}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  function isActive(r: SessionRow): boolean {
    try {
      return Date.now() - new Date(r.lastSeen).getTime() < 24 * 60 * 60 * 1000;
    } catch {
      return false;
    }
  }

  $: counts = (() => {
    let total = 0;
    let active = 0;
    let playground = 0;
    let mcp = 0;
    for (const r of rows) {
      total++;
      if (isActive(r)) active++;
      if (r.isPlayground) playground++;
      else mcp++;
    }
    return { total, active, playground, mcp };
  })();

  $: tenantOptions = (() => {
    const uniq = new Set<string>();
    for (const r of rows) if (r.tenant_id) uniq.add(r.tenant_id);
    return [
      { value: '', label: $t('sessions.filter.anyTenant') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  $: selected = filtered.find((r) => r.session_id === selectedId) ?? null;

  $: selectedSnapshots = selected
    ? snapshots.filter((s) => s.session_id === selected!.session_id)
    : [];

  $: inspectorTabs = [
    { id: 'overview', label: $t('sessions.inspector.tab.overview') },
    { id: 'snapshots', label: $t('sessions.inspector.tab.snapshots') }
  ];

  function fmt(time: string): string {
    try {
      return new Date(time).toLocaleString();
    } catch {
      return time;
    }
  }

  function relativeFromNow(time: string): string {
    if (!time) return '';
    try {
      const target = new Date(time).getTime();
      const now = Date.now();
      const diffSec = Math.round((now - target) / 1000);
      const abs = Math.abs(diffSec);
      let value: string;
      if (abs < 60) value = `${abs}s`;
      else if (abs < 3600) value = `${Math.round(abs / 60)}m`;
      else if (abs < 86400) value = `${Math.round(abs / 3600)}h`;
      else value = `${Math.round(abs / 86400)}d`;
      return $t('sessions.relative.ago', { value });
    } catch {
      return time;
    }
  }

  function viewInAudit(sid: string) {
    goto(`/audit?q=${encodeURIComponent(sid)}`);
  }
  function viewSnapshot(snapId: string) {
    goto(`/snapshots?selected=${encodeURIComponent(snapId)}`);
  }

  // === Composition ====================================================

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh(),
      loading
    }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('sessions.metric.total'),
      value: counts.total.toString(),
      helper: $t('sessions.metric.total.helper'),
      icon: IconActivity as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'active',
      label: $t('sessions.metric.active'),
      value: counts.active.toString(),
      helper: $t('sessions.metric.active.helper'),
      icon: IconClock as ComponentType<any>
    },
    {
      id: 'playground',
      label: $t('sessions.metric.playground'),
      value: counts.playground.toString(),
      helper: $t('sessions.metric.playground.helper'),
      icon: IconPlay as ComponentType<any>
    },
    {
      id: 'mcp',
      label: $t('sessions.metric.mcp'),
      value: counts.mcp.toString(),
      helper: $t('sessions.metric.mcp.helper'),
      icon: IconBox as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('sessions.filter.all'), count: counts.total },
    { id: 'active', label: $t('sessions.filter.active'), count: counts.active },
    { id: 'playground', label: $t('sessions.filter.playground'), count: counts.playground },
    { id: 'mcp', label: $t('sessions.filter.mcp'), count: counts.mcp }
  ];

  $: dropdowns = [
    {
      id: 'tenant',
      label: $t('sessions.filter.tenant'),
      value: tenantFilter,
      options: tenantOptions
    }
  ];

  $: columns = [
    { key: 'session_id', label: $t('sessions.col.session') },
    { key: 'tenant_id', label: $t('sessions.col.tenant'), width: '140px' },
    { key: 'source', label: $t('sessions.col.source'), width: '120px' },
    ...(selected
      ? []
      : [
          { key: 'snapshots', label: $t('sessions.col.snapshots'), width: '110px' },
          { key: 'events', label: $t('sessions.col.events'), width: '100px' },
          { key: 'lastSeen', label: $t('sessions.col.lastSeen'), width: '160px' }
        ])
  ];
</script>

<PageHeader title={$t('sessions.title')} description={$t('sessions.description')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('sessions.title')} />
    <FilterChipBar
      searchValue={search_q}
      searchPlaceholder={$t('sessions.filter.search')}
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
      empty={$t('sessions.filter.empty.title')}
      onRowClick={selectRow}
      selectedKey={selectedId}
      rowKeyField="session_id"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const r = row}
        {#if column.key === 'session_id'}
          <IdentityCell
            primary={r.session_id}
            secondary={isActive(r)
              ? $t('sessions.cell.activeSubline', { value: relativeFromNow(r.lastSeen) })
              : relativeFromNow(r.lastSeen)}
            mono
            size="sm"
          />
        {:else if column.key === 'tenant_id'}
          <span class="muted">{r.tenant_id || '—'}</span>
        {:else if column.key === 'source'}
          {#if r.isPlayground}
            <Badge tone="info">{$t('sessions.source.playground')}</Badge>
          {:else}
            <Badge tone="neutral">{$t('sessions.source.mcp')}</Badge>
          {/if}
        {:else if column.key === 'snapshots'}
          <span class="muted">{r.snapshots}</span>
        {:else if column.key === 'events'}
          <span class="muted">{r.events}</span>
        {:else if column.key === 'lastSeen'}
          <span class="muted">{fmt(r.lastSeen)}</span>
        {:else}
          {r[column.key] ?? '—'}
        {/if}
      </svelte:fragment>
      <svelte:fragment slot="empty">
        {#if rows.length === 0}
          <EmptyState
            title={$t('sessions.empty.title')}
            description={$t('sessions.empty.description')}
            compact
          />
        {:else}
          <EmptyState
            title={$t('sessions.filter.empty.title')}
            description={$t('sessions.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('sessions.filter.empty.action')}
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
    emptyTitle={$t('sessions.inspector.empty.title')}
    emptyDescription={$t('sessions.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell
          primary={selected.session_id}
          secondary={selected.isPlayground
            ? $t('sessions.source.playground')
            : $t('sessions.source.mcp')}
          mono
          size="lg"
        />
      {/if}
    </svelte:fragment>

    {#if selected}
      {#if inspectorTab === 'overview'}
        <section class="card">
          <h4>{$t('sessions.inspector.section.identity')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('sessions.col.tenant'), value: selected.tenant_id || '—' },
              { label: $t('sessions.col.source'), value: selected.isPlayground
                ? $t('sessions.source.playground')
                : $t('sessions.source.mcp') }
            ]}
            columns={1}
          />
        </section>
        <section class="card">
          <h4>{$t('sessions.inspector.section.activity')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('sessions.metric.snapshots'), value: String(selected.snapshots) },
              { label: $t('sessions.metric.events'), value: String(selected.events) },
              { label: $t('sessions.col.firstSeen'), value: fmt(selected.firstSeen) },
              { label: $t('sessions.col.lastSeen'), value: fmt(selected.lastSeen) }
            ]}
            columns={1}
          />
        </section>
        <section class="card decisions">
          <div class="decisions-row">
            <Button variant="secondary" on:click={() => viewInAudit(selected.session_id)}>
              <IconExternalLink slot="leading" size={14} />
              {$t('sessions.action.openAudit')}
            </Button>
          </div>
        </section>
      {:else if inspectorTab === 'snapshots'}
        <section class="card">
          <h4>{$t('sessions.inspector.section.snapshots')}</h4>
          {#if selectedSnapshots.length === 0}
            <p class="muted">{$t('sessions.inspector.section.noSnapshots')}</p>
          {:else}
            <ul class="snap-list">
              {#each selectedSnapshots as s (s.id)}
                <li>
                  <button class="snap-row" on:click={() => viewSnapshot(s.id)}>
                    <IdBadge value={s.id} />
                    <span class="muted">{fmt(s.created_at)}</span>
                  </button>
                </li>
              {/each}
            </ul>
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
  .decisions-row {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .snap-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .snap-row {
    width: 100%;
    background: var(--color-bg-canvas);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    display: flex;
    align-items: center;
    justify-content: space-between;
    cursor: pointer;
    color: var(--color-text-primary);
    text-align: left;
  }
  .snap-row:hover {
    border-color: var(--color-accent);
  }
</style>
