<script lang="ts">
  import { onMount } from 'svelte';
  import { Badge, Button, EmptyState, PageHeader, Toaster } from '$lib/components';
  import { pushToast } from '$lib/components/toast';
  import { api, type PlaygroundCase } from '$lib/api';
  import { t } from '$lib/i18n';

  let cases: PlaygroundCase[] = [];
  let loading = true;

  async function reload() {
    try {
      const res = await api.listPlaygroundCases();
      cases = res.cases ?? [];
    } catch (err) {
      console.error(err);
    } finally {
      loading = false;
    }
  }

  async function replay(c: PlaygroundCase) {
    try {
      const run = await api.replayPlaygroundCase(c.id);
      pushToast({
        tone: run.status === 'ok' ? 'success' : 'info',
        title: `${$t('playground.cases.replay')}: ${run.status}`
      });
    } catch (err) {
      pushToast({ tone: 'danger', title: 'Replay failed' });
      console.error(err);
    }
  }

  async function remove(c: PlaygroundCase) {
    try {
      await api.deletePlaygroundCase(c.id);
      cases = cases.filter((x) => x.id !== c.id);
    } catch (err) {
      console.error(err);
    }
  }

  onMount(reload);
</script>

<PageHeader title={$t('playground.cases.title')} />

{#if loading}
  <p>{$t('common.loading')}</p>
{:else if cases.length === 0}
  <EmptyState title={$t('playground.cases.empty')} description={$t('playground.empty.pickTool')} />
{:else}
  <table class="cases-table">
    <thead>
      <tr>
        <th>{$t('playground.composer.target')}</th>
        <th>{$t('playground.composer.kind')}</th>
        <th>{$t('playground.cases.tags')}</th>
        <th></th>
      </tr>
    </thead>
    <tbody>
      {#each cases as c (c.id)}
        <tr>
          <td>
            <a href={`/playground/cases/${c.id}`}>{c.name}</a>
            <br />
            <span class="muted">{c.target}</span>
          </td>
          <td>
            <Badge tone="info">{c.kind}</Badge>
          </td>
          <td>
            {#each c.tags ?? [] as tg}
              <Badge tone="neutral">{tg}</Badge>
            {/each}
          </td>
          <td>
            <Button on:click={() => replay(c)} variant="secondary"
              >{$t('playground.cases.replay')}</Button
            >
            <Button on:click={() => remove(c)} variant="destructive">{$t('common.delete')}</Button>
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

<Toaster />

<style>
  .cases-table {
    width: 100%;
    border-collapse: collapse;
    background: var(--surface-1);
  }
  .cases-table th,
  .cases-table td {
    text-align: left;
    padding: var(--space-2) var(--space-3);
    border-bottom: 1px solid var(--color-border-subtle);
  }
  .muted {
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
  }
</style>
