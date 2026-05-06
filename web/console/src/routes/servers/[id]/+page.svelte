<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type ServerSpec, type InstanceRecord } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    Button,
    EmptyState,
    KeyValueGrid,
    PageHeader,
    StatusDot
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';

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

  type Tone = 'success' | 'danger' | 'warning' | 'neutral' | 'info';
  function statusTone(s?: string): Tone {
    const v = (s ?? '').toLowerCase();
    if (v === 'ready' || v === 'running') return 'success';
    if (v === 'crashed' || v === 'error') return 'danger';
    if (v === 'circuit_open' || v === 'backoff') return 'warning';
    if (v === 'starting') return 'info';
    return 'neutral';
  }

  function instanceTone(s: string): Tone {
    return statusTone(s);
  }

  $: specItems = server
    ? [
        { label: 'Transport', value: server.transport, mono: true },
        { label: 'Runtime mode', value: server.runtime_mode, mono: true },
        { label: 'Status', value: server.status },
        { label: 'Enabled', value: server.enabled ? 'yes' : 'no' },
        ...(server.stdio?.command
          ? [{ label: 'Command', value: server.stdio.command, mono: true, full: true as const }]
          : []),
        ...(server.stdio?.args && server.stdio.args.length > 0
          ? [
              {
                label: 'Args',
                value: server.stdio.args.join(' '),
                mono: true,
                full: true as const
              }
            ]
          : []),
        ...(server.http?.url
          ? [{ label: 'URL', value: server.http.url, mono: true, full: true as const }]
          : [])
      ]
    : [];
</script>

<PageHeader title={server?.display_name || id} description={server ? undefined : ''}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: $t('nav.servers'), href: '/servers' }, { label: id }]}
  />
  <div slot="meta">
    <Badge tone="neutral" mono>{id}</Badge>
    {#if server}<Badge tone={statusTone(server.status)}>{server.status}</Badge>{/if}
  </div>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
    {#if server}
      <Button on:click={reload}>{$t('serverDetail.action.reload')}</Button>
    {/if}
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if server}
  <section class="grid">
    <article class="card">
      <h2>{$t('serverDetail.spec')}</h2>
      <KeyValueGrid items={specItems} columns={2} />
    </article>

    <article class="card">
      <h2>{$t('serverDetail.instances', { count: instances.length })}</h2>
      {#if instances.length === 0}
        <EmptyState
          title={$t('serverDetail.instances.empty.title')}
          description={$t('serverDetail.instances.empty.description')}
          compact
        />
      {:else}
        <ul class="instance-list">
          {#each instances as i (i.instance_key)}
            <li>
              <StatusDot tone={instanceTone(i.state)} />
              <code class="key">{i.instance_key}</code>
              <Badge tone={instanceTone(i.state)}>{i.state}</Badge>
              {#if i.pid}<span class="pid">pid {i.pid}</span>{/if}
            </li>
          {/each}
        </ul>
      {/if}
    </article>
  </section>
{:else if !loading}
  <EmptyState
    title={$t('serverDetail.notFound.title')}
    description={$t('serverDetail.notFound.description', { id })}
  />
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-4);
  }
  @media (max-width: 880px) {
    .grid {
      grid-template-columns: 1fr;
    }
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-5);
  }
  .card h2 {
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
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
    font-size: var(--font-size-body-sm);
    flex-wrap: wrap;
  }
  .key {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
  }
  .pid {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-tertiary);
  }
</style>
