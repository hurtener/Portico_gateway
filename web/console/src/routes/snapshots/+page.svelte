<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Snapshot } from '$lib/api';
  import { Badge, Button, EmptyState, IdBadge, PageHeader, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';

  let snapshots: Snapshot[] = [];
  let cursor = '';
  let loading = true;
  let error = '';

  async function refresh(append = false) {
    loading = true;
    error = '';
    try {
      const res = await api.listSnapshots({
        cursor: append ? cursor : undefined,
        limit: 50
      });
      cursor = res.next_cursor || '';
      snapshots = append ? [...snapshots, ...res.snapshots] : res.snapshots;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(() => refresh(false));

  function isPlaygroundSession(sid?: string | null): boolean {
    return typeof sid === 'string' && sid.startsWith('psn_');
  }

  const columns = [
    { key: 'created_at', label: 'Created' },
    { key: 'source', label: 'Source', width: '120px' },
    { key: 'tools', label: 'Tools', align: 'right' as const, width: '80px' },
    { key: 'tenant_id', label: 'Tenant' },
    { key: 'session_id', label: 'Session', width: '180px' },
    { key: 'id', label: 'Snapshot', width: '200px' },
    { key: 'overall_hash', label: 'Fingerprint', width: '160px' }
  ];

  function fmt(t: string): string {
    try {
      return new Date(t).toLocaleString();
    } catch {
      return t;
    }
  }
</script>

<PageHeader title={$t('snapshots.title')} description={$t('snapshots.description')}>
  <div slot="actions">
    <Button variant="secondary" on:click={() => refresh(false)} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<Table
  {columns}
  rows={snapshots}
  empty="No snapshots yet."
  onRowClick={(row) => (window.location.href = `/snapshots/${row.id}`)}
>
  <svelte:fragment slot="cell" let:row let:column>
    {#if column.key === 'id'}
      <a href={`/snapshots/${row.id}`} class="link-row">
        <IdBadge value={row.id} />
      </a>
    {:else if column.key === 'session_id'}
      {#if row.session_id}
        <IdBadge value={row.session_id} />
      {:else}
        <span class="muted">—</span>
      {/if}
    {:else if column.key === 'source'}
      {#if isPlaygroundSession(row.session_id)}
        <Badge tone="info">playground</Badge>
      {:else}
        <Badge tone="neutral">mcp</Badge>
      {/if}
    {:else if column.key === 'tools'}
      <Badge tone="neutral">{row.tools.length}</Badge>
    {:else if column.key === 'created_at'}
      <span class="muted">{fmt(row.created_at)}</span>
    {:else if column.key === 'overall_hash'}
      <IdBadge value={row.overall_hash} chars={8} />
    {:else}
      {row[column.key] ?? ''}
    {/if}
  </svelte:fragment>
  <svelte:fragment slot="empty">
    <EmptyState
      title={$t('snapshots.empty.title')}
      description={$t('snapshots.empty.description')}
      compact
    />
  </svelte:fragment>
</Table>

{#if cursor}
  <div class="more">
    <Button variant="secondary" {loading} on:click={() => refresh(true)}>
      {$t('common.loadMore')}
    </Button>
  </div>
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .muted {
    color: var(--color-text-tertiary);
  }
  .more {
    margin-top: var(--space-4);
    display: flex;
    justify-content: center;
  }
</style>
