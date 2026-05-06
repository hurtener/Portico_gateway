<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type Snapshot } from '$lib/api';

  let snap: Snapshot | null = null;
  let loading = true;
  let error = '';

  $: id = $page.params.id ?? '';

  async function load() {
    loading = true;
    error = '';
    try {
      if (!id) throw new Error('missing snapshot id');
      snap = await api.getSnapshot(id);
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
  <h1>Snapshot {id}</h1>
</header>

{#if error}
  <p class="error">{error}</p>
{/if}

{#if loading && !snap}
  <p class="muted">Loading…</p>
{:else if snap}
  <section class="meta">
    <dl>
      <dt>Tenant</dt>
      <dd>{snap.tenant_id}</dd>
      <dt>Session</dt>
      <dd>{snap.session_id ?? '—'}</dd>
      <dt>Created</dt>
      <dd>{new Date(snap.created_at).toLocaleString()}</dd>
      <dt>Overall hash</dt>
      <dd><code>{snap.overall_hash.slice(0, 16)}…</code></dd>
    </dl>
  </section>

  {#if snap.warnings && snap.warnings.length > 0}
    <section class="warnings">
      <h2>Warnings</h2>
      <ul>
        {#each snap.warnings as w}<li>{w}</li>{/each}
      </ul>
    </section>
  {/if}

  <section>
    <h2>Servers ({snap.servers.length})</h2>
    <table>
      <thead>
        <tr><th>ID</th><th>Transport</th><th>Mode</th><th>Schema hash</th><th>Health</th></tr>
      </thead>
      <tbody>
        {#each snap.servers as s, i (i)}
          <tr>
            <td><code>{s.id}</code></td>
            <td>{s.transport}</td>
            <td class="muted">{s.runtime_mode ?? '—'}</td>
            <td><code>{s.schema_hash.slice(0, 12)}…</code></td>
            <td>{s.health}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </section>

  <section>
    <h2>Tools ({snap.tools.length})</h2>
    <table>
      <thead>
        <tr><th>Name</th><th>Risk</th><th>Approval</th><th>Skill</th><th>Hash</th></tr>
      </thead>
      <tbody>
        {#each snap.tools as t, i (i)}
          <tr>
            <td><code>{t.namespaced_name}</code></td>
            <td>{t.risk_class}</td>
            <td>{t.requires_approval ? 'yes' : '—'}</td>
            <td class="muted">{t.skill_id ?? '—'}</td>
            <td><code>{t.hash.slice(0, 12)}…</code></td>
          </tr>
        {/each}
      </tbody>
    </table>
  </section>

  {#if snap.skills && snap.skills.length > 0}
    <section>
      <h2>Skills</h2>
      <table>
        <thead><tr><th>ID</th><th>Version</th><th>Session enabled</th></tr></thead>
        <tbody>
          {#each snap.skills as s, i (i)}
            <tr>
              <td><code>{s.id}</code></td>
              <td>{s.version}</td>
              <td>{s.enabled_for_session ? 'yes' : 'no'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </section>
  {/if}
{/if}

<style>
  .page-head {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin-bottom: var(--space-4);
  }
  .back {
    font-size: var(--font-sm);
    color: var(--color-text-muted);
  }
  .meta dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-1) var(--space-3);
    margin-bottom: var(--space-4);
  }
  .meta dt {
    color: var(--color-text-muted);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    margin-bottom: var(--space-6);
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
  .warnings {
    background: var(--color-warning-soft, #fff3cd);
    padding: var(--space-3);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-4);
  }
</style>
