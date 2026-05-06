<script lang="ts">
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable, type SecretRef } from '$lib/api';
  import { Badge, Button, EmptyState, Input, PageHeader, Table, toast } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconTrash from 'lucide-svelte/icons/trash-2';
  import IconLock from 'lucide-svelte/icons/lock';

  type State = 'loading' | 'ready' | 'unavailable';

  let secrets: SecretRef[] = [];
  let state: State = 'loading';
  let error = '';
  let formTenant = '';
  let formName = '';
  let formValue = '';
  let saving = false;

  async function refresh() {
    try {
      secrets = await api.listSecrets();
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

  async function createSecret() {
    if (!formTenant || !formName || !formValue) {
      error = $t('secrets.form.required');
      return;
    }
    saving = true;
    error = '';
    try {
      await api.putSecret(formTenant, formName, formValue);
      toast.success(
        $t('secrets.toast.saved.title'),
        $t('secrets.toast.saved.description', { tenant: formTenant, name: formName })
      );
      formValue = '';
      await refresh();
    } catch (e) {
      const msg = (e as Error).message;
      error = msg;
      toast.danger($t('secrets.toast.saveFailed.title'), msg);
    } finally {
      saving = false;
    }
  }

  async function deleteSecret(s: SecretRef) {
    if (!confirm($t('secrets.confirmDelete', { tenant: s.tenant_id, name: s.name }))) return;
    try {
      await api.deleteSecret(s.tenant_id, s.name);
      toast.info(
        $t('secrets.toast.deleted.title'),
        $t('secrets.toast.deleted.description', { tenant: s.tenant_id, name: s.name })
      );
      await refresh();
    } catch (e) {
      const msg = (e as Error).message;
      error = msg;
      toast.danger($t('secrets.toast.deleteFailed.title'), msg);
    }
  }

  onMount(refresh);

  $: columns = [
    { key: 'tenant_id', label: $t('secrets.col.tenant') },
    { key: 'name', label: $t('secrets.col.name'), mono: true },
    { key: 'actions', label: '', align: 'right' as const, width: '120px' }
  ];
</script>

<PageHeader title={$t('secrets.title')} description={$t('secrets.description')} />

{#if state === 'unavailable'}
  <EmptyState
    title={$t('secrets.unavailable.title')}
    description={$t('secrets.unavailable.description')}
  >
    <span slot="illustration"><IconLock size={56} aria-hidden="true" /></span>
  </EmptyState>
{:else}
  <section class="form-card">
    <h2 class="form-title">{$t('secrets.form.title')}</h2>
    <form on:submit|preventDefault={createSecret}>
      <Input
        bind:value={formTenant}
        label={$t('secrets.form.tenant')}
        placeholder={$t('secrets.form.tenant.placeholder')}
        required
        block={false}
      />
      <Input
        bind:value={formName}
        label={$t('secrets.form.name')}
        placeholder={$t('secrets.form.name.placeholder')}
        required
        block={false}
      />
      <Input
        bind:value={formValue}
        type="password"
        label={$t('secrets.form.value')}
        placeholder={$t('secrets.form.value.placeholder')}
        required
        block={false}
      />
      <Button type="submit" loading={saving}>
        <IconPlus slot="leading" size={14} />
        {$t('common.save')}
      </Button>
    </form>
  </section>

  {#if error}<p class="error">{error}</p>{/if}

  <section>
    <h2 class="section-title">{$t('secrets.section.existing')}</h2>
    <Table {columns} rows={secrets} empty={$t('common.empty')}>
      <svelte:fragment slot="cell" let:row let:column>
        {#if column.key === 'tenant_id'}
          <Badge tone="neutral">{row.tenant_id}</Badge>
        {:else if column.key === 'actions'}
          <Button
            size="sm"
            variant="ghost"
            on:click={(e) => {
              e.stopPropagation();
              deleteSecret(row);
            }}
          >
            <IconTrash slot="leading" size={14} />
            {$t('common.delete')}
          </Button>
        {:else}
          {row[column.key] ?? ''}
        {/if}
      </svelte:fragment>
      <svelte:fragment slot="empty">
        <EmptyState
          title={$t('secrets.empty.title')}
          description={$t('secrets.empty.description')}
          compact
        />
      </svelte:fragment>
    </Table>
  </section>
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .form-card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-5);
    margin-bottom: var(--space-6);
  }
  .form-title {
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
  }
  .form-card form {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr auto;
    gap: var(--space-3);
    align-items: end;
  }
  @media (max-width: 880px) {
    .form-card form {
      grid-template-columns: 1fr;
    }
  }
  .section-title {
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    margin: 0 0 var(--space-3) 0;
  }
</style>
