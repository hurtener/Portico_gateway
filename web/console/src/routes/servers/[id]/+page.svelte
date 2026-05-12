<script lang="ts">
  /**
   * Server detail — Phase 10.8 detail-page sub-vocabulary.
   *
   * Already had Tabs (Overview / Logs / Activity); this rewrite
   * normalizes the rest of the page on the new vocabulary:
   *   - Header actions move into PageActionGroup (was a 4-button
   *     <div slot="actions">).
   *   - Mini-KPI strip above the tabs (Instances / Healthy / Log
   *     lines streamed).
   *   - Overview body uses .card sections with the <h4> SECTION-LABEL
   *     header (was a hand-rolled .card h2 pattern).
   *   - Logs pane wraps in a .card so styling is consistent.
   *
   * Log lines streamed counts the lines received this session, not
   * a cumulative total — useful as an "is the stream working" signal
   * which is the load-bearing operator question on this page.
   */
  import { onDestroy, onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import {
    api,
    isFeatureUnavailable,
    type GatewayInfo,
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
    MetricStrip,
    PageActionGroup,
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
  import IconBox from 'lucide-svelte/icons/box';
  import IconActivity from 'lucide-svelte/icons/activity';
  import IconScroll from 'lucide-svelte/icons/scroll-text';
  import IconCopy from 'lucide-svelte/icons/copy';
  import IconExternalLink from 'lucide-svelte/icons/external-link';
  import type { ComponentType } from 'svelte';

  let server: ServerSpec | null = null;
  let instances: InstanceRecord[] = [];
  let gatewayInfo: GatewayInfo | null = null;
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
      instances = (await api.listInstances(id)) ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
    // Best-effort: gateway info populates the Connect tab. The other
    // tabs work fine if this fails, so we never surface an error.
    try {
      gatewayInfo = await api.gatewayInfo();
    } catch {
      gatewayInfo = null;
    }
  }

  /**
   * Build the gateway endpoint URL from the live bind. Same logic as
   * /connect — substitute the operator's hostname when bind is wildcard.
   */
  function endpointURL(g: GatewayInfo | null): string {
    if (!g) return '';
    let host = g.bind || '';
    if (host.startsWith('0.0.0.0:')) {
      const port = host.slice('0.0.0.0:'.length);
      const base = typeof window !== 'undefined' ? window.location.hostname : 'localhost';
      host = `${base}:${port}`;
    }
    return `http://${host}${g.mcp_path}`;
  }

  $: gatewayURL = endpointURL(gatewayInfo);

  /**
   * The first tool name suggested in the sample tools/call payload.
   * Defaults to "<tool>" when we don't know — the operator overrides
   * with the real name from `tools/list`.
   */
  $: connectItems = [
    { label: $t('serverDetail.connect.serverId'), value: id, mono: true },
    { label: $t('serverDetail.connect.toolPrefix'), value: `${id}.*`, mono: true },
    {
      label: $t('serverDetail.connect.endpoint'),
      value: gatewayURL || '—',
      mono: true,
      full: true as const
    },
    {
      label: $t('serverDetail.connect.auth'),
      value: gatewayInfo
        ? gatewayInfo.auth.mode === 'dev'
          ? $t('serverDetail.connect.auth.dev')
          : $t('serverDetail.connect.auth.jwt')
        : '—'
    }
  ];

  $: sampleToolName = `${id}.<tool>`;
  $: sampleCallPayload = JSON.stringify(
    {
      jsonrpc: '2.0',
      id: 1,
      method: 'tools/call',
      params: {
        name: sampleToolName,
        arguments: { '...': '...' }
      }
    },
    null,
    2
  );

  async function copyText(text: string, what: string) {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      toast.success($t('connect.toast.copied', { what }));
    } catch {
      toast.danger($t('connect.toast.copyFailed'), '');
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

  function fmt(iso?: string): string {
    if (!iso) return '—';
    try {
      return new Date(iso).toLocaleString();
    } catch {
      return iso;
    }
  }

  $: specItems = server
    ? [
        { label: $t('servers.field.transport'), value: server.transport, mono: true },
        { label: $t('servers.field.runtimeMode'), value: server.runtime_mode, mono: true },
        { label: $t('servers.field.status'), value: server.status },
        {
          label: $t('servers.field.enabled'),
          value: server.enabled ? $t('common.yes') : $t('common.no')
        },
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
    { id: 'connect', label: $t('serverDetail.tabs.connect') },
    { id: 'logs', label: $t('serverDetail.tabs.logs') },
    { id: 'activity', label: $t('serverDetail.tabs.activity') }
  ];

  $: activityColumns = [
    { key: 'occurred_at', label: $t('activity.col.when'), width: '180px' },
    { key: 'event_type', label: $t('activity.col.event'), mono: true, width: '220px' },
    { key: 'actor_user_id', label: $t('activity.col.actor'), width: '160px' },
    { key: 'summary', label: $t('activity.col.summary') }
  ];

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh(),
      loading
    },
    ...(server
      ? [
          {
            label: $t('common.edit'),
            icon: IconEdit,
            href: `/servers/${encodeURIComponent(id)}/edit`
          },
          {
            label: $t('servers.action.restart'),
            icon: IconRotateCcw,
            onClick: restartServer,
            loading: restartPending
          },
          {
            label: $t('servers.action.delete'),
            icon: IconTrash,
            variant: 'destructive' as const,
            onClick: deleteServer,
            loading: deletePending
          }
        ]
      : [])
  ];

  $: healthyInstances = instances.filter((i) =>
    ['ready', 'running', 'healthy'].includes(i.state.toLowerCase())
  ).length;

  $: metrics = server
    ? [
        {
          id: 'instances',
          label: $t('serverDetail.metric.instances'),
          value: instances.length.toString(),
          icon: IconBox as ComponentType<any>,
          tone: 'brand' as const
        },
        {
          id: 'healthy',
          label: $t('serverDetail.metric.healthy'),
          value: healthyInstances.toString(),
          icon: IconActivity as ComponentType<any>,
          tone:
            healthyInstances === instances.length && instances.length > 0
              ? ('success' as const)
              : ('default' as const),
          attention: instances.length > 0 && healthyInstances === 0
        },
        {
          id: 'logs',
          label: $t('serverDetail.metric.logs'),
          value: logLines.length.toString(),
          icon: IconScroll as ComponentType<any>
        }
      ]
    : [];
