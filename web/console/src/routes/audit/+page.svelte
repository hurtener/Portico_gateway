<script lang="ts">
  /**
   * Audit log — Phase 10.7b redesign.
   *
   * Composes the design vocabulary: PageHeader (compact) + KPI strip
   * + filter chip bar + paginated table inside the left main-col,
   * sticky Inspector right rail when a row is selected.
   *
   * Severity is derived from the event type slug — there is no
   * `severity` field on the audit row. Conservative mapping: anything
   * containing `failed`, `error`, `denied` → error; `drift`,
   * `warning`, soft denials → warning; everything else → info.
   * The KPI strip + filter chips both use this derivation.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type AuditEvent } from '$lib/api';
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
  import IconHistory from 'lucide-svelte/icons/history';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import IconAlertOctagon from 'lucide-svelte/icons/alert-octagon';
  import IconShieldOff from 'lucide-svelte/icons/shield-off';
  import type { ComponentType } from 'svelte';

  // === Loading + pagination ===========================================

  let events: AuditEvent[] = [];
  let cursor = '';
  let loading = false;
  let error = '';

  async function search(append = false) {
    loading = true;
    error = '';
    try {
      // Phase 11: when the user has typed a query OR pivoted to a
      // specific session, route through the FTS-backed search
      // endpoint. Otherwise fall back to the V1 typed lister so
      // pagination is the same shape it always was.
      const useFTS = !!search_q || !!sessionFilter;
      const res = useFTS
        ? await api.auditSearch({
            q: search_q || undefined,
            session_id: sessionFilter || undefined,
            type: typeFilter || undefined,
            cursor: append ? cursor : undefined,
            limit: 50
          })
        : await api.queryAudit({
            type: typeFilter || undefined,
            cursor: append ? cursor : undefined,
            limit: 50
          });
      cursor = res.next_cursor || '';
      events = append ? [...events, ...res.events] : res.events;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(() => search(false));

  // === URL state ======================================================

  let chip = '';
  let typeFilter = '';
  let search_q = '';
  let sessionFilter = '';
  let selectedId: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('severity') || 'all';
    typeFilter = u.get('type') || '';
    search_q = u.get('q') || '';
    sessionFilter = u.get('session_id') || '';
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
    pushUrl({ severity: chip });
  }
  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'type') {
      typeFilter = e.detail.value;
      cursor = '';
      pushUrl({ type: e.detail.value });
      search(false);
    }
  }
  function onSearchChange(e: CustomEvent<string>) {
    search_q = e.detail;
    cursor = '';
    pushUrl({ q: search_q });
    // Re-run server-side when q changes — FTS is the source of truth
    // for matched rows, not the client-side filter that follows.
    search(false);
  }
  function selectRow(row: AuditRow) {
    selectedId = row.rowKey;
    pushUrl({ selected: row.rowKey });
  }
  function closeInspector() {
    selectedId = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    typeFilter = '';
    search_q = '';
    sessionFilter = '';
    cursor = '';
    pushUrl({ severity: null, type: null, q: null, session_id: null });
    search(false);
  }

  // === Substrate ======================================================

  type Severity = 'error' | 'warning' | 'info';

  function severityOf(type: string): Severity {
    const v = type.toLowerCase();
    if (v.includes('failed') || v.includes('error') || v.includes('denied')) return 'error';
    if (v.includes('drift') || v.includes('warning') || v.includes('reject')) return 'warning';
    return 'info';
  }

  type AuditRow = AuditEvent & { rowKey: string; severity: Severity };

  $: rows = events.map<AuditRow>((e, i) => ({
    ...e,
    // events don't always carry id; fall back to (occurred_at + i)
    // for stable per-render keys.
    rowKey: e.id ?? `${e.occurred_at}-${e.type}-${i}`,
    severity: severityOf(e.type)
  }));

  $: filtered = rows.filter((r) => {
    if (chip === 'errors' && r.severity !== 'error') return false;
    if (chip === 'warnings' && r.severity !== 'warning') return false;
    if (chip === 'policy' && !r.type.startsWith('policy.')) return false;
    if (chip === 'drift' && !r.type.includes('drift')) return false;
    if (search_q) {
      const needle = search_q.toLowerCase();
      const hay = `${r.type} ${r.tenant_id ?? ''} ${r.user_id ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let errors = 0;
    let warnings = 0;
    let policyDenies = 0;
    const types = new Set<string>();
    for (const r of rows) {
      total++;
      if (r.severity === 'error') errors++;
      else if (r.severity === 'warning') warnings++;
      if (r.type.startsWith('policy.') && r.severity === 'error') policyDenies++;
      if (r.type) types.add(r.type);
    }
    return { total, errors, warnings, policyDenies, types: types.size };
  })();

  $: typeOptions = (() => {
    const uniq = new Set<string>();
    for (const r of rows) if (r.type) uniq.add(r.type);
    return [
      { value: '', label: $t('audit.filter.any') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  $: selected = filtered.find((r) => r.rowKey === selectedId) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('audit.inspector.tab.overview') },
    { id: 'payload', label: $t('audit.inspector.tab.payload') },
    { id: 'trace', label: $t('audit.inspector.tab.trace') }
  ];

  // === Helpers ========================================================

  function fmt(t: string): string {
    try {
      return new Date(t).toLocaleString();
    } catch {
      return t;
    }
  }

  type Tone = 'success' | 'danger' | 'warning' | 'neutral' | 'info' | 'accent';
  function severityTone(s: Severity): Tone {
    switch (s) {
      case 'error':
        return 'danger';
      case 'warning':
        return 'warning';
      default:
        return 'neutral';
    }
  }
  function severityLabel(s: Severity): string {
    return s === 'error'
      ? $t('audit.severity.error')
      : s === 'warning'
        ? $t('audit.severity.warning')
        : $t('audit.severity.info');
  }

  // === Composition ====================================================

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => {
        cursor = '';
        search(false);
      },
      loading
    }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('audit.metric.total'),
      value: counts.total.toString(),
      helper: $t('audit.metric.total.helper'),
      icon: IconHistory as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'errors',
      label: $t('audit.metric.errors'),
      value: counts.errors.toString(),
      helper: $t('audit.metric.errors.helper'),
      icon: IconAlertOctagon as ComponentType<any>,
      attention: counts.errors > 0,
      tone: 'danger' as const
    },
    {
      id: 'warnings',
      label: $t('audit.metric.warnings'),
      value: counts.warnings.toString(),
      helper: $t('audit.metric.warnings.helper'),
      icon: IconAlertTriangle as ComponentType<any>
    },
    {
      id: 'policy',
      label: $t('audit.metric.policy'),
      value: counts.policyDenies.toString(),
      helper: $t('audit.metric.policy.helper'),
      icon: IconShieldOff as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('audit.filter.all'), count: counts.total },
    { id: 'errors', label: $t('audit.filter.errors'), count: counts.errors },
    { id: 'warnings', label: $t('audit.filter.warnings'), count: counts.warnings },
    { id: 'policy', label: $t('audit.filter.policy') },
    { id: 'drift', label: $t('audit.filter.drift') }
  ];

  $: dropdowns = [
    {
      id: 'type',
      label: $t('audit.filter.type'),
      value: typeFilter,
      options: typeOptions
    }
  ];

  $: columns = [
    { key: 'when', label: $t('audit.col.when'), width: '170px' },
    { key: 'event', label: $t('audit.col.event'), width: '260px' },
    { key: 'severity', label: $t('audit.col.severity'), width: '110px' },
    ...(selected
      ? []
      : [
          { key: 'tenant_id', label: $t('audit.col.tenant'), width: '120px' },
          { key: 'session_id', label: $t('audit.col.session'), width: '160px' }
        ])
  ];
</script>

<PageHeader title={$t('audit.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('audit.title')} />
    {#if sessionFilter}
      <div class="session-pivot" role="status">
        Filtered to session
        <code>{sessionFilter}</code>
        <button
          type="button"
          class="pivot-clear"
          on:click={() => {
            sessionFilter = '';
            cursor = '';
            pushUrl({ session_id: null });
            search(false);
          }}
        >
          clear
        </button>
      </div>
    {/if}
    <FilterChipBar
      searchValue={search_q}
      searchPlaceholder={$t('audit.filter.search')}
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
      empty={$t('audit.filter.empty.title')}
      onRowClick={selectRow}
      selectedKey={selectedId}
      rowKeyField="rowKey"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const r = row}
        {#if column.key === 'when'}
          <span class="muted">{fmt(r.occurred_at)}</span>
        {:else if column.key === 'event'}
          <IdentityCell primary={r.type} secondary={r.user_id ?? r.tenant_id} mono size="sm" />
        {:else if column.key === 'severity'}
          <Badge tone={severityTone(r.severity)}>{severityLabel(r.severity)}</Badge>
        {:else if column.key === 'tenant_id'}
          <span class="muted">{r.tenant_id}</span>
        {:else if column.key === 'session_id'}
          {#if r.session_id}
            <IdBadge value={r.session_id} />
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
            title={$t('audit.empty.title')}
            description={$t('audit.empty.description')}
            compact
          />
        {:else}
          <EmptyState
            title={$t('audit.filter.empty.title')}
            description={$t('audit.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('audit.filter.empty.action')}
              </Button>
            </svelte:fragment>
          </EmptyState>
        {/if}
      </svelte:fragment>
    </Table>

    {#if cursor}
      <div class="more">
        <Button variant="secondary" {loading} on:click={() => search(true)}>
          {$t('common.loadMore')}
        </Button>
      </div>
    {/if}
  </div>

  <Inspector
    open={selected !== null}
    tabs={inspectorTabs}
    bind:activeTab={inspectorTab}
    emptyTitle={$t('audit.inspector.empty.title')}
    emptyDescription={$t('audit.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell
          primary={selected.type}
          secondary={fmt(selected.occurred_at)}
          mono
          size="lg"
        />
      {/if}
    </svelte:fragment>

    {#if selected}
      {#if inspectorTab === 'overview'}
        <section class="card">
          <h4>{$t('audit.inspector.section.identity')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('audit.col.severity'), value: severityLabel(selected.severity) },
              { label: $t('audit.col.when'), value: fmt(selected.occurred_at) }
            ]}
            columns={2}
          />
        </section>
        <section class="card">
          <h4>{$t('audit.inspector.section.context')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('audit.col.tenant'), value: selected.tenant_id || '—' },
              { label: 'user_id', value: selected.user_id || '—' },
              { label: $t('audit.col.session'), value: selected.session_id || '—' }
            ]}
            columns={1}
          />
        </section>
      {:else if inspectorTab === 'payload'}
        <section class="card">
          <h4>{$t('audit.inspector.section.payload')}</h4>
          <pre class="raw"><code>{JSON.stringify(selected.payload ?? {}, null, 2)}</code></pre>
        </section>
      {:else if inspectorTab === 'trace'}
        <section class="card">
          <h4>{$t('audit.inspector.section.trace')}</h4>
          {#if selected.trace_id || selected.span_id}
            <KeyValueGrid
              items={[
                { label: 'trace_id', value: selected.trace_id || '—' },
                { label: 'span_id', value: selected.span_id || '—' }
              ]}
              columns={1}
            />
          {:else}
            <p class="muted">{$t('audit.inspector.section.noTrace')}</p>
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
  .session-pivot {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border-subtle);
    border-left: 3px solid var(--color-accent-fg);
    border-radius: var(--radius-sm);
    margin-bottom: var(--space-2);
    font-size: var(--font-size-sm);
  }
  .session-pivot code {
    font-family: var(--font-mono);
  }
  .pivot-clear {
    margin-left: auto;
    background: none;
    border: 1px solid var(--color-border);
    color: var(--color-text);
    font-size: 11px;
    padding: 2px 8px;
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .pivot-clear:hover {
    background: var(--color-surface);
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
  .raw {
    margin: 0;
    max-height: 360px;
    overflow: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: var(--space-3);
    color: var(--color-text-secondary);
    white-space: pre-wrap;
    word-break: break-all;
  }
  .more {
    margin-top: var(--space-4);
    display: flex;
    justify-content: center;
  }
</style>
