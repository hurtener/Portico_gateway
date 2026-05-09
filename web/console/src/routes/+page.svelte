<script lang="ts">
  /**
   * Root landing — Phase 10.8 Step 3 redesign.
   *
   * The pre-10.6 landing predated the design vocabulary: a 30%-of-
   * viewport hero, a bespoke `DashboardTile` grid, and three hand-
   * rolled `<header class="block-head">` sections. This rewrite
   * compresses the hero, replaces the tile grid with `MetricStrip`,
   * and moves the "Recent X" blocks into `.card` sections with the
   * `<h4>` SECTION-LABEL header that the redesigned list pages use —
   * so navigating between landing and any list page reads as a
   * single coherent product.
   *
   * `DashboardTile` retires with this PR; nothing else imports it.
   * `Sparkline` retires from the landing too — the metric values
   * stand on their own without the visual noise.
   */
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import {
    api,
    isFeatureUnavailable,
    type Approval,
    type AuditEvent,
    type Snapshot
  } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    IdBadge,
    Logo,
    MetricStrip,
    Table
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconArrowRight from 'lucide-svelte/icons/arrow-right';
  import IconActivity from 'lucide-svelte/icons/activity';
  import IconShield from 'lucide-svelte/icons/shield';
  import IconHeartPulse from 'lucide-svelte/icons/heart-pulse';
  import IconClock from 'lucide-svelte/icons/clock';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import type { ComponentType } from 'svelte';

  type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'accent';

  let healthOk: boolean | null = null;
  let readyOk: boolean | null = null;

  let approvals: Approval[] | null = null;
  let approvalsUnavailable = false;

  let snapshots: Snapshot[] | null = null;
  let snapshotsUnavailable = false;

  let auditEvents: AuditEvent[] | null = null;
  let auditUnavailable = false;
  let driftCount24h = 0;
  let sessionsCount24h = 0;

  function fmtRelative(iso: string | null | undefined): string {
    if (!iso) return $t('landing.relTime.never');
    const ts = new Date(iso).getTime();
    if (isNaN(ts)) return $t('landing.relTime.never');
    const delta = Date.now() - ts;
    const m = Math.floor(delta / 60_000);
    if (m < 1) return $t('landing.relTime.justNow');
    if (m < 60) return $t('landing.relTime.minutes', { n: m });
    const h = Math.floor(m / 60);
    if (h < 24) return $t('landing.relTime.hours', { n: h });
    const d = Math.floor(h / 24);
    return $t('landing.relTime.days', { n: d });
  }

  function fmtTime(iso: string): string {
    try {
      return new Date(iso).toLocaleTimeString();
    } catch {
      return iso;
    }
  }

  function isPlaygroundSession(sid: string | null | undefined): boolean {
    return typeof sid === 'string' && sid.startsWith('psn_');
  }

  function countLast24h(events: AuditEvent[], match: (e: AuditEvent) => boolean): number {
    let n = 0;
    const now = Date.now();
    for (const e of events) {
      if (!match(e)) continue;
      const ts = new Date(e.occurred_at).getTime();
      if (isNaN(ts)) continue;
      const ageH = (now - ts) / 3_600_000;
      if (ageH >= 0 && ageH < 24) n++;
    }
    return n;
  }

  onMount(async () => {
    try {
      healthOk = (await api.health()).status === 'ok';
    } catch {
      healthOk = false;
    }
    try {
      const r = await api.ready();
      readyOk = r.status === 'ok' || r.status === 'ready';
    } catch {
      readyOk = false;
    }

    try {
      approvals = (await api.listApprovals()) ?? [];
    } catch (e) {
      if (isFeatureUnavailable(e)) approvalsUnavailable = true;
      approvals = [];
    }

    try {
      const res = await api.listSnapshots({ limit: 5 });
      snapshots = res.snapshots ?? [];
    } catch (e) {
      if (isFeatureUnavailable(e)) snapshotsUnavailable = true;
      snapshots = [];
    }

    try {
      const res = await api.queryAudit({ limit: 50 });
      auditEvents = res.events ?? [];
      driftCount24h = countLast24h(
        auditEvents,
        (e) => e.type === 'schema.drift' && !isPlaygroundSession(e.session_id)
      );
      sessionsCount24h = countLast24h(
        auditEvents,
        (e) => e.type === 'session.created' || e.type === 'tool_call.complete'
      );
    } catch (e) {
      if (isFeatureUnavailable(e)) auditUnavailable = true;
      auditEvents = [];
    }
  });

  $: pendingApprovals = approvals ? approvals.length : 0;
  $: lastSnapshotAge =
    snapshots && snapshots.length > 0 ? fmtRelative(snapshots[0].created_at) : null;

  // schema.drift events firing against playground sessions (psn_*) are
  // operator-noise; suppress them in the surfaced audit list.
  $: noticeableAudit = (auditEvents ?? [])
    .filter((e) => {
      if (e.type === 'schema.drift' && isPlaygroundSession(e.session_id)) return false;
      return (
        e.type === 'tool_call.failed' ||
        e.type === 'policy.denied' ||
        e.type === 'schema.drift' ||
        e.type === 'audit.dropped' ||
        e.type === 'approval.expired'
      );
    })
    .slice(0, 5);

  $: approvalRows = (approvals ?? []).slice(0, 5);

  $: snapshotColumns = [
    { key: 'id', label: $t('snapshots.col.id'), mono: true },
    { key: 'source', label: $t('snapshots.col.source'), width: '110px' },
    { key: 'session_id', label: $t('snapshots.col.session'), mono: true },
    { key: 'tools', label: $t('snapshots.col.tools'), align: 'right' as const, width: '80px' },
    { key: 'created_at', label: $t('snapshots.col.created') }
  ];
  $: approvalColumns = [
    { key: 'tool', label: $t('approvals.col.tool'), mono: true },
    { key: 'risk_class', label: $t('approvals.col.risk'), width: '140px' },
    { key: 'created_at', label: $t('approvals.col.created') }
  ];
  $: auditColumns = [
    { key: 'occurred_at', label: $t('audit.col.when'), width: '160px' },
    { key: 'type', label: $t('audit.col.type'), mono: true },
    { key: 'session_id', label: $t('audit.col.session'), mono: true }
  ];

  function riskTone(rc: string): Tone {
    const v = rc.toLowerCase();
    if (v === 'destructive' || v === 'sensitive_read') return 'danger';
    if (v === 'external_side_effect') return 'warning';
    if (v === 'idempotent_read') return 'info';
    return 'neutral';
  }

  // === Substrate for KPI strip ========================================

  /**
   * "System" combines health + ready into a single tone. Health is the
   * load-bearing signal; ready going false alone reads as "starting up"
   * (warning) rather than "down" (danger).
   */
  function systemValue(): string {
    if (healthOk === null && readyOk === null) return '—';
    if (healthOk === false) return $t('landing.system.down');
    if (readyOk === false) return $t('landing.system.degraded');
    return $t('landing.system.healthy');
  }
  function systemTone(): 'success' | 'warning' | 'danger' | 'default' {
    if (healthOk === null && readyOk === null) return 'default';
    if (healthOk === false) return 'danger';
    if (readyOk === false) return 'warning';
    return 'success';
  }

  $: metrics = [
    {
      id: 'system',
      label: $t('landing.metric.system'),
      value: systemValue(),
      helper: $t('landing.metric.system.helper'),
      icon: IconHeartPulse as ComponentType<any>,
      tone: systemTone(),
      attention: healthOk === false
    },
    {
      id: 'sessions',
      label: $t('landing.metric.sessions'),
      value: sessionsCount24h.toString(),
      helper: $t('landing.metric.sessions.helper'),
      icon: IconActivity as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'approvals',
      label: $t('landing.metric.approvals'),
      value: pendingApprovals.toString(),
      helper: $t('landing.metric.approvals.helper'),
      icon: IconShield as ComponentType<any>,
      attention: pendingApprovals > 0,
      onClick: () => goto('/approvals')
    },
    {
      id: 'snapshot',
      label: $t('landing.metric.lastSnapshot'),
      value: lastSnapshotAge ?? $t('landing.relTime.never'),
      helper: $t('landing.metric.lastSnapshot.helper'),
      icon: IconClock as ComponentType<any>,
      onClick: () => goto('/snapshots')
    },
    {
      id: 'drift',
      label: $t('landing.metric.drift'),
      value: driftCount24h.toString(),
      helper: $t('landing.metric.drift.helper'),
      icon: IconAlertTriangle as ComponentType<any>,
      tone: 'danger' as const,
      attention: driftCount24h > 0,
      onClick: () => goto('/audit?type=schema.drift')
    }
  ];
