<script lang="ts">
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type ServerSpec } from '$lib/api';
  import { PageHeader, ServerForm, Skeleton, toast } from '$lib/components';
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

<PageHeader title={$t('servers.edit.title')} description={id} />

{#if !loaded}
  <div class="loading-stack">
    <Skeleton height="2.5rem" />
    <Skeleton height="2.5rem" />
    <Skeleton height="2.5rem" />
    <Skeleton height="6rem" />
  </div>
{:else}
  <ServerForm mode="edit" {initial} {saving} {error} on:submit={onSubmit} on:cancel={onCancel} />
{/if}

<style>
  .loading-stack {
    display: grid;
    gap: var(--space-3);
    max-width: 720px;
  }
</style>
