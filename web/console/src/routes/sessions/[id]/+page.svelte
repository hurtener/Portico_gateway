<script lang="ts">
  /**
   * Session detail summary. Provides a fast overview + a CTA to open
   * the time-travel inspector. The inspector is a heavy view (full
   * timeline + state-at-time scrubber) that we don't want to default
   * to — operators triaging a known session jump straight there, but
   * casual browsing comes through here first.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import {
    Badge,
    Breadcrumbs,
    EmptyState,
    IdBadge,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader
  } from '$lib/components';
  import { api, isFeatureUnavailable, type SessionBundle } from '$lib/api';
  import { t } from '$lib/i18n';
  import IconDownload from 'lucide-svelte/icons/download';
  import IconActivity from 'lucide-svelte/icons/activity';

  $: sid = ($page.params.id ?? '').trim();
  $: imported = sid.startsWith('imported:');

  let bundle: SessionBundle | null = null;
  let loading = true;
  let error = '';
  let unavailable = false;

  async function load() {
    if (!sid) return;
    loading = true;
    error = '';
    unavailable = false;
    try {
      bundle = await api.getSessionBundle(sid);
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

  onMount(load);

  function formatDuration(from: string, to: string): string {
    const start = Date.parse(from);
    const end = Date.parse(to);
    if (!Number.isFinite(start) || !Number.isFinite(end)) return '—';
    const ms = Math.max(0, end - start);
    if (ms < 1000) return `${ms}ms`;
    if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
    return `${(ms / 60_000).toFixed(1)}m`;
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
</script>

<Breadcrumbs items={[{ label: $t('nav.sessions'), href: '/sessions' }, { label: sid }]} />

<PageHeader title={sid} description="">
  <div slot="meta">
    {#if imported}
      <Badge tone="warning">Imported (read-only)</Badge>
    {/if}
    {#if bundle?.session.tenant_id}
      <span>tenant <IdBadge value={bundle.session.tenant_id} /></span>
    {/if}
  </div>
  <div slot="actions">
    <PageActionGroup
      actions={[
        {
          label: 'Open inspector',
          icon: IconActivity,
          variant: 'primary',
          href: `/sessions/${encodeURIComponent(sid)}/inspect`
        },
        {
          label: 'Export bundle',
          icon: IconDownload,
          onClick: exportBundle
        }
      ]}
    />
  </div>
</PageHeader>

{#if loading}
  <p class="loading">Loading bundle…</p>
{:else if unavailable}
  <EmptyState
    title="Inspector not configured"
    description="The Phase 11 bundle endpoint is not available in this build. Restart with the SQLite backend to enable it."
  />
{:else if error}
  <EmptyState title="Failed to load session" description={error} />
{:else if bundle}
  <MetricStrip
    compact
    metrics={[
      { id: 'spans', label: 'spans', value: bundle.manifest.counts.spans },
      { id: 'audit', label: 'audit', value: bundle.manifest.counts.audit },
      { id: 'policy', label: 'policy', value: bundle.manifest.counts.policy },
      { id: 'drift', label: 'drift', value: bundle.manifest.counts.drift }
    ]}
  />

  <section class="card">
    <h4>SESSION</h4>
    <KeyValueGrid
      items={[
        { label: 'id', value: bundle.session.id },
        { label: 'tenant', value: bundle.session.tenant_id },
        { label: 'snapshot', value: bundle.session.snapshot_id || '—' },
        { label: 'started', value: new Date(bundle.session.started_at).toLocaleString() },
        {
          label: 'ended',
          value: bundle.session.ended_at ? new Date(bundle.session.ended_at).toLocaleString() : '—'
        },
        {
          label: 'duration',
          value: bundle.session.ended_at
            ? formatDuration(bundle.session.started_at, bundle.session.ended_at)
            : '—'
        }
      ]}
    />
  </section>

  <section class="card">
    <h4>BUNDLE</h4>
    <KeyValueGrid
      items={[
        { label: 'bundle id', value: bundle.manifest.bundle_id },
        { label: 'schema', value: bundle.manifest.schema },
        { label: 'checksum', value: bundle.manifest.checksum },
        { label: 'generated', value: new Date(bundle.manifest.generated_at).toLocaleString() },
        { label: 'approvals', value: String(bundle.manifest.counts.approvals) }
      ]}
    />
  </section>
{/if}

<style>
  .loading {
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
  }
  .card {
    margin-top: var(--space-3);
    padding: var(--space-3);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
  }
  .card h4 {
    font-size: 11px;
    letter-spacing: 0.05em;
    color: var(--color-text-muted);
    margin: 0 0 var(--space-2);
    font-weight: 700;
  }
</style>
