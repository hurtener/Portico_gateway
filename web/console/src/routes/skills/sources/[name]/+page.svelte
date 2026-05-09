<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type SkillSource, type SourcePack } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    Button,
    CodeBlock,
    EmptyState,
    KeyValueGrid,
    PageHeader,
    Table,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';

  let source: SkillSource | null = null;
  let packs: SourcePack[] = [];
  let loading = true;
  let error = '';

  $: name = $page.params.name ?? '';

  async function refresh() {
    if (!name) return;
    loading = true;
    error = '';
    try {
      source = await api.getSkillSource(name);
      try {
        const out = await api.listSkillSourcePacks(name);
        packs = out.items ?? [];
      } catch {
        packs = [];
      }
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function trigger() {
    try {
      await api.refreshSkillSource(name);
      toast.success($t('sources.action.refresh'));
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);

  $: details = source
    ? [
        { label: 'Driver', value: source.driver, mono: true },
        { label: 'Priority', value: String(source.priority ?? 100) },
        { label: 'Enabled', value: source.enabled ? $t('common.yes') : $t('common.no') },
        {
          label: $t('sources.detail.lastRefresh'),
          value: source.last_refresh_at ?? $t('common.dash')
        },
        { label: $t('sources.detail.lastError'), value: source.last_error ?? $t('common.dash') }
      ]
    : [];

  const packCols = [
    { key: 'id', label: 'ID', mono: true },
    { key: 'version', label: 'Version', mono: true }
  ];
</script>

<PageHeader title={name} description={source?.driver}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[
      { label: $t('nav.skills'), href: '/skills' },
      { label: $t('nav.sources'), href: '/skills/sources' },
      { label: name }
    ]}
  />
  <div slot="meta">
    {#if source}<Badge tone="neutral" mono>{source.driver}</Badge>{/if}
  </div>
  <div slot="actions">
    <Button variant="secondary" on:click={trigger} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('sources.action.refresh')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if !source && !loading}
  <EmptyState title={$t('sources.detail.notFound.title')} />
{:else if source}
  <section class="card">
    <KeyValueGrid items={details} />
    {#if source.config}
      <h3>{$t('sources.form.driver')} config</h3>
      <CodeBlock language="json" code={JSON.stringify(source.config, null, 2)} />
    {/if}
  </section>

  <section class="card">
    <h3>{$t('sources.detail.packs')}</h3>
    {#if packs.length === 0}
      <EmptyState title={$t('common.empty')} compact />
    {:else}
      <Table columns={packCols} rows={packs} />
    {/if}
  </section>
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .card {
    margin-top: var(--space-4);
    background: var(--color-bg-elevated);
    border-radius: var(--radius-md);
    padding: var(--space-4);
  }
  h3 {
    margin: var(--space-3) 0 var(--space-2) 0;
    font-size: var(--font-size-body-sm);
    font-weight: 600;
    color: var(--color-text-secondary);
  }
</style>
