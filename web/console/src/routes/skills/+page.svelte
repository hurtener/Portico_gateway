<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type SkillsIndex, type SkillIndexEntry } from '$lib/api';
  import { Badge, Button, EmptyState, PageHeader, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';

  let index: SkillsIndex | null = null;
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      index = await api.listSkills();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function toggle(s: SkillIndexEntry) {
    try {
      if (s.enabled_for_tenant) {
        await api.disableSkill(s.id);
      } else {
        await api.enableSkill(s.id);
      }
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);

  function gotoSkill(row: SkillIndexEntry) {
    window.location.href = `/skills/${encodeURIComponent(row.id)}`;
  }

  const columns = [
    { key: 'id', label: 'ID', mono: true },
    { key: 'title', label: 'Title' },
    { key: 'version', label: 'Version', mono: true, width: '100px' },
    { key: 'status', label: 'Status', width: '140px' },
    { key: 'missing', label: 'Missing tools' },
    { key: 'actions', label: '', align: 'right' as const, width: '120px' }
  ];
</script>

<PageHeader title={$t('skills.title')} description={$t('skills.description')}>
  <div slot="actions">
    <Button variant="secondary" on:click={() => (window.location.href = '/skills/sources')}>
      {$t('nav.sources')}
    </Button>
    <Button variant="secondary" on:click={() => (window.location.href = '/skills/authored')}>
      {$t('nav.authored')}
    </Button>
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<Table {columns} rows={index?.skills ?? []} empty="No skills loaded." onRowClick={gotoSkill}>
  <svelte:fragment slot="cell" let:row let:column>
    {#if column.key === 'id'}
      <a href={`/skills/${encodeURIComponent(row.id)}`}><code class="id">{row.id}</code></a>
    {:else if column.key === 'version'}
      <Badge tone="neutral" mono>{row.version}</Badge>
    {:else if column.key === 'status'}
      {#if (row.missing_tools ?? []).length > 0}
        <Badge tone="danger">{$t('skills.status.missingTools')}</Badge>
      {:else if row.enabled_for_tenant}
        <Badge tone="success">{$t('skills.status.enabled')}</Badge>
      {:else}
        <Badge tone="neutral">{$t('skills.status.disabled')}</Badge>
      {/if}
    {:else if column.key === 'missing'}
      {#if (row.missing_tools ?? []).length > 0}
        <span class="missing">
          {#each row.missing_tools ?? [] as t (t)}
            <Badge tone="danger" mono>{t}</Badge>
          {/each}
        </span>
      {:else}
        <span class="muted">—</span>
      {/if}
    {:else if column.key === 'actions'}
      <Button
        size="sm"
        variant="ghost"
        on:click={(e) => {
          e.stopPropagation();
          toggle(row);
        }}
      >
        {row.enabled_for_tenant ? $t('common.disable') : $t('common.enable')}
      </Button>
    {:else}
      {row[column.key] ?? ''}
    {/if}
  </svelte:fragment>
  <svelte:fragment slot="empty">
    <EmptyState
      title={$t('skills.empty.title')}
      description={$t('skills.empty.description')}
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
  .id {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
  }
  .missing {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
  .muted {
    color: var(--color-text-tertiary);
  }
</style>
