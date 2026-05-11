<script lang="ts">
  /**
   * Root landing — Phase 10.9 reshape.
   *
   * Replaces the Phase 10.8 runtime-telemetry KPI strip (Sessions
   * 24h / Approvals / Drift / Last snapshot) with a setup+status
   * layout that answers "is the gateway wired correctly, and what
   * do I do next?" — the question a day-one operator asks.
   *
   * Composition (top → bottom):
   *   - Hero
   *   - 5 setup KPIs (Endpoint / Servers / Skills / Tenants / Auth),
   *     each clickable into the page that owns it
   *   - Configuration Status card (green/yellow with hints)
   *   - Quick Actions card (Connect agent → Add server → Author skill
   *     → Test in playground — chronological setup flow)
   *   - "Recent activity" section below the fold with the previous
   *     Recent approvals / snapshots / audit cards (demoted but kept)
   *
   * The runtime view (drift, sessions 24h) is still reachable from the
   * dedicated pages — but it doesn't dominate the operator's first
   * impression. Borrows the dataflow-explicit layout from
   * agentgateway's Overview.
   */
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import {
    api,
    isFeatureUnavailable,
    type Approval,
    type AuditEvent,
    type GatewayInfo,
    type ServerSummary,
    type SkillsIndex,
    type Snapshot,
    type Tenant
  } from '$lib/api';
  import { Badge, Button, EmptyState, IdBadge, Logo, MetricStrip, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconArrowRight from 'lucide-svelte/icons/arrow-right';
  import IconPlug from 'lucide-svelte/icons/plug';
  import IconServer from 'lucide-svelte/icons/server';
  import IconPackage from 'lucide-svelte/icons/package';
  import IconUsers from 'lucide-svelte/icons/users';
  import IconShield from 'lucide-svelte/icons/shield';
  import IconCheckCircle2 from 'lucide-svelte/icons/check-circle-2';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import IconPlay from 'lucide-svelte/icons/play';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconFileEdit from 'lucide-svelte/icons/file-edit';
  import type { ComponentType } from 'svelte';

  type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'accent';

  // === Setup substrate ================================================

  let info: GatewayInfo | null = null;
  let servers: ServerSummary[] = [];
  let skills: SkillsIndex | null = null;
  let tenants: Tenant[] = [];

  // === Activity substrate (demoted) ===================================

  let approvals: Approval[] | null = null;
  let snapshots: Snapshot[] | null = null;
  let auditEvents: AuditEvent[] | null = null;

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

  onMount(async () => {
    // Setup-state queries: gateway info + catalog counts. All
    // best-effort — a missing endpoint never blocks the landing.
    try {
      info = await api.gatewayInfo();
    } catch {
      info = null;
    }
    try {
      servers = (await api.listServers()) ?? [];
    } catch {
      servers = [];
    }
    try {
      skills = await api.listSkills();
    } catch {
      skills = null;
    }
    try {
      tenants = (await api.listTenants()) ?? [];
    } catch {
      tenants = [];
    }

    // Activity substrate (demoted; same calls as before).
    try {
      approvals = (await api.listApprovals()) ?? [];
    } catch (e) {
      if (!isFeatureUnavailable(e)) approvals = [];
      else approvals = [];
    }
    try {
      const res = await api.listSnapshots({ limit: 5 });
      snapshots = res.snapshots ?? [];
    } catch {
      snapshots = [];
    }
    try {
      const res = await api.queryAudit({ limit: 50 });
      auditEvents = res.events ?? [];
    } catch {
      auditEvents = [];
    }
  });

  // === Setup KPI strip ================================================

  $: skillCount = skills?.skills?.length ?? 0;
  $: serverCount = servers.length;
  $: tenantCount = tenants.length;

  $: metrics = info
    ? [
        {
          id: 'endpoint',
          label: $t('landing.metric.endpoint'),
          value: info.bind,
          helper: info.mcp_path,
          icon: IconPlug as ComponentType<any>,
          tone: 'brand' as const,
          onClick: () => goto('/connect')
        },
        {
          id: 'servers',
          label: $t('landing.metric.servers'),
          value: serverCount.toString(),
          helper: $t('landing.metric.servers.helper'),
          icon: IconServer as ComponentType<any>,
          attention: serverCount === 0,
          onClick: () => goto('/servers')
        },
        {
          id: 'skills',
          label: $t('landing.metric.skills'),
          value: skillCount.toString(),
          helper: $t('landing.metric.skills.helper'),
          icon: IconPackage as ComponentType<any>,
          onClick: () => goto('/skills')
        },
        {
          id: 'tenants',
          label: $t('landing.metric.tenants'),
          value: tenantCount.toString(),
          helper: $t('landing.metric.tenants.helper'),
          icon: IconUsers as ComponentType<any>,
          onClick: () => goto('/admin/tenants')
        },
        {
          id: 'auth',
          label: $t('landing.metric.auth'),
          value: info.auth.mode,
          helper:
            info.auth.mode === 'dev'
              ? $t('landing.metric.auth.dev')
              : $t('landing.metric.auth.jwt'),
          icon: IconShield as ComponentType<any>,
          tone: info.auth.mode === 'dev' ? ('warning' as const) : ('success' as const),
          attention: info.auth.mode === 'dev',
          onClick: () => goto('/connect#auth')
        }
      ]
    : [];

  // === Configuration status ===========================================

  type StatusCheck = { ok: boolean; label: string; hint?: string };

  $: statusChecks = ((): StatusCheck[] => {
    if (!info) return [];
    return [
      {
        ok: serverCount > 0,
        label: $t('landing.status.check.servers'),
        hint: $t('landing.status.check.servers.hint')
      },
      {
        ok: tenantCount > 0,
        label: $t('landing.status.check.tenants'),
        hint: $t('landing.status.check.tenants.hint')
      },
      {
        // Auth-OK means: dev mode (intentional) OR JWT issuer is set.
        // Dev mode is intentionally allowed because it's the
        // load-bearing happy path for local development.
        ok: info.dev_mode || (info.auth.issuer ?? '') !== '',
        label:
          info.auth.mode === 'dev'
            ? $t('landing.status.check.auth.dev')
            : $t('landing.status.check.auth.jwt'),
        hint: $t('landing.status.check.auth.hint')
      }
    ];
  })();

  $: allGreen = statusChecks.length > 0 && statusChecks.every((c) => c.ok);
  $: failingChecks = statusChecks.filter((c) => !c.ok);

  // === Quick actions ==================================================

  const quickActions = [
    {
      icon: IconPlug,
      labelKey: 'landing.quick.connect',
      descriptionKey: 'landing.quick.connect.help',
      href: '/connect'
    },
    {
      icon: IconPlus,
      labelKey: 'landing.quick.addServer',
      descriptionKey: 'landing.quick.addServer.help',
      href: '/servers/new'
    },
    {
      icon: IconFileEdit,
      labelKey: 'landing.quick.authorSkill',
      descriptionKey: 'landing.quick.authorSkill.help',
      href: '/skills/authored/new'
    },
    {
      icon: IconPlay,
      labelKey: 'landing.quick.playground',
      descriptionKey: 'landing.quick.playground.help',
      href: '/playground'
    }
  ];

  // === Activity tables (demoted) ======================================

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
</script>

<section class="hero">
  <div class="hero-mark"><Logo size={36} /></div>
  <h1 class="title">{$t('landing.title')}</h1>
</section>

{#if info}
  <MetricStrip {metrics} label={$t('landing.metric.aria')} />
{/if}

<section class="grid-two">
  <!-- Configuration Status -->
  <section class="card setup-status-card" class:ok={allGreen}>
    <header class="card-head">
      <h4>
        {#if allGreen}
          <IconCheckCircle2 size={14} aria-hidden="true" />
        {:else}
          <IconAlertTriangle size={14} aria-hidden="true" />
        {/if}
        {$t('landing.status.title')}
      </h4>
    </header>
    {#if allGreen}
      <p class="muted body status-ok">
        {info?.dev_mode ? $t('landing.status.allGreen.dev') : $t('landing.status.allGreen.jwt')}
      </p>
    {:else if statusChecks.length === 0}
      <p class="muted body">{$t('common.loading')}</p>
    {:else}
      <ul class="status-list">
        {#each statusChecks as c (c.label)}
          <li class:done={c.ok}>
            <span class="dot" aria-hidden="true">{c.ok ? '✓' : '○'}</span>
            <span class="check-label">{c.label}</span>
          </li>
        {/each}
      </ul>
      {#if failingChecks.length > 0 && failingChecks[0].hint}
        <p class="muted body next-hint">
          <strong>{$t('landing.status.next')}</strong>
          {failingChecks[0].hint}
        </p>
      {/if}
    {/if}
  </section>

  <!-- Quick Actions -->
  <section class="card">
    <header class="card-head">
      <h4>{$t('landing.quick.title')}</h4>
    </header>
    <ul class="quick-list">
      {#each quickActions as q (q.href)}
        <li>
          <a class="quick-row" href={q.href}>
            <span class="quick-icon" aria-hidden="true">
              <svelte:component this={q.icon} size={16} />
            </span>
            <span class="quick-text">
              <span class="quick-label">{$t(q.labelKey)}</span>
              <span class="quick-help muted">{$t(q.descriptionKey)}</span>
            </span>
            <IconArrowRight size={14} aria-hidden="true" />
          </a>
        </li>
      {/each}
    </ul>
  </section>
</section>

<!-- Recent activity section (demoted from the primary view) -->
<section class="recent-activity">
  <header class="recent-head">
    <h4>{$t('landing.section.recentActivity')}</h4>
    <p class="muted body">{$t('landing.section.recentActivity.help')}</p>
  </header>

  <div class="grid-two">
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
  </div>

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
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
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
  .body {
    line-height: 1.5;
    font-size: var(--font-size-body-sm);
  }
  .link-row {
    text-decoration: none;
    color: inherit;
  }

  /* Status card */
  .setup-status-card.ok {
    border-color: var(--color-success);
  }
  .setup-status-card.ok h4 {
    color: var(--color-success);
  }
  .status-ok {
    color: var(--color-success);
  }
  .status-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .status-list li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-sm);
  }
  .status-list li.done {
    color: var(--color-success);
  }
  .status-list .dot {
    display: inline-flex;
    width: 18px;
    height: 18px;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    border: 1px solid currentColor;
    font-size: 10px;
    font-weight: var(--font-weight-semibold);
  }
  .status-list li.done .dot {
    background: var(--color-success);
    color: var(--color-bg-canvas);
    border-color: var(--color-success);
  }
  .next-hint {
    border-top: 1px solid var(--color-border-soft);
    padding-top: var(--space-3);
  }
  .next-hint strong {
    color: var(--color-text-primary);
  }

  /* Quick actions */
  .quick-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .quick-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3);
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border-soft);
    background: var(--color-bg-canvas);
    color: var(--color-text-primary);
    text-decoration: none;
    transition: border-color var(--motion-fast) var(--ease-default);
  }
  .quick-row:hover {
    border-color: var(--color-accent-primary);
  }
  .quick-icon {
    display: inline-flex;
    color: var(--color-accent-primary);
  }
  .quick-text {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .quick-label {
    font-weight: var(--font-weight-medium);
    font-size: var(--font-size-body-sm);
  }
  .quick-help {
    font-size: var(--font-size-label);
  }

  /* Recent activity (demoted) */
  .recent-activity {
    margin-top: var(--space-6);
  }
  .recent-head {
    margin-bottom: var(--space-3);
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .recent-head h4 {
    margin: 0;
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
</style>
