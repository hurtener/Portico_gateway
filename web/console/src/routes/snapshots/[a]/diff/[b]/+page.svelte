<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type SnapshotDiff } from '$lib/api';

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
</script>

<header class="page-head">
  <a class="back" href="/snapshots">← Snapshots</a>
  <h1>Diff</h1>
  <p class="muted"><code>{a}</code> → <code>{b}</code></p>
</header>

{#if error}
  <p class="error">{error}</p>
{/if}

{#if loading && !diff}
  <p class="muted">Loading…</p>
{:else if diff}
  <section>
    <h2>Tools</h2>
    {#if (diff.tools.added?.length ?? 0) === 0 && (diff.tools.removed?.length ?? 0) === 0 && (diff.tools.modified?.length ?? 0) === 0}
      <p class="muted">No tool changes.</p>
    {:else}
      {#if diff.tools.added && diff.tools.added.length > 0}
        <h3>Added</h3>
        <ul>
          {#each diff.tools.added as n}<li><code>{n}</code></li>{/each}
        </ul>
      {/if}
      {#if diff.tools.removed && diff.tools.removed.length > 0}
        <h3>Removed</h3>
        <ul>
          {#each diff.tools.removed as n}<li><code>{n}</code></li>{/each}
        </ul>
      {/if}
      {#if diff.tools.modified && diff.tools.modified.length > 0}
        <h3>Modified</h3>
        <ul>
          {#each diff.tools.modified as m}
            <li>
              <code>{m.name}</code>
              <span class="muted"> — fields changed: {m.fields_changed.join(', ')}</span>
            </li>
          {/each}
        </ul>
      {/if}
    {/if}
  </section>

  <section>
    <h2>Resources</h2>
    {#if (diff.resources.added?.length ?? 0) + (diff.resources.removed?.length ?? 0) === 0}
      <p class="muted">No resource changes.</p>
    {:else}
      {#if diff.resources.added && diff.resources.added.length > 0}
        <h3>Added</h3>
        <ul>
          {#each diff.resources.added as n}<li><code>{n}</code></li>{/each}
        </ul>
      {/if}
      {#if diff.resources.removed && diff.resources.removed.length > 0}
        <h3>Removed</h3>
        <ul>
          {#each diff.resources.removed as n}<li><code>{n}</code></li>{/each}
        </ul>
      {/if}
    {/if}
  </section>

  <section>
    <h2>Prompts</h2>
    {#if (diff.prompts.added?.length ?? 0) + (diff.prompts.removed?.length ?? 0) === 0}
      <p class="muted">No prompt changes.</p>
    {:else}
      {#if diff.prompts.added && diff.prompts.added.length > 0}
        <h3>Added</h3>
        <ul>
          {#each diff.prompts.added as n}<li><code>{n}</code></li>{/each}
        </ul>
      {/if}
      {#if diff.prompts.removed && diff.prompts.removed.length > 0}
        <h3>Removed</h3>
        <ul>
          {#each diff.prompts.removed as n}<li><code>{n}</code></li>{/each}
        </ul>
      {/if}
    {/if}
  </section>

  <section>
    <h2>Skills</h2>
    {#if (diff.skills.added?.length ?? 0) + (diff.skills.removed?.length ?? 0) === 0}
      <p class="muted">No skill changes.</p>
    {:else}
      {#if diff.skills.added && diff.skills.added.length > 0}
        <h3>Added</h3>
        <ul>
          {#each diff.skills.added as n}<li><code>{n}</code></li>{/each}
        </ul>
      {/if}
      {#if diff.skills.removed && diff.skills.removed.length > 0}
        <h3>Removed</h3>
        <ul>
          {#each diff.skills.removed as n}<li><code>{n}</code></li>{/each}
        </ul>
      {/if}
    {/if}
  </section>
{/if}

<style>
  .page-head {
    margin-bottom: var(--space-4);
  }
  .back {
    font-size: var(--font-sm);
    color: var(--color-text-muted);
  }
  section {
    margin-bottom: var(--space-5);
  }
  h3 {
    margin-top: var(--space-3);
    margin-bottom: var(--space-1);
  }
  ul {
    padding-left: var(--space-4);
  }
  .muted {
    color: var(--color-text-muted);
  }
  .error {
    color: var(--color-danger);
  }
</style>
