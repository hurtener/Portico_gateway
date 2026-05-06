<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type SkillsIndex, type SkillIndexEntry } from '$lib/api';

  let index: SkillsIndex | null = null;
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      index = await api.listSkills();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function toggle(s: SkillIndexEntry) {
    try {
      if (s.enabled_for_tenant) {
        await api.disableSkill(s.id);
      } else {
        await api.enableSkill(s.id);
      }
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);
</script>

<header class="page-head">
  <h1>Skills</h1>
  <button class="btn" on:click={refresh} disabled={loading}>Refresh</button>
</header>

{#if error}
  <p class="error">{error}</p>
{/if}

{#if loading && !index}
  <p class="muted">Loading…</p>
{:else if !index || index.skills.length === 0}
  <p class="muted">No skills loaded. Configure <code>skills.sources</code> in portico.yaml.</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>ID</th>
        <th>Title</th>
        <th>Version</th>
        <th>Status</th>
        <th>Missing tools</th>
        <th></th>
      </tr>
    </thead>
    <tbody>
      {#each index.skills as s (s.id)}
        <tr>
          <td><a href={`/skills/${encodeURIComponent(s.id)}`}><code>{s.id}</code></a></td>
          <td>{s.title}</td>
          <td><code>{s.version}</code></td>
          <td>
            {#if (s.missing_tools ?? []).length > 0}
              <span class="badge danger">missing tools</span>
            {:else if s.enabled_for_tenant}
              <span class="badge ok">enabled</span>
            {:else}
              <span class="badge muted">disabled</span>
            {/if}
          </td>
          <td>
            {#if (s.missing_tools ?? []).length > 0}
              {#each s.missing_tools ?? [] as t (t)}
                <code class="missing">{t}</code>
              {/each}
            {:else}
              <span class="muted">—</span>
            {/if}
          </td>
          <td>
            <button class="btn btn-secondary" on:click={() => toggle(s)}>
              {s.enabled_for_tenant ? 'Disable' : 'Enable'}
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
    vertical-align: middle;
  }
  code {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    background: var(--color-surface-2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
  }
  code.missing {
    background: var(--color-danger-soft);
    color: var(--color-danger);
    margin-right: var(--space-1);
  }
  .badge {
    font-size: var(--text-xs);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-pill);
    background: var(--color-surface-2);
    color: var(--color-text-muted);
  }
  .badge.ok {
    background: var(--color-success-soft);
    color: var(--color-success);
  }
  .badge.danger {
    background: var(--color-danger-soft);
    color: var(--color-danger);
  }
  .badge.muted {
    background: var(--color-surface-2);
    color: var(--color-text-muted);
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
