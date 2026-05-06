<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type AppEntry } from '$lib/api';

  let items: AppEntry[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      const r = await api.listApps();
      items = r.items ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }
  onMount(refresh);
</script>

<header class="page-head">
  <h1>MCP Apps</h1>
  <button class="btn" on:click={refresh} disabled={loading}>Refresh</button>
</header>

<p class="muted">
  Every <code>ui://</code> resource discovered across downstream servers. Phase 5 adds policy filtering;
  preview rendering arrives later in dev mode.
</p>

{#if error}<p class="error">{error}</p>{/if}

{#if loading && items.length === 0}
  <p class="muted">Loading…</p>
{:else if items.length === 0}
  <p class="muted">No MCP App resources discovered yet.</p>
{:else}
  <div class="grid">
    {#each items as a (a.uri)}
      <article>
        <h2>{a.name ?? a.uri}</h2>
        <p class="muted small">{a.description ?? ''}</p>
        <dl>
          <dt>Server</dt>
          <dd><code>{a.serverId}</code></dd>
          <dt>URI</dt>
          <dd><code class="uri">{a.uri}</code></dd>
          <dt>Upstream</dt>
          <dd><code class="uri">{a.upstreamUri}</code></dd>
          <dt>MIME</dt>
          <dd><code>{a.mimeType ?? ''}</code></dd>
        </dl>
      </article>
    {/each}
  </div>
{/if}

<style>
  .page-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: var(--space-4);
  }
  h1 {
    margin: 0;
    font-size: var(--text-2xl);
    font-weight: var(--weight-semibold);
  }
  .muted {
    color: var(--color-text-muted);
    margin-bottom: var(--space-6);
  }
  .small {
    font-size: var(--text-sm);
  }
  .error {
    color: var(--color-danger);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: var(--space-4);
  }
  article {
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    padding: var(--space-4);
  }
  article h2 {
    margin: 0 0 var(--space-2) 0;
    font-size: var(--text-lg);
    font-weight: var(--weight-semibold);
  }
  dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-1) var(--space-3);
    margin: 0;
  }
  dt {
    color: var(--color-text-muted);
    font-size: var(--text-xs);
  }
  dd {
    margin: 0;
    font-size: var(--text-sm);
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
