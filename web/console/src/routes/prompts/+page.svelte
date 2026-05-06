<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Prompt } from '$lib/api';

  let prompts: Prompt[] = [];
  let loading = true;
  let error = '';

  function serverID(name: string): string {
    const i = name.indexOf('.');
    return i > 0 ? name.slice(0, i) : '';
  }

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
</script>

<header class="page-head">
  <h1>Prompts</h1>
  <button class="btn" on:click={refresh} disabled={loading}>Refresh</button>
</header>

{#if error}<p class="error">{error}</p>{/if}

{#if loading && prompts.length === 0}
  <p class="muted">Loading…</p>
{:else if prompts.length === 0}
  <p class="muted">No prompts available.</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>Server</th>
        <th>Name</th>
        <th>Description</th>
        <th>Arguments</th>
      </tr>
    </thead>
    <tbody>
      {#each prompts as p (p.name)}
        <tr>
          <td><code>{serverID(p.name)}</code></td>
          <td><code>{p.name}</code></td>
          <td>{p.description ?? ''}</td>
          <td>
            {#if p.arguments && p.arguments.length > 0}
              {#each p.arguments as a (a.name)}
                <code class="arg" class:required={a.required}>{a.name}</code>
              {/each}
            {/if}
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

<style>
  .page-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: var(--space-6);
  }
  h1 {
    margin: 0;
    font-size: var(--text-2xl);
    font-weight: var(--weight-semibold);
  }
  .muted {
    color: var(--color-text-muted);
  }
  .error {
    color: var(--color-danger);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--text-sm);
  }
  thead th {
    text-align: left;
    padding: var(--space-2) var(--space-3);
    border-bottom: 1px solid var(--color-border);
    color: var(--color-text-muted);
    font-weight: var(--weight-medium);
  }
  tbody td {
    padding: var(--space-3);
    border-bottom: 1px solid var(--color-border);
  }
  code {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    background: var(--color-surface-2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
  }
  code.arg {
    margin-right: var(--space-1);
  }
  code.arg.required {
    background: var(--color-warning-soft);
    color: var(--color-warning);
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
</style>
