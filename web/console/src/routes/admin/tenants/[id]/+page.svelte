<script lang="ts">
  /**
   * Tenant detail — Phase 10.8 detail-page sub-vocabulary.
   *
   * Adopts: Breadcrumbs slot, meta badges, PageActionGroup, compact
   * MetricStrip mini-KPI, Tabs, and .card sections with the <h4>
   * SECTION-LABEL header. Activity tab moves from a bespoke <ul> to
   * the standard Table component (matches /audit and /admin/tenants).
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type EntityActivityRow, type Tenant } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    EmptyState,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    Table,
    Tabs,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconArchive from 'lucide-svelte/icons/archive';
  import IconActivity from 'lucide-svelte/icons/activity';
  import IconClock from 'lucide-svelte/icons/clock';
  import IconShield from 'lucide-svelte/icons/shield';
  import type { ComponentType } from 'svelte';

  let tenant: Tenant | null = null;
  let activity: EntityActivityRow[] = [];
  let loading = true;
  let error = '';
  let archiving = false;
  let activeTab: 'overview' | 'quotas' | 'activity' = 'overview';

  $: id = ($page.params.id ?? '') as string;

  async function refresh() {
    if (!id) return;
    loading = true;
    error = '';
    try {
      tenant = await api.getTenant(id);
      activity = await api.tenantActivity(id);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function archive() {
    if (!id || !confirm($t('crud.confirmDelete'))) return;
    archiving = true;
    try {
      await api.archiveTenant(id);
      toast.info($t('crud.deletedToast'), id);
      void goto('/admin/tenants');
    } catch (e) {
      error = (e as Error).message;
    } finally {
      archiving = false;
    }
  }

  function fmt(iso?: string): string {
    if (!iso) return '—';
    try {
      return new Date(iso).toLocaleString();
    } catch {
      return iso;
    }
  }

  type Tone = 'success' | 'warning' | 'neutral' | 'danger' | 'info';
  function statusTone(s?: string): Tone {
    if (s === 'archived') return 'warning';
    return 'success';
  }

  $: tabs = [
    { id: 'overview', label: $t('tenantDetail.tab.overview') },
    { id: 'quotas', label: $t('tenantDetail.tab.quotas') },
    { id: 'activity', label: $t('tenantDetail.tab.activity') }
  ];

  $: identityKV = tenant
    ? [
        { label: $t('tenants.field.id'), value: tenant.id, mono: true },
        { label: $t('tenants.field.displayName'), value: tenant.display_name },
        { label: $t('tenants.field.plan'), value: tenant.plan },
        { label: $t('tenants.field.runtimeMode'), value: tenant.runtime_mode ?? '—' }
      ]
    : [];

  $: quotaKV = tenant
    ? [
        {
          label: $t('tenants.field.maxSessions'),
          value: String(tenant.max_concurrent_sessions ?? '—')
        },
        {
          label: $t('tenants.field.maxRpm'),
          value: String(tenant.max_requests_per_minute ?? '—')
        },
        {
          label: $t('tenants.field.retention'),
          value: String(tenant.audit_retention_days ?? '—')
        }
      ]
    : [];

  $: authKV = tenant
    ? [
        { label: $t('tenants.field.jwtIssuer'), value: tenant.jwt_issuer ?? '—' },
        { label: $t('tenants.field.jwtJwks'), value: tenant.jwt_jwks_url ?? '—' }
      ]
    : [];

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh(),
      loading
    },
    ...(tenant && tenant.status !== 'archived'
      ? [
          {
            label: $t('crud.archive'),
            icon: IconArchive,
            variant: 'destructive' as const,
            onClick: archive,
            loading: archiving
          }
        ]
      : [])
  ];

  $: metrics = tenant
    ? [
        {
          id: 'sessions',
          label: $t('tenantDetail.metric.sessions'),
          value: String(tenant.max_concurrent_sessions ?? '—'),
          icon: IconActivity as ComponentType<any>,
          tone: 'brand' as const
        },
        {
          id: 'rpm',
          label: $t('tenantDetail.metric.rpm'),
          value: String(tenant.max_requests_per_minute ?? '—'),
          icon: IconShield as ComponentType<any>
        },
        {
          id: 'retention',
          label: $t('tenantDetail.metric.retention'),
          value: tenant.audit_retention_days != null
            ? $t('tenantDetail.metric.retention.value', { n: tenant.audit_retention_days })
            : '—',
          icon: IconClock as ComponentType<any>
        }
      ]
    : [];

  $: activityColumns = [
    { key: 'occurred_at', label: $t('activity.col.when'), width: '180px' },
    { key: 'event_type', label: $t('activity.col.event'), width: '220px' },
    { key: 'actor_user_id', label: $t('activity.col.actor'), width: '160px' },
    { key: 'summary', label: $t('activity.col.summary') }
  ];

  onMount(refresh);
</script>

<PageHeader title={tenant?.display_name ?? id} description={tenant?.id}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: $t('nav.tenants'), href: '/admin/tenants' }, { label: id }]}
  />
  <div slot="meta">
    <Badge tone="neutral" mono>{id}</Badge>
    {#if tenant?.plan}<Badge tone="neutral">{tenant.plan}</Badge>{/if}
    {#if tenant}<Badge tone={statusTone(tenant.status)}>{tenant.status ?? 'active'}</Badge>{/if}
  </div>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if tenant}
  <MetricStrip {metrics} compact label={$t('tenantDetail.metric.aria')} />
  <Tabs {tabs} bind:active={activeTab} />

  {#if activeTab === 'overview'}
    <section class="card">
      <h4>{$t('tenantDetail.section.identity')}</h4>
      <KeyValueGrid items={identityKV} columns={2} />
    </section>
    <section class="card">
      <h4>{$t('tenantDetail.section.auth')}</h4>
      <KeyValueGrid items={authKV} columns={1} />
    </section>
  {:else if activeTab === 'quotas'}
    <section class="card">
      <h4>{$t('tenantDetail.section.quotas')}</h4>
      <KeyValueGrid items={quotaKV} columns={1} />
    </section>
  {:else if activeTab === 'activity'}
    <section class="card">
      <h4>{$t('tenantDetail.section.activity')}</h4>
      <Table columns={activityColumns} rows={activity} empty={$t('activity.empty.title')}>
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'occurred_at'}
            <span class="muted">{fmt(row.occurred_at)}</span>
          {:else if column.key === 'event_type'}
            <code class="mono">{row.event_type}</code>
          {:else if column.key === 'actor_user_id'}
            <span class="muted">{row.actor_user_id ?? '—'}</span>
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
        <svelte:fragment slot="empty">
          <EmptyState
            title={$t('activity.empty.title')}
            description={$t('activity.empty.description')}
            compact
          />
        </svelte:fragment>
      </Table>
    </section>
  {/if}
{:else if !loading}
  <EmptyState
    title={$t('tenantDetail.notFound.title')}
    description={$t('tenantDetail.notFound.description', { id })}
  />
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-3) 0;
    font-size: var(--font-size-body-sm);
  }
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    margin-top: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .card h4 {
    margin: 0;
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
</style>
