<script lang="ts">
  /**
   * Timeline — horizontal lanes that lay every Phase 11 telemetry
   * source on a single time axis. SVG-based so the result is cheap
   * to render, snapshot-able for tests, and naturally scalable.
   *
   * Lanes (top to bottom): spans, audit, policy, drift, approvals.
   * The user can click a marker to pin a time `t`, or drag the
   * cursor across the timeline. The pinned time bubbles up via the
   * `selectTime` event consumed by `StateAtTime`.
   *
   * SVG fills the available width; the parent `.timeline-shell` is
   * sized via CSS to ~150px tall, matching the Phase 11 plan's
   * "top: ~140px" lane.
   */
  import { createEventDispatcher } from 'svelte';
  import type { AuditEvent, BundleApproval, BundleSpan } from '$lib/api';

  export let spans: BundleSpan[] = [];
  export let audit: AuditEvent[] = [];
  export let policy: AuditEvent[] = [];
  export let drift: AuditEvent[] = [];
  export let approvals: BundleApproval[] = [];

  /**
   * Pinned time. When defined, the cursor renders at this position;
   * the parent owns the canonical value so multiple components can
   * stay in sync.
   */
  export let pinnedAt: string | null = null;

  const dispatch = createEventDispatcher<{ selectTime: { at: string } }>();

  // Layout constants — kept here (not in tokens.css) because the
  // SVG calculations need numeric values, not CSS variables.
  const LANES = ['spans', 'audit', 'policy', 'drift', 'approvals'] as const;
  type Lane = (typeof LANES)[number];
  const LANE_LABELS: Record<Lane, string> = {
    spans: 'Spans',
    audit: 'Audit',
    policy: 'Policy',
    drift: 'Drift',
    approvals: 'Approvals'
  };
  const LANE_HEIGHT = 22;
  const LANE_GAP = 4;
  const LEFT_GUTTER = 88; // room for lane labels
  const RIGHT_PADDING = 16;
  const TOP_PADDING = 12;
  const TOTAL_HEIGHT = TOP_PADDING + LANES.length * (LANE_HEIGHT + LANE_GAP);

  // Reactively compute the time domain from every record we receive.
  $: domain = computeDomain(spans, audit, policy, drift, approvals);

  function computeDomain(
    sp: BundleSpan[],
    a: AuditEvent[],
    p: AuditEvent[],
    d: AuditEvent[],
    ap: BundleApproval[]
  ): { from: number; to: number } {
    const stamps: number[] = [];
    sp.forEach((s) => {
      stamps.push(toMs(s.started_at));
      if (s.ended_at) stamps.push(toMs(s.ended_at));
    });
    a.forEach((e) => stamps.push(toMs(e.occurred_at)));
    p.forEach((e) => stamps.push(toMs(e.occurred_at)));
    d.forEach((e) => stamps.push(toMs(e.occurred_at)));
    ap.forEach((r) => {
      stamps.push(toMs(r.created_at));
      if (r.decided_at) stamps.push(toMs(r.decided_at));
    });
    const valid = stamps.filter((n) => Number.isFinite(n));
    if (valid.length === 0) {
      const now = Date.now();
      return { from: now - 60_000, to: now };
    }
    const from = Math.min(...valid);
    const to = Math.max(...valid);
    if (from === to) return { from: from - 1000, to: to + 1000 };
    return { from, to };
  }

  function toMs(s: string | undefined): number {
    if (!s) return NaN;
    const t = Date.parse(s);
    return Number.isFinite(t) ? t : NaN;
  }

  function laneTop(lane: Lane): number {
    const idx = LANES.indexOf(lane);
    return TOP_PADDING + idx * (LANE_HEIGHT + LANE_GAP);
  }

  // Width is responsive — bind to the SVG container and recompute
  // markers when it changes.
  let containerEl: HTMLDivElement;
  let width = 800;
  $: pixelDomain = width - LEFT_GUTTER - RIGHT_PADDING;

  $: if (containerEl) {
    width = containerEl.clientWidth || width;
  }

  function xFor(at: number): number {
    if (!Number.isFinite(at)) return LEFT_GUTTER;
    const ratio = (at - domain.from) / (domain.to - domain.from);
    return LEFT_GUTTER + Math.max(0, Math.min(1, ratio)) * pixelDomain;
  }

  function widthFor(start: number, end: number): number {
    if (!Number.isFinite(start) || !Number.isFinite(end)) return 1;
    const span = (end - start) / (domain.to - domain.from);
    return Math.max(1, span * pixelDomain);
  }

  function onResize() {
    if (containerEl) width = containerEl.clientWidth;
  }

  function handleClick(ev: MouseEvent) {
    const rect = (ev.currentTarget as SVGElement).getBoundingClientRect();
    const x = ev.clientX - rect.left;
    const ratio = (x - LEFT_GUTTER) / pixelDomain;
    if (ratio < 0 || ratio > 1) return;
    const at = domain.from + ratio * (domain.to - domain.from);
    dispatch('selectTime', { at: new Date(at).toISOString() });
  }

  // Color tokens — match the Phase 10.6 substrate vocabulary so the
  // timeline doesn't need its own palette. Colors are pulled from
  // CSS vars at SVG-render time via fill="currentColor" + class.
  function policyColor(decision: string): string {
    if (decision === 'policy.allowed') return 'var(--color-status-ok-fg, #15803d)';
    if (decision === 'policy.denied') return 'var(--color-status-error-fg, #b91c1c)';
    return 'var(--color-warning-fg, #b45309)';
  }
