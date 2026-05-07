<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type SnapshotDiff } from '$lib/api';
  import { Badge, Breadcrumbs, EmptyState, PageHeader } from '$lib/components';

  let diff: SnapshotDiff | null = null;
  let error = '';

  $: a = $page.params.a ?? '';
  $: b = $page.params.b ?? '';

  async function load() {
    error = '';
    try {
      if (!a || !b) throw new Error('missing snapshot ids');
      diff = await api.diffSnapshots(a, b);
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(load);

  function noChanges(d: SnapshotDiff | null): boolean {
    if (!d) return true;
    const groups = [d.tools, d.resources, d.prompts, d.skills];
    return (
      groups.every((g) => (g.added?.length ?? 0) + (g.removed?.length ?? 0) === 0) &&
      (d.tools.modified?.length ?? 0) === 0
    );
  }
</script>

<PageHeader title="Snapshot diff" description={`Comparing ${a} → ${b}`}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: 'Snapshots', href: '/snapshots' }, { label: 'Diff' }]}
  />
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if diff && noChanges(diff)}
  <EmptyState
    title="No changes"
    description="Both snapshots resolve to the same canonical fingerprint."
  />
{:else if diff}
  <section class="block">
    <h2 class="section-title">Tools</h2>
    {#if (diff.tools.added?.length ?? 0) === 0 && (diff.tools.removed?.length ?? 0) === 0 && (diff.tools.modified?.length ?? 0) === 0}
      <p class="muted">No tool changes.</p>
    {:else}
      {#if diff.tools.added && diff.tools.added.length > 0}
        <h3 class="sub-title">Added</h3>
        <div class="chips">
          {#each diff.tools.added as n (n)}
            <Badge tone="success" mono>+ {n}</Badge>
          {/each}
        </div>
      {/if}
      {#if diff.tools.removed && diff.tools.removed.length > 0}
        <h3 class="sub-title">Removed</h3>
        <div class="chips">
          {#each diff.tools.removed as n (n)}
            <Badge tone="danger" mono>− {n}</Badge>
          {/each}
        </div>
      {/if}
      {#if diff.tools.modified && diff.tools.modified.length > 0}
        <h3 class="sub-title">Modified</h3>
        <ul class="modified">
          {#each diff.tools.modified as m (m.name)}
            <li>
              <Badge tone="warning" mono>{m.name}</Badge>
              <span class="muted">fields changed: {m.fields_changed.join(', ')}</span>
            </li>
          {/each}
        </ul>
      {/if}
    {/if}
  </section>

  <section class="block">
    <h2 class="section-title">Resources</h2>
    {#if (diff.resources.added?.length ?? 0) + (diff.resources.removed?.length ?? 0) === 0}
      <p class="muted">No resource changes.</p>
    {:else}
      {#if diff.resources.added && diff.resources.added.length > 0}
        <h3 class="sub-title">Added</h3>
        <div class="chips">
          {#each diff.resources.added as n (n)}
            <Badge tone="success" mono>+ {n}</Badge>
          {/each}
        </div>
      {/if}
      {#if diff.resources.removed && diff.resources.removed.length > 0}
        <h3 class="sub-title">Removed</h3>
        <div class="chips">
          {#each diff.resources.removed as n (n)}
            <Badge tone="danger" mono>− {n}</Badge>
          {/each}
        </div>
      {/if}
    {/if}
  </section>

  <section class="block">
    <h2 class="section-title">Prompts</h2>
    {#if (diff.prompts.added?.length ?? 0) + (diff.prompts.removed?.length ?? 0) === 0}
      <p class="muted">No prompt changes.</p>
    {:else}
      {#if diff.prompts.added && diff.prompts.added.length > 0}
        <h3 class="sub-title">Added</h3>
        <div class="chips">
          {#each diff.prompts.added as n (n)}
            <Badge tone="success" mono>+ {n}</Badge>
          {/each}
        </div>
      {/if}
      {#if diff.prompts.removed && diff.prompts.removed.length > 0}
        <h3 class="sub-title">Removed</h3>
        <div class="chips">
          {#each diff.prompts.removed as n (n)}
            <Badge tone="danger" mono>− {n}</Badge>
          {/each}
        </div>
      {/if}
    {/if}
  </section>

  <section class="block">
    <h2 class="section-title">Skills</h2>
    {#if (diff.skills.added?.length ?? 0) + (diff.skills.removed?.length ?? 0) === 0}
      <p class="muted">No skill changes.</p>
    {:else}
      {#if diff.skills.added && diff.skills.added.length > 0}
        <h3 class="sub-title">Added</h3>
        <div class="chips">
          {#each diff.skills.added as n (n)}
            <Badge tone="success" mono>+ {n}</Badge>
          {/each}
        </div>
      {/if}
      {#if diff.skills.removed && diff.skills.removed.length > 0}
        <h3 class="sub-title">Removed</h3>
        <div class="chips">
          {#each diff.skills.removed as n (n)}
            <Badge tone="danger" mono>− {n}</Badge>
          {/each}
        </div>
      {/if}
    {/if}
  </section>
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .block {
    margin-bottom: var(--space-6);
  }
  .section-title {
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    margin: 0 0 var(--space-3) 0;
  }
  .sub-title {
    margin: var(--space-3) 0 var(--space-2) 0;
    font-size: var(--font-size-label);
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
    padding: 0;
    margin: 0;
  }
  .modified li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) 0;
  }
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
</style>
