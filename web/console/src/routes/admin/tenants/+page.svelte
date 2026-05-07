<script lang="ts">
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable, type Tenant } from '$lib/api';
  import { Badge, Button, EmptyState, PageHeader, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconUsers from 'lucide-svelte/icons/users';

  type State = 'loading' | 'ready' | 'unavailable';
  let tenants: Tenant[] = [];
  let state: State = 'loading';
  let error = '';

  async function refresh() {
    try {
      tenants = await api.listTenants();
      state = 'ready';
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        state = 'unavailable';
        return;
      }
      error = (e as Error).message;
      state = 'ready';
    }
  }

  onMount(refresh);

  $: columns = [
    { key: 'id', label: $t('tenants.field.id'), mono: true },
    { key: 'display_name', label: $t('tenants.field.displayName') },
    { key: 'runtime_mode', label: $t('tenants.field.runtimeMode') },
    { key: 'status', label: $t('tenants.field.status') },
    {
      key: 'max_concurrent_sessions',
      label: $t('tenants.field.maxSessions'),
      align: 'right' as const
    },
    { key: 'max_requests_per_minute', label: $t('tenants.field.maxRpm'), align: 'right' as const }
  ];
</script>

<PageHeader title={$t('tenants.title')} description={$t('tenants.subtitle')}>
  <Button slot="actions" href="/admin/tenants/new">
    <IconPlus slot="leading" size={14} />
    {$t('crud.create')}
  </Button>
</PageHeader>

{#if state === 'unavailable'}
  <EmptyState title={$t('tenants.title')} description={$t('crud.permissionDenied')}>
    <span slot="illustration"><IconUsers size={56} aria-hidden="true" /></span>
  </EmptyState>
{:else}
  {#if error}<p class="error">{error}</p>{/if}
  <Table {columns} rows={tenants} empty={$t('common.empty')}>
    <svelte:fragment slot="cell" let:row let:column>
      {#if column.key === 'status'}
        <Badge tone={row.status === 'archived' ? 'warning' : 'success'}>
          {row.status === 'archived' ? $t('tenants.status.archived') : $t('tenants.status.active')}
        </Badge>
      {:else if column.key === 'id'}
        <a href={`/admin/tenants/${encodeURIComponent(row.id)}`} class="link">{row.id}</a>
      {:else}
        {row[column.key] ?? ''}
      {/if}
    </svelte:fragment>
  </Table>
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-3) 0;
  }
  .link {
    color: var(--color-accent-primary);
    text-decoration: none;
  }
  .link:hover {
    text-decoration: underline;
  }
</style>
