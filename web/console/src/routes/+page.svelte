<script lang="ts">
  import { onMount } from 'svelte';
  import {
    api,
    isFeatureUnavailable,
    type Approval,
    type AuditEvent,
    type Snapshot
  } from '$lib/api';
  import { Badge, DashboardTile, EmptyState, Logo, Sparkline, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconArrowRight from 'lucide-svelte/icons/arrow-right';

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
  let sessionsByHour: number[] = [];

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

  function bucketByHour(events: AuditEvent[]): number[] {
    const buckets = new Array(24).fill(0);
    const now = Date.now();
    for (const e of events) {
      const t = new Date(e.occurred_at).getTime();
      if (isNaN(t)) continue;
      const ageH = (now - t) / 3_600_000;
      if (ageH < 0 || ageH >= 24) continue;
      buckets[23 - Math.floor(ageH)]++;
    }
    return buckets;
  }

  onMount(async () => {
    // Health & ready run cheap and almost always succeed.
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
      driftCount24h = auditEvents.filter((e) => {
        if (e.type !== 'schema.drift') return false;
        const ageH = (Date.now() - new Date(e.occurred_at).getTime()) / 3_600_000;
        return ageH >= 0 && ageH < 24;
      }).length;
      sessionsByHour = bucketByHour(
        auditEvents.filter((e) => e.type === 'session.created' || e.type === 'tool_call.complete')
      );
    } catch (e) {
      if (isFeatureUnavailable(e)) auditUnavailable = true;
      auditEvents = [];
    }
  });

  $: pendingApprovals = approvals ? approvals.length : null;
  $: lastSnapshotAge =
    snapshots && snapshots.length > 0 ? fmtRelative(snapshots[0].created_at) : null;
  $: noticeableAudit = (auditEvents ?? [])
    .filter(
      (e) =>
        e.type === 'tool_call.failed' ||
        e.type === 'policy.denied' ||
        e.type === 'schema.drift' ||
        e.type === 'audit.dropped' ||
        e.type === 'approval.expired'
    )
    .slice(0, 5);

  $: approvalRows = (approvals ?? []).slice(0, 5);

  $: snapshotColumns = [
    { key: 'id', label: $t('snapshots.col.id'), mono: true },
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

  function statusTone(ok: boolean | null, downTone: Tone = 'danger'): Tone {
    if (ok === null) return 'neutral';
    return ok ? 'success' : downTone;
  }
  function statusValue(
    ok: boolean | null,
    okKey = 'landing.status.ok',
    downKey = 'landing.status.down'
  ): string {
    if (ok === null) return '—';
    return ok ? $t(okKey) : $t(downKey);
  }
  function riskTone(rc: string): Tone {
    const v = rc.toLowerCase();
    if (v === 'destructive' || v === 'sensitive_read') return 'danger';
    if (v === 'external_side_effect') return 'warning';
    if (v === 'idempotent_read') return 'info';
    return 'neutral';
  }
</script>

<section class="hero">
  <div class="hero-mark"><Logo size={56} /></div>
  <p class="eyebrow">{$t('brand.tagline')}</p>
  <h1 class="title">{$t('landing.title')}</h1>
  <p class="lede">{$t('landing.lede')}</p>
</section>

<section class="tiles">
  <DashboardTile
    label={$t('landing.tile.health')}
    value={statusValue(healthOk)}
    tone={statusTone(healthOk)}
    dot={statusTone(healthOk)}
    loading={healthOk === null}
  />
  <DashboardTile
    label={$t('landing.tile.ready')}
    value={statusValue(readyOk, 'landing.status.ok', 'landing.status.pending')}
    tone={statusTone(readyOk, 'warning')}
    dot={statusTone(readyOk, 'warning')}
    loading={readyOk === null}
  />
  <DashboardTile
    label={$t('landing.tile.sessions')}
    value={sessionsByHour.length > 0 ? sessionsByHour.reduce((s, n) => s + n, 0) : 0}
    tone="accent"
    loading={auditEvents === null}
  >
    <span slot="foot"><Sparkline data={sessionsByHour} width={120} height={28} /></span>
  </DashboardTile>
  <DashboardTile
    label={$t('landing.tile.approvals')}
    value={pendingApprovals ?? 0}
    tone={pendingApprovals && pendingApprovals > 0 ? 'warning' : 'neutral'}
    href="/approvals"
    loading={approvals === null && !approvalsUnavailable}
  />
  <DashboardTile
    label={$t('landing.tile.lastSnapshot')}
    value={lastSnapshotAge ?? $t('landing.relTime.never')}
    tone="info"
    href="/snapshots"
    loading={snapshots === null && !snapshotsUnavailable}
  />
  <DashboardTile
    label={$t('landing.tile.drift24h')}
    value={driftCount24h}
    tone={driftCount24h > 0 ? 'danger' : 'neutral'}
    href={`/audit?type=schema.drift`}
    loading={auditEvents === null && !auditUnavailable}
  />
</section>

<section class="grid-two">
  <div class="block">
    <header class="block-head">
      <h2>{$t('landing.section.recentApprovals')}</h2>
      <a class="more" href="/approvals">{$t('common.viewAll')} <IconArrowRight size={12} /></a>
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
  </div>

  <div class="block">
    <header class="block-head">
      <h2>{$t('landing.section.recentSessions')}</h2>
      <a class="more" href="/snapshots">{$t('common.viewAll')} <IconArrowRight size={12} /></a>
    </header>
    {#if (snapshots ?? []).length === 0}
      <EmptyState title={$t('landing.empty.sessions')} compact />
    {:else}
      <Table columns={snapshotColumns} rows={snapshots ?? []} compact>
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'id'}
            <a href={`/snapshots/${row.id}`}><code class="mono">{row.id}</code></a>
          {:else if column.key === 'session_id'}
            <span class="muted">{row.session_id ?? '—'}</span>
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
  </div>
</section>

<section class="block">
  <header class="block-head">
    <h2>{$t('landing.section.recentAudit')}</h2>
    <a class="more" href="/audit">{$t('common.viewAll')} <IconArrowRight size={12} /></a>
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
          <span class="muted">{row.session_id ?? '—'}</span>
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
    gap: var(--space-3);
    padding: var(--space-6) 0 var(--space-8);
    border-bottom: 1px solid var(--color-border-soft);
    margin-bottom: var(--space-8);
  }
  .hero-mark {
    margin-bottom: var(--space-2);
  }
  .eyebrow {
    font-family: var(--font-serif);
    font-style: italic;
    font-size: var(--font-size-body-md);
    color: var(--color-accent-primary);
    margin: 0;
  }
  .title {
    font-size: var(--font-size-heading-1);
    line-height: var(--font-line-heading-1);
    font-weight: var(--font-weight-semibold);
    letter-spacing: -0.02em;
    margin: 0;
  }
  .lede {
    color: var(--color-text-secondary);
    margin: 0;
    max-width: 64ch;
    font-size: var(--font-size-body-md);
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: var(--space-3);
    margin-bottom: var(--space-8);
  }
  .grid-two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-4);
    margin-bottom: var(--space-8);
  }
  @media (max-width: 880px) {
    .grid-two {
      grid-template-columns: 1fr;
    }
  }
  .block {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4) var(--space-5);
    margin-bottom: var(--space-6);
  }
  .block-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: var(--space-3);
  }
  .block-head h2 {
    margin: 0;
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
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
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
  }
</style>
