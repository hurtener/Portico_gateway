<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type AuditEvent } from '$lib/api';
  import { Badge, Button, EmptyState, Input, PageHeader, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconSearch from 'lucide-svelte/icons/search';

  let events: AuditEvent[] = [];
  let cursor = '';
  let loading = false;
  let error = '';
  let typeFilter = '';
  let pendingType = '';

  async function search(append = false) {
    loading = true;
    error = '';
    try {
      const res = await api.queryAudit({
        type: typeFilter || undefined,
        cursor: append ? cursor : undefined,
        limit: 50
      });
      cursor = res.next_cursor || '';
      events = append ? [...events, ...res.events] : res.events;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  function applyFilter() {
    typeFilter = pendingType;
    cursor = '';
    search(false);
  }

  onMount(() => search(false));

  const columns = [
    { key: 'occurred_at', label: 'When', width: '180px' },
    { key: 'type', label: 'Type', mono: true },
    { key: 'tenant_id', label: 'Tenant' },
    { key: 'session_id', label: 'Session', mono: true },
    { key: 'payload', label: 'Payload' }
  ];

  function fmt(t: string): string {
    try {
      return new Date(t).toLocaleString();
    } catch {
      return t;
    }
  }
</script>

<PageHeader title={$t('audit.title')} description={$t('audit.description')}>
  <form slot="actions" class="filters" on:submit|preventDefault={applyFilter}>
    <Input
      bind:value={pendingType}
      placeholder={$t('audit.filter.placeholder')}
      size="md"
      block={false}
    >
      <IconSearch slot="leading" size={14} />
    </Input>
    <Button type="submit" {loading}>{$t('common.search')}</Button>
  </form>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<Table {columns} rows={events} empty="No events match.">
  <svelte:fragment slot="cell" let:row let:column>
    {#if column.key === 'occurred_at'}
      <span class="muted">{fmt(row.occurred_at)}</span>
    {:else if column.key === 'type'}
      <Badge tone="neutral" mono>{row.type}</Badge>
    {:else if column.key === 'tenant_id'}
      <span class="muted">{row.tenant_id}</span>
    {:else if column.key === 'session_id'}
      <span class="muted">{row.session_id ?? '—'}</span>
    {:else if column.key === 'payload'}
      <pre class="payload">{JSON.stringify(row.payload ?? {}, null, 0)}</pre>
    {:else}
      {row[column.key] ?? ''}
    {/if}
  </svelte:fragment>
  <svelte:fragment slot="empty">
    <EmptyState
      title={$t('audit.empty.title')}
      description={$t('audit.empty.description')}
      compact
    />
  </svelte:fragment>
</Table>

{#if cursor}
  <div class="more">
    <Button variant="secondary" {loading} on:click={() => search(true)}>
      {$t('common.loadMore')}
    </Button>
  </div>
{/if}

<style>
  .filters {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .payload {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
    max-width: 32rem;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .more {
    margin-top: var(--space-4);
    display: flex;
    justify-content: center;
  }
</style>
