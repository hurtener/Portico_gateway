<script lang="ts">
  /**
   * Approvals — Phase 10.7c redesign.
   *
   * Composes the design vocabulary: PageHeader (compact) + KPI strip
   * + filter chip bar + table inside the left main-col, sticky
   * Inspector on the right when a row is selected. Approve / deny
   * decisions live in the Inspector footer so the operator stays in
   * the same context while triaging a queue.
   *
   * The 2s polling cadence from the Phase 9 page is preserved — the
   * queue mutates from outside (skill runs hit the gate) and stale
   * data here is the worst kind of UX bug.
   */
  import { onMount, onDestroy } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type Approval } from '$lib/api';
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
    Table,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconCheck from 'lucide-svelte/icons/check';
  import IconX from 'lucide-svelte/icons/x';
  import IconShield from 'lucide-svelte/icons/shield';
  import IconShieldAlert from 'lucide-svelte/icons/shield-alert';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import IconClock from 'lucide-svelte/icons/clock';
  import type { ComponentType } from 'svelte';

  // === Loading + polling ==============================================

  let approvals: Approval[] = [];
  let loading = true;
  let error = '';
  let timer: ReturnType<typeof setInterval> | null = null;
  let busyId: string | null = null;

  async function refresh() {
    try {
      approvals = await api.listApprovals();
      error = '';
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function approve(id: string) {
    busyId = id;
    try {
      await api.approveApproval(id);
      toast.success($t('approvals.toast.approved'), id);
      if (selectedId === id) selectedId = null;
      pushUrl({ selected: null });
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busyId = null;
    }
  }

  async function deny(id: string) {
    busyId = id;
    try {
      await api.denyApproval(id);
      toast.info($t('approvals.toast.denied'), id);
      if (selectedId === id) selectedId = null;
      pushUrl({ selected: null });
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busyId = null;
    }
  }

  onMount(() => {
    refresh();
    timer = setInterval(refresh, 2000);
  });

  onDestroy(() => {
    if (timer !== null) clearInterval(timer);
  });

  // === URL state ======================================================

  let chip = '';
  let riskFilter = '';
  let search_q = '';
  let selectedId: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('risk') || 'all';
    riskFilter = u.get('class') || '';
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
    pushUrl({ risk: chip });
  }
  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'class') {
      riskFilter = e.detail.value;
      pushUrl({ class: e.detail.value });
    }
  }
  function onSearchChange(e: CustomEvent<string>) {
    search_q = e.detail;
    pushUrl({ q: search_q });
  }
  function selectRow(row: Approval) {
    selectedId = row.id;
    pushUrl({ selected: row.id });
  }
  function closeInspector() {
    selectedId = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    riskFilter = '';
    search_q = '';
    pushUrl({ risk: null, class: null, q: null });
  }

  // === Substrate ======================================================

  type RiskBucket = 'destructive' | 'sensitive' | 'sideEffect' | 'read' | 'other';

  function bucketOf(rc: string): RiskBucket {
    const v = (rc ?? '').toLowerCase();
    if (v === 'destructive') return 'destructive';
    if (v === 'sensitive_read') return 'sensitive';
    if (v === 'external_side_effect') return 'sideEffect';
    if (v === 'idempotent_read' || v === 'read') return 'read';
    return 'other';
  }

  type Tone = 'success' | 'danger' | 'warning' | 'neutral' | 'info' | 'accent';
  function riskTone(rc: string): Tone {
    const b = bucketOf(rc);
    if (b === 'destructive' || b === 'sensitive') return 'danger';
    if (b === 'sideEffect') return 'warning';
    if (b === 'read') return 'info';
    return 'neutral';
  }

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
      const diffSec = Math.round((target - now) / 1000);
      const abs = Math.abs(diffSec);
      const past = diffSec < 0;
      let value: string;
      if (abs < 60) value = `${abs}s`;
      else if (abs < 3600) value = `${Math.round(abs / 60)}m`;
      else if (abs < 86400) value = `${Math.round(abs / 3600)}h`;
      else value = `${Math.round(abs / 86400)}d`;
      return past
        ? $t('approvals.relative.ago', { value })
        : $t('approvals.relative.in', { value });
    } catch {
      return time;
    }
  }

  $: filtered = approvals.filter((a) => {
    const b = bucketOf(a.risk_class);
    if (chip === 'destructive' && b !== 'destructive') return false;
    if (chip === 'sensitive' && b !== 'sensitive') return false;
    if (chip === 'side' && b !== 'sideEffect') return false;
    if (chip === 'read' && b !== 'read') return false;
    if (riskFilter && a.risk_class !== riskFilter) return false;
    if (search_q) {
      const needle = search_q.toLowerCase();
      const hay =
        `${a.tool} ${a.session_id ?? ''} ${a.user_id ?? ''} ${a.tenant_id ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let destructive = 0;
    let sensitive = 0;
    let sideEffect = 0;
    let read = 0;
    let expiringSoon = 0;
    const now = Date.now();
    for (const a of approvals) {
      total++;
      const b = bucketOf(a.risk_class);
      if (b === 'destructive') destructive++;
      else if (b === 'sensitive') sensitive++;
      else if (b === 'sideEffect') sideEffect++;
      else if (b === 'read') read++;
      try {
        const exp = new Date(a.expires_at).getTime();
        if (exp - now < 5 * 60 * 1000) expiringSoon++;
      } catch {
        /* ignore */
      }
    }
    return { total, destructive, sensitive, sideEffect, read, expiringSoon };
  })();

  $: riskOptions = (() => {
    const uniq = new Set<string>();
    for (const a of approvals) if (a.risk_class) uniq.add(a.risk_class);
    return [
      { value: '', label: $t('approvals.filter.any') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  $: selected = filtered.find((a) => a.id === selectedId) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('approvals.inspector.tab.overview') },
    { id: 'args', label: $t('approvals.inspector.tab.args') }
  ];

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
      label: $t('approvals.metric.total'),
      value: counts.total.toString(),
      helper: $t('approvals.metric.total.helper'),
      icon: IconShield as ComponentType<any>,
      tone: 'brand' as const,
      attention: counts.total > 0
    },
    {
      id: 'destructive',
      label: $t('approvals.metric.destructive'),
      value: counts.destructive.toString(),
      helper: $t('approvals.metric.destructive.helper'),
      icon: IconShieldAlert as ComponentType<any>,
      tone: 'danger' as const,
      attention: counts.destructive > 0
    },
    {
      id: 'sensitive',
      label: $t('approvals.metric.sensitive'),
      value: counts.sensitive.toString(),
      helper: $t('approvals.metric.sensitive.helper'),
      icon: IconAlertTriangle as ComponentType<any>
    },
    {
      id: 'expiring',
      label: $t('approvals.metric.expiring'),
      value: counts.expiringSoon.toString(),
      helper: $t('approvals.metric.expiring.helper'),
      icon: IconClock as ComponentType<any>,
      attention: counts.expiringSoon > 0
    }
  ];

  $: chips = [
    { id: 'all', label: $t('approvals.filter.all'), count: counts.total },
    {
      id: 'destructive',
      label: $t('approvals.filter.destructive'),
      count: counts.destructive
    },
    { id: 'sensitive', label: $t('approvals.filter.sensitive'), count: counts.sensitive },
    { id: 'side', label: $t('approvals.filter.sideEffect'), count: counts.sideEffect },
    { id: 'read', label: $t('approvals.filter.read'), count: counts.read }
  ];

  $: dropdowns = [
    {
      id: 'class',
      label: $t('approvals.filter.class'),
      value: riskFilter,
      options: riskOptions
    }
  ];

  $: columns = [
    { key: 'tool', label: $t('approvals.col.tool') },
    { key: 'risk_class', label: $t('approvals.col.risk'), width: '160px' },
    ...(selected
      ? []
      : [
          { key: 'session_id', label: $t('approvals.col.session'), width: '180px' },
          { key: 'created_at', label: $t('approvals.col.created'), width: '170px' },
          { key: 'expires_at', label: $t('approvals.col.expires'), width: '120px' }
        ])
  ];
</script>

<PageHeader title={$t('approvals.title')} description={$t('approvals.description')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('approvals.title')} />
    <FilterChipBar
      searchValue={search_q}
      searchPlaceholder={$t('approvals.filter.search')}
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
      empty={$t('approvals.empty.title')}
      onRowClick={selectRow}
      selectedKey={selectedId}
      rowKeyField="id"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const a = row}
        {#if column.key === 'tool'}
          <IdentityCell primary={a.tool} secondary={a.user_id ?? a.tenant_id} mono size="sm" />
        {:else if column.key === 'risk_class'}
          <Badge tone={riskTone(a.risk_class)}>{a.risk_class}</Badge>
        {:else if column.key === 'session_id'}
          <IdBadge value={a.session_id} />
        {:else if column.key === 'created_at'}
          <span class="muted">{fmt(a.created_at)}</span>
        {:else if column.key === 'expires_at'}
          <span class="muted">{relativeFromNow(a.expires_at)}</span>
        {:else}
          {a[column.key] ?? '—'}
        {/if}
      </svelte:fragment>
      <svelte:fragment slot="empty">
        {#if approvals.length === 0}
          <EmptyState
            title={$t('approvals.empty.title')}
            description={$t('approvals.empty.description')}
            compact
          />
        {:else}
          <EmptyState
            title={$t('approvals.filter.empty.title')}
            description={$t('approvals.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('approvals.filter.empty.action')}
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
    emptyTitle={$t('approvals.inspector.empty.title')}
    emptyDescription={$t('approvals.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell primary={selected.tool} secondary={selected.id} mono size="lg" />
      {/if}
    </svelte:fragment>

    {#if selected}
      {#if inspectorTab === 'overview'}
        <section class="card">
          <h4>{$t('approvals.inspector.section.identity')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('approvals.col.risk'), value: selected.risk_class },
              { label: 'status', value: selected.status }
            ]}
            columns={2}
          />
        </section>
        <section class="card">
          <h4>{$t('approvals.inspector.section.context')}</h4>
          <KeyValueGrid
            items={[
              { label: 'tenant_id', value: selected.tenant_id || '—' },
              { label: 'user_id', value: selected.user_id || '—' },
              { label: $t('approvals.col.session'), value: selected.session_id || '—' }
            ]}
            columns={1}
          />
        </section>
        <section class="card">
          <h4>{$t('approvals.inspector.section.timing')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('approvals.col.created'), value: fmt(selected.created_at) },
              { label: $t('approvals.col.expires'), value: fmt(selected.expires_at) },
              { label: $t('approvals.relative.label'), value: relativeFromNow(selected.expires_at) }
            ]}
            columns={1}
          />
        </section>
      {:else if inspectorTab === 'args'}
        <section class="card">
          <h4>{$t('approvals.inspector.section.argsSummary')}</h4>
          {#if selected.args_summary}
            <pre class="raw"><code>{selected.args_summary}</code></pre>
          {:else}
            <p class="muted">{$t('approvals.inspector.section.noArgs')}</p>
          {/if}
        </section>
        {#if selected.metadata && Object.keys(selected.metadata).length > 0}
          <section class="card">
            <h4>{$t('approvals.inspector.section.metadata')}</h4>
            <pre class="raw"><code>{JSON.stringify(selected.metadata, null, 2)}</code></pre>
          </section>
        {/if}
      {/if}

      <section class="card decisions">
        <h4>{$t('approvals.inspector.section.decision')}</h4>
        <p class="muted">{$t('approvals.inspector.section.decisionHelp')}</p>
        <div class="decisions-row">
          <Button
            variant="primary"
            loading={busyId === selected.id}
            on:click={() => approve(selected.id)}
          >
            <IconCheck slot="leading" size={14} />
            {$t('approvals.action.approve')}
          </Button>
          <Button
            variant="destructive"
            loading={busyId === selected.id}
            on:click={() => deny(selected.id)}
          >
            <IconX slot="leading" size={14} />
            {$t('approvals.action.deny')}
          </Button>
        </div>
      </section>
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
  .raw {
    margin: 0;
    max-height: 280px;
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
  .decisions-row {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
</style>
