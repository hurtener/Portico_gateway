<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    Badge,
    Button,
    CodeBlock,
    EmptyState,
    Modal,
    PageHeader,
    Tabs,
    Textarea,
    Toaster
  } from '$lib/components';
  import { pushToast } from '$lib/components/toast';
  import { api, type PlaygroundSession, type CorrelationBundle } from '$lib/api';
  import { t } from '$lib/i18n';

  let session: PlaygroundSession | null = null;
  let correlation: CorrelationBundle | null = null;
  let composerKind: 'tool_call' | 'resource_read' | 'prompt_get' = 'tool_call';
  let composerTarget = '';
  let composerArgs = '{}';
  let output = '';
  let activeTab: 'trace' | 'audit' | 'policy' | 'drift' = 'trace';
  let pollHandle: ReturnType<typeof setInterval> | null = null;
  let tabs: Array<{ id: 'trace' | 'audit' | 'policy' | 'drift'; label: string }> = [];

  $: tabs = [
    { id: 'trace', label: $t('playground.tabs.trace') },
    { id: 'audit', label: $t('playground.tabs.audit') },
    { id: 'policy', label: $t('playground.tabs.policy') },
    { id: 'drift', label: $t('playground.tabs.drift') }
  ];

  async function startSession() {
    try {
      session = await api.startPlaygroundSession({});
      pushToast({ tone: 'success', title: $t('playground.session.start') });
      pollCorrelation();
      pollHandle = setInterval(pollCorrelation, 5000);
    } catch (err) {
      pushToast({ tone: 'danger', title: $t('playground.error.startFailed') });
      console.error(err);
    }
  }

  async function endSession() {
    if (!session) return;
    try {
      await api.endPlaygroundSession(session.id);
    } finally {
      if (pollHandle) clearInterval(pollHandle);
      session = null;
      correlation = null;
    }
  }

  async function pollCorrelation() {
    if (!session) return;
    try {
      correlation = await api.getPlaygroundCorrelation(session.id);
    } catch (err) {
      console.error(err);
    }
  }

  async function runCall() {
    if (!session) return;
    output = '';
    let argsParsed: unknown = {};
    try {
      argsParsed = JSON.parse(composerArgs || '{}');
    } catch (err) {
      pushToast({ tone: 'danger', title: 'Invalid JSON in args' });
      return;
    }
    try {
      const env = await api.issuePlaygroundCall(session.id, {
        kind: composerKind,
        target: composerTarget,
        arguments: argsParsed
      });
      const url = `/api/playground/sessions/${encodeURIComponent(session.id)}/calls/${encodeURIComponent(env.call_id)}/stream`;
      const es = new EventSource(url);
      es.addEventListener('chunk', (e) => {
        output += (e as MessageEvent).data + '\n';
      });
      es.addEventListener('end', () => {
        es.close();
        pollCorrelation();
      });
      es.addEventListener('error', () => es.close());
    } catch (err) {
      pushToast({ tone: 'danger', title: $t('playground.error.callFailed') });
      console.error(err);
    }
  }

  let saveModalOpen = false;
  let saveName = '';
  let saveDesc = '';
  async function saveAsCase() {
    if (!composerTarget) return;
    try {
      let argsParsed: unknown = {};
      try {
        argsParsed = JSON.parse(composerArgs || '{}');
      } catch (_) {
        /* ignore */
      }
      await api.createPlaygroundCase({
        name: saveName || composerTarget,
        description: saveDesc,
        kind: composerKind,
        target: composerTarget,
        payload: argsParsed,
        tags: []
      });
      saveModalOpen = false;
      saveName = '';
      saveDesc = '';
      pushToast({ tone: 'success', title: $t('playground.composer.saveAsCase') });
    } catch (err) {
      pushToast({ tone: 'danger', title: 'Save failed' });
      console.error(err);
    }
  }

  function tabTone(status: string): 'success' | 'danger' | 'info' {
    if (status === 'ok') return 'success';
    if (status === 'error') return 'danger';
    return 'info';
  }

  onMount(() => {
    startSession();
  });

  onDestroy(() => {
    if (pollHandle) clearInterval(pollHandle);
  });
</script>

