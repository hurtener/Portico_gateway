<script lang="ts">
  /**
   * Edit server — Phase 10.8 Step 5 form-page sub-vocabulary.
   *
   * Thin wrapper around ServerForm; this rewrite adds Breadcrumbs
   * back to the parent server detail and wraps the loading skeleton
   * in a .card so the layout doesn't reflow when ServerForm appears.
   * ServerForm itself is out of scope (its own audit comes in a
   * follow-up).
   */
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type ServerSpec } from '$lib/api';
  import { Breadcrumbs, PageHeader, ServerForm, Skeleton, toast } from '$lib/components';
  import { t } from '$lib/i18n';

  $: id = $page.params.id ?? '';

  let initial: Partial<ServerSpec> = {};
  let loaded = false;
  let saving = false;
  let error = '';

  async function load() {
    try {
      initial = await api.getServer(id);
      loaded = true;
    } catch (e) {
      error = (e as Error).message;
      loaded = true;
    }
  }

  async function onSubmit(e: CustomEvent<Partial<ServerSpec>>) {
    saving = true;
    error = '';
    try {
      await api.upsertServer(e.detail);
      toast.success($t('crud.updatedToast'), e.detail.id ?? id);
      void goto(`/servers/${encodeURIComponent(id)}`);
    } catch (err) {
      error = (err as Error).message;
    } finally {
      saving = false;
    }
  }

  function onCancel() {
    void goto(`/servers/${encodeURIComponent(id)}`);
  }

  onMount(load);
</script>

<PageHeader title={$t('servers.edit.title')} description={id} compact>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[
      { label: $t('nav.servers'), href: '/servers' },
      { label: id, href: `/servers/${encodeURIComponent(id)}` },
      { label: $t('common.edit') }
    ]}
  />
</PageHeader>

{#if !loaded}
  <section class="card">
    <div class="loading-stack">
      <Skeleton height="2.5rem" />
      <Skeleton height="2.5rem" />
      <Skeleton height="2.5rem" />
      <Skeleton height="6rem" />
    </div>
  </section>
{:else}
  <ServerForm mode="edit" {initial} {saving} {error} on:submit={onSubmit} on:cancel={onCancel} />
{/if}

<style>
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    margin-top: var(--space-4);
  }
  .loading-stack {
    display: grid;
    gap: var(--space-3);
    max-width: 720px;
  }
</style>
