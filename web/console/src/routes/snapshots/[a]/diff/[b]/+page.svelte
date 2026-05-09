<script lang="ts">
  /**
   * Snapshot diff — Phase 10.8 Step 6 redesign.
   *
   * The pre-10.8 page lacked an orientation strip (4 stacked sections
   * with no overall counts), had no filter chips, and gave no way to
   * swap A/B without manual URL editing. This rewrite composes the
   * design vocabulary onto the compare flow:
   *   - Mini-KPI strip with cross-category Added / Removed (attention)
   *     / Modified / Unchanged categories.
   *   - Filter chips: All / Tools / Resources / Prompts / Skills /
   *     Only changes (default).
   *   - "Only changes" hides categories with no diff so the operator
   *     scrolls less.
   *   - Swap A/B action flips the route in place.
   *   - Per-category .card with the <h4> SECTION-LABEL header, body
   *     splits added / removed / modified using the existing Badge
   *     tones.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type SnapshotDiff } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    EmptyState,
    FilterChipBar,
    IdBadge,
    MetricStrip,
    PageActionGroup,
    PageHeader
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconArrowLeftRight from 'lucide-svelte/icons/arrow-left-right';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconMinus from 'lucide-svelte/icons/minus';
  import IconEdit from 'lucide-svelte/icons/edit';
  import IconCheck from 'lucide-svelte/icons/check';
  import type { ComponentType } from 'svelte';

  let diff: SnapshotDiff | null = null;
  let loading = true;
  let error = '';

  $: a = $page.params.a ?? '';
  $: b = $page.params.b ?? '';

  async function load() {
    loading = true;
    error = '';
    try {
      if (!a || !b) throw new Error('missing snapshot ids');
      diff = await api.diffSnapshots(a, b);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(load);
  // Re-load on route param change (swap action navigates to /b/diff/a).
  $: if (a && b) {
    void load();
  }

  function swapAB() {
    void goto(`/snapshots/${encodeURIComponent(b)}/diff/${encodeURIComponent(a)}`);
  }

  // === URL state ======================================================

  let chip = '';
  $: {
    chip = $page.url.searchParams.get('cat') || 'changes';
  }
  function pushUrl(updates: Record<string, string | null>) {
    if (typeof window === 'undefined') return;
    const params = new URLSearchParams($page.url.searchParams);
    for (const [k, v] of Object.entries(updates)) {
      if (v === null || v === '' || v === 'changes') params.delete(k);
      else params.set(k, v);
    }
    const qs = params.toString();
    void goto(qs ? `?${qs}` : '?', { replaceState: true, keepFocus: true, noScroll: true });
  }
  function onChipChange(e: CustomEvent<string>) {
    chip = e.detail;
    pushUrl({ cat: chip });
  }

  // === Substrate ======================================================

  type CategoryKey = 'tools' | 'resources' | 'prompts' | 'skills';
  const CATEGORIES: CategoryKey[] = ['tools', 'resources', 'prompts', 'skills'];

  function categoryHasChanges(cat: CategoryKey): boolean {
    if (!diff) return false;
    const g = diff[cat];
    const added = g.added?.length ?? 0;
    const removed = g.removed?.length ?? 0;
    const modified = cat === 'tools' ? (diff.tools.modified?.length ?? 0) : 0;
    return added + removed + modified > 0;
  }

  function isVisible(cat: CategoryKey): boolean {
    if (chip === 'all') return true;
    if (chip === 'changes') return categoryHasChanges(cat);
    return chip === cat;
  }

  $: counts = (() => {
    if (!diff) return { added: 0, removed: 0, modified: 0, total: 0 };
    let added = 0;
    let removed = 0;
    for (const cat of CATEGORIES) {
      const g = diff[cat];
      added += g.added?.length ?? 0;
      removed += g.removed?.length ?? 0;
    }
    const modified = diff.tools.modified?.length ?? 0;
    return { added, removed, modified, total: added + removed + modified };
  })();

  $: categoryCounts = (() => {
    if (!diff) return { tools: 0, resources: 0, prompts: 0, skills: 0 };
    return {
      tools:
        (diff.tools.added?.length ?? 0) +
        (diff.tools.removed?.length ?? 0) +
        (diff.tools.modified?.length ?? 0),
      resources: (diff.resources.added?.length ?? 0) + (diff.resources.removed?.length ?? 0),
      prompts: (diff.prompts.added?.length ?? 0) + (diff.prompts.removed?.length ?? 0),
      skills: (diff.skills.added?.length ?? 0) + (diff.skills.removed?.length ?? 0)
    };
  })();

  $: chips = [
    { id: 'changes', label: $t('snapshotDiff.filter.changes'), count: counts.total },
    { id: 'all', label: $t('snapshotDiff.filter.all') },
    { id: 'tools', label: $t('snapshotDiff.cat.tools'), count: categoryCounts.tools },
    { id: 'resources', label: $t('snapshotDiff.cat.resources'), count: categoryCounts.resources },
    { id: 'prompts', label: $t('snapshotDiff.cat.prompts'), count: categoryCounts.prompts },
    { id: 'skills', label: $t('snapshotDiff.cat.skills'), count: categoryCounts.skills }
  ];

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => load(),
      loading
    },
    {
      label: $t('snapshotDiff.action.swap'),
      icon: IconArrowLeftRight,
      onClick: swapAB
    }
  ];

  $: metrics = diff
    ? [
        {
          id: 'added',
          label: $t('snapshotDiff.metric.added'),
          value: counts.added.toString(),
          icon: IconPlus as ComponentType<any>,
          tone: 'success' as const
        },
        {
          id: 'removed',
          label: $t('snapshotDiff.metric.removed'),
          value: counts.removed.toString(),
          icon: IconMinus as ComponentType<any>,
          tone: 'danger' as const,
          attention: counts.removed > 0
        },
        {
          id: 'modified',
          label: $t('snapshotDiff.metric.modified'),
          value: counts.modified.toString(),
          icon: IconEdit as ComponentType<any>,
          tone: counts.modified > 0 ? ('warning' as const) : ('default' as const)
        },
        {
          id: 'identical',
          label: $t('snapshotDiff.metric.identical'),
          value: counts.total === 0 ? $t('common.yes') : $t('common.no'),
          icon: IconCheck as ComponentType<any>,
          tone: counts.total === 0 ? ('success' as const) : ('default' as const)
        }
      ]
    : [];

  function categoryLabel(cat: CategoryKey): string {
    return $t(`snapshotDiff.section.${cat}`);
  }
</script>

<PageHeader title={$t('snapshotDiff.title')}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: $t('nav.snapshots'), href: '/snapshots' }, { label: $t('snapshotDiff.crumb') }]}
  />
  <div slot="meta">
    <IdBadge value={a} chars={8} label={$t('snapshotDiff.label.from')} />
    <span class="arrow" aria-hidden="true">→</span>
    <IdBadge value={b} chars={8} label={$t('snapshotDiff.label.to')} />
  </div>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if diff}
  <MetricStrip {metrics} compact label={$t('snapshotDiff.metric.aria')} />

  <FilterChipBar
    showSearch={false}
    {chips}
    activeChip={chip}
    on:chipChange={onChipChange}
  />

  {#if counts.total === 0}
    <section class="card">
      <h4>{$t('snapshotDiff.identical.title')}</h4>
      <p class="muted">{$t('snapshotDiff.identical.description')}</p>
    </section>
  {:else}
    {#each CATEGORIES as cat (cat)}
      {#if isVisible(cat)}
        <section class="card">
          <h4>{categoryLabel(cat)}</h4>
          {#if !categoryHasChanges(cat)}
            <p class="muted">{$t('snapshotDiff.section.unchanged')}</p>
          {:else}
            {@const g = diff[cat]}
            {#if g.added && g.added.length > 0}
              <div class="row">
                <span class="row-label">{$t('snapshotDiff.label.added')}</span>
                <div class="chips">
                  {#each g.added as n (n)}
                    <Badge tone="success" mono>+ {n}</Badge>
                  {/each}
                </div>
              </div>
            {/if}
            {#if g.removed && g.removed.length > 0}
              <div class="row">
                <span class="row-label">{$t('snapshotDiff.label.removed')}</span>
                <div class="chips">
                  {#each g.removed as n (n)}
                    <Badge tone="danger" mono>− {n}</Badge>
                  {/each}
                </div>
              </div>
            {/if}
            {#if cat === 'tools' && diff.tools.modified && diff.tools.modified.length > 0}
              <div class="row">
                <span class="row-label">{$t('snapshotDiff.label.modified')}</span>
                <ul class="modified">
                  {#each diff.tools.modified as m (m.name)}
                    <li>
                      <Badge tone="warning" mono>{m.name}</Badge>
                      <span class="muted">
                        {$t('snapshotDiff.label.fieldsChanged')}:
                        {m.fields_changed.join(', ')}
                      </span>
                    </li>
                  {/each}
                </ul>
              </div>
            {/if}
          {/if}
        </section>
      {/if}
    {/each}
  {/if}
{:else if !loading}
  <EmptyState
    title={$t('snapshotDiff.error.title')}
    description={error || $t('snapshotDiff.error.description')}
  />
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .arrow {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-body-sm);
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    margin-top: var(--space-4);
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
  .row {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .row-label {
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .modified {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .modified li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--font-size-body-sm);
  }
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
</style>
