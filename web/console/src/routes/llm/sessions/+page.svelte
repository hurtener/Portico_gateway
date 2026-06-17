<script lang="ts">
  /**
   * LLM sessions (Phase 13). Read-only observability list of conversations the
   * gateway brokered: every successful chat completion is recorded (redacted)
   * as a session. Rows link to the transcript detail at /llm/sessions/{chat_id}.
   * Mirrors /llm/health: load + states + MetricStrip + Table + Refresh.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable, type LLMSessionSummary } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    MetricStrip,
    PageHeader,
    Skeleton,
    Table,
    toast
  } from '$lib/components';
  import type { Metric } from '$lib/components/MetricStrip.svelte';
  import IconRefresh from 'lucide-svelte/icons/refresh-cw';

  let sessions: LLMSessionSummary[] = [];
  let loading = true;
  let refreshing = false;
  let unavailable = false;
  let error = '';

  const columns = [
    { key: 'chat_id', label: 'Chat', mono: true },
    { key: 'alias', label: 'Model' },
    { key: 'user_id', label: 'User' },
    { key: 'started_at', label: 'Started' },
    { key: 'summary', label: 'Summary' }
  ];

  onMount(load);

  async function load() {
    error = '';
    try {
      sessions = (await api.listLLMSessions()) ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        unavailable = true;
      } else {
        error = e instanceof Error ? e.message : 'Failed to load sessions';
      }
    } finally {
      loading = false;
      refreshing = false;
    }
  }

  async function refresh() {
    refreshing = true;
    await load();
    if (!error && !unavailable) toast.success('Sessions refreshed');
  }

  function shortId(id: string): string {
    return id.length > 12 ? id.slice(0, 12) : id;
  }
  function fmtTime(s: string): string {
    const d = new Date(s);
    return Number.isNaN(d.getTime()) ? s : d.toLocaleString();
  }

  $: endedCount = sessions.filter((s) => s.ended_at).length;
  $: metrics = [
    { id: 'total', label: 'Sessions', value: sessions.length },
    { id: 'ended', label: 'Ended', value: endedCount },
    { id: 'open', label: 'Open', value: sessions.length - endedCount }
  ] satisfies Metric[];
</script>

<PageHeader
  title="LLM Sessions"
  description="Conversations brokered through the gateway, recorded with secrets redacted."
  compact
>
  <div slot="actions">
    <Button variant="ghost" on:click={refresh} disabled={refreshing || loading || unavailable}>
      <IconRefresh slot="leading" size={14} />{refreshing ? 'Refreshing…' : 'Refresh'}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="LLM gateway not configured"
    description="The LLM session store is not wired in this build."
  />
{:else if loading}
  <div class="stack">
    <Skeleton height="96px" />
    <Skeleton height="200px" />
  </div>
{:else if sessions.length === 0}
  <EmptyState
    title="No sessions yet"
    description="Chats made through the gateway will appear here, most recent first."
  />
{:else}
  <div class="stack">
    <MetricStrip {metrics} label="LLM session summary" />
    <Table {columns} rows={sessions} rowKeyField="chat_id">
      <svelte:fragment slot="cell" let:row let:column>
        {#if column.key === 'chat_id'}
          <a class="chat-link" href={`/llm/sessions/${row.chat_id}`}>{shortId(row.chat_id)}</a>
        {:else if column.key === 'alias'}
          <Badge tone="neutral" mono>{row.alias}</Badge>
        {:else if column.key === 'user_id'}
          {row.user_id || '—'}
        {:else if column.key === 'started_at'}
          {fmtTime(row.started_at)}
        {:else if column.key === 'summary'}
          <span class="summary">{row.summary || '—'}</span>
        {/if}
      </svelte:fragment>
    </Table>
  </div>
{/if}

<style>
  .error {
    color: var(--color-danger-fg, var(--color-text));
    margin: var(--space-2) 0;
  }
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .chat-link {
    font-family: var(--font-mono);
    color: var(--color-accent-primary);
    text-decoration: none;
  }
  .chat-link:hover {
    text-decoration: underline;
  }
  .summary {
    display: inline-block;
    max-width: 42ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-muted);
    vertical-align: bottom;
  }
</style>
