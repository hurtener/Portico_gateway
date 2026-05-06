<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type ServerSummary } from '$lib/api';

  let servers: ServerSummary[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      const r = await api.listServers();
      servers = r.items ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function toggle(s: ServerSummary) {
    try {
      if (s.enabled) {
        await api.disableServer(s.id);
      } else {
        await api.enableServer(s.id);
      }
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);
</script>

<header class="page-head">
  <h1>Servers</h1>
  <button class="btn" on:click={refresh} disabled={loading}>Refresh</button>
</header>

{#if error}
  <p class="error">{error}</p>
{/if}

{#if loading && servers.length === 0}
  <p class="muted">Loading…</p>
{:else if servers.length === 0}
  <p class="muted">No servers registered.</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>ID</th>
        <th>Display name</th>
        <th>Transport</th>
        <th>Mode</th>
        <th>Status</th>
        <th>Enabled</th>
        <th></th>
      </tr>
    </thead>
    <tbody>
      {#each servers as s (s.id)}
        <tr>
          <td><a href={`/servers/${encodeURIComponent(s.id)}`}>{s.id}</a></td>
          <td>{s.display_name ?? ''}</td>
          <td><code>{s.transport}</code></td>
          <td><code>{s.runtime_mode}</code></td>
          <td>
            <span class="status status-{s.status}">{s.status}</span>
          </td>
          <td>{s.enabled ? 'yes' : 'no'}</td>
          <td>
            <button class="btn btn-secondary" on:click={() => toggle(s)}>
              {s.enabled ? 'Disable' : 'Enable'}
            </button>
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
    gap: var(--space-4);
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
    margin: 0 0 var(--space-4) 0;
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
    vertical-align: middle;
  }
  code {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    background: var(--color-surface-2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
  }

  .status {
    display: inline-block;
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-pill);
    font-size: var(--text-xs);
    font-family: var(--font-mono);
    background: var(--color-surface-2);
    color: var(--color-text-muted);
  }
  .status-ready,
  .status-running {
    background: var(--color-success-soft);
    color: var(--color-success);
  }
  .status-crashed,
  .status-error {
    background: var(--color-danger-soft);
    color: var(--color-danger);
  }
  .status-circuit_open,
  .status-backoff {
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
    transition: background var(--motion-fast) var(--ease-standard);
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
