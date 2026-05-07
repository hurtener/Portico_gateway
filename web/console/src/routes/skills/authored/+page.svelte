<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type AuthoredSkillSummary } from '$lib/api';
  import { Badge, Button, EmptyState, PageHeader, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';

  let items: AuthoredSkillSummary[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      const res = await api.listAuthoredSkills();
      items = res.items ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  $: columns = [
    { key: 'skill_id', label: $t('authored.col.skillId'), mono: true },
    { key: 'version', label: $t('authored.col.version'), mono: true, width: '110px' },
    { key: 'status', label: $t('authored.col.status'), width: '120px' },
    { key: 'checksum', label: $t('authored.col.checksum'), mono: true, width: '160px' },
    { key: 'created_at', label: $t('authored.col.created') }
  ];

  function gotoEditor(row: AuthoredSkillSummary) {
    window.location.href = `/skills/authored/${encodeURIComponent(row.skill_id)}`;
  }

  function statusTone(status: string): 'success' | 'warning' | 'neutral' {
    if (status === 'published') return 'success';
    if (status === 'draft') return 'warning';
    return 'neutral';
  }
</script>

<PageHeader title={$t('authored.title')} description={$t('authored.description')}>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
    <Button on:click={() => (window.location.href = '/skills/authored/new')}>
      <IconPlus slot="leading" size={14} />
      {$t('authored.action.new')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<Table {columns} rows={items} onRowClick={gotoEditor}>
  <svelte:fragment slot="cell" let:row let:column>
    {#if column.key === 'skill_id'}
      <a href={`/skills/authored/${encodeURIComponent(row.skill_id)}`}>
        <code>{row.skill_id}</code>
      </a>
    {:else if column.key === 'status'}
      <Badge tone={statusTone(row.status)}>{row.status}</Badge>
    {:else if column.key === 'checksum'}
      <span class="trunc">{row.checksum?.slice(0, 16) ?? ''}…</span>
    {:else}
      {row[column.key] ?? ''}
    {/if}
  </svelte:fragment>
  <svelte:fragment slot="empty">
    <EmptyState
      title={$t('authored.empty.title')}
      description={$t('authored.empty.description')}
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
  .trunc {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-tertiary);
  }
</style>
