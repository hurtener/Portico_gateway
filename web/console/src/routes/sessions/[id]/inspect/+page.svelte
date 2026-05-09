<script lang="ts">
  /**
   * Session inspector — Phase 11.
   *
   * Three-pane layout:
   *  - top: Timeline component with all five lanes
   *  - centre: Tabs (Trace, Audit, Policy, Drift, Approvals)
   *  - right: StateAtTime scrubber
   *
   * The pinned time `t` lives in the URL hash so reload restores it
   * (and so two tabs on the same session don't pollute each other).
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { browser } from '$app/environment';
  import { goto } from '$app/navigation';
  import {
    Badge,
    Breadcrumbs,
    EmptyState,
    IdBadge,
    PageActionGroup,
    PageHeader,
    StateAtTime,
    Tabs,
    Timeline
  } from '$lib/components';
  import { api, isFeatureUnavailable, type AuditEvent, type SessionBundle } from '$lib/api';
  import { t } from '$lib/i18n';
  import IconDownload from 'lucide-svelte/icons/download';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';

  $: sid = ($page.params.id ?? '').trim();
  $: imported = sid.startsWith('imported:');

  let bundle: SessionBundle | null = null;
  let loading = true;
  let error = '';
  let unavailable = false;
  let pinnedAt: string | null = null;
  let activeTab = 'trace';

  async function load() {
    loading = true;
    error = '';
    unavailable = false;
    try {
      bundle = await api.getSessionBundle(sid);
      // If we have any events at all, default the pin to the first
      // non-empty marker. Operators expect the right pane to show
      // something useful on first paint.
      if (!pinnedAt && bundle) {
        const candidates: string[] = [];
        if (bundle.audit[0]?.occurred_at) candidates.push(bundle.audit[0].occurred_at);
        if (bundle.spans[0]?.started_at) candidates.push(bundle.spans[0].started_at);
        if (bundle.policy[0]?.occurred_at) candidates.push(bundle.policy[0].occurred_at);
        if (candidates.length > 0) {
          candidates.sort();
          pinnedAt = candidates[0];
        }
      }
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        unavailable = true;
      } else {
        error = (e as Error).message ?? String(e);
      }
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    // Restore pin from URL hash on initial load.
    if (browser && window.location.hash.startsWith('#t=')) {
      pinnedAt = decodeURIComponent(window.location.hash.slice(3));
    }
    load();
  });

  function handleSelectTime(ev: CustomEvent<{ at: string }>) {
    pinnedAt = ev.detail.at;
    if (browser) {
      window.history.replaceState(null, '', `#t=${encodeURIComponent(pinnedAt)}`);
    }
  }

  async function exportBundle() {
    try {
      const blob = await api.exportSessionBundle(sid);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${sid}.portico-bundle.tar.gz`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (e) {
      error = `Export failed: ${(e as Error).message ?? e}`;
    }
  }

  function fmtTime(s: string | undefined): string {
    if (!s) return '—';
    const t = Date.parse(s);
    return Number.isFinite(t) ? new Date(t).toLocaleTimeString() : s;
  }

  function policyRule(ev: import('$lib/api').AuditEvent): string {
    const rule = ev.payload?.rule;
    return typeof rule === 'string' ? rule : '—';
  }

  function driftServer(ev: import('$lib/api').AuditEvent): string {
    const server = ev.payload?.server;
    return typeof server === 'string' ? server : '—';
  }

  function toolOf(ev: import('$lib/api').AuditEvent): string {
    const tool = ev.payload?.tool;
    return typeof tool === 'string' ? tool : '';
  }

  let replayError = '';
  let replayPending = false;

  async function handleReplay(ev: CustomEvent<{ event: AuditEvent }>) {
    const auditEv = ev.detail.event;
    const tool = (auditEv.payload?.tool as string | undefined) ?? '';
    if (!tool) {
      replayError = 'No tool name on the selected audit row — cannot replay.';
      return;
    }
    const cid = auditEv.span_id || auditEv.id || `at-${auditEv.occurred_at}`;
    // Pass the full payload through; the playground will run it
    // against the snapshot the inspector loaded.
    const args =
      (auditEv.payload?.arguments as Record<string, unknown> | undefined) ??
      (auditEv.payload as Record<string, unknown> | undefined) ??
      {};
    replayPending = true;
    replayError = '';
    try {
      const res = await api.replaySessionCall(sid, cid, {
        kind: 'tool_call',
        target: tool,
        payload: args,
        snapshot_id: bundle?.session.snapshot_id || undefined
      });
      // Hand off to /playground/runs/<run_id> so the operator sees
      // the live progress.
      goto(`/playground/runs/${encodeURIComponent(res.run.id)}`);
    } catch (e) {
      replayError = (e as Error).message ?? String(e);
    } finally {
      replayPending = false;
    }
  }
</script>

<Breadcrumbs items={[
  { label: $t('nav.sessions'), href: '/sessions' },
  { label: sid, href: `/sessions/${encodeURIComponent(sid)}` },
  { label: 'inspect' }
]} />

<PageHeader title="Session inspector" description="">
  <div slot="meta">
    <span>session <IdBadge value={sid} /></span>
    {#if imported}
      <Badge tone="warning">Imported · read-only</Badge>
    {/if}
  </div>
  <div slot="actions">
    <PageActionGroup
      actions={[
        { label: 'Refresh', icon: IconRefreshCw, onClick: load },
        { label: 'Export bundle', icon: IconDownload, onClick: exportBundle }
      ]}
    />
  </div>
</PageHeader>

{#if replayError}
  <p class="error" role="alert">Replay failed: {replayError}</p>
{/if}
{#if replayPending}
  <p class="status">Starting replay run…</p>
{/if}

{#if loading}
  <p class="status">Loading…</p>
{:else if unavailable}
  <EmptyState
    title="Inspector not configured"
    description="The Phase 11 telemetry surface is not available in this build."
  />
{:else if error}
  <EmptyState title="Failed to load session" description={error} />
{:else if bundle}
  <div class="inspector-grid">
    <Timeline
      spans={bundle.spans}
      audit={bundle.audit}
      policy={bundle.policy}
      drift={bundle.drift}
      approvals={bundle.approvals}
      {pinnedAt}
      on:selectTime={handleSelectTime}
    />

    <div class="centre-pane">
      <Tabs
        tabs={[
          { id: 'trace', label: `Trace (${bundle.spans.length})` },
          { id: 'audit', label: `Audit (${bundle.audit.length})` },
          { id: 'policy', label: `Policy (${bundle.policy.length})` },
          { id: 'drift', label: `Drift (${bundle.drift.length})` },
          { id: 'approvals', label: `Approvals (${bundle.approvals.length})` }
        ]}
        bind:active={activeTab}
      />

      {#if activeTab === 'trace'}
        {#if bundle.spans.length === 0}
          <EmptyState title="No spans" description="This session produced no traces." />
        {:else}
          <ul class="lane-list">
            {#each bundle.spans as span}
              <li>
                <span class="badge">{span.kind}</span>
                <code>{span.name}</code>
                <span class="meta">
                  {fmtTime(span.started_at)} → {fmtTime(span.ended_at)}
                </span>
                <Badge
                  tone={span.status === 'ok'
                    ? 'success'
                    : span.status === 'error'
                      ? 'danger'
                      : 'neutral'}
                >
                  {span.status}
                </Badge>
              </li>
            {/each}
          </ul>
        {/if}
      {:else if activeTab === 'audit'}
        {#if bundle.audit.length === 0}
          <EmptyState title="No audit events" description="" />
        {:else}
          <ul class="lane-list">
            {#each bundle.audit as ev}
              {@const toolName = toolOf(ev)}
              <li>
                <code>{ev.type}</code>
                {#if toolName}
                  <a
                    class="pivot"
                    href="/audit?q={encodeURIComponent(toolName)}"
                    title="Pivot: every session that called this tool"
                  >
                    {toolName}
                  </a>
                {/if}
                <a
                  class="pivot"
                  href="/audit?session_id={encodeURIComponent(sid)}"
                  title="Pivot: every audit event for this session"
                >
                  audit log →
                </a>
                <span class="meta">{fmtTime(ev.occurred_at)}</span>
              </li>
            {/each}
          </ul>
        {/if}
      {:else if activeTab === 'policy'}
        {#if bundle.policy.length === 0}
          <EmptyState title="No policy decisions" description="" />
        {:else}
          <ul class="lane-list">
            {#each bundle.policy as ev}
              <li>
                <Badge tone={ev.type === 'policy.allowed' ? 'success' : 'danger'}>
                  {ev.type.replace('policy.', '')}
                </Badge>
                <code>{policyRule(ev)}</code>
                <span class="meta">{fmtTime(ev.occurred_at)}</span>
              </li>
            {/each}
          </ul>
        {/if}
      {:else if activeTab === 'drift'}
        {#if bundle.drift.length === 0}
          <EmptyState title="No drift events" description="" />
        {:else}
          <ul class="lane-list">
            {#each bundle.drift as ev}
              <li>
                <Badge tone="warning">drift</Badge>
                <code>{driftServer(ev)}</code>
                <span class="meta">{fmtTime(ev.occurred_at)}</span>
              </li>
            {/each}
          </ul>
        {/if}
      {:else if activeTab === 'approvals'}
        {#if bundle.approvals.length === 0}
          <EmptyState title="No approvals" description="" />
        {:else}
          <ul class="lane-list">
            {#each bundle.approvals as appr}
              <li>
                <code>{appr.tool}</code>
                <Badge
                  tone={appr.status === 'approved'
                    ? 'success'
                    : appr.status === 'denied'
                      ? 'danger'
                      : 'neutral'}
                >
                  {appr.status}
                </Badge>
                <span class="meta">{fmtTime(appr.created_at)}</span>
              </li>
            {/each}
          </ul>
        {/if}
      {/if}
    </div>

    <StateAtTime
      {bundle}
      {pinnedAt}
      canReplay={!imported}
      on:replay={handleReplay}
    />
  </div>
{/if}

<style>
  .inspector-grid {
    display: grid;
    grid-template-columns: 1fr 360px;
    grid-template-rows: auto 1fr;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
  .inspector-grid > :global(.timeline-shell) {
    grid-column: 1 / span 2;
  }
  .centre-pane {
    grid-column: 1;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    padding: var(--space-3);
    overflow: auto;
    min-height: 300px;
  }
  .lane-list {
    list-style: none;
    margin: var(--space-3) 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .lane-list li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2);
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border-subtle);
    border-radius: var(--radius-sm);
    font-size: var(--font-size-sm);
  }
  .lane-list code {
    font-family: var(--font-mono);
    font-size: 12px;
  }
  .lane-list .meta {
    margin-left: auto;
    color: var(--color-text-muted);
    font-size: 12px;
  }
  .lane-list .pivot {
    color: var(--color-accent-fg);
    font-size: 11px;
    text-decoration: none;
    border-bottom: 1px dotted var(--color-accent-fg);
    padding-bottom: 1px;
  }
  .lane-list .pivot:hover {
    text-decoration: underline;
  }
  .lane-list .badge {
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-size: 10px;
    color: var(--color-text-muted);
    background: var(--color-surface);
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border);
  }
  .status {
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
  }
  .error {
    color: var(--color-status-error-fg, #b91c1c);
    font-size: var(--font-size-sm);
    padding: var(--space-2);
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-status-error-fg, #b91c1c);
    border-radius: var(--radius-sm);
  }
</style>
