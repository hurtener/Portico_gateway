<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api, type Approval } from '$lib/api';

  let approvals: Approval[] = [];
  let loading = true;
  let error = '';
  let timer: ReturnType<typeof setInterval> | null = null;

  async function refresh() {
    try {
      approvals = await api.listApprovals();
      error = '';
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function approve(id: string) {
    try {
      await api.approveApproval(id);
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  async function deny(id: string) {
    try {
      await api.denyApproval(id);
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(() => {
    refresh();
    timer = setInterval(refresh, 2000);
  });

  onDestroy(() => {
    if (timer !== null) clearInterval(timer);
  });
</script>

<header class="page-head">
  <h1>Pending approvals</h1>
  <button class="btn" on:click={refresh} disabled={loading}>Refresh</button>
</header>

{#if error}
  <p class="error">{error}</p>
{/if}

{#if loading && approvals.length === 0}
  <p class="muted">Loading…</p>
{:else if approvals.length === 0}
  <p class="muted">No pending approvals.</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>Tool</th>
        <th>Risk</th>
        <th>Session</th>
        <th>Created</th>
        <th>Expires</th>
        <th></th>
      </tr>
    </thead>
    <tbody>
      {#each approvals as a (a.id)}
        <tr>
          <td><code>{a.tool}</code></td>
          <td><span class="risk risk-{a.risk_class}">{a.risk_class}</span></td>
          <td class="muted">{a.session_id}</td>
          <td class="muted">{new Date(a.created_at).toLocaleString()}</td>
          <td class="muted">{new Date(a.expires_at).toLocaleString()}</td>
          <td class="actions">
            <button class="btn btn-approve" on:click={() => approve(a.id)}>Approve</button>
            <button class="btn btn-deny" on:click={() => deny(a.id)}>Deny</button>
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
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
  th, td {
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
  .actions {
    display: flex;
    gap: var(--space-2);
  }
  .btn {
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    cursor: pointer;
  }
  .btn-approve {
    background: var(--color-success);
    color: var(--color-on-success);
    border-color: var(--color-success);
  }
  .btn-deny {
    background: var(--color-danger);
    color: var(--color-on-danger);
    border-color: var(--color-danger);
  }
  .risk {
    padding: 2px 8px;
    border-radius: var(--radius-sm);
    font-size: var(--font-sm);
    background: var(--color-surface-alt);
  }
  .risk-destructive, .risk-external_side_effect {
    background: var(--color-warning-bg, #fff3cd);
    color: var(--color-warning-fg, #856404);
  }
</style>
