<script lang="ts">
  /**
   * Snapshot detail — Phase 10.8 detail-page sub-vocabulary.
   *
   * The pre-10.8 page stacked three tables (Servers / Tools / Skills)
   * on one scroll and silently dropped four data classes (Resources,
   * Prompts, Credentials, Policies) that already existed in the
   * payload. This rewrite tabs them out and adds a mini-KPI strip
   * (Total tools / Servers / Resources / Risky tools attention).
   *
   * Risky = tools where `requires_approval === true`. Surfacing the
   * count is the load-bearing operator-ness — a snapshot with risky
   * tools needs different scrutiny than one without.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type Snapshot } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    EmptyState,
    IdBadge,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    Table,
    Tabs
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import IconWrench from 'lucide-svelte/icons/wrench';
  import IconServer from 'lucide-svelte/icons/server';
  import IconBox from 'lucide-svelte/icons/box';
  import IconShieldAlert from 'lucide-svelte/icons/shield-alert';
  import type { ComponentType } from 'svelte';

  let snap: Snapshot | null = null;
  let loading = true;
  let error = '';
  let activeTab = 'overview';

  $: id = $page.params.id ?? '';

  async function load() {
    loading = true;
    error = '';
    try {
      if (!id) throw new Error('missing snapshot id');
      snap = await api.getSnapshot(id);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(load);

  function fmt(time: string): string {
    try {
      return new Date(time).toLocaleString();
    } catch {
      return time;
    }
  }

  $: riskyToolCount = snap ? snap.tools.filter((t) => t.requires_approval).length : 0;

  $: tabs = [
    { id: 'overview', label: $t('snapshotDetail.tab.overview') },
    { id: 'servers', label: $t('snapshotDetail.tab.servers') },
    { id: 'tools', label: $t('snapshotDetail.tab.tools') },
    { id: 'resources', label: $t('snapshotDetail.tab.resources') },
    { id: 'prompts', label: $t('snapshotDetail.tab.prompts') },
    { id: 'skills', label: $t('snapshotDetail.tab.skills') },
    { id: 'credentials', label: $t('snapshotDetail.tab.credentials') }
  ];

  $: identityKV = snap
    ? [
        { label: $t('snapshotDetail.field.tenant'), value: snap.tenant_id },
        { label: $t('snapshotDetail.field.created'), value: fmt(snap.created_at) },
        { label: $t('snapshotDetail.field.session'), value: snap.session_id ?? '—', mono: true },
        {
          label: $t('snapshotDetail.field.fingerprint'),
          value: snap.overall_hash,
          mono: true,
          full: true as const
        }
      ]
    : [];

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => load(),
      loading
    }
  ];

  $: metrics = snap
    ? [
        {
          id: 'tools',
          label: $t('snapshotDetail.metric.tools'),
          value: snap.tools.length.toString(),
          icon: IconWrench as ComponentType<any>,
          tone: 'brand' as const
        },
        {
          id: 'servers',
          label: $t('snapshotDetail.metric.servers'),
          value: snap.servers.length.toString(),
          icon: IconServer as ComponentType<any>
        },
        {
          id: 'resources',
          label: $t('snapshotDetail.metric.resources'),
          value: snap.resources.length.toString(),
          icon: IconBox as ComponentType<any>
        },
        {
          id: 'risky',
          label: $t('snapshotDetail.metric.risky'),
          value: riskyToolCount.toString(),
          icon: IconShieldAlert as ComponentType<any>,
          tone: 'danger' as const,
          attention: riskyToolCount > 0
        }
      ]
    : [];

  const serverColumns = [
    { key: 'id', label: $t('snapshotDetail.col.server') },
    { key: 'transport', label: $t('snapshotDetail.col.transport'), width: '110px' },
    { key: 'runtime_mode', label: $t('snapshotDetail.col.mode'), width: '120px' },
    { key: 'schema_hash', label: $t('snapshotDetail.col.fingerprint'), width: '180px' },
    { key: 'health', label: $t('snapshotDetail.col.health'), width: '110px' }
  ];

  const toolColumns = [
    { key: 'namespaced_name', label: $t('snapshotDetail.col.name'), mono: true },
    { key: 'risk_class', label: $t('snapshotDetail.col.risk'), width: '160px' },
    { key: 'requires_approval', label: $t('snapshotDetail.col.approval'), width: '110px' },
    { key: 'skill_id', label: $t('snapshotDetail.col.skill'), width: '160px' },
    { key: 'hash', label: $t('snapshotDetail.col.fingerprint'), width: '120px' }
  ];

  const resourceColumns = [
    { key: 'uri', label: $t('snapshotDetail.col.uri'), mono: true },
    { key: 'server_id', label: $t('snapshotDetail.col.server'), width: '180px' },
    { key: 'mime_type', label: $t('snapshotDetail.col.mime'), width: '160px' }
  ];

  const promptColumns = [
    { key: 'namespaced_name', label: $t('snapshotDetail.col.name'), mono: true },
    { key: 'server_id', label: $t('snapshotDetail.col.server'), width: '180px' },
    { key: 'arguments', label: $t('snapshotDetail.col.args'), width: '120px' }
  ];

  const skillColumns = [
    { key: 'id', label: $t('snapshotDetail.col.skill') },
    { key: 'version', label: $t('snapshotDetail.col.version'), mono: true, width: '110px' },
    { key: 'enabled_for_session', label: $t('snapshotDetail.col.sessionEnabled'), width: '160px' }
  ];

  const credColumns = [
    { key: 'server_id', label: $t('snapshotDetail.col.server') },
    { key: 'strategy', label: $t('snapshotDetail.col.strategy'), width: '160px' },
    { key: 'secret_refs', label: $t('snapshotDetail.col.refs') }
  ];
</script>

<PageHeader title={`Snapshot ${id}`}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: $t('nav.snapshots'), href: '/snapshots' }, { label: id }]}
  />
  <div slot="meta">
    {#if snap}
      <IdBadge value={snap.overall_hash} chars={8} />
      {#if snap.session_id}<IdBadge value={snap.session_id} />{/if}
    {/if}
  </div>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if snap}
  <MetricStrip {metrics} compact label={$t('snapshotDetail.metric.aria')} />
  <Tabs {tabs} bind:active={activeTab} />

  {#if activeTab === 'overview'}
    <section class="card">
      <h4>{$t('snapshotDetail.section.identity')}</h4>
      <KeyValueGrid items={identityKV} columns={2} />
    </section>
    {#if snap.warnings && snap.warnings.length > 0}
      <section class="card warn">
        <h4>
          <IconAlertTriangle size={14} aria-hidden="true" />
          {$t('snapshotDetail.section.warnings')}
        </h4>
        <ul>
          {#each snap.warnings as w (w)}<li>{w}</li>{/each}
        </ul>
      </section>
    {/if}
    {#if snap.policies && (snap.policies.allow_list?.length || snap.policies.deny_list?.length || snap.policies.approval_timeout || snap.policies.default_risk_class)}
      <section class="card">
        <h4>{$t('snapshotDetail.section.policies')}</h4>
        <KeyValueGrid
          items={[
            {
              label: $t('snapshotDetail.field.allowList'),
              value: snap.policies.allow_list?.join(', ') || '—'
            },
            {
              label: $t('snapshotDetail.field.denyList'),
              value: snap.policies.deny_list?.join(', ') || '—'
            },
            {
              label: $t('snapshotDetail.field.approvalTimeout'),
              value: snap.policies.approval_timeout != null
                ? String(snap.policies.approval_timeout)
                : '—'
            },
            {
              label: $t('snapshotDetail.field.defaultRisk'),
              value: snap.policies.default_risk_class ?? '—'
            }
          ]}
          columns={2}
        />
      </section>
    {/if}
  {:else if activeTab === 'servers'}
    <section class="card">
      <h4>{$t('snapshotDetail.section.servers', { n: snap.servers.length })}</h4>
      <Table
        columns={serverColumns}
        rows={snap.servers}
        empty={$t('snapshotDetail.empty.servers')}
      >
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'id'}
            <span class="server-name">
              <span class="display">{row.display_name || row.id}</span>
              {#if row.display_name && row.display_name !== row.id}
                <code class="muted-id">{row.id}</code>
              {/if}
            </span>
          {:else if column.key === 'transport'}
            <Badge tone="neutral" mono>{row.transport}</Badge>
          {:else if column.key === 'runtime_mode'}
            <span class="muted">{row.runtime_mode ?? '—'}</span>
          {:else if column.key === 'schema_hash'}
            <IdBadge value={row.schema_hash} chars={8} />
          {:else if column.key === 'health'}
            <Badge
              tone={row.health === 'ready' || row.health === 'healthy' ? 'success' : 'neutral'}
            >
              {row.health}
            </Badge>
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
      </Table>
    </section>
  {:else if activeTab === 'tools'}
    <section class="card">
      <h4>{$t('snapshotDetail.section.tools', { n: snap.tools.length })}</h4>
      <Table columns={toolColumns} rows={snap.tools} empty={$t('snapshotDetail.empty.tools')}>
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'risk_class'}
            <Badge
              tone={row.risk_class === 'destructive' || row.risk_class === 'sensitive_read'
                ? 'danger'
                : row.risk_class === 'external_side_effect'
                  ? 'warning'
                  : 'neutral'}
            >
              {row.risk_class}
            </Badge>
          {:else if column.key === 'requires_approval'}
            <Badge tone={row.requires_approval ? 'warning' : 'neutral'}>
              {row.requires_approval ? $t('common.yes') : $t('common.no')}
            </Badge>
          {:else if column.key === 'skill_id'}
            {#if row.skill_id}
              <IdBadge value={row.skill_id} />
            {:else}
              <span class="muted">—</span>
            {/if}
          {:else if column.key === 'hash'}
            <IdBadge value={row.hash} chars={8} />
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
      </Table>
    </section>
  {:else if activeTab === 'resources'}
    <section class="card">
      <h4>{$t('snapshotDetail.section.resources', { n: snap.resources.length })}</h4>
      <Table
        columns={resourceColumns}
        rows={snap.resources}
        empty={$t('snapshotDetail.empty.resources')}
      >
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'uri'}
            <code class="mono">{row.uri}</code>
          {:else if column.key === 'mime_type'}
            <span class="muted">{row.mime_type ?? '—'}</span>
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
      </Table>
    </section>
  {:else if activeTab === 'prompts'}
    <section class="card">
      <h4>{$t('snapshotDetail.section.prompts', { n: snap.prompts.length })}</h4>
      <Table
        columns={promptColumns}
        rows={snap.prompts}
        empty={$t('snapshotDetail.empty.prompts')}
      >
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'arguments'}
            <Badge tone="neutral">{row.arguments?.length ?? 0}</Badge>
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
      </Table>
    </section>
  {:else if activeTab === 'skills'}
    <section class="card">
      <h4>{$t('snapshotDetail.section.skills', { n: snap.skills.length })}</h4>
      {#if snap.skills.length === 0}
        <EmptyState title={$t('snapshotDetail.empty.skills')} compact />
      {:else}
        <Table columns={skillColumns} rows={snap.skills}>
          <svelte:fragment slot="cell" let:row let:column>
            {#if column.key === 'id'}
              <IdBadge value={row.id} />
            {:else if column.key === 'enabled_for_session'}
              <Badge tone={row.enabled_for_session ? 'success' : 'neutral'}>
                {row.enabled_for_session ? $t('common.yes') : $t('common.no')}
              </Badge>
            {:else}
              {row[column.key] ?? ''}
            {/if}
          </svelte:fragment>
        </Table>
      {/if}
    </section>
  {:else if activeTab === 'credentials'}
    <section class="card">
      <h4>{$t('snapshotDetail.section.credentials', { n: snap.credentials.length })}</h4>
      {#if snap.credentials.length === 0}
        <EmptyState title={$t('snapshotDetail.empty.credentials')} compact />
      {:else}
        <Table columns={credColumns} rows={snap.credentials}>
          <svelte:fragment slot="cell" let:row let:column>
            {#if column.key === 'strategy'}
              <Badge tone="neutral" mono>{row.strategy ?? '—'}</Badge>
            {:else if column.key === 'secret_refs'}
              <span class="muted">{row.secret_refs?.join(', ') || '—'}</span>
            {:else}
              {row[column.key] ?? ''}
            {/if}
          </svelte:fragment>
        </Table>
      {/if}
    </section>
  {/if}
{:else if !loading}
  <EmptyState
    title={$t('snapshotDetail.notFound.title')}
    description={$t('snapshotDetail.notFound.description', { id })}
  />
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
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
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  .card.warn {
    border-color: var(--color-warning);
    background: var(--color-warning-soft);
  }
  .card.warn h4 {
    color: var(--color-warning);
  }
  .card.warn ul {
    margin: 0;
    padding-left: var(--space-5);
    color: var(--color-warning);
  }
  .server-name {
    display: inline-flex;
    flex-direction: column;
    gap: 2px;
  }
  .server-name .display {
    color: var(--color-text-primary);
    font-weight: var(--font-weight-medium);
  }
  .server-name .muted-id {
    color: var(--color-text-tertiary);
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
  }
</style>
