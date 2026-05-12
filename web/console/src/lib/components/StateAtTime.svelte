<script lang="ts">
  /**
   * StateAtTime — right pane of the inspector. Reconstructs the
   * session's state at a pinned time `t` by walking the audit lane
   * up to `t` and surfacing:
   *  - the active catalog snapshot (always the bound one)
   *  - the most recent tool result before t
   *  - vault refs requested before t (names only — never plaintext)
   *  - active approvals (pending at t)
   *
   * Phase 11 MVP. Replay button is wired but the click handler is
   * exposed via dispatch so the parent inspector decides whether
   * Phase 10 playback is available.
   */
  import { createEventDispatcher } from 'svelte';
  import type { AuditEvent, BundleApproval, BundleSession, SessionBundle } from '$lib/api';
  import IconClock from 'lucide-svelte/icons/clock';
  import IconShield from 'lucide-svelte/icons/shield';
  import IconKey from 'lucide-svelte/icons/key';
  import IconRotateCcw from 'lucide-svelte/icons/rotate-ccw';

  export let bundle: SessionBundle;
  export let pinnedAt: string | null = null;
  /**
   * Disables the replay CTA on imported sessions, where re-running
   * a call against another instance would be misleading.
   */
  export let canReplay = true;

  const dispatch = createEventDispatcher<{
    replay: { event: AuditEvent };
  }>();

  $: pinMs = pinnedAt ? Date.parse(pinnedAt) : Number.POSITIVE_INFINITY;
  $: state = computeState(bundle, pinMs);

  interface ReconstructedState {
    snapshotId: string | undefined;
    lastTool: AuditEvent | null;
    vaultRefs: string[];
    pendingApprovals: BundleApproval[];
  }

  function computeState(b: SessionBundle, pin: number): ReconstructedState {
    const session: BundleSession = b.session;
    let lastTool: AuditEvent | null = null;
    const vaultSet = new Set<string>();
    const pending: BundleApproval[] = [];

    for (const ev of b.audit) {
      const t = Date.parse(ev.occurred_at);
      if (!Number.isFinite(t) || t > pin) continue;
      if (ev.type === 'tool_call.complete' || ev.type === 'tool_call.failed') {
        if (!lastTool || Date.parse(lastTool.occurred_at) <= t) {
          lastTool = ev;
        }
      }
      if (ev.type === 'vault.get' && ev.payload) {
        const name = (ev.payload as Record<string, unknown>).name;
        if (typeof name === 'string') vaultSet.add(name);
      }
    }

    for (const a of b.approvals) {
      const created = Date.parse(a.created_at);
      const decided = a.decided_at ? Date.parse(a.decided_at) : NaN;
      if (created > pin) continue;
      if (Number.isFinite(decided) && decided <= pin) continue;
      pending.push(a);
    }

    return {
      snapshotId: session.snapshot_id || undefined,
      lastTool,
      vaultRefs: Array.from(vaultSet).sort(),
      pendingApprovals: pending
    };
  }

  function fmt(at: string): string {
    const t = Date.parse(at);
    if (!Number.isFinite(t)) return at;
    return new Date(t).toLocaleTimeString();
  }

  function handleReplay() {
    if (!state.lastTool || !canReplay) return;
    dispatch('replay', { event: state.lastTool });
  }
</script>

<aside class="state-at-time">
  <header>
    <div class="header-row">
      <IconClock size={14} />
      <span class="title">State at time</span>
    </div>
    <div class="pinned-at">
      {pinnedAt ? new Date(pinnedAt).toLocaleString() : 'No time pinned'}
    </div>
  </header>

  <section class="card">
    <h4>SNAPSHOT</h4>
    {#if state.snapshotId}
      <code>{state.snapshotId}</code>
    {:else}
      <span class="empty">No snapshot bound</span>
    {/if}
  </section>

  <section class="card">
    <h4>LAST TOOL RESULT</h4>
    {#if state.lastTool}
      <div class="kv">
        <span class="k">type</span>
        <span class="v">{state.lastTool.type}</span>
      </div>
      <div class="kv">
        <span class="k">at</span>
        <span class="v">{fmt(state.lastTool.occurred_at)}</span>
      </div>
      {#if state.lastTool.payload}
        <pre class="payload">{JSON.stringify(state.lastTool.payload, null, 2)}</pre>
      {/if}
      {#if canReplay}
        <button type="button" class="replay-btn" on:click={handleReplay}>
          <IconRotateCcw size={14} />
          Replay this call
        </button>
      {:else}
        <span class="replay-disabled">Replay disabled (imported)</span>
      {/if}
    {:else}
      <span class="empty">No tool calls before pin</span>
    {/if}
  </section>

  <section class="card">
    <h4>VAULT REFS IN SCOPE</h4>
    {#if state.vaultRefs.length > 0}
      <ul class="refs">
        {#each state.vaultRefs as name}
          <li>
            <IconKey size={12} />
            <code>{name}</code>
          </li>
        {/each}
      </ul>
    {:else}
      <span class="empty">No vault refs requested before pin</span>
    {/if}
  </section>

  <section class="card">
    <h4>PENDING APPROVALS</h4>
    {#if state.pendingApprovals.length > 0}
      <ul class="approvals">
        {#each state.pendingApprovals as appr}
          <li>
            <IconShield size={12} />
            <span class="tool">{appr.tool}</span>
            <span class="status">{appr.status}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <span class="empty">None</span>
    {/if}
  </section>
</aside>

<style>
  .state-at-time {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    width: 360px;
    max-width: 100%;
  }
  header {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .header-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-text-muted);
  }
  .title {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--color-text);
  }
  .pinned-at {
    font-size: var(--font-size-sm);
    color: var(--color-text);
  }
  .card {
    padding: var(--space-2);
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border-subtle);
    border-radius: var(--radius-sm);
  }
  h4 {
    font-size: 11px;
    letter-spacing: 0.05em;
    color: var(--color-text-muted);
    margin: 0 0 var(--space-1);
    font-weight: 700;
  }
  .empty {
    color: var(--color-text-muted);
    font-style: italic;
    font-size: var(--font-size-sm);
  }
  code {
    font-family: var(--font-mono);
    font-size: 12px;
  }
  .kv {
    display: flex;
    gap: var(--space-2);
    font-size: var(--font-size-sm);
  }
  .k {
    color: var(--color-text-muted);
    width: 50px;
  }
  .payload {
    margin-top: var(--space-2);
    padding: var(--space-2);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    max-height: 200px;
    overflow: auto;
    font-size: 11px;
    font-family: var(--font-mono);
  }
  .replay-btn {
    margin-top: var(--space-2);
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 4px 8px;
    background: var(--color-accent-bg);
    color: var(--color-accent-fg);
    border: 1px solid var(--color-accent-fg);
    border-radius: var(--radius-sm);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }
  .replay-btn:hover {
    background: var(--color-accent-bg-strong, var(--color-accent-bg));
  }
  .replay-disabled {
    margin-top: var(--space-2);
    display: inline-block;
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
    font-style: italic;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  ul li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--font-size-sm);
  }
  .approvals .tool {
    font-family: var(--font-mono);
  }
  .approvals .status {
    margin-left: auto;
    color: var(--color-text-muted);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
</style>
