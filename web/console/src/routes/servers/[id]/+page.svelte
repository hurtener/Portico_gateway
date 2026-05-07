<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import {
    api,
    isFeatureUnavailable,
    type ServerSpec,
    type InstanceRecord,
    type EntityActivityRow
  } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    Button,
    CodeBlock,
    EmptyState,
    KeyValueGrid,
    PageHeader,
    StatusDot,
    Table,
    Tabs,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconEdit from 'lucide-svelte/icons/pencil';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconRotateCcw from 'lucide-svelte/icons/rotate-ccw';
  import IconTrash from 'lucide-svelte/icons/trash-2';

  let server: ServerSpec | null = null;
  let instances: InstanceRecord[] = [];
  let loading = true;
  let error = '';
  let activeTab = 'overview';

  // Logs tab state — SSE-driven from /api/servers/{id}/logs.
  let logLines: string[] = [];
  let logState: 'idle' | 'streaming' | 'error' = 'idle';
  let logError = '';
  let logStream: EventSource | null = null;

  // Activity tab state.
  let activity: EntityActivityRow[] = [];
  let activityLoading = false;

  // Destructive-action state.
  let restartPending = false;
  let deletePending = false;

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

  async function restartServer() {
    if (!id || !confirm($t('serverDetail.confirmRestart', { id }))) return;
    restartPending = true;
    try {
      await api.restartServer(id, 'console.user_restart');
      toast.info($t('serverDetail.toast.restartIssued'));
      await refresh();
    } catch (e) {
      toast.danger($t('serverDetail.toast.restartFailed.title'), (e as Error).message);
    } finally {
      restartPending = false;
    }
  }

  async function deleteServer() {
    if (!id) return;
    if (!confirm($t('serverDetail.confirmDelete', { id }))) return;
    deletePending = true;
    try {
      await api.deleteServer(id);
      toast.success($t('serverDetail.toast.deleted'), id);
      void goto('/servers');
    } catch (e) {
      // The Phase 10 approval gate may intercept some deletes with 202;
      // when wired, this falls into the typed APIError code path above
      // and the operator approves out-of-band. For now, surface the
      // status to the user.
      const err = e as Error & { status?: number; code?: string };
      if (err.status === 202) {
        toast.info(
          $t('serverDetail.toast.approvalRequired.title'),
          $t('serverDetail.toast.approvalRequired.description')
        );
      } else {
        toast.danger($t('serverDetail.toast.deleteFailed.title'), err.message);
      }
    } finally {
      deletePending = false;
    }
  }

  function startLogStream() {
    stopLogStream();
    if (!id) return;
    logState = 'streaming';
    logError = '';
    logLines = [];
    const url = `/api/servers/${encodeURIComponent(id)}/logs`;
    try {
      logStream = new EventSource(url, { withCredentials: true });
      logStream.onmessage = (ev) => {
        if (typeof ev.data !== 'string') return;
        logLines = [...logLines, ev.data].slice(-500);
      };
      logStream.onerror = () => {
        logState = 'error';
        logError = $t('serverDetail.logs.error');
        stopLogStream();
      };
    } catch (e) {
      logState = 'error';
      logError = (e as Error).message;
    }
  }

  function stopLogStream() {
    if (logStream) {
      logStream.close();
      logStream = null;
    }
    if (logState === 'streaming') logState = 'idle';
  }

  async function loadActivity() {
    if (!id) return;
    activityLoading = true;
    try {
      activity = (await api.serverActivity(id, 50)) ?? [];
    } catch (e) {
      if (!isFeatureUnavailable(e)) {
        toast.danger($t('common.error'), (e as Error).message);
      }
      activity = [];
    } finally {
      activityLoading = false;
    }
  }

  // React to tab change — start/stop streams + lazy fetches.
  $: {
    if (activeTab === 'logs' && logState === 'idle') startLogStream();
    if (activeTab !== 'logs') stopLogStream();
    if (activeTab === 'activity' && activity.length === 0 && !activityLoading) {
      void loadActivity();
    }
  }

  onMount(refresh);
  onDestroy(stopLogStream);

  type Tone = 'success' | 'danger' | 'warning' | 'neutral' | 'info';
  function statusTone(s?: string): Tone {
    const v = (s ?? '').toLowerCase();
    if (v === 'ready' || v === 'running' || v === 'healthy') return 'success';
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
        { label: $t('servers.field.transport'), value: server.transport, mono: true },
        { label: $t('servers.field.runtimeMode'), value: server.runtime_mode, mono: true },
        { label: $t('servers.field.status'), value: server.status },
        { label: $t('servers.field.enabled'), value: server.enabled ? 'yes' : 'no' },
        ...(server.stdio?.command
          ? [
              {
                label: $t('servers.field.command'),
                value: server.stdio.command,
                mono: true,
                full: true as const
              }
            ]
          : []),
        ...(server.stdio?.args && server.stdio.args.length > 0
          ? [
              {
                label: $t('servers.field.args'),
                value: server.stdio.args.join(' '),
                mono: true,
                full: true as const
              }
            ]
          : []),
        ...(server.http?.url
          ? [
              {
                label: $t('servers.field.url'),
                value: server.http.url,
                mono: true,
                full: true as const
              }
            ]
          : [])
      ]
    : [];

  $: tabs = [
    { id: 'overview', label: $t('serverDetail.tabs.overview') },
    { id: 'logs', label: $t('serverDetail.tabs.logs') },
    { id: 'activity', label: $t('serverDetail.tabs.activity') }
  ];

  $: activityColumns = [
    { key: 'occurred_at', label: $t('activity.col.when') },
    { key: 'event_type', label: $t('activity.col.event'), mono: true },
    { key: 'actor_user_id', label: $t('activity.col.actor') },
    { key: 'summary', label: $t('activity.col.summary') }
  ];
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
      <Button variant="secondary" href={`/servers/${encodeURIComponent(id)}/edit`}>
        <IconEdit slot="leading" size={14} />
        {$t('common.edit')}
      </Button>
      <Button variant="secondary" on:click={restartServer} loading={restartPending}>
        <IconRotateCcw slot="leading" size={14} />
        {$t('servers.action.restart')}
      </Button>
      <Button variant="destructive" on:click={deleteServer} loading={deletePending}>
        <IconTrash slot="leading" size={14} />
        {$t('servers.action.delete')}
      </Button>
    {/if}
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if server}
  <Tabs {tabs} bind:active={activeTab} />

  {#if activeTab === 'overview'}
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
  {:else if activeTab === 'logs'}
    <section class="logs-section">
      <div class="logs-header">
        <h2>{$t('serverDetail.logs.title')}</h2>
        <div class="logs-controls">
          {#if logState === 'streaming'}
            <Badge tone="success">{$t('serverDetail.logs.live')}</Badge>
            <Button size="sm" variant="secondary" on:click={stopLogStream}>
              {$t('serverDetail.logs.pause')}
            </Button>
          {:else}
            <Badge tone="neutral">{$t('serverDetail.logs.idle')}</Badge>
            <Button size="sm" variant="secondary" on:click={startLogStream}>
              {$t('serverDetail.logs.resume')}
            </Button>
          {/if}
        </div>
      </div>
      {#if logError}<p class="error">{logError}</p>{/if}
      <div class="logs-pane">
        {#if logLines.length === 0}
          <EmptyState
            title={$t('serverDetail.logs.empty.title')}
            description={$t('serverDetail.logs.empty.description')}
            compact
          />
        {:else}
          <CodeBlock language="text" code={logLines.join('\n')} />
        {/if}
      </div>
    </section>
  {:else if activeTab === 'activity'}
    <section>
      <Table columns={activityColumns} rows={activity} empty={$t('common.empty')}>
        <svelte:fragment slot="empty">
          <EmptyState
            title={$t('activity.empty.title')}
            description={$t('activity.empty.description')}
            compact
          />
        </svelte:fragment>
      </Table>
    </section>
  {/if}
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
    margin-top: var(--space-4);
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
  .logs-section {
    margin-top: var(--space-4);
    display: grid;
    gap: var(--space-3);
  }
  .logs-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .logs-header h2 {
    margin: 0;
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
  }
  .logs-controls {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }
  .logs-pane {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
  }
</style>
