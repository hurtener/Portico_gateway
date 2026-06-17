<script lang="ts">
  /**
   * LLM session transcript (Phase 13). The redacted conversation for one
   * brokered chat: GET /api/llm/sessions/{chat_id}. Read-only — message
   * content was redacted at record time, so secrets never reach the browser.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, HTTPError, isFeatureUnavailable, type LLMSessionTranscript } from '$lib/api';
  import { Badge, Button, EmptyState, KeyValueGrid, PageHeader, Skeleton } from '$lib/components';
  import IconArrowLeft from 'lucide-svelte/icons/arrow-left';

  let transcript: LLMSessionTranscript | null = null;
  let loading = true;
  let notFound = false;
  let unavailable = false;
  let error = '';

  const chatId = $page.params.chat_id ?? '';

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    notFound = false;
    try {
      transcript = await api.getLLMSession(chatId);
      unavailable = false;
    } catch (e) {
      if (e instanceof HTTPError && e.status === 404) {
        notFound = true;
      } else if (isFeatureUnavailable(e)) {
        unavailable = true;
      } else {
        error = e instanceof Error ? e.message : 'Failed to load transcript';
      }
    } finally {
      loading = false;
    }
  }

  function fmtTime(s: string | undefined): string {
    if (!s) return '—';
    const d = new Date(s);
    return Number.isNaN(d.getTime()) ? s : d.toLocaleString();
  }

  function roleTone(role: string): 'accent' | 'success' | 'neutral' | 'warning' {
    if (role === 'user') return 'accent';
    if (role === 'assistant') return 'success';
    if (role === 'tool') return 'warning';
    return 'neutral';
  }

  $: meta = transcript
    ? [
        { label: 'Model', value: transcript.alias },
        { label: 'User', value: transcript.user_id || '—' },
        { label: 'Started', value: fmtTime(transcript.started_at) },
        { label: 'Ended', value: fmtTime(transcript.ended_at) }
      ]
    : [];
</script>

<PageHeader
  title="Session transcript"
  description="Redacted conversation brokered through the gateway."
  compact
>
  <div slot="actions">
    <Button href="/llm/sessions" variant="ghost">
      <IconArrowLeft slot="leading" size={14} />Back to sessions
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="LLM gateway not configured"
    description="The LLM session store is not wired in this build."
  />
{:else if notFound}
  <EmptyState
    title="Session not found"
    description="No transcript exists for this chat id, or it belongs to another tenant."
  >
    <svelte:fragment slot="actions">
      <Button href="/llm/sessions" variant="secondary">Back to sessions</Button>
    </svelte:fragment>
  </EmptyState>
{:else if loading}
  <div class="stack">
    <Skeleton height="80px" />
    <Skeleton height="240px" />
  </div>
{:else if transcript}
  <div class="stack">
    <section class="meta">
      <KeyValueGrid items={meta} />
      <p class="chatid mono">{transcript.chat_id}</p>
    </section>

    {#if transcript.messages.length === 0}
      <EmptyState title="No messages" description="This session has no recorded messages." />
    {:else}
      <ol class="messages">
        {#each transcript.messages as m (m.seq)}
          <li class="msg" class:assistant={m.role === 'assistant'}>
            <div class="msg-head">
              <Badge tone={roleTone(m.role)}>{m.role}</Badge>
              {#if m.tool_call_id}<span class="muted mono">tool_call {m.tool_call_id}</span>{/if}
              <span class="muted ts">{fmtTime(m.timestamp)}</span>
            </div>
            <div class="msg-body">{m.content}</div>
          </li>
        {/each}
      </ol>
    {/if}
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
  .meta {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-4);
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-default);
    border-radius: var(--radius-lg);
  }
  .chatid {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
    margin: 0;
  }
  .messages {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .msg {
    padding: var(--space-3);
    border: 1px solid var(--color-border-default);
    border-radius: var(--radius-md);
    background: var(--color-bg-subtle, var(--color-bg-elevated));
  }
  .msg.assistant {
    background: var(--color-bg-elevated);
    border-color: var(--color-border-strong);
  }
  .msg-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-bottom: var(--space-2);
  }
  .msg-body {
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--color-text-primary);
    font-size: var(--font-size-body-md);
  }
  .ts {
    margin-left: auto;
  }
  .muted {
    color: var(--color-text-muted);
    font-size: var(--font-size-label);
  }
  .mono {
    font-family: var(--font-mono);
  }
</style>
