<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Resource } from '$lib/api';
  import { Badge, Button, EmptyState, PageHeader, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';

  let resources: Resource[] = [];
  let loading = true;
  let error = '';

  function serverIDFor(r: Resource): string {
    if (r._meta && typeof r._meta === 'object' && 'serverID' in r._meta) {
      return String((r._meta as { serverID?: string }).serverID ?? '');
    }
    if (r.uri.startsWith('mcp+server://')) return r.uri.slice('mcp+server://'.length).split('/')[0];
    if (r.uri.startsWith('ui://')) return r.uri.slice('ui://'.length).split('/')[0];
    return '';
  }

  async function refresh() {
    loading = true;
    error = '';
    try {
      const r = await api.listResources();
      resources = r.resources ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }
  onMount(refresh);

  $: columns = [
    { key: 'server', label: $t('resources.col.server'), mono: true, width: '160px' },
    { key: 'uri', label: $t('resources.col.uri'), mono: true },
    { key: 'name', label: $t('resources.col.name') },
    { key: 'mimeType', label: $t('resources.col.mime'), mono: true, width: '160px' }
  ];

  $: rows = resources.map((r) => ({
    server: serverIDFor(r),
    uri: r.uri,
    name: r.name ?? '',
    mimeType: r.mimeType ?? ''
  }));
</script>

<PageHeader title={$t('resources.title')} description={$t('resources.description')}>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<Table {columns} {rows} empty={$t('common.empty')}>
  <svelte:fragment slot="cell" let:row let:column let:value>
    {#if column.key === 'server'}
      <Badge tone="neutral" mono>{row.server}</Badge>
    {:else}
      {value ?? ''}
    {/if}
  </svelte:fragment>
  <svelte:fragment slot="empty">
    <EmptyState
      title={$t('resources.empty.title')}
      description={$t('resources.empty.description')}
      compact
    />
  </svelte:fragment>
</Table>

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
</style>