</script>

<PageHeader title={server?.display_name || id}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: $t('nav.servers'), href: '/servers' }, { label: id }]}
  />
  <div slot="meta">
    <Badge tone="neutral" mono>{id}</Badge>
    {#if server}<Badge tone={statusTone(server.status)}>{server.status}</Badge>{/if}
  </div>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if server}
  <MetricStrip {metrics} compact label={$t('serverDetail.metric.aria')} />
  <Tabs {tabs} bind:active={activeTab} />

  {#if activeTab === 'overview'}
    <div class="grid">
      <section class="card">
        <h4>{$t('serverDetail.section.spec')}</h4>
        <KeyValueGrid items={specItems} columns={2} />
      </section>

      <section class="card">
        <h4>{$t('serverDetail.section.instances', { count: instances.length })}</h4>
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
      </section>
    </div>
  {:else if activeTab === 'connect'}
    <section class="card">
      <h4>{$t('serverDetail.connect.routing')}</h4>
      <KeyValueGrid items={connectItems} columns={1} />
      <div class="actions-row">
        <Button variant="secondary" href="/connect">
          <IconExternalLink slot="leading" size={14} />
          {$t('serverDetail.connect.openGuide')}
        </Button>
      </div>
    </section>

    <section class="card">
      <header class="card-head">
        <h4>{$t('serverDetail.connect.sample')}</h4>
        <Button
          variant="secondary"
          size="sm"
          on:click={() => copyText(sampleCallPayload, $t('serverDetail.connect.payloadWhat'))}
        >
          <IconCopy slot="leading" size={14} />
          {$t('common.copy')}
        </Button>
      </header>
      <p class="muted body">{$t('serverDetail.connect.sampleHelp')}</p>
      <pre class="raw"><code>{sampleCallPayload}</code></pre>
      <p class="muted body">
        {$t('serverDetail.connect.listHint')}
        <a href="/playground">{$t('nav.playground')}</a>.
      </p>
    </section>
  {:else if activeTab === 'logs'}
    <section class="card">
      <header class="card-head">
        <h4>{$t('serverDetail.logs.title')}</h4>
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
      </header>
      {#if logError}<p class="error">{logError}</p>{/if}
      {#if logLines.length === 0}
        <EmptyState
          title={$t('serverDetail.logs.empty.title')}
          description={$t('serverDetail.logs.empty.description')}
          compact
        />
      {:else}
        <CodeBlock language="text" code={logLines.join('\n')} />
      {/if}
    </section>
  {:else if activeTab === 'activity'}
    <section class="card">
      <h4>{$t('serverDetail.section.activity')}</h4>
      <Table columns={activityColumns} rows={activity} empty={$t('common.empty')}>
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'occurred_at'}
            <span class="muted">{fmt(row.occurred_at)}</span>
          {:else if column.key === 'event_type'}
            <code class="mono">{row.event_type}</code>
          {:else if column.key === 'actor_user_id'}
            <span class="muted">{row.actor_user_id ?? '—'}</span>
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
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
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
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
    padding: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .grid .card {
    margin-top: 0;
  }
  /* Stand-alone cards (logs, activity) flow vertically */
  .card:not(.grid > .card) {
    margin-top: var(--space-4);
  }
  .card h4 {
    margin: 0;
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .card-head {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
  }
  .logs-controls {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
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
  .body {
    line-height: 1.5;
    font-size: var(--font-size-body-sm);
  }
  .actions-row {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-2);
  }
  .raw {
    margin: 0;
    max-height: 360px;
    overflow: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: var(--space-3);
    color: var(--color-text-primary);
    white-space: pre;
    word-break: break-all;
  }
</style>
