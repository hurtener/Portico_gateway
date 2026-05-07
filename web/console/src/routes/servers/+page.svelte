<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type ServerSummary } from '$lib/api';
  import { Badge, Button, EmptyState, PageHeader, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';

  let servers: ServerSummary[] = [];
  let loading = true;
  let error = '';

  type StatusTone = 'success' | 'danger' | 'warning' | 'neutral' | 'info';
  function statusTone(s: string): StatusTone {
    const v = s.toLowerCase();
    if (v === 'ready' || v === 'running') return 'success';
    if (v === 'crashed' || v === 'error') return 'danger';
    if (v === 'circuit_open' || v === 'backoff') return 'warning';
    if (v === 'starting') return 'info';
    return 'neutral';
  }

  async function refresh() {
    loading = true;
    error = '';
    try {
      const r = await api.listServers();
      servers = r.items ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function toggle(s: ServerSummary) {
    try {
      if (s.enabled) {
        await api.disableServer(s.id);
      } else {
        await api.enableServer(s.id);
      }
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);

  function gotoServer(row: ServerSummary) {
    window.location.href = `/servers/${encodeURIComponent(row.id)}`;
  }

  $: columns = [
    { key: 'id', label: $t('servers.col.id'), mono: true },
    { key: 'display_name', label: $t('servers.col.displayName') },
    { key: 'transport', label: $t('servers.col.transport') },
    { key: 'runtime_mode', label: $t('servers.col.mode') },
    { key: 'status', label: $t('servers.col.status') },
    { key: 'enabled', label: $t('servers.col.enabled'), align: 'center' as const },
    { key: 'actions', label: '', align: 'right' as const, width: '120px' }
  ];
</script>

<PageHeader title={$t('servers.title')} description={$t('servers.description')}>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
    <Button href="/servers/new">
      <IconPlus slot="leading" size={14} />
      {$t('servers.action.add')}
    </Button>
  </div>
</PageHeader>

{#if error}
  <p class="error">{error}</p>
{/if}

<Table {columns} rows={servers} empty="No servers registered." onRowClick={gotoServer}>
  <svelte:fragment slot="cell" let:row let:column let:value>
    {#if column.key === 'id'}
      <a href={`/servers/${encodeURIComponent(row.id)}`}>{row.id}</a>
    {:else if column.key === 'transport'}
      <Badge tone="neutral" mono>{row.transport}</Badge>
    {:else if column.key === 'runtime_mode'}
      <Badge tone="neutral" mono>{row.runtime_mode}</Badge>
    {:else if column.key === 'status'}
      <Badge tone={statusTone(row.status)}>{row.status}</Badge>
    {:else if column.key === 'enabled'}
      <Badge tone={row.enabled ? 'success' : 'neutral'}>
        {row.enabled ? $t('common.yes') : $t('common.no')}
      </Badge>
    {:else if column.key === 'actions'}
      <Button
        size="sm"
        variant="ghost"
        on:click={(e) => {
          e.stopPropagation();
          toggle(row);
        }}
      >
        {row.enabled ? $t('common.disable') : $t('common.enable')}
      </Button>
    {:else}
      {value ?? ''}
    {/if}
  </svelte:fragment>
  <svelte:fragment slot="empty">
    <EmptyState
      title={$t('servers.empty.title')}
      description={$t('servers.empty.description')}
      compact
    >
      <svelte:fragment slot="actions">
        <Button href="/servers/new">
          <IconPlus slot="leading" size={14} />
          {$t('servers.action.add')}
        </Button>
      </svelte:fragment>
    </EmptyState>
  </svelte:fragment>
</Table>

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
</style>
