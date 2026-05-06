<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Snapshot } from '$lib/api';

  let snapshots: Snapshot[] = [];
  let cursor = '';
  let loading = true;
  let error = '';

  async function refresh(append = false) {
    loading = true;
    error = '';
    try {
      const res = await api.listSnapshots({
        cursor: append ? cursor : undefined,
        limit: 50
      });
      cursor = res.next_cursor || '';
      snapshots = append ? [...snapshots, ...res.snapshots] : res.snapshots;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(() => refresh(false));
</script>

<header class="page-head">
  <h1>Catalog snapshots</h1>
  <button class="btn" on:click={() => refresh(false)} disabled={loading}>Refresh</button>
</header>

{#if error}
  <p class="error">{error}</p>
{/if}

{#if loading && snapshots.length === 0}
  <p class="muted">Loading…</p>
{:else if snapshots.length === 0}
  <p class="muted">No snapshots yet — create a session against the gateway to materialize one.</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>ID</th>
        <th>Tenant</th>
        <th>Session</th>
        <th>Tools</th>
        <th>Created</th>
        <th>Hash</th>
      </tr>
    </thead>
    <tbody>
      {#each snapshots as s, i (i)}
        <tr>
          <td><a href="/snapshots/{s.id}"><code>{s.id}</code></a></td>
          <td>{s.tenant_id}</td>
          <td class="muted">{s.session_id ?? '—'}</td>
          <td>{s.tools.length}</td>
          <td class="muted">{new Date(s.created_at).toLocaleString()}</td>
          <td><code>{s.overall_hash.slice(0, 12)}…</code></td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

{#if cursor}
  <button class="btn load-more" on:click={() => refresh(true)} disabled={loading}>
    Load more
  </button>
{/if}

<style>
  .page-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-4);
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  th,
  td {
    padding: var(--space-2) var(--space-3);
    text-align: left;
    border-bottom: 1px solid var(--color-border);
  }
  .muted {
    color: var(--color-text-muted);
  }
  .error {
    color: var(--color-danger);
  }
  .btn {
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    cursor: pointer;
  }
  .load-more {
    margin-top: var(--space-4);
  }
</style>