</script>

<section class="hero">
  <div class="hero-mark"><Logo size={36} /></div>
  <h1 class="title">{$t('landing.title')}</h1>
  <p class="lede">{$t('landing.lede')}</p>
</section>

<MetricStrip {metrics} label={$t('landing.metric.aria')} />

<section class="grid-two">
  <section class="card">
    <header class="card-head">
      <h4>{$t('landing.section.recentApprovals')}</h4>
      <a class="more" href="/approvals">
        {$t('common.viewAll')}
        <IconArrowRight size={12} />
      </a>
    </header>
    {#if approvalRows.length === 0}
      <EmptyState title={$t('landing.empty.approvals')} compact />
    {:else}
      <Table columns={approvalColumns} rows={approvalRows} compact>
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'risk_class'}
            <Badge tone={riskTone(row.risk_class)}>{row.risk_class}</Badge>
          {:else if column.key === 'created_at'}
            <span class="muted">{fmtRelative(row.created_at)}</span>
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
      </Table>
    {/if}
  </section>

  <section class="card">
    <header class="card-head">
      <h4>{$t('landing.section.recentSnapshots')}</h4>
      <a class="more" href="/snapshots">
        {$t('common.viewAll')}
        <IconArrowRight size={12} />
      </a>
    </header>
    {#if (snapshots ?? []).length === 0}
      <EmptyState title={$t('landing.empty.snapshots')} compact />
    {:else}
      <Table columns={snapshotColumns} rows={snapshots ?? []} compact>
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'id'}
            <a href={`/snapshots/${row.id}`} class="link-row">
              <IdBadge value={row.id} hint="snapshot" />
            </a>
          {:else if column.key === 'source'}
            {#if isPlaygroundSession(row.session_id)}
              <Badge tone="info">playground</Badge>
            {:else}
              <Badge tone="neutral">mcp</Badge>
            {/if}
          {:else if column.key === 'session_id'}
            {#if row.session_id}
              <IdBadge value={row.session_id} />
            {:else}
              <span class="muted">—</span>
            {/if}
          {:else if column.key === 'tools'}
            <Badge tone="neutral">{row.tools.length}</Badge>
          {:else if column.key === 'created_at'}
            <span class="muted">{fmtRelative(row.created_at)}</span>
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
      </Table>
    {/if}
  </section>
</section>

<section class="card">
  <header class="card-head">
    <h4>{$t('landing.section.recentAudit')}</h4>
    <a class="more" href="/audit">
      {$t('common.viewAll')}
      <IconArrowRight size={12} />
    </a>
  </header>
  {#if noticeableAudit.length === 0}
    <EmptyState title={$t('landing.empty.audit')} compact />
  {:else}
    <Table columns={auditColumns} rows={noticeableAudit} compact>
      <svelte:fragment slot="cell" let:row let:column>
        {#if column.key === 'occurred_at'}
          <span class="muted">{fmtTime(row.occurred_at)}</span>
        {:else if column.key === 'type'}
          <Badge
            tone={row.type.includes('failed') || row.type.includes('denied')
              ? 'danger'
              : row.type === 'schema.drift'
                ? 'warning'
                : 'neutral'}
            mono
          >
            {row.type}
          </Badge>
        {:else if column.key === 'session_id'}
          {#if row.session_id}
            <IdBadge value={row.session_id} />
          {:else}
            <span class="muted">—</span>
          {/if}
        {:else}
          {row[column.key] ?? ''}
        {/if}
      </svelte:fragment>
    </Table>
  {/if}
</section>

<style>
  .hero {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3) 0 var(--space-5);
    margin-bottom: var(--space-5);
  }
  .hero-mark {
    margin-bottom: var(--space-1);
  }
  .title {
    font-size: var(--font-size-heading-2);
    line-height: var(--font-line-heading-2, 1.25);
    font-weight: var(--font-weight-semibold);
    letter-spacing: -0.02em;
    margin: 0;
  }
  .lede {
    color: var(--color-text-secondary);
    margin: 0;
    max-width: 64ch;
    font-size: var(--font-size-body-sm);
  }
  .grid-two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-4);
    margin-bottom: var(--space-4);
  }
  @media (max-width: 880px) {
    .grid-two {
      grid-template-columns: 1fr;
    }
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4) var(--space-5);
    margin-bottom: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .card-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
  }
  .card-head h4 {
    margin: 0;
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .more {
    color: var(--color-accent-primary);
    font-size: var(--font-size-label);
    text-decoration: none;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .more:hover {
    color: var(--color-accent-primary-hover);
  }
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .link-row {
    text-decoration: none;
    color: inherit;
  }
</style>
