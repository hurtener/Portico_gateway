<script lang="ts">
  /**
   * Authored skills — Phase 10.8 redesign.
   *
   * Composes the design vocabulary: PageHeader (compact) + KPI strip
   * + filter chip bar + table inside the left main-col, sticky
   * Inspector right rail when a row is selected. Versions tab
   * lazy-loads via api.authoredSkillVersions so the list payload
   * stays small.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type AuthoredSkillSummary } from '$lib/api';
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
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconFileEdit from 'lucide-svelte/icons/file-edit';
  import IconCheckCircle2 from 'lucide-svelte/icons/check-circle-2';
  import IconArchive from 'lucide-svelte/icons/archive';
  import IconLayers from 'lucide-svelte/icons/layers';
  import IconExternalLink from 'lucide-svelte/icons/external-link';
  import type { ComponentType } from 'svelte';

  // === Loading ========================================================

  let items: AuthoredSkillSummary[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      const res = await api.listAuthoredSkills();
      items = res.items ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  // === Versions detail (lazy) =========================================

  let versions: AuthoredSkillSummary[] = [];
  let versionsLoading = false;
  let versionsError = '';

  async function loadVersions(skillId: string) {
    versionsLoading = true;
    versionsError = '';
    try {
      const res = await api.authoredSkillVersions(skillId);
      versions = res.items ?? [];
    } catch (e) {
      versionsError = (e as Error).message;
    } finally {
      versionsLoading = false;
    }
  }

  // === URL state ======================================================

  let chip = '';
  let search_q = '';
  let selectedId: string | null = null;
  let inspectorTab = 'overview';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('status') || 'all';
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
  function onSearchChange(e: CustomEvent<string>) {
    search_q = e.detail;
    pushUrl({ q: search_q });
  }
  function selectRow(row: AuthoredSkillSummary) {
    selectedId = row.skill_id;
    inspectorTab = 'overview';
    versions = [];
    pushUrl({ selected: row.skill_id });
  }
  function closeInspector() {
    selectedId = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    search_q = '';
    pushUrl({ status: null, q: null });
  }

  // === Substrate ======================================================

  type Tone = 'success' | 'warning' | 'neutral' | 'info' | 'danger' | 'accent';
  function statusTone(status: string): Tone {
    if (status === 'published') return 'success';
    if (status === 'draft') return 'warning';
    if (status === 'archived') return 'neutral';
    return 'neutral';
  }

  $: filtered = items.filter((s) => {
    if (chip === 'draft' && s.status !== 'draft') return false;
    if (chip === 'published' && s.status !== 'published') return false;
    if (chip === 'archived' && s.status !== 'archived') return false;
    if (search_q) {
      const needle = search_q.toLowerCase();
      const hay = `${s.skill_id} ${s.title ?? ''} ${s.description ?? ''}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let drafts = 0;
    let published = 0;
    let archived = 0;
    for (const s of items) {
      total++;
      if (s.status === 'draft') drafts++;
      else if (s.status === 'published') published++;
      else if (s.status === 'archived') archived++;
    }
    return { total, drafts, published, archived };
  })();

  $: selected = filtered.find((s) => s.skill_id === selectedId) ?? null;

  $: if (selected && inspectorTab === 'versions' && versions.length === 0 && !versionsLoading) {
    loadVersions(selected.skill_id);
  }

  $: inspectorTabs = [
    { id: 'overview', label: $t('authored.inspector.tab.overview') },
    { id: 'versions', label: $t('authored.inspector.tab.versions') }
  ];

  function fmt(time?: string): string {
    if (!time) return '—';
    try {
      return new Date(time).toLocaleString();
    } catch {
      return time;
    }
  }

  function openEditor(skillId: string) {
    goto(`/skills/authored/${encodeURIComponent(skillId)}`);
  }

  // === Composition ====================================================

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh(),
      loading
    },
    {
      label: $t('authored.action.new'),
      icon: IconPlus,
      variant: 'primary' as const,
      onClick: () => goto('/skills/authored/new')
    }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('authored.metric.total'),
      value: counts.total.toString(),
      helper: $t('authored.metric.total.helper'),
      icon: IconLayers as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'drafts',
      label: $t('authored.metric.drafts'),
      value: counts.drafts.toString(),
      helper: $t('authored.metric.drafts.helper'),
      icon: IconFileEdit as ComponentType<any>,
      attention: counts.drafts > 0
    },
    {
      id: 'published',
      label: $t('authored.metric.published'),
      value: counts.published.toString(),
      helper: $t('authored.metric.published.helper'),
      icon: IconCheckCircle2 as ComponentType<any>,
      tone: 'success' as const
    },
    {
      id: 'archived',
      label: $t('authored.metric.archived'),
      value: counts.archived.toString(),
      helper: $t('authored.metric.archived.helper'),
      icon: IconArchive as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('authored.filter.all'), count: counts.total },
    { id: 'draft', label: $t('authored.filter.draft'), count: counts.drafts },
    { id: 'published', label: $t('authored.filter.published'), count: counts.published },
    { id: 'archived', label: $t('authored.filter.archived'), count: counts.archived }
  ];

  $: columns = [
    { key: 'skill_id', label: $t('authored.col.skillId') },
    { key: 'version', label: $t('authored.col.version'), width: '110px' },
    { key: 'status', label: $t('authored.col.status'), width: '120px' },
    ...(selected
      ? []
      : [
          { key: 'checksum', label: $t('authored.col.checksum'), width: '160px' },
          { key: 'created_at', label: $t('authored.col.created'), width: '170px' }
        ])
  ];
</script>

<PageHeader title={$t('authored.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('authored.title')} />
    <FilterChipBar
      searchValue={search_q}
      searchPlaceholder={$t('authored.filter.search')}
      {chips}
      activeChip={chip}
      on:chipChange={onChipChange}
      on:searchChange={onSearchChange}
    />

    <Table
      {columns}
      rows={filtered}
      empty={$t('authored.empty.title')}
      onRowClick={selectRow}
      selectedKey={selectedId}
      rowKeyField="skill_id"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const s = row}
        {#if column.key === 'skill_id'}
          <IdentityCell
            primary={s.skill_id}
            secondary={s.title ?? s.description ?? ''}
            mono
            size="sm"
          />
        {:else if column.key === 'status'}
          <Badge tone={statusTone(s.status)}>{s.status}</Badge>
        {:else if column.key === 'checksum'}
          <span class="trunc">{s.checksum?.slice(0, 16) ?? ''}…</span>
        {:else if column.key === 'created_at'}
          <span class="muted">{fmt(s.created_at)}</span>
        {:else}
          {s[column.key] ?? '—'}
        {/if}
      </svelte:fragment>
      <svelte:fragment slot="empty">
        {#if items.length === 0}
          <EmptyState
            title={$t('authored.empty.title')}
            description={$t('authored.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="primary" on:click={() => goto('/skills/authored/new')}>
                <IconPlus slot="leading" size={14} />
                {$t('authored.action.new')}
              </Button>
            </svelte:fragment>
          </EmptyState>
        {:else}
          <EmptyState
            title={$t('authored.filter.empty.title')}
            description={$t('authored.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('authored.filter.empty.action')}
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
    emptyTitle={$t('authored.inspector.empty.title')}
    emptyDescription={$t('authored.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if selected}
        <IdentityCell primary={selected.skill_id} secondary={selected.version} mono size="lg" />
      {/if}
    </svelte:fragment>

    {#if selected}
      {#if inspectorTab === 'overview'}
        <section class="card">
          <h4>{$t('authored.inspector.section.identity')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('authored.col.status'), value: selected.status },
              { label: $t('authored.col.version'), value: selected.version }
            ]}
            columns={2}
          />
        </section>
        <section class="card">
          <h4>{$t('authored.inspector.section.context')}</h4>
          <KeyValueGrid
            items={[
              { label: $t('authored.col.checksum'), value: selected.checksum ?? '—' },
              { label: 'author', value: selected.author_user_id ?? '—' },
              { label: $t('authored.col.created'), value: fmt(selected.created_at) },
              { label: $t('authored.col.published'), value: fmt(selected.published_at) }
            ]}
            columns={1}
          />
        </section>
        {#if selected.description}
          <section class="card">
            <h4>{$t('authored.inspector.section.description')}</h4>
            <p class="muted body">{selected.description}</p>
          </section>
        {/if}
        <section class="card decisions">
          <div class="decisions-row">
            <Button variant="primary" on:click={() => openEditor(selected.skill_id)}>
              <IconExternalLink slot="leading" size={14} />
              {$t('authored.action.openEditor')}
            </Button>
          </div>
        </section>
      {:else if inspectorTab === 'versions'}
        <section class="card">
          <h4>{$t('authored.inspector.section.versions')}</h4>
          {#if versionsLoading}
            <p class="muted">{$t('common.loading')}</p>
          {:else if versionsError}
            <p class="error">{versionsError}</p>
          {:else if versions.length === 0}
            <p class="muted">{$t('authored.inspector.section.noVersions')}</p>
          {:else}
            <ul class="version-list">
              {#each versions as v (v.version)}
                <li>
                  <span class="vrow">
                    <code class="mono">v{v.version}</code>
                    <Badge tone={statusTone(v.status)}>{v.status}</Badge>
                    <span class="muted">{fmt(v.created_at)}</span>
                  </span>
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
  .body {
    line-height: 1.5;
    font-size: var(--font-size-body-sm);
  }
  .trunc {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-tertiary);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
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
  .version-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .vrow {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    background: var(--color-bg-canvas);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
  }
</style>
