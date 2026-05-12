<script lang="ts">
  /**
   * Skill sources — Phase 10.8 redesign.
   *
   * Composes the design vocabulary: PageHeader (compact) + KPI strip
   * + filter chip bar + table inside the left main-col, sticky
   * Inspector right rail when a row is selected. The legacy Modal-
   * based add form moves into a collapsible card behind the primary
   * "Add source" PageActionGroup action — same pattern as
   * /admin/secrets, so the operator vocabulary stays consistent
   * across the console.
   *
   * Per-row Refresh / Delete buttons retire from the table; both move
   * into the Inspector decisions card so the table row stays clean
   * and the operator stays on the selected source while triaging.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, isFeatureUnavailable, type SkillSource } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    FilterChipBar,
    IdentityCell,
    Input,
    Inspector,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    Select,
    Table,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconTrash2 from 'lucide-svelte/icons/trash-2';
  import IconLayers from 'lucide-svelte/icons/layers';
  import IconCheckCircle2 from 'lucide-svelte/icons/check-circle-2';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import IconGitBranch from 'lucide-svelte/icons/git-branch';
  import IconExternalLink from 'lucide-svelte/icons/external-link';
  import type { ComponentType } from 'svelte';

  type State = 'loading' | 'ready' | 'unavailable';

  // === Loading ========================================================

  let sources: SkillSource[] = [];
  let state: State = 'loading';
  let error = '';

  async function refresh() {
    try {
      const out = await api.listSkillSources();
      sources = out.items ?? [];
      state = 'ready';
      error = '';
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

  // === Add form ========================================================

  let showForm = false;
  let formName = '';
  let formDriver = 'git';
  let formURL = '';
  let formBranch = '';
  let formFeed = '';
  let formCredential = '';
  let formPriorityStr = '100';
  let saving = false;

  function resetForm() {
    formName = '';
    formDriver = 'git';
    formURL = '';
    formBranch = '';
    formFeed = '';
    formCredential = '';
    formPriorityStr = '100';
  }

  async function submitForm() {
    if (!formName || !formDriver) {
      error = $t('sources.form.required');
      return;
    }
    saving = true;
    error = '';
    let config: Record<string, unknown> = {};
    if (formDriver === 'git') {
      config = { url: formURL, branch: formBranch };
    } else if (formDriver === 'http') {
      config = { feed_url: formFeed };
    }
    try {
      await api.upsertSkillSource({
        name: formName,
        driver: formDriver,
        config,
        credential_ref: formCredential || undefined,
        priority: Number(formPriorityStr) || 100,
        enabled: true
      });
      toast.success($t('sources.toast.saved.title'));
      showForm = false;
      resetForm();
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      saving = false;
    }
  }

  // === Per-row actions =================================================

  let busyName: string | null = null;

  async function refreshSource(name: string) {
    busyName = name;
    try {
      await api.refreshSkillSource(name);
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busyName = null;
    }
  }

  async function deleteSource(name: string) {
    if (!confirm($t('sources.confirm.delete', { name }))) return;
    busyName = name;
    try {
      await api.deleteSkillSource(name);
      toast.info($t('sources.toast.deleted.title'));
      if (selectedId === name) {
        selectedId = null;
        pushUrl({ selected: null });
      }
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busyName = null;
    }
  }

  // === URL state =======================================================

  let chip = '';
  let driverFilter = '';
  let search_q = '';
  let selectedId: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('status') || 'all';
    driverFilter = u.get('driver') || '';
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
    pushUrl({ status: chip });
  }
  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'driver') {
      driverFilter = e.detail.value;
      pushUrl({ driver: e.detail.value });
    }
  }
  function onSearchChange(e: CustomEvent<string>) {
    search_q = e.detail;
    pushUrl({ q: search_q });
  }
  function selectRow(row: SkillSource) {
    selectedId = row.name;
    inspectorTab = 'overview';
    pushUrl({ selected: row.name });
  }
  function closeInspector() {
    selectedId = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    driverFilter = '';
    search_q = '';
    pushUrl({ status: null, driver: null, q: null });
  }

  // === Substrate =======================================================

  $: filtered = sources.filter((s) => {
    if (chip === 'enabled' && !s.enabled) return false;
    if (chip === 'disabled' && s.enabled) return false;
    if (chip === 'failing' && !s.last_error) return false;
    if (driverFilter && s.driver !== driverFilter) return false;
    if (search_q) {
      const needle = search_q.toLowerCase();
      const hay = `${s.name} ${s.driver} ${JSON.stringify(s.config ?? {})}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let enabled = 0;
    let failing = 0;
    const drivers = new Set<string>();
    for (const s of sources) {
      total++;
      if (s.enabled) enabled++;
      if (s.last_error) failing++;
      if (s.driver) drivers.add(s.driver);
    }
    return { total, enabled, failing, drivers: drivers.size };
  })();

  $: driverOptions = (() => {
    const uniq = new Set<string>();
    for (const s of sources) if (s.driver) uniq.add(s.driver);
    return [
      { value: '', label: $t('sources.filter.anyDriver') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  $: selected = filtered.find((s) => s.name === selectedId) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('sources.inspector.tab.overview') },
    { id: 'config', label: $t('sources.inspector.tab.config') }
  ];

  function fmt(time?: string): string {
    if (!time) return '—';
    try {
      return new Date(time).toLocaleString();
    } catch {
      return time;
    }
  }

  function gotoSourceDetail(name: string) {
    goto(`/skills/sources/${encodeURIComponent(name)}`);
  }

  // === Composition =====================================================

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh()
    },
    {
      label: $t('sources.action.add'),
      icon: IconPlus,
      variant: 'primary' as const,
      onClick: () => {
        showForm = !showForm;
      }
    }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('sources.metric.total'),
      value: counts.total.toString(),
      helper: $t('sources.metric.total.helper'),
      icon: IconLayers as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'enabled',
      label: $t('sources.metric.enabled'),
      value: counts.enabled.toString(),
      helper: $t('sources.metric.enabled.helper'),
      icon: IconCheckCircle2 as ComponentType<any>,
      tone: 'success' as const
    },
    {
      id: 'failing',
      label: $t('sources.metric.failing'),
      value: counts.failing.toString(),
      helper: $t('sources.metric.failing.helper'),
      icon: IconAlertTriangle as ComponentType<any>,
      tone: 'danger' as const,
      attention: counts.failing > 0
    },
    {
      id: 'drivers',
      label: $t('sources.metric.drivers'),
      value: counts.drivers.toString(),
      helper: $t('sources.metric.drivers.helper'),
      icon: IconGitBranch as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('sources.filter.all'), count: counts.total },
    { id: 'enabled', label: $t('sources.filter.enabled'), count: counts.enabled },
    { id: 'disabled', label: $t('sources.filter.disabled'), count: counts.total - counts.enabled },
    { id: 'failing', label: $t('sources.filter.failing'), count: counts.failing }
  ];

  $: dropdowns = [
    {
      id: 'driver',
      label: $t('sources.filter.driver'),
      value: driverFilter,
      options: driverOptions
    }
  ];

  $: columns = [
    { key: 'name', label: $t('sources.col.name') },
    { key: 'driver', label: $t('sources.col.driver'), width: '110px' },
    { key: 'status', label: $t('sources.col.statusCol'), width: '120px' },
    ...(selected
      ? []
      : [
          { key: 'priority', label: $t('sources.col.priority'), width: '90px' },
          { key: 'last', label: $t('sources.col.lastRefresh'), width: '180px' }
        ])
  ];
</script>

<PageHeader title={$t('sources.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if showForm}
  <section class="card form-card">
    <h4>{$t('sources.action.add')}</h4>
    <form on:submit|preventDefault={submitForm} class="form">
      <div class="row">
        <Input bind:value={formName} label={$t('sources.form.name')} required block />
        <Select
          bind:value={formDriver}
          label={$t('sources.form.driver')}
          options={[
            { value: 'git', label: 'git' },
            { value: 'http', label: 'http' }
          ]}
        />
      </div>
      {#if formDriver === 'git'}
        <Input bind:value={formURL} label={$t('sources.form.config.url')} required block />
        <Input bind:value={formBranch} label={$t('sources.form.config.branch')} block />
      {:else if formDriver === 'http'}
        <Input bind:value={formFeed} label={$t('sources.form.config.feedUrl')} required block />
      {/if}
      <div class="row">
        <Input bind:value={formCredential} label={$t('sources.form.credentialRef')} block />
        <Input
          bind:value={formPriorityStr}
          label={$t('sources.form.priority')}
          type="number"
          block
        />
      </div>
      <div class="form-actions">
        <Button type="submit" variant="primary" loading={saving}>
          {$t('common.save')}
        </Button>
        <Button
          type="button"
          variant="secondary"
          on:click={() => {
            showForm = false;
            resetForm();
          }}
        >
          {$t('common.cancel')}
        </Button>
      </div>
    </form>
  </section>
{/if}

{#if state === 'unavailable'}
  <EmptyState title={$t('sources.empty.title')} description={$t('sources.empty.description')} />
{:else}
  <div class="layout" class:has-selection={selected !== null}>
    <div class="main-col">
      <MetricStrip {metrics} label={$t('sources.title')} />
      <FilterChipBar
        searchValue={search_q}
        searchPlaceholder={$t('sources.filter.search')}
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
        empty={$t('sources.empty.title')}
        onRowClick={selectRow}
        selectedKey={selectedId}
        rowKeyField="name"
      >
        <svelte:fragment slot="cell" let:row let:column>
          {@const s = row}
          {#if column.key === 'name'}
            <IdentityCell primary={s.name} secondary={s.credential_ref ?? ''} mono size="sm" />
          {:else if column.key === 'driver'}
            <Badge tone="neutral">{s.driver}</Badge>
          {:else if column.key === 'status'}
            {#if s.last_error}
              <Badge tone="danger">{$t('sources.status.failing')}</Badge>
            {:else if !s.enabled}
              <Badge tone="neutral">{$t('sources.status.disabled')}</Badge>
            {:else}
              <Badge tone="success">{$t('sources.status.enabled')}</Badge>
            {/if}
          {:else if column.key === 'priority'}
            <span class="muted">{s.priority ?? '—'}</span>
          {:else if column.key === 'last'}
            {#if s.last_error}
              <span class="err-cell">{s.last_error}</span>
            {:else}
              <span class="muted">{fmt(s.last_refresh_at)}</span>
            {/if}
          {:else}
            {s[column.key] ?? '—'}
          {/if}
        </svelte:fragment>
        <svelte:fragment slot="empty">
          {#if sources.length === 0}
            <EmptyState
              title={$t('sources.empty.title')}
              description={$t('sources.empty.description')}
              compact
            >
              <svelte:fragment slot="actions">
                <Button variant="primary" on:click={() => (showForm = true)}>
                  <IconPlus slot="leading" size={14} />
                  {$t('sources.action.add')}
                </Button>
              </svelte:fragment>
            </EmptyState>
          {:else}
            <EmptyState
              title={$t('sources.filter.empty.title')}
              description={$t('sources.filter.empty.description')}
              compact
            >
              <svelte:fragment slot="actions">
                <Button variant="secondary" on:click={clearFilters}>
                  {$t('sources.filter.empty.action')}
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
      emptyTitle={$t('sources.inspector.empty.title')}
      emptyDescription={$t('sources.inspector.empty.description')}
      on:close={closeInspector}
    >
      <svelte:fragment slot="header">
        {#if selected}
          <IdentityCell primary={selected.name} secondary={selected.driver} mono size="lg" />
        {/if}
      </svelte:fragment>

      {#if selected}
        {#if inspectorTab === 'overview'}
          <section class="card">
            <h4>{$t('sources.inspector.section.identity')}</h4>
            <KeyValueGrid
              items={[
                { label: $t('sources.col.driver'), value: selected.driver },
                { label: $t('sources.col.priority'), value: String(selected.priority ?? '—') }
              ]}
              columns={2}
            />
          </section>
          <section class="card">
            <h4>{$t('sources.inspector.section.timing')}</h4>
            <KeyValueGrid
              items={[
                {
                  label: $t('sources.col.lastRefresh'),
                  value: fmt(selected.last_refresh_at)
                },
                {
                  label: $t('sources.col.createdAt'),
                  value: fmt(selected.created_at)
                }
              ]}
              columns={1}
            />
          </section>
          {#if selected.last_error}
            <section class="card">
              <h4>{$t('sources.inspector.section.lastError')}</h4>
              <p class="err-block">{selected.last_error}</p>
            </section>
          {/if}
          <section class="card decisions">
            <div class="decisions-row">
              <Button
                variant="secondary"
                loading={busyName === selected.name}
                on:click={() => refreshSource(selected.name)}
              >
                <IconRefreshCw slot="leading" size={14} />
                {$t('sources.action.refresh')}
              </Button>
              <Button variant="secondary" on:click={() => gotoSourceDetail(selected.name)}>
                <IconExternalLink slot="leading" size={14} />
                {$t('sources.action.openDetail')}
              </Button>
              <Button
                variant="destructive"
                loading={busyName === selected.name}
                on:click={() => deleteSource(selected.name)}
              >
                <IconTrash2 slot="leading" size={14} />
                {$t('common.delete')}
              </Button>
            </div>
          </section>
        {:else if inspectorTab === 'config'}
          <section class="card">
            <h4>{$t('sources.inspector.section.config')}</h4>
            <pre class="raw"><code>{JSON.stringify(selected.config ?? {}, null, 2)}</code></pre>
          </section>
          {#if selected.credential_ref}
            <section class="card">
              <h4>{$t('sources.inspector.section.credential')}</h4>
              <code class="mono">{selected.credential_ref}</code>
            </section>
          {/if}
        {/if}
      {/if}
    </Inspector>
  </div>
{/if}

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
    grid-template-columns: minmax(0, 1fr) 320px;
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
  .err-cell {
    color: var(--color-danger);
    font-size: var(--font-size-label);
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
  .form-card {
    margin-bottom: var(--space-5);
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
  }
  .form-actions {
    display: flex;
    gap: var(--space-2);
    justify-content: flex-end;
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
