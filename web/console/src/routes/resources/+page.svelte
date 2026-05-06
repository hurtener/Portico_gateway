<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Resource } from '$lib/api';

  let resources: Resource[] = [];
  let loading = true;
  let error = '';

  function serverIDFor(r: Resource): string {
    if (r._meta && typeof r._meta === 'object' && 'serverID' in r._meta) {
      return String((r._meta as { serverID?: string }).serverID ?? '');
    }
    if (r.uri.startsWith('mcp+server://')) return r.uri.slice('mcp+server://'.length).split('/')[0];
    if (r.uri.startsWith('ui://')) return r.uri.slice('ui://'.length).split('/')[0];
    return '';
  }

  async function refresh() {
    loading = true;
    error = '';
    try {
      const r = await api.listResources();
      resources = r.resources ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }
  onMount(refresh);
</script>

<header class="page-head">
  <h1>Resources</h1>
  <button class="btn" on:click={refresh} disabled={loading}>Refresh</button>
</header>

{#if error}<p class="error">{error}</p>{/if}

{#if loading && resources.length === 0}
  <p class="muted">Loading…</p>
{:else if resources.length === 0}
  <p class="muted">No resources discovered.</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>Server</th>
        <th>URI</th>
        <th>Name</th>
        <th>MIME</th>
      </tr>
    </thead>
    <tbody>
      {#each resources as r (r.uri)}
        <tr>
          <td><code>{serverIDFor(r)}</code></td>
          <td><code class="uri">{r.uri}</code></td>
          <td>{r.name ?? ''}</td>
          <td><code>{r.mimeType ?? ''}</code></td>
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
    color: var(--code-muted, var(--color-text-muted));
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
  code.uri {
    word-break: break-all;
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
