<script lang="ts">
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable, type SkillSource } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    Input,
    Modal,
    PageHeader,
    Select,
    Table,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconTrash from 'lucide-svelte/icons/trash-2';

  type State = 'loading' | 'ready' | 'unavailable';

  let sources: SkillSource[] = [];
  let state: State = 'loading';
  let error = '';

  // Form state
  let showForm = false;
  let formName = '';
  let formDriver = 'git';
  let formURL = '';
  let formBranch = '';
  let formFeed = '';
  let formCredential = '';
  let formPriorityStr = '100';
  let saving = false;

  async function refresh() {
    try {
      const out = await api.listSkillSources();
      sources = out.items ?? [];
      state = 'ready';
      error = '';
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        state = 'unavailable';
        return;
      }
      error = (e as Error).message;
      state = 'ready';
    }
  }

  function resetForm() {
    formName = '';
    formDriver = 'git';
    formURL = '';
    formBranch = '';
    formFeed = '';
    formCredential = '';
    formPriorityStr = '100';
  }

  async function submitForm() {
    if (!formName || !formDriver) {
      error = $t('sources.form.required');
      return;
    }
    saving = true;
    error = '';
    let config: Record<string, unknown> = {};
    if (formDriver === 'git') {
      config = { url: formURL, branch: formBranch };
    } else if (formDriver === 'http') {
      config = { feed_url: formFeed };
    }
    try {
      await api.upsertSkillSource({
        name: formName,
        driver: formDriver,
        config,
        credential_ref: formCredential || undefined,
        priority: Number(formPriorityStr) || 100,
        enabled: true
      });
      toast.success($t('sources.toast.saved.title'));
      showForm = false;
      resetForm();
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      saving = false;
    }
  }

  async function refreshSource(s: SkillSource) {
    try {
      await api.refreshSkillSource(s.name);
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  async function deleteSource(s: SkillSource) {
    if (!confirm(`Delete source ${s.name}?`)) return;
    try {
      await api.deleteSkillSource(s.name);
      toast.info($t('sources.toast.deleted.title'));
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);

  $: columns = [
    { key: 'name', label: $t('sources.col.name'), mono: true },
    { key: 'driver', label: $t('sources.col.driver') },
    { key: 'priority', label: $t('sources.col.priority'), width: '90px' },
    { key: 'enabled', label: $t('sources.col.enabled'), width: '90px' },
    { key: 'last', label: $t('sources.col.lastRefresh') },
    { key: 'actions', label: '', align: 'right' as const, width: '160px' }
  ];

  function gotoSource(row: SkillSource) {
    window.location.href = `/skills/sources/${encodeURIComponent(row.name)}`;
  }
</script>

<PageHeader title={$t('sources.title')} description={$t('sources.description')}>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
    <Button on:click={() => (showForm = true)}>
      <IconPlus slot="leading" size={14} />
      {$t('sources.action.add')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if state === 'unavailable'}
  <EmptyState title={$t('sources.empty.title')} description={$t('sources.empty.description')} />
{:else}
  <Table {columns} rows={sources} onRowClick={gotoSource}>
    <svelte:fragment slot="cell" let:row let:column>
      {#if column.key === 'name'}
        <a href={`/skills/sources/${encodeURIComponent(row.name)}`}><code>{row.name}</code></a>
      {:else if column.key === 'driver'}
        <Badge tone="neutral" mono>{row.driver}</Badge>
      {:else if column.key === 'enabled'}
        {#if row.enabled}
          <Badge tone="success">{$t('common.yes')}</Badge>
        {:else}
          <Badge tone="neutral">{$t('common.no')}</Badge>
        {/if}
      {:else if column.key === 'last'}
        {#if row.last_error}
          <Badge tone="danger">{row.last_error}</Badge>
        {:else if row.last_refresh_at}
          <span class="muted">{row.last_refresh_at}</span>
        {:else}
          <span class="muted">{$t('common.dash')}</span>
        {/if}
      {:else if column.key === 'actions'}
        <span class="actions">
          <Button
            variant="ghost"
            size="sm"
            on:click={(e) => {
              e.stopPropagation();
              refreshSource(row);
            }}
          >
            <IconRefreshCw slot="leading" size={14} />
            {$t('sources.action.refresh')}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            on:click={(e) => {
              e.stopPropagation();
              deleteSource(row);
            }}
          >
            <IconTrash slot="leading" size={14} />
            {$t('common.delete')}
          </Button>
        </span>
      {:else}
        {row[column.key] ?? ''}
      {/if}
    </svelte:fragment>
    <svelte:fragment slot="empty">
      <EmptyState
        title={$t('sources.empty.title')}
        description={$t('sources.empty.description')}
        compact
      />
    </svelte:fragment>
  </Table>
{/if}

<Modal bind:open={showForm} title={$t('sources.action.add')}>
  <form on:submit|preventDefault={submitForm} class="form">
    <Input bind:value={formName} label={$t('sources.form.name')} required />
    <Select
      bind:value={formDriver}
      label={$t('sources.form.driver')}
      options={[
        { value: 'git', label: 'git' },
        { value: 'http', label: 'http' }
      ]}
    />
    {#if formDriver === 'git'}
      <Input bind:value={formURL} label={$t('sources.form.config.url')} required />
      <Input bind:value={formBranch} label={$t('sources.form.config.branch')} />
    {:else if formDriver === 'http'}
      <Input bind:value={formFeed} label={$t('sources.form.config.feedUrl')} required />
    {/if}
    <Input bind:value={formCredential} label={$t('sources.form.credentialRef')} />
    <Input bind:value={formPriorityStr} label={$t('sources.form.priority')} type="number" />
    <div class="form-actions">
      <Button type="submit" loading={saving}>
        {$t('common.save')}
      </Button>
      <Button
        variant="ghost"
        type="button"
        on:click={() => {
          showForm = false;
        }}
      >
        {$t('common.cancel')}
      </Button>
    </div>
  </form>
</Modal>

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .muted {
    color: var(--color-text-tertiary);
  }
  .actions {
    display: inline-flex;
    gap: var(--space-2);
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .form-actions {
    display: flex;
    gap: var(--space-2);
    justify-content: flex-end;
  }
</style>
