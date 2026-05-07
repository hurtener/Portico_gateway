<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type AppEntry } from '$lib/api';
  import { Badge, Button, EmptyState, KeyValueGrid, PageHeader } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconBoxes from 'lucide-svelte/icons/boxes';

  let items: AppEntry[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      const r = await api.listApps();
      items = r.items ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }
  onMount(refresh);
</script>

<PageHeader title={$t('apps.title')} description={$t('apps.description')}>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if items.length === 0 && !loading}
  <EmptyState title={$t('apps.empty.title')} description={$t('apps.empty.description')} />
{:else}
  <div class="grid">
    {#each items as a (a.uri)}
      <article class="card">
        <header class="card-head">
          <span class="ico"><IconBoxes size={18} /></span>
          <div>
            <h2>{a.name ?? a.uri}</h2>
            {#if a.description}<p class="desc">{a.description}</p>{/if}
          </div>
          {#if a.mimeType}<Badge tone="accent" mono>{a.mimeType}</Badge>{/if}
        </header>
        <KeyValueGrid
          columns={1}
          items={[
            { label: 'Server', value: a.serverId, mono: true },
            { label: 'URI', value: a.uri, mono: true, full: true },
            { label: 'Upstream', value: a.upstreamUri, mono: true, full: true }
          ]}
        />
      </article>
    {/each}
  </div>
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
    gap: var(--space-4);
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-5);
  }
  .card-head {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    margin-bottom: var(--space-4);
  }
  .ico {
    color: var(--color-accent-primary);
    background: var(--color-accent-primary-subtle);
    width: 36px;
    height: 36px;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .card-head > div {
    flex: 1;
    min-width: 0;
  }
  .card h2 {
    margin: 0;
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    word-break: break-word;
  }
  .desc {
    margin: 4px 0 0 0;
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-sm);
  }
</style>
