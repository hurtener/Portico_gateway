<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type ServerSpec, type InstanceRecord } from '$lib/api';

  let server: ServerSpec | null = null;
  let instances: InstanceRecord[] = [];
  let loading = true;
  let error = '';

  $: id = $page.params.id ?? '';

  async function refresh() {
    if (!id) return;
    loading = true;
    error = '';
    try {
      server = await api.getServer(id);
      const inst = await api.listInstances(id);
      instances = inst.items ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function reload() {
    if (!id) return;
    try {
      await api.reloadServer(id);
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);
</script>

<header class="page-head">
  <div>
    <a href="/servers" class="back">← All servers</a>
    <h1>{server?.display_name || id}</h1>
    <p class="muted"><code>{id}</code></p>
  </div>
  <div class="actions">
    <button class="btn btn-secondary" on:click={refresh} disabled={loading}>Refresh</button>
    <button class="btn" on:click={reload}>Drain & reload</button>
  </div>
</header>

{#if error}
  <p class="error">{error}</p>
{/if}

{#if server}
  <section class="grid">
    <article>
      <h2>Spec</h2>
      <dl>
        <dt>Transport</dt>
        <dd><code>{server.transport}</code></dd>
        <dt>Runtime mode</dt>
        <dd><code>{server.runtime_mode}</code></dd>
        <dt>Status</dt>
        <dd>{server.status}</dd>
        <dt>Enabled</dt>
        <dd>{server.enabled ? 'yes' : 'no'}</dd>
        {#if server.stdio}
          <dt>Command</dt>
          <dd><code>{server.stdio.command}</code></dd>
          {#if server.stdio.args && server.stdio.args.length > 0}
            <dt>Args</dt>
            <dd><code>{server.stdio.args.join(' ')}</code></dd>
          {/if}
        {/if}
        {#if server.http}
          <dt>URL</dt>
          <dd><code>{server.http.url}</code></dd>
        {/if}
      </dl>
    </article>

    <article>
      <h2>Instances ({instances.length})</h2>
      {#if instances.length === 0}
        <p class="muted">No active instances.</p>
      {:else}
        <ul class="instance-list">
          {#each instances as i (i.instance_key)}
            <li>
              <code>{i.instance_key}</code>
              <span class="status">{i.state}</span>
              {#if i.pid}<span class="pid">pid {i.pid}</span>{/if}
            </li>
          {/each}
        </ul>
      {/if}
    </article>
  </section>
{:else if !loading}
  <p class="muted">Server not found.</p>
{/if}

<style>
  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    margin-bottom: var(--space-6);
  }
  .back {
    font-size: var(--text-sm);
    color: var(--color-text-muted);
  }
  h1 {
    margin: var(--space-1) 0 var(--space-1) 0;
    font-size: var(--text-2xl);
    font-weight: var(--weight-semibold);
  }
  .muted {
    color: var(--color-text-muted);
    margin: 0;
  }
  .actions {
    display: flex;
    gap: var(--space-2);
  }
  .error {
    color: var(--color-danger);
  }

  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-6);
  }
  @media (max-width: 700px) {
    .grid {
      grid-template-columns: 1fr;
    }
  }
  article {
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    background: var(--color-surface);
    padding: var(--space-5);
  }
  article h2 {
    margin: 0 0 var(--space-4) 0;
    font-size: var(--text-lg);
    font-weight: var(--weight-semibold);
  }

  dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-2) var(--space-4);
    margin: 0;
  }
  dt {
    color: var(--color-text-muted);
    font-size: var(--text-sm);
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

  .instance-list {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .instance-list li {
    display: flex;
    gap: var(--space-2);
    align-items: center;
    font-size: var(--text-sm);
  }
  .status {
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-pill);
    font-size: var(--text-xs);
    background: var(--color-surface-2);
    color: var(--color-text-muted);
  }
  .pid {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--color-text-subtle);
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
