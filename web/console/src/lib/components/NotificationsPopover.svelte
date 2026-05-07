<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Popover from './Popover.svelte';
  import { api, type AuditEvent } from '$lib/api';
  import { t } from '$lib/i18n';
  import IconBell from 'lucide-svelte/icons/bell';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import IconAlertCircle from 'lucide-svelte/icons/alert-circle';
  import IconInfo from 'lucide-svelte/icons/info';

  let open = false;
  let events: AuditEvent[] = [];
  let lastSeenAt: string | null = null;
  let pollHandle: ReturnType<typeof setInterval> | null = null;

  // Audit event types we surface as notifications.
  const NOISE_TYPES = new Set<string>([
    'tool_call.failed',
    'policy.denied',
    'audit.dropped',
    'schema.drift',
    'approval.expired',
    'credential.passthrough'
  ]);

  async function refresh() {
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return;
    try {
      const res = await api.queryAudit({ limit: 25 });
      events = (res.events ?? []).filter((e) => NOISE_TYPES.has(e.type)).slice(0, 10);
    } catch {
      // Ignore — the bell goes silent if audit isn't reachable.
    }
  }

  $: unread = events.filter((e) => !lastSeenAt || e.occurred_at > lastSeenAt);

  function markAllRead() {
    if (events.length > 0) lastSeenAt = events[0].occurred_at;
  }

  function fmt(ts: string): string {
    try {
      return new Date(ts).toLocaleString();
    } catch {
      return ts;
    }
  }

  function toneFor(type: string): 'danger' | 'warning' | 'info' {
    if (type.includes('failed') || type.includes('denied') || type === 'audit.dropped')
      return 'danger';
    if (type === 'schema.drift' || type === 'approval.expired') return 'warning';
    return 'info';
  }

  onMount(() => {
    refresh();
    pollHandle = setInterval(refresh, 15_000);
  });
  onDestroy(() => {
    if (pollHandle !== null) clearInterval(pollHandle);
  });
</script>

<Popover placement="bottom-end" bind:open>
  <span slot="trigger" let:toggle>
    <button
      type="button"
      class="bell"
      aria-label={$t('topbar.notifications')}
      title={$t('topbar.notifications')}
      on:click={toggle}
    >
      <IconBell size={16} />
      {#if unread.length > 0}
        <span class="badge" aria-label={`${unread.length} unread`}>
          {unread.length > 9 ? '9+' : unread.length}
        </span>
      {/if}
    </button>
  </span>

  <div class="panel">
    <header class="head">
      <span class="title">{$t('topbar.notifications')}</span>
      {#if unread.length > 0}
        <button type="button" class="mark" on:click={markAllRead}>
          {$t('topbar.notifications.markRead')}
        </button>
      {/if}
    </header>
    {#if events.length === 0}
      <p class="empty">{$t('topbar.notifications.empty')}</p>
    {:else}
      <ul class="list">
        {#each events as e, i (i)}
          <li>
            <a
              class="row tone-{toneFor(e.type)}"
              href={`/audit?type=${encodeURIComponent(e.type)}`}
            >
              <span class="ico" aria-hidden="true">
                {#if toneFor(e.type) === 'danger'}<IconAlertCircle
                    size={14}
                  />{:else if toneFor(e.type) === 'warning'}<IconAlertTriangle
                    size={14}
                  />{:else}<IconInfo size={14} />{/if}
              </span>
              <span class="copy">
                <span class="type">{e.type}</span>
                <span class="when">{fmt(e.occurred_at)}</span>
              </span>
            </a>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</Popover>

<style>
  .bell {
    appearance: none;
    background: transparent;
    border: none;
    cursor: pointer;
    color: var(--color-icon-default);
    width: 32px;
    height: 32px;
    border-radius: var(--radius-pill);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    position: relative;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default);
  }
  .bell:hover {
    background: var(--color-bg-subtle);
    color: var(--color-text-primary);
  }
  .bell:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .badge {
    position: absolute;
    top: -2px;
    right: -2px;
    min-width: 16px;
    height: 16px;
    padding: 0 4px;
    border-radius: 999px;
    background: var(--color-danger);
    color: var(--color-text-inverse);
    font-size: 10px;
    font-weight: var(--font-weight-semibold);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    line-height: 1;
  }
  .panel {
    width: 320px;
    max-height: 400px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3) var(--space-1);
  }
  .title {
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    font-weight: var(--font-weight-medium);
  }
  .mark {
    appearance: none;
    border: none;
    background: transparent;
    color: var(--color-accent-primary);
    font-size: var(--font-size-label);
    cursor: pointer;
    padding: 2px var(--space-2);
    border-radius: var(--radius-xs);
  }
  .mark:hover {
    background: var(--color-accent-primary-subtle);
  }
  .empty {
    text-align: center;
    color: var(--color-text-tertiary);
    padding: var(--space-6) var(--space-3);
    font-size: var(--font-size-body-sm);
    margin: 0;
  }
  .list {
    list-style: none;
    padding: 0;
    margin: 0;
    overflow-y: auto;
  }
  .row {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    text-decoration: none;
  }
  .row:hover {
    background: var(--color-bg-subtle);
  }
  .row:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .ico {
    display: inline-flex;
    margin-top: 2px;
    flex-shrink: 0;
  }
  .tone-danger .ico {
    color: var(--color-danger);
  }
  .tone-warning .ico {
    color: var(--color-warning);
  }
  .tone-info .ico {
    color: var(--color-info);
  }
  .copy {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .type {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
    word-break: break-word;
  }
  .when {
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
  }
</style>