<PageHeader title={$t('playground.title')} description={$t('playground.subtitle')}>
  <svelte:fragment slot="actions">
    {#if session}
      <Badge tone="info">{$t('playground.session.id')}: {session.id.slice(0, 14)}…</Badge>
      <Button variant="secondary" on:click={endSession}>{$t('playground.session.end')}</Button>
    {:else}
      <Button on:click={startSession}>{$t('playground.session.start')}</Button>
    {/if}
  </svelte:fragment>
</PageHeader>

{#if !session}
  <EmptyState title={$t('playground.title')} description={$t('playground.empty.pickTool')} />
{:else}
  <div class="grid">
    <section class="composer">
      <h2>{$t('playground.composer.title')}</h2>
      <label>
        {$t('playground.composer.kind')}
        <select bind:value={composerKind}>
          <option value="tool_call">{$t('playground.composer.kind.tool')}</option>
          <option value="resource_read">{$t('playground.composer.kind.resource')}</option>
          <option value="prompt_get">{$t('playground.composer.kind.prompt')}</option>
        </select>
      </label>
      <label>
        {$t('playground.composer.target')}
        <input type="text" bind:value={composerTarget} placeholder="server.tool" />
      </label>
      <Textarea label={$t('playground.composer.raw')} bind:value={composerArgs} rows={6} mono />
      <div class="row">
        <Button on:click={runCall} disabled={!composerTarget}
          >{$t('playground.composer.run')}</Button
        >
        <Button
          variant="secondary"
          on:click={() => (saveModalOpen = true)}
          disabled={!composerTarget}
        >
          {$t('playground.composer.saveAsCase')}
        </Button>
      </div>
    </section>

    <section class="output">
      <h2>{$t('playground.output.title')}</h2>
      {#if output}
        <CodeBlock language="json" code={output} />
      {:else}
        <p class="muted">{$t('playground.output.empty')}</p>
      {/if}
    </section>

    <aside class="rail">
      <Tabs {tabs} bind:active={activeTab} />
      {#if activeTab === 'trace'}
        {#if correlation && correlation.spans.length}
          <ul>
            {#each correlation.spans as span (span.span_id)}
              <li>
                <strong>{span.name}</strong>
                <Badge tone={tabTone(span.status)}>{span.status}</Badge>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="muted">No spans yet.</p>
        {/if}
      {:else if activeTab === 'audit'}
        {#if correlation && correlation.audits.length}
          <ul>
            {#each correlation.audits as ev, i (i)}
              <li>
                {ev.type} <span class="muted">{new Date(ev.occurred_at).toLocaleTimeString()}</span>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="muted">No audit events yet.</p>
        {/if}
      {:else if activeTab === 'policy'}
        {#if correlation && correlation.policy.length}
          <ul>
            {#each correlation.policy as p, i (i)}
              <li><strong>{p.tool}</strong> → {p.decision} {p.reason ?? ''}</li>
            {/each}
          </ul>
        {:else}
          <p class="muted">No policy decisions yet.</p>
        {/if}
      {:else if correlation && correlation.drift.length}
        <ul>
          {#each correlation.drift as d, i (i)}
            <li>{d.type}</li>
          {/each}
        </ul>
      {:else}
        <p class="muted">No drift events.</p>
      {/if}
    </aside>
  </div>
{/if}

<Modal bind:open={saveModalOpen} title={$t('playground.composer.saveAsCase')}>
  <label>
    Name <input bind:value={saveName} type="text" />
  </label>
  <label>
    Description <input bind:value={saveDesc} type="text" />
  </label>
  <svelte:fragment slot="footer">
    <Button on:click={saveAsCase}>{$t('common.save')}</Button>
  </svelte:fragment>
</Modal>

<Toaster />

<style>
  .grid {
    display: grid;
    grid-template-columns: 1fr 380px;
    grid-template-rows: auto auto;
    gap: var(--space-4);
  }
  .composer,
  .output {
    background: var(--surface-2);
    padding: var(--space-4);
    border-radius: var(--radius-md);
    border: 1px solid var(--color-border-subtle);
  }
  .composer label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin-bottom: var(--space-3);
  }
  .row {
    display: flex;
    gap: var(--space-2);
  }
  .rail {
    grid-row: span 2;
    background: var(--surface-1);
    border: 1px solid var(--color-border-subtle);
    border-radius: var(--radius-md);
    padding: var(--space-3);
  }
  .rail ul {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .muted {
    color: var(--color-text-muted);
  }
</style>
