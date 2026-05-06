<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type AuditEvent } from '$lib/api';

  let events: AuditEvent[] = [];
  let cursor = '';
  let loading = false;
  let error = '';
  let typeFilter = '';
  let pendingType = '';

  async function search(append = false) {
    loading = true;
    error = '';
    try {
      const res = await api.queryAudit({ type: typeFilter || undefined, cursor: append ? cursor : undefined, limit: 50 });
      cursor = res.next_cursor || '';
      events = append ? [...events, ...res.events] : res.events;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  function applyFilter() {
    typeFilter = pendingType;
    cursor = '';
    search(false);
  }

  onMount(() => search(false));
</script>

<header class="page-head">
  <h1>Audit log</h1>
  <form class="filters" on:submit|preventDefault={applyFilter}>
    <input
      type="text"
      placeholder="event type (e.g. tool_call.complete)"
      bind:value={pendingType}
    />
    <button class="btn" type="submit" disabled={loading}>Search</button>
  </form>
</header>

{#if error}
  <p class="error">{error}</p>
{/if}

{#if events.length === 0 && !loading}
  <p class="muted">No events match.</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>When</th>
        <th>Type</th>
        <th>Tenant</th>
        <th>Session</th>
        <th>Payload</th>
      </tr>
    </thead>
    <tbody>
      {#each events as e, i (i)}
        <tr>
          <td class="muted">{new Date(e.occurred_at).toLocaleString()}</td>
          <td><code>{e.type}</code></td>
          <td class="muted">{e.tenant_id}</td>
          <td class="muted">{e.session_id ?? '—'}</td>
          <td><pre>{JSON.stringify(e.payload ?? {}, null, 0)}</pre></td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

{#if cursor}
  <button class="btn load-more" on:click={() => search(true)} disabled={loading}>
    Load more
  </button>
{/if}

<style>
  .page-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-4);
    gap: var(--space-3);
  }
  .filters {
    display: flex;
    gap: var(--space-2);
  }
  .filters input {
    min-width: 18rem;
    padding: var(--space-1) var(--space-2);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  th, td {
    padding: var(--space-2) var(--space-3);
    text-align: left;
    border-bottom: 1px solid var(--color-border);
    vertical-align: top;
  }
  pre {
    margin: 0;
    font-size: var(--font-sm);
    max-width: 32rem;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .muted { color: var(--color-text-muted); }
  .error { color: var(--color-danger); }
  .btn {
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    cursor: pointer;
  }
  .load-more { margin-top: var(--space-4); }
</style>
