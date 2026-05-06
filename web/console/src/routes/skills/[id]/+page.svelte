<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type SkillDetail } from '$lib/api';

  let detail: SkillDetail | null = null;
  let loading = true;
  let error = '';

  $: id = $page.params.id ?? '';

  async function refresh() {
    if (!id) return;
    loading = true;
    error = '';
    try {
      detail = await api.getSkill(id);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function toggle() {
    if (!detail) return;
    try {
      if (detail.enabled_for_tenant) {
        await api.disableSkill(detail.id);
      } else {
        await api.enableSkill(detail.id);
      }
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);
</script>

<header class="page-head">
  <div>
    <a class="back" href="/skills">← All skills</a>
    <h1>{detail?.title || id}</h1>
    <p class="muted"><code>{id}</code> · v{detail?.version ?? '?'}</p>
  </div>
  <div class="actions">
    <button class="btn btn-secondary" on:click={refresh} disabled={loading}>Refresh</button>
    {#if detail}
      <button class="btn" on:click={toggle}>
        {detail.enabled_for_tenant ? 'Disable for tenant' : 'Enable for tenant'}
      </button>
    {/if}
  </div>
</header>

{#if error}<p class="error">{error}</p>{/if}

{#if detail}
  {#if detail.description}
    <p class="lede">{detail.description}</p>
  {/if}

  {#if detail.warnings && detail.warnings.length > 0}
    <section class="warnings">
      <h2>Warnings</h2>
      <ul>
        {#each detail.warnings as w (w)}
          <li>{w}</li>
        {/each}
      </ul>
    </section>
  {/if}

  <section>
    <h2>Manifest</h2>
    <pre><code>{JSON.stringify(detail.manifest, null, 2)}</code></pre>
  </section>
{:else if !loading}
  <p class="muted">Skill not found.</p>
{/if}

<style>
  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    margin-bottom: var(--space-6);
  }
  .back {
    font-size: var(--text-sm);
    color: var(--color-text-muted);
  }
  h1 {
    margin: var(--space-1) 0;
    font-size: var(--text-2xl);
    font-weight: var(--weight-semibold);
  }
  h2 {
    margin: var(--space-6) 0 var(--space-3) 0;
    font-size: var(--text-lg);
    font-weight: var(--weight-semibold);
  }
  .muted {
    color: var(--color-text-muted);
    margin: 0;
  }
  .lede {
    color: var(--color-text-muted);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
  }
  .error {
    color: var(--color-danger);
  }
  .warnings {
    border: 1px solid var(--color-warning);
    background: var(--color-warning-soft);
    color: var(--color-warning);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-md);
  }
  .warnings h2 {
    margin: 0 0 var(--space-2) 0;
    font-size: var(--text-base);
  }
  pre {
    background: var(--color-surface-2);
    padding: var(--space-4);
    border-radius: var(--radius-md);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    overflow-x: auto;
  }
  code {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
  }
  .btn {
    border: 1px solid var(--color-brand);
    background: var(--color-brand);
    color: var(--color-on-brand);
    padding: var(--space-2) var(--space-4);
    border-radius: var(--radius-md);
    font-size: var(--text-sm);
    cursor: pointer;
  }
  .btn:hover:not(:disabled) {
    background: var(--color-brand-hover);
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .btn-secondary {
    background: var(--color-surface);
    color: var(--color-text);
    border-color: var(--color-border-strong);
  }
  .btn-secondary:hover:not(:disabled) {
    background: var(--color-surface-2);
  }
</style>
