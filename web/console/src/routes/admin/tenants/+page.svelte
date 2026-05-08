<script lang="ts">
  /**
   * Tenants — Phase 10.7b redesign.
   *
   * The operator's tenant directory. Composes the design vocabulary
   * (KPI strip + filter chip bar + sticky Inspector) on top of the
   * existing /api/admin/tenants list. Substrate is in-page derivation
   * — counts of active/archived, distinct plans, etc.
   *
   * `isFeatureUnavailable` keeps the page graceful when a build
   * boots without the admin surface wired (e.g. dev mode without a
   * tenants store).
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, isFeatureUnavailable, type Tenant } from '$lib/api';
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
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconUsers from 'lucide-svelte/icons/users';
  import IconUserCheck from 'lucide-svelte/icons/user-check';
  import IconArchive from 'lucide-svelte/icons/archive';
  import IconLayers from 'lucide-svelte/icons/layers';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import type { ComponentType } from 'svelte';

  // === Loading + state ================================================

  type State = 'loading' | 'ready' | 'unavailable';
  let tenants: Tenant[] = [];
  let state: State = 'loading';
  let error = '';

  async function refresh() {
    try {
      tenants = await api.listTenants();
      state = 'ready';
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        state = 'unavailable';
        return;
      }
      error = (e as Error).message;
      state = 'ready';
    }
  }
  onMount(refresh);

  // === URL state ======================================================

  let chip = '';
  let planFilter = '';
  let search = '';
  let selectedId: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('status') || 'all';
    planFilter = u.get('plan') || '';
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
    pushUrl({ status: chip });
  }
  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'plan') {
      planFilter = e.detail.value;
      pushUrl({ plan: e.detail.value });
    }
  }
  function onSearchChange(e: CustomEvent<string>) {
    search = e.detail;
    pushUrl({ q: search });
  }
  function selectRow(row: Tenant) {
    selectedId = row.id;
    pushUrl({ selected: row.id });
  }
  function closeInspector() {
    selectedId = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    planFilter = '';
    search = '';
    pushUrl({ status: null, plan: null, q: null });
  }

  // === Substrate ======================================================

  $: filtered = tenants.filter((t0) => {
    const status = t0.status === 'archived' ? 'archived' : 'active';
    if (chip === 'active' && status !== 'active') return false;
    if (chip === 'archived' && status !== 'archived') return false;
    if (planFilter && t0.plan !== planFilter) return false;
    if (search) {
      const needle = search.toLowerCase();
      const hay = `${t0.id} ${t0.display_name ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let active = 0;
    let archived = 0;
    const plans = new Set<string>();
    for (const t0 of tenants) {
      total++;
      if (t0.status === 'archived') archived++;
      else active++;
      if (t0.plan) plans.add(t0.plan);
    }
    return { total, active, archived, plans: plans.size };
  })();

  $: planOptions = (() => {
    const uniq = new Set<string>();
    for (const t0 of tenants) if (t0.plan) uniq.add(t0.plan);
    return [
      { value: '', label: $t('tenants.filter.any') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  $: selected = filtered.find((tt) => tt.id === selectedId) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('tenants.inspector.tab.overview') },
    { id: 'quotas', label: $t('tenants.inspector.tab.quotas') },
    { id: 'auth', label: $t('tenants.inspector.tab.auth') }
  ];

  // === Composition ====================================================

  $: pageActions = [
    { label: $t('common.refresh'), icon: IconRefreshCw, onClick: refresh },
    {
      label: $t('tenants.inspector.action.create'),
      variant: 'primary' as const,
      icon: IconPlus,
      href: '/admin/tenants/new'
    }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('tenants.metric.total'),
      value: counts.total.toString(),
      helper: $t('tenants.metric.total.helper'),
      icon: IconUsers as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'active',
      label: $t('tenants.metric.active'),
      value: counts.active.toString(),
      helper: $t('tenants.metric.active.helper'),
      icon: IconUserCheck as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'archived',
      label: $t('tenants.metric.archived'),
      value: counts.archived.toString(),
      helper: $t('tenants.metric.archived.helper'),
      icon: IconArchive as ComponentType<any>,
      attention: counts.archived > 0
    },
    {
      id: 'plans',
      label: $t('tenants.metric.plans'),
      value: counts.plans.toString(),
      helper: $t('tenants.metric.plans.helper'),
      icon: IconLayers as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('tenants.filter.all'), count: counts.total },
    { id: 'active', label: $t('tenants.filter.active'), count: counts.active },
    { id: 'archived', label: $t('tenants.filter.archived'), count: counts.archived }
  ];

  $: dropdowns = [
    {
      id: 'plan',
      label: $t('tenants.filter.plan'),
      value: planFilter,
      options: planOptions
    }
  ];

  $: columns = [
    { key: 'tenant', label: $t('tenants.col.tenant'), width: '260px' },
    { key: 'plan', label: $t('tenants.col.plan'), width: '110px' },
    { key: 'runtime_mode', label: $t('tenants.field.runtimeMode'), width: '140px' },
    { key: 'status', label: $t('tenants.field.status'), width: '110px' },
    ...(selected ? [] : [{ key: 'quotas', label: $t('tenants.col.quotas'), width: '180px' }])
  ];
</script>

<PageHeader title={$t('tenants.title')} description={$t('tenants.subtitle')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if state === 'unavailable'}
  <EmptyState title={$t('tenants.title')} description={$t('crud.permissionDenied')}>
    <span slot="illustration"><IconUsers size={56} aria-hidden="true" /></span>
  </EmptyState>
{:else}
  {#if error}<p class="error">{error}</p>{/if}
  <div class="layout" class:has-selection={selected !== null}>
    <div class="main-col">
      <MetricStrip {metrics} label={$t('tenants.title')} />
      <FilterChipBar
        searchValue={search}
        searchPlaceholder={$t('tenants.filter.search')}
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
        empty={$t('tenants.filter.empty.title')}
        onRowClick={selectRow}
        selectedKey={selectedId}
        rowKeyField="id"
      >
        <svelte:fragment slot="cell" let:row let:column>
          {@const r = row}
          {#if column.key === 'tenant'}
            <IdentityCell primary={r.id} secondary={r.display_name} mono size="md" />
          {:else if column.key === 'plan'}
            <Badge tone="accent">{r.plan ?? '—'}</Badge>
          {:else if column.key === 'runtime_mode'}
            {#if r.runtime_mode}
              <Badge tone="neutral" mono>{r.runtime_mode}</Badge>
            {:else}
              <span class="muted">—</span>
            {/if}
          {:else if column.key === 'status'}
            <Badge tone={r.status === 'archived' ? 'warning' : 'success'}>
              {r.status === 'archived'
                ? $t('tenants.status.archived')
                : $t('tenants.status.active')}
            </Badge>
          {:else if column.key === 'quotas'}
            <span class="quotas">
              {r.max_concurrent_sessions ?? '∞'} sess · {r.max_requests_per_minute ?? '∞'} rpm
            </span>
          {:else}
            {r[column.key] ?? '—'}
          {/if}
        </svelte:fragment>
        <svelte:fragment slot="empty">
          {#if tenants.length === 0}
            <EmptyState title={$t('common.empty')} description="" compact />
          {:else}
            <EmptyState
              title={$t('tenants.filter.empty.title')}
              description={$t('tenants.filter.empty.description')}
              compact
            >
              <svelte:fragment slot="actions">
                <Button variant="secondary" on:click={clearFilters}>
                  {$t('tenants.filter.empty.action')}
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
      emptyTitle={$t('tenants.inspector.empty.title')}
      emptyDescription={$t('tenants.inspector.empty.description')}
      on:close={closeInspector}
    >
      <svelte:fragment slot="header">
        {#if selected}
          <IdentityCell primary={selected.id} secondary={selected.display_name} mono size="lg" />
        {/if}
      </svelte:fragment>

      <svelte:fragment slot="actions">
        {#if selected}
          <Button
            variant="secondary"
            size="sm"
            href={`/admin/tenants/${encodeURIComponent(selected.id)}`}
          >
            {$t('tenants.inspector.action.viewDetails')}
          </Button>
        {/if}
      </svelte:fragment>

      {#if selected}
        {#if inspectorTab === 'overview'}
          <section class="card">
            <h4>{$t('tenants.inspector.section.identity')}</h4>
            <KeyValueGrid
              items={[
                { label: $t('tenants.field.plan'), value: selected.plan },
                {
                  label: $t('tenants.field.status'),
                  value:
                    selected.status === 'archived'
                      ? $t('tenants.status.archived')
                      : $t('tenants.status.active')
                }
              ]}
              columns={2}
            />
          </section>
          <section class="card">
            <h4>{$t('tenants.inspector.section.runtime')}</h4>
            <KeyValueGrid
              items={[
                {
                  label: $t('tenants.field.runtimeMode'),
                  value: selected.runtime_mode || '—'
                },
                {
                  label: $t('tenants.field.retention'),
                  value:
                    selected.audit_retention_days != null
                      ? String(selected.audit_retention_days)
                      : '—'
                }
              ]}
              columns={2}
            />
          </section>
        {:else if inspectorTab === 'quotas'}
          <section class="card">
            <h4>{$t('tenants.inspector.section.quotas')}</h4>
            <KeyValueGrid
              items={[
                {
                  label: $t('tenants.field.maxSessions'),
                  value:
                    selected.max_concurrent_sessions != null
                      ? String(selected.max_concurrent_sessions)
                      : '∞'
                },
                {
                  label: $t('tenants.field.maxRpm'),
                  value:
                    selected.max_requests_per_minute != null
                      ? String(selected.max_requests_per_minute)
                      : '∞'
                }
              ]}
              columns={1}
            />
          </section>
        {:else if inspectorTab === 'auth'}
          <section class="card">
            <h4>{$t('tenants.inspector.section.jwt')}</h4>
            <KeyValueGrid
              items={[
                { label: $t('tenants.field.jwtIssuer'), value: selected.jwt_issuer || '—' },
                { label: $t('tenants.field.jwtJwks'), value: selected.jwt_jwks_url || '—' }
              ]}
              columns={1}
            />
          </section>
        {/if}
      {/if}
    </Inspector>
  </div>
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-3) 0;
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
  .quotas {
    color: var(--color-text-secondary);
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
</style>
