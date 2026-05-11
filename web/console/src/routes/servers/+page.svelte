<script lang="ts">
  /**
   * Servers — Phase 10.6 redesign.
   *
   * Composition: PageHeader + MetricStrip + FilterChipBar + 2-column
   * grid (table on the left, sticky Inspector on the right). The table
   * cells consume the substrate fields (capabilities, skills_count,
   * policy_state, auth_state, last_seen) added in Step 2.
   *
   * URL state:
   *   ?selected=<server-id>   inspector selection (preserved across reload)
   *   ?status=<chip>          chip filter (all|online|offline|review|skills|authError)
   *   ?transport=<stdio|http>
   *   ?runtime=<runtime-mode>
   *   ?q=<search>
   */
  import { onMount, tick } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type ServerSummary, type ServerPolicyState, type ServerAuthState } from '$lib/api';
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
    Table,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconServer from 'lucide-svelte/icons/server';
  import IconBox from 'lucide-svelte/icons/box';
  import IconShield from 'lucide-svelte/icons/shield';
  import IconShieldAlert from 'lucide-svelte/icons/shield-alert';
  import IconWorkflow from 'lucide-svelte/icons/workflow';
  import type { ComponentType } from 'svelte';

  // === Loading + data state ============================================

  let servers: ServerSummary[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      servers = (await api.listServers()) ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  // === Filter state — sourced from URL on first paint ==================

  let chip = '';
  let transport = '';
  let runtime = '';
  let search = '';
  let selectedId: string | null = null;
  let inspectorTab = 'overview';

  // Hydrate from URL on first mount + every navigation (back/forward).
  $: {
    const u = $page.url.searchParams;
    chip = u.get('status') || 'all';
    transport = u.get('transport') || '';
    runtime = u.get('runtime') || '';
    search = u.get('q') || '';
    selectedId = u.get('selected');
  }

  // Push filter / selection state to the URL without reloading. Using
  // replaceState avoids history-stack bloat on every chip click.
  function pushUrl(updates: Record<string, string | null>) {
    if (typeof window === 'undefined') return;
    const params = new URLSearchParams($page.url.searchParams);
    for (const [k, v] of Object.entries(updates)) {
      if (v === null || v === '' || v === 'all') {
        params.delete(k);
      } else {
        params.set(k, v);
      }
    }
    const qs = params.toString();
    goto(qs ? `?${qs}` : '?', { replaceState: true, keepFocus: true, noScroll: true });
  }

  function onChipChange(e: CustomEvent<string>) {
    chip = e.detail;
    pushUrl({ status: chip });
  }
  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'transport') transport = e.detail.value;
    else if (e.detail.id === 'runtime') runtime = e.detail.value;
    pushUrl({ [e.detail.id]: e.detail.value });
  }
  function onSearchChange(e: CustomEvent<string>) {
    search = e.detail;
    pushUrl({ q: search });
  }

  function selectRow(row: ServerSummary) {
    selectedId = row.id;
    pushUrl({ selected: row.id });
  }
  function closeInspector() {
    selectedId = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    transport = '';
    runtime = '';
    search = '';
    pushUrl({ status: null, transport: null, runtime: null, q: null });
  }

  // === Filtering =======================================================

  function statusBucket(s: ServerSummary): 'online' | 'offline' | 'review' | 'other' {
    const v = (s.status ?? '').toLowerCase();
    if (v === 'ready' || v === 'running' || v === 'healthy') return 'online';
    if (v === 'crashed' || v === 'error' || v === 'unhealthy' || v === 'circuit_open')
      return 'offline';
    if (v === 'starting' || v === 'backoff') return 'review';
    return 'other';
  }

  $: filtered = servers.filter((s) => {
    if (chip === 'online' && statusBucket(s) !== 'online') return false;
    if (chip === 'offline' && statusBucket(s) !== 'offline') return false;
    if (chip === 'review' && statusBucket(s) !== 'review') return false;
    if (chip === 'skills' && (s.skills_count ?? 0) === 0) return false;
    if (chip === 'authError' && s.auth_state !== 'none' && (s.status ?? '') !== 'unhealthy')
      // No real "auth error" status yet — proxy: missing auth on a non-OK status.
      // Treat the chip as "show servers with auth concerns" for now.
      return false;
    if (transport && s.transport !== transport) return false;
    if (runtime && s.runtime_mode !== runtime) return false;
    if (search) {
      const needle = search.toLowerCase();
      const hay = `${s.id} ${s.display_name ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  // === Aggregates for the KPI strip ====================================

  $: counts = (() => {
    let online = 0;
    let offline = 0;
    let review = 0;
    let withSkills = 0;
    let totalTools = 0;
    let totalResources = 0;
    let totalPrompts = 0;
    let approvalGated = 0;
    let drift = 0;
    for (const s of servers) {
      const b = statusBucket(s);
      if (b === 'online') online++;
      if (b === 'offline') offline++;
      if (b === 'review') review++;
      if ((s.skills_count ?? 0) > 0) withSkills++;
      totalTools += s.capabilities?.tools ?? 0;
      totalResources += s.capabilities?.resources ?? 0;
      totalPrompts += s.capabilities?.prompts ?? 0;
      if (s.policy_state === 'approval') approvalGated++;
      // No "needs review" status surface yet — proxy via 'review' bucket.
      if (b === 'review') drift++;
    }
    return {
      total: servers.length,
      online,
      offline,
      review,
      withSkills,
      totalTools,
      totalResources,
      totalPrompts,
      approvalGated,
      drift
    };
  })();

  // === UI helpers ======================================================

  function fmtRelative(iso?: string | null): string {
    if (!iso) return $t('landing.relTime.never');
    const ts = new Date(iso).getTime();
    if (isNaN(ts)) return $t('landing.relTime.never');
    const delta = Date.now() - ts;
    const m = Math.floor(delta / 60_000);
    if (m < 1) return $t('landing.relTime.justNow');
    if (m < 60) return $t('landing.relTime.minutes', { n: m });
    const h = Math.floor(m / 60);
    if (h < 24) return $t('landing.relTime.hours', { n: h });
    const d = Math.floor(h / 24);
    return $t('landing.relTime.days', { n: d });
  }

  type Tone = 'success' | 'danger' | 'warning' | 'neutral' | 'info';
  function statusTone(s: string): Tone {
    const v = s.toLowerCase();
    if (v === 'ready' || v === 'running' || v === 'healthy') return 'success';
    if (v === 'crashed' || v === 'error' || v === 'unhealthy') return 'danger';
    if (v === 'circuit_open' || v === 'backoff') return 'warning';
    if (v === 'starting') return 'info';
    return 'neutral';
  }
  function policyTone(p: ServerPolicyState | undefined): Tone {
    switch (p) {
      case 'enforced':
        return 'success';
      case 'approval':
        return 'warning';
      case 'disabled':
        return 'danger';
      default:
        return 'neutral';
    }
  }
  function policyLabel(p: ServerPolicyState | undefined): string {
    switch (p) {
      case 'enforced':
        return $t('servers.policy.enforced');
      case 'approval':
        return $t('servers.policy.approval');
      case 'disabled':
        return $t('servers.policy.disabled');
      default:
        return $t('servers.policy.none');
    }
  }
  function authTone(a: ServerAuthState | undefined): Tone {
    if (!a || a === 'none') return 'neutral';
    if (a === 'oauth') return 'info';
    return 'neutral';
  }
  function authLabel(a: ServerAuthState | undefined): string {
    switch (a) {
      case 'env':
        return $t('servers.auth.env');
      case 'header':
        return $t('servers.auth.header');
      case 'oauth':
        return $t('servers.auth.oauth');
      case 'vault_ref':
        return $t('servers.auth.vault_ref');
      case 'none':
      case undefined:
      case null:
        return $t('servers.auth.none');
      default:
        return a; // future strategy → render literal in a neutral badge
    }
  }

  // === Action wiring ===================================================

  async function restart(s: ServerSummary) {
    try {
      await api.restartServer(s.id, 'console.user_restart');
      toast.success($t('servers.action.restart'));
      await refresh();
    } catch (e) {
      toast.danger((e as Error).message);
    }
  }

  // === Selected row + inspector tabs ===================================

  $: selected = filtered.find((s) => s.id === selectedId) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('servers.inspector.tab.overview') },
    { id: 'capabilities', label: $t('servers.inspector.tab.tools') },
    { id: 'skills', label: $t('servers.inspector.tab.skills') },
    { id: 'more', label: $t('servers.inspector.tab.more') }
  ];

  // Reset to overview when selection changes (avoid lingering on a tab
  // whose contents would be empty for the new server).
  $: if (selected) {
    // no-op — keeping reactive but explicit
  }

  // Page-level actions
  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: refresh,
      loading
    },
    {
      label: $t('servers.action.add'),
      variant: 'primary' as const,
      icon: IconPlus,
      href: '/servers/new'
    }
  ];

  // KPI strip metrics
  $: metrics = [
    {
      id: 'servers',
      label: $t('servers.metric.servers'),
      value: counts.total.toString(),
      helper: $t('servers.metric.servers.helper', {
        online: counts.online,
        offline: counts.offline
      }),
      icon: IconServer as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'capabilities',
      label: $t('servers.metric.capabilities'),
      value: (counts.totalTools + counts.totalResources + counts.totalPrompts).toString(),
      helper: $t('servers.metric.capabilities.helper', {
        tools: counts.totalTools,
        resources: counts.totalResources,
        prompts: counts.totalPrompts
      }),
      icon: IconBox as ComponentType<any>
    },
    {
      id: 'policies',
      label: $t('servers.metric.policies'),
      value: counts.approvalGated.toString(),
      helper: $t('servers.metric.policies.helper', { approval: counts.approvalGated }),
      icon: IconShield as ComponentType<any>
    },
    {
      id: 'drift',
      label: $t('servers.metric.drift'),
      value: counts.drift > 0 ? counts.drift.toString() : '0',
      helper:
        counts.drift > 0 ? $t('servers.metric.drift.helper') : $t('servers.metric.drift.none'),
      icon: IconShieldAlert as ComponentType<any>,
      attention: counts.drift > 0
    }
  ];

  // Filter chips with live counts.
  $: chips = [
    { id: 'all', label: $t('servers.filter.all'), count: counts.total },
    { id: 'online', label: $t('servers.filter.online'), count: counts.online },
    { id: 'offline', label: $t('servers.filter.offline'), count: counts.offline },
    { id: 'review', label: $t('servers.filter.review'), count: counts.review },
    { id: 'skills', label: $t('servers.filter.skills'), count: counts.withSkills }
  ];
  $: dropdowns = [
    {
      id: 'transport',
      label: $t('servers.filter.transport'),
      value: transport,
      options: [
        { value: '', label: $t('servers.filter.any') },
        { value: 'stdio', label: 'stdio' },
        { value: 'http', label: 'http' }
      ]
    },
    {
      id: 'runtime',
      label: $t('servers.filter.runtime'),
      value: runtime,
      options: [
        { value: '', label: $t('servers.filter.any') },
        { value: 'shared_global', label: 'shared_global' },
        { value: 'per_session', label: 'per_session' },
        { value: 'per_user', label: 'per_user' },
        { value: 'per_tenant', label: 'per_tenant' }
      ]
    }
  ];

  // Table columns. Width hints keep the layout stable as cell content
  // changes. When the Inspector is open the available table width
  // shrinks by ~340px (304 inspector + 24 gutter + 12 inset), so we
  // drop two cells that are already visible in the inspector body
  // (`auth`, `lastSeen`) — keeps the row fully visible without the
  // user having to horizontally scroll.
  $: columns = [
    { key: 'name', label: $t('servers.col.server'), width: '220px' },
    { key: 'status', label: $t('servers.col.status'), width: '100px' },
    { key: 'transport', label: $t('servers.col.transport'), width: '90px' },
    { key: 'runtime', label: $t('servers.col.mode'), width: '120px' },
    { key: 'capabilities', label: $t('servers.col.capabilities'), width: '140px' },
    {
      key: 'skills',
      label: $t('servers.col.skills'),
      align: 'center' as const,
      width: '70px'
    },
    { key: 'policy', label: $t('servers.col.policy'), width: '110px' },
    ...(selected
      ? []
      : [
          { key: 'auth', label: $t('servers.col.auth'), width: '90px' },
          { key: 'lastSeen', label: $t('servers.col.lastSeen'), width: '110px' }
        ])
  ];

  // Spec for the inspector "Runtime" KeyValueGrid.
  $: runtimeFacts = selected
    ? [
        { label: $t('servers.field.transport'), value: selected.transport },
        { label: $t('servers.field.runtimeMode'), value: selected.runtime_mode },
        { label: $t('servers.col.status'), value: selected.status }
      ]
    : [];

  $: capFacts = selected
    ? [
        { label: 'tools', value: String(selected.capabilities?.tools ?? 0) },
        { label: 'resources', value: String(selected.capabilities?.resources ?? 0) },
        { label: 'prompts', value: String(selected.capabilities?.prompts ?? 0) },
        { label: 'apps', value: String(selected.capabilities?.apps ?? 0) }
      ]
    : [];
</script>

<PageHeader title={$t('servers.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<!-- Two-column shell: the right rail (Inspector) sits sticky at the top
     of the content area when a row is selected. The KPI strip and
     filter bar live INSIDE the left column so they shrink alongside
     the table when the rail opens — instead of always spanning the
     full canvas above it. -->
<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('servers.title')} />

    <FilterChipBar
      searchValue={search}
      searchPlaceholder={$t('servers.filter.search')}
      {chips}
      activeChip={chip}
      {dropdowns}
      on:chipChange={onChipChange}
      on:dropdownChange={onDropdownChange}
      on:searchChange={onSearchChange}
    />
    {#if !loading && servers.length === 0}
      <EmptyState title={$t('servers.empty.title')} description={$t('servers.empty.description')}>
        <svelte:fragment slot="actions">
          <Button href="/servers/new"
            ><IconPlus slot="leading" size={14} />{$t('servers.action.add')}</Button
          >
        </svelte:fragment>
      </EmptyState>
    {:else}
      <Table
        {columns}
        rows={filtered}
        empty={$t('servers.filter.empty.title')}
        onRowClick={selectRow}
        selectedKey={selectedId}
        rowKeyField="id"
      >
        <svelte:fragment slot="cell" let:row let:column>
          {@const r = row}
          {#if column.key === 'name'}
            <IdentityCell
              primary={r.display_name || r.id}
              secondary={r.display_name && r.display_name !== r.id ? r.id : undefined}
              size="md"
            />
          {:else if column.key === 'status'}
            <Badge tone={statusTone(r.status)}>{r.status}</Badge>
          {:else if column.key === 'transport'}
            <Badge tone="neutral" mono>{r.transport}</Badge>
          {:else if column.key === 'runtime'}
            <Badge tone="neutral" mono>{r.runtime_mode}</Badge>
          {:else if column.key === 'capabilities'}
            {#if r.capabilities && (r.capabilities.tools || r.capabilities.resources || r.capabilities.prompts || r.capabilities.apps)}
              <span class="caps">
                {#if r.capabilities.tools}<span class="cap" title="tools"
                    >{r.capabilities.tools}<span class="cap-unit">t</span></span
                  >{/if}
                {#if r.capabilities.resources}<span class="cap" title="resources"
                    >{r.capabilities.resources}<span class="cap-unit">r</span></span
                  >{/if}
                {#if r.capabilities.prompts}<span class="cap" title="prompts"
                    >{r.capabilities.prompts}<span class="cap-unit">p</span></span
                  >{/if}
                {#if r.capabilities.apps}<span class="cap" title="apps"
                    >{r.capabilities.apps}<span class="cap-unit">a</span></span
                  >{/if}
              </span>
            {:else}
              <span class="muted">—</span>
            {/if}
          {:else if column.key === 'skills'}
            {#if (r.skills_count ?? 0) > 0}
              <Badge tone="accent">{r.skills_count}</Badge>
            {:else}
              <span class="muted">—</span>
            {/if}
          {:else if column.key === 'policy'}
            <Badge tone={policyTone(r.policy_state)}>{policyLabel(r.policy_state)}</Badge>
          {:else if column.key === 'auth'}
            <Badge tone={authTone(r.auth_state)}>{authLabel(r.auth_state)}</Badge>
          {:else if column.key === 'lastSeen'}
            <span class="muted">{fmtRelative(r.last_seen ?? r.updated_at)}</span>
          {:else}
            {r[column.key] ?? '—'}
          {/if}
        </svelte:fragment>
        <svelte:fragment slot="empty">
          {#if servers.length === 0}
            <EmptyState
              title={$t('servers.empty.title')}
              description={$t('servers.empty.description')}
              compact
            />
          {:else}
            <EmptyState
              title={$t('servers.filter.empty.title')}
              description={$t('servers.filter.empty.description')}
              compact
            >
              <svelte:fragment slot="actions">
                <Button variant="secondary" on:click={clearFilters}>
                  {$t('servers.filter.empty.action')}
                </Button>
              </svelte:fragment>
            </EmptyState>
          {/if}
        </svelte:fragment>
      </Table>
    {/if}
  </div>

  <Inspector
    open={selected !== null}
    tabs={inspectorTabs}
    bind:activeTab={inspectorTab}
    emptyTitle={$t('servers.inspector.empty.title')}
    emptyDescription={$t('servers.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell
          primary={selected.display_name || selected.id}
          secondary={selected.display_name && selected.display_name !== selected.id
            ? selected.id
            : undefined}
          size="lg"
        />
      {/if}
    </svelte:fragment>

    <svelte:fragment slot="actions">
      {#if selected}
        <Button variant="secondary" size="sm" href={`/servers/${encodeURIComponent(selected.id)}`}>
          {$t('servers.inspector.action.viewDetails')}
        </Button>
        <Button variant="ghost" size="sm" on:click={() => restart(selected)}>
          {$t('servers.inspector.action.restart')}
        </Button>
      {/if}
    </svelte:fragment>

    {#if selected}
      {#if inspectorTab === 'overview'}
        <section class="card">
          <h4>{$t('servers.inspector.section.runtime')}</h4>
          <KeyValueGrid items={runtimeFacts} columns={2} />
        </section>
        <section class="card">
          <h4>{$t('servers.inspector.section.capabilities')}</h4>
          {#if selected.capabilities}
            <KeyValueGrid items={capFacts} columns={2} />
          {:else}
            <p class="empty-line">{$t('servers.inspector.section.snapshotMissing')}</p>
          {/if}
        </section>
        <section class="card row">
          <Badge tone={policyTone(selected.policy_state)}>
            {$t('servers.col.policy')}: {policyLabel(selected.policy_state)}
          </Badge>
          <Badge tone={authTone(selected.auth_state)}>
            {$t('servers.col.auth')}: {authLabel(selected.auth_state)}
          </Badge>
          {#if (selected.skills_count ?? 0) > 0}
            <Badge tone="accent">
              {$t('servers.col.skills')}: {selected.skills_count}
            </Badge>
          {/if}
        </section>
      {:else if inspectorTab === 'capabilities'}
        {#if selected.capabilities && (selected.capabilities.tools || selected.capabilities.resources || selected.capabilities.prompts)}
          <KeyValueGrid items={capFacts} columns={1} />
        {:else}
          <div class="empty-block">
            <h4>{$t('servers.inspector.section.snapshotMissing')}</h4>
            <p>{$t('servers.inspector.section.snapshotMissingDesc')}</p>
          </div>
        {/if}
      {:else if inspectorTab === 'skills'}
        {#if (selected.skills_count ?? 0) > 0}
          <p class="empty-line">
            {selected.skills_count}
            {$t('servers.col.skills')}.
          </p>
        {:else}
          <p class="empty-line">{$t('servers.inspector.section.snapshotMissing')}</p>
        {/if}
      {:else if inspectorTab === 'more'}
        <details class="raw">
          <summary>{$t('serverDetail.spec')}</summary>
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
    /* MetricStrip already carries margin-bottom: var(--space-7); the
     * FilterChipBar's bottom margin handles the rest. */
  }
  .muted {
    color: var(--color-text-tertiary);
  }
  .caps {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
  }
  .cap {
    display: inline-flex;
    align-items: baseline;
    gap: 1px;
  }
  .cap-unit {
    color: var(--color-text-tertiary);
    font-size: 10px;
    margin-left: 1px;
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
  .card.row {
    flex-direction: row;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .empty-line {
    margin: 0;
    color: var(--color-text-tertiary);
    font-size: var(--font-size-body-sm);
  }
  .empty-block {
    text-align: center;
    color: var(--color-text-tertiary);
    padding: var(--space-4);
  }
  .empty-block h4 {
    margin: 0 0 var(--space-2);
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-sm);
    font-weight: var(--font-weight-semibold);
  }
  .empty-block p {
    margin: 0;
    font-size: var(--font-size-label);
    line-height: 1.5;
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