</script>

<svelte:window on:resize={onResize} />

<div class="timeline-shell" bind:this={containerEl}>
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <svg
    role="button"
    tabindex="0"
    aria-label="Session timeline"
    width="100%"
    height={TOTAL_HEIGHT}
    viewBox={`0 0 ${width} ${TOTAL_HEIGHT}`}
    on:click={handleClick}
    on:keydown
  >
    <!-- Lane labels -->
    {#each LANES as lane}
      <text
        x={LEFT_GUTTER - 8}
        y={laneTop(lane) + LANE_HEIGHT / 2 + 4}
        text-anchor="end"
        class="lane-label"
      >
        {LANE_LABELS[lane]}
      </text>
      <line
        x1={LEFT_GUTTER}
        x2={width - RIGHT_PADDING}
        y1={laneTop(lane) + LANE_HEIGHT / 2}
        y2={laneTop(lane) + LANE_HEIGHT / 2}
        class="lane-rule"
      />
    {/each}

    <!-- Spans: rectangles with width = duration -->
    {#each spans as span}
      {#if Number.isFinite(toMs(span.started_at))}
        <rect
          x={xFor(toMs(span.started_at))}
          y={laneTop('spans') + 3}
          width={widthFor(toMs(span.started_at), toMs(span.ended_at))}
          height={LANE_HEIGHT - 6}
          rx="2"
          class="marker-span"
        >
          <title>{span.name} ({span.kind})</title>
        </rect>
      {/if}
    {/each}

    <!-- Audit: small dots -->
    {#each audit as ev}
      {#if Number.isFinite(toMs(ev.occurred_at))}
        <circle
          cx={xFor(toMs(ev.occurred_at))}
          cy={laneTop('audit') + LANE_HEIGHT / 2}
          r="3"
          class="marker-audit"
        >
          <title>{ev.type}</title>
        </circle>
      {/if}
    {/each}

    <!-- Policy: filled circles, color by decision -->
    {#each policy as ev}
      {#if Number.isFinite(toMs(ev.occurred_at))}
        <circle
          cx={xFor(toMs(ev.occurred_at))}
          cy={laneTop('policy') + LANE_HEIGHT / 2}
          r="4"
          fill={policyColor(ev.type)}
        >
          <title>{ev.type}</title>
        </circle>
      {/if}
    {/each}

    <!-- Drift: red triangles -->
    {#each drift as ev}
      {#if Number.isFinite(toMs(ev.occurred_at))}
        {@const cx = xFor(toMs(ev.occurred_at))}
        {@const cy = laneTop('drift') + LANE_HEIGHT / 2}
        <polygon
          points={`${cx - 5},${cy + 4} ${cx + 5},${cy + 4} ${cx},${cy - 5}`}
          class="marker-drift"
        >
          <title>{ev.type}</title>
        </polygon>
      {/if}
    {/each}

    <!-- Approvals: hollow rectangles -->
    {#each approvals as appr}
      {#if Number.isFinite(toMs(appr.created_at))}
        {@const w = widthFor(
          toMs(appr.created_at),
          toMs(appr.decided_at ?? appr.expires_at)
        )}
        <rect
          x={xFor(toMs(appr.created_at))}
          y={laneTop('approvals') + 4}
          width={w}
          height={LANE_HEIGHT - 8}
          rx="2"
          class="marker-approval"
        >
          <title>{appr.tool} ({appr.status})</title>
        </rect>
      {/if}
    {/each}

    <!-- Pinned cursor -->
    {#if pinnedAt && Number.isFinite(toMs(pinnedAt))}
      <line
        x1={xFor(toMs(pinnedAt))}
        x2={xFor(toMs(pinnedAt))}
        y1={TOP_PADDING - 4}
        y2={TOTAL_HEIGHT}
        class="pin-cursor"
      />
    {/if}
  </svg>
</div>

<style>
  .timeline-shell {
    width: 100%;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    padding: var(--space-2);
  }
  svg {
    display: block;
    cursor: crosshair;
  }
  .lane-label {
    fill: var(--color-text-muted);
    font-size: 11px;
    font-family: var(--font-sans);
  }
  .lane-rule {
    stroke: var(--color-border-subtle);
    stroke-width: 1;
    stroke-dasharray: 2 4;
  }
  .marker-span {
    fill: var(--color-accent-bg);
    stroke: var(--color-accent-fg);
    stroke-width: 1;
  }
  .marker-audit {
    fill: var(--color-text-muted);
  }
  .marker-drift {
    fill: var(--color-status-error-fg, #b91c1c);
  }
  .marker-approval {
    fill: none;
    stroke: var(--color-warning-fg, #b45309);
    stroke-width: 1.5;
  }
  .pin-cursor {
    stroke: var(--color-accent-fg);
    stroke-width: 1.5;
    stroke-dasharray: 4 3;
  }
</style>
