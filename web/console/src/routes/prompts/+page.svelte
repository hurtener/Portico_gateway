<script lang="ts">
  /**
   * Prompts — Phase 10.7a redesign.
   *
   * Same composition as `/resources`: PageHeader (compact) + KPI strip
   * + filter chip bar + table inside the left main-col, sticky
   * Inspector right rail when a row is selected.
   *
   * Substrate is derived from the prompt list itself: the namespaced
   * name (`<server>.<prompt>`) gives the source server, and the
   * argument list buckets each prompt into "required args" /
   * "optional only" / "no args".
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type Prompt } from '$lib/api';
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
  import IconFileText from 'lucide-svelte/icons/file-text';
  import IconAsterisk from 'lucide-svelte/icons/asterisk';
  import IconCheckSquare from 'lucide-svelte/icons/check-square';
  import IconMinus from 'lucide-svelte/icons/minus';
  import type { ComponentType } from 'svelte';

  // === Loading ========================================================

  let prompts: Prompt[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      const r = await api.listPrompts();
      prompts = r.prompts ?? [];
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
  let selectedName: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('args') || 'all';
    serverFilter = u.get('server') || '';
    search = u.get('q') || '';
    selectedName = u.get('selected');
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
    pushUrl({ args: chip });
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
  function selectRow(row: PromptRow) {
    selectedName = row.name;
    pushUrl({ selected: row.name });
  }
  function closeInspector() {
    selectedName = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    serverFilter = '';
    search = '';
    pushUrl({ args: null, server: null, q: null });
  }

  // === Substrate ======================================================

  type ArgBucket = 'required' | 'optional' | 'noargs';

  function serverIDOf(name: string): string {
    const i = name.indexOf('.');
    return i > 0 ? name.slice(0, i) : '';
  }
  function shortName(name: string): string {
    const i = name.indexOf('.');
    return i > 0 ? name.slice(i + 1) : name;
  }
  function bucketOf(p: Prompt): ArgBucket {
    const args = p.arguments ?? [];
    if (args.length === 0) return 'noargs';
    if (args.some((a) => a.required)) return 'required';
    return 'optional';
  }

  type PromptRow = Prompt & {
    server: string;
    bucket: ArgBucket;
    requiredCount: number;
    optionalCount: number;
  };

  $: rows = prompts.map<PromptRow>((p) => {
    const args = p.arguments ?? [];
    const requiredCount = args.filter((a) => a.required).length;
    return {
      ...p,
      server: serverIDOf(p.name),
      bucket: bucketOf(p),
      requiredCount,
      optionalCount: args.length - requiredCount
    };
  });

  $: filtered = rows.filter((r) => {
    if (chip === 'required' && r.bucket !== 'required') return false;
    if (chip === 'optional' && r.bucket !== 'optional') return false;
    if (chip === 'noargs' && r.bucket !== 'noargs') return false;
    if (serverFilter && r.server !== serverFilter) return false;
    if (search) {
      const needle = search.toLowerCase();
      const hay = `${r.name} ${r.description ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let required = 0;
    let optional = 0;
    let noargs = 0;
    const servers = new Set<string>();
    for (const r of rows) {
      total++;
      switch (r.bucket) {
        case 'required':
          required++;
          break;
        case 'optional':
          optional++;
          break;
        case 'noargs':
          noargs++;
          break;
      }
      if (r.server) servers.add(r.server);
    }
    return { total, required, optional, noargs, servers: servers.size };
  })();

  $: serverOptions = (() => {
    const uniq = new Set<string>();
    for (const r of rows) if (r.server) uniq.add(r.server);
    return [
      { value: '', label: $t('prompts.filter.any') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  $: selected = filtered.find((r) => r.name === selectedName) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('prompts.inspector.tab.overview') },
    {
      id: 'arguments',
      label: $t('prompts.inspector.tab.arguments'),
      disabled: !selected || (selected.arguments ?? []).length === 0
    }
  ];

  // === Composition ====================================================

  $: pageActions = [
    { label: $t('common.refresh'), icon: IconRefreshCw, onClick: refresh, loading }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('prompts.metric.total'),
      value: counts.total.toString(),
      helper: $t('prompts.metric.total.helper', { n: counts.servers }),
      icon: IconFileText as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'required',
      label: $t('prompts.metric.required'),
      value: counts.required.toString(),
      helper: $t('prompts.metric.required.helper'),
      icon: IconAsterisk as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'optional',
      label: $t('prompts.metric.optional'),
      value: counts.optional.toString(),
      helper: $t('prompts.metric.optional.helper'),
      icon: IconCheckSquare as ComponentType<any>
    },
    {
      id: 'noargs',
      label: $t('prompts.metric.noArgs'),
      value: counts.noargs.toString(),
      helper: $t('prompts.metric.noArgs.helper'),
      icon: IconMinus as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('prompts.filter.all'), count: counts.total },
    { id: 'required', label: $t('prompts.filter.required'), count: counts.required },
    { id: 'optional', label: $t('prompts.filter.optional'), count: counts.optional },
    { id: 'noargs', label: $t('prompts.filter.noArgs'), count: counts.noargs }
  ];

  $: dropdowns = [
    {
      id: 'server',
      label: $t('prompts.filter.server'),
      value: serverFilter,
      options: serverOptions
    }
  ];

  $: columns = [
    { key: 'prompt', label: $t('prompts.col.prompt'), width: '260px' },
    { key: 'description', label: $t('prompts.col.description') },
    { key: 'args', label: $t('prompts.col.args'), width: '170px' },
    ...(selected
      ? []
      : [{ key: 'server', label: $t('prompts.col.server'), width: '140px' }])
  ];
</script>

<PageHeader title={$t('prompts.title')} description={$t('prompts.description')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('prompts.title')} />
    <FilterChipBar
      searchValue={search}
      searchPlaceholder={$t('prompts.filter.search')}
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
      empty={$t('prompts.filter.empty.title')}
      onRowClick={selectRow}
      selectedKey={selectedName}
      rowKeyField="name"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const r = row}
        {#if column.key === 'prompt'}
          <IdentityCell primary={r.name} secondary={shortName(r.name)} mono size="md" />
        {:else if column.key === 'description'}
          {#if r.description}
            <span class="desc">{r.description}</span>
          {:else}
            <span class="muted">—</span>
          {/if}
        {:else if column.key === 'args'}
          {#if (r.arguments ?? []).length === 0}
            <span class="muted">—</span>
          {:else}
            <span class="args">
              {#if r.requiredCount > 0}
                <Badge tone="warning">{r.requiredCount} required</Badge>
              {/if}
              {#if r.optionalCount > 0}
                <Badge tone="neutral">{r.optionalCount} opt</Badge>
              {/if}
            </span>
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
            title={$t('prompts.empty.title')}
            description={$t('prompts.empty.description')}
            compact
          />
        {:else}
          <EmptyState
            title={$t('prompts.filter.empty.title')}
            description={$t('prompts.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('prompts.filter.empty.action')}
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
    emptyTitle={$t('prompts.inspector.empty.title')}
    emptyDescription={$t('prompts.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell primary={selected.name} secondary={shortName(selected.name)} mono size="lg" />
      {/if}
    </svelte:fragment>

    <svelte:fragment slot="actions">
      {#if selected && selected.server}
        <Button
          variant="secondary"
          size="sm"
          href={`/servers/${encodeURIComponent(selected.server)}`}
        >
          {$t('prompts.inspector.action.openServer')}
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
          <h4>{$t('prompts.inspector.section.server')}</h4>
          {#if selected.server}
            <a class="server-link" href={`/servers/${encodeURIComponent(selected.server)}`}>
              <Badge tone="neutral" mono>{selected.server}</Badge>
            </a>
          {:else}
            <span class="muted">—</span>
          {/if}
        </section>
        <section class="card">
          <h4>{$t('prompts.inspector.section.args')}</h4>
          {#if (selected.arguments ?? []).length === 0}
            <span class="muted">—</span>
          {:else}
            <KeyValueGrid
              items={[
                {
                  label: $t('prompts.inspector.args.required'),
                  value: String(selected.requiredCount)
                },
                {
                  label: $t('prompts.inspector.args.optional'),
                  value: String(selected.optionalCount)
                }
              ]}
              columns={2}
            />
          {/if}
        </section>
      {:else if inspectorTab === 'arguments'}
        <section class="card">
          <h4>{$t('prompts.inspector.section.args')}</h4>
          <ul class="arg-list">
            {#each selected.arguments ?? [] as arg (arg.name)}
              <li>
                <div class="arg-head">
                  <code class="arg-name">{arg.name}</code>
                  <Badge tone={arg.required ? 'warning' : 'neutral'}>
                    {arg.required
                      ? $t('prompts.inspector.args.required')
                      : $t('prompts.inspector.args.optional')}
                  </Badge>
                </div>
                {#if arg.description}<p class="arg-desc">{arg.description}</p>{/if}
              </li>
            {/each}
          </ul>
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
  .desc {
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
  }
  .args {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--space-1);
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
  .arg-list {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .arg-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .arg-name {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-xs);
    padding: 1px var(--space-2);
  }
  .arg-desc {
    margin: var(--space-1) 0 0 0;
    color: var(--color-text-secondary);
    font-size: var(--font-size-label);
    line-height: 1.5;
  }
</style>
