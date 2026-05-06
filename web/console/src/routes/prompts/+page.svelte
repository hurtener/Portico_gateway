<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Prompt } from '$lib/api';
  import { Badge, Button, EmptyState, PageHeader, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';

  let prompts: Prompt[] = [];
  let loading = true;
  let error = '';

  function serverID(name: string): string {
    const i = name.indexOf('.');
    return i > 0 ? name.slice(0, i) : '';
  }

  async function refresh() {
    loading = true;
    error = '';
    try {
      const r = await api.listPrompts();
      prompts = r.prompts ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }
  onMount(refresh);

  const columns = [
    { key: 'server', label: 'Server', mono: true, width: '140px' },
    { key: 'name', label: 'Name', mono: true },
    { key: 'description', label: 'Description' },
    { key: 'arguments', label: 'Arguments' }
  ];
</script>

<PageHeader title={$t('prompts.title')} description={$t('prompts.description')}>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<Table {columns} rows={prompts} empty="No prompts available.">
  <svelte:fragment slot="cell" let:row let:column let:value>
    {#if column.key === 'server'}
      <Badge tone="neutral" mono>{serverID(row.name)}</Badge>
    {:else if column.key === 'arguments'}
      <span class="args">
        {#if row.arguments && row.arguments.length > 0}
          {#each row.arguments as a (a.name)}
            <Badge tone={a.required ? 'warning' : 'neutral'} mono>
              {a.name}{a.required ? '*' : ''}
            </Badge>
          {/each}
        {:else}
          <span class="muted">—</span>
        {/if}
      </span>
    {:else}
      {value ?? ''}
    {/if}
  </svelte:fragment>
  <svelte:fragment slot="empty">
    <EmptyState
      title={$t('prompts.empty.title')}
      description={$t('prompts.empty.description')}
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
  .args {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
  .muted {
    color: var(--color-text-tertiary);
  }
</style>
