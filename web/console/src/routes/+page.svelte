<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';

  let healthOk = false;
  let readyOk = false;
  let error = '';

  onMount(async () => {
    try {
      const h = await api.health();
      healthOk = h.status === 'ok';
    } catch (e) {
      error = (e as Error).message;
    }
    try {
      const r = await api.ready();
      readyOk = r.status === 'ok' || r.status === 'ready';
    } catch (e) {
      // /readyz may legitimately return non-OK; surface separately
      readyOk = false;
    }
  });
</script>

<section class="hero">
  <h1>Portico Console</h1>
  <p class="lede">
    Multi-tenant MCP gateway. The Console is the operator surface for managing servers, skills, and
    active sessions.
  </p>

  <div class="status-row" data-testid="health-row">
    <span class="status" class:ok={healthOk} class:err={!healthOk}>
      health: {healthOk ? 'ok' : 'down'}
    </span>
    <span class="status" class:ok={readyOk} class:err={!readyOk}>
      ready: {readyOk ? 'ok' : 'pending'}
    </span>
  </div>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  <div class="cards">
    <a class="card" href="/servers">
      <h2>Servers</h2>
      <p>Register and inspect downstream MCP servers.</p>
    </a>
    <a class="card" href="/skills">
      <h2>Skills</h2>
      <p>Browse loaded Skill Packs and their tools.</p>
    </a>
    <a class="card" href="/sessions">
      <h2>Sessions</h2>
      <p>Watch active gateway sessions and inspect history.</p>
    </a>
  </div>
</section>

<style>
  .hero {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }
  h1 {
    font-size: var(--text-3xl);
    font-weight: var(--weight-bold);
    margin: 0;
  }
  .lede {
    color: var(--color-text-muted);
    margin: 0;
    max-width: 56ch;
  }
  .status-row {
    display: flex;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  .status {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-pill);
    border: 1px solid var(--color-border);
  }
  .status.ok {
    background: var(--color-success-soft);
    color: var(--color-success);
    border-color: var(--color-success);
  }
  .status.err {
    background: var(--color-danger-soft);
    color: var(--color-danger);
    border-color: var(--color-danger);
  }
  .error {
    color: var(--color-danger);
    font-size: var(--text-sm);
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: var(--space-4);
  }
  .card {
    display: block;
    padding: var(--space-5);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    color: var(--color-text);
    transition:
      border-color var(--motion-fast) var(--ease-standard),
      box-shadow var(--motion-fast) var(--ease-standard);
  }
  .card:hover {
    border-color: var(--color-border-strong);
    box-shadow: var(--shadow-sm);
  }
  .card h2 {
    margin: 0 0 var(--space-2) 0;
    font-size: var(--text-lg);
    font-weight: var(--weight-semibold);
  }
  .card p {
    margin: 0;
    color: var(--color-text-muted);
    font-size: var(--text-sm);
  }
</style>
