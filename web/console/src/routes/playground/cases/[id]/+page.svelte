<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { Badge, Button, CodeBlock, EmptyState, PageHeader, Toaster } from '$lib/components';
  import { pushToast } from '$lib/components/toast';
  import { api, type PlaygroundCase, type PlaygroundRun } from '$lib/api';
  import { t } from '$lib/i18n';

  let id = '';
  let detail: PlaygroundCase | null = null;
  let runs: PlaygroundRun[] = [];

  async function reload() {
    try {
      detail = await api.getPlaygroundCase(id);
      const r = await api.caseRuns(id);
      runs = r.runs ?? [];
    } catch (err) {
      console.error(err);
    }
  }

  async function replay() {
    if (!detail) return;
    try {
      const run = await api.replayPlaygroundCase(detail.id);
      pushToast({
        tone: run.status === 'ok' ? 'success' : 'info',
        title: `${$t('playground.cases.replay')}: ${run.status}`
      });
      reload();
    } catch (err) {
      pushToast({ tone: 'danger', title: 'Replay failed' });
      console.error(err);
    }
  }

  onMount(() => {
    id = $page.params.id ?? '';
    reload();
  });
</script>

<PageHeader title={$t('playground.cases.detail')} description={$t('playground.subtitle')}>
  <svelte:fragment slot="actions">
    <Button on:click={replay}>{$t('playground.cases.replay')}</Button>
  </svelte:fragment>
</PageHeader>

{#if detail}
  <section class="detail">
    <h2>{detail.name}</h2>
    <p class="muted">{detail.description ?? ''}</p>
    <p>
      <Badge tone="info">{detail.kind}</Badge>
      <code>{detail.target}</code>
    </p>
    <CodeBlock language="json" code={JSON.stringify(detail.payload, null, 2)} />
  </section>

  <section>
    <h3>{$t('playground.cases.history')}</h3>
    {#if runs.length === 0}
      <EmptyState
        title={$t('playground.cases.history')}
        description={$t('playground.empty.pickTool')}
      />
    {:else}
      <ul>
        {#each runs as run (run.id)}
          <li>
            <strong>{new Date(run.started_at).toLocaleString()}</strong>
            <Badge tone={run.status === 'ok' ? 'success' : 'danger'}>{run.status}</Badge>
            {#if run.drift_detected}
              <Badge tone="warning">{$t('playground.runs.drift')}</Badge>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{:else}
  <p>{$t('common.loading')}</p>
{/if}

<Toaster />

<style>
  .detail {
    margin-bottom: var(--space-4);
  }
  .muted {
    color: var(--color-text-muted);
  }
</style>
