<script lang="ts">
  /**
   * Authored skill detail — Phase 10.8 detail-page sub-vocabulary.
   *
   * Adopts: Breadcrumbs slot, meta badges, PageActionGroup, compact
   * MetricStrip mini-KPI (Versions / Status / Last published age),
   * .card sections with the <h4> SECTION-LABEL header. Tabs already
   * existed (Manifest / Files / Versions) — kept; tab content
   * re-wraps in the standard card pattern. Per-version
   * publish/archive moves into a row decisions card so it lines up
   * with the rest of the redesigned console.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type AuthoredSkillDetail, type AuthoredSkillSummary } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    Button,
    CodeBlock,
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
  import IconUpload from 'lucide-svelte/icons/upload';
  import IconArchive from 'lucide-svelte/icons/archive';
  import IconLayers from 'lucide-svelte/icons/layers';
  import IconCheckCircle2 from 'lucide-svelte/icons/check-circle-2';
  import IconClock from 'lucide-svelte/icons/clock';
  import type { ComponentType } from 'svelte';

  let detail: AuthoredSkillDetail | null = null;
  let history: AuthoredSkillSummary[] = [];
  let loading = true;
  let error = '';
  let activeTab = 'manifest';

  $: id = $page.params.id ?? '';

  async function refresh() {
    if (!id) return;
    loading = true;
    error = '';
    try {
      detail = await api.getAuthoredSkill(id);
    } catch {
      detail = null;
    }
    try {
      const res = await api.authoredSkillVersions(id);
      history = res.items ?? [];
      if (!detail && history.length > 0) {
        detail = await api.getAuthoredSkillVersion(id, history[0].version);
      }
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function publish(version: string) {
    try {
      await api.publishAuthoredSkill(id, version);
      toast.success($t('authored.toast.published'));
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  async function archive(version: string) {
    if (!confirm(`Archive ${id}@${version}?`)) return;
    try {
      await api.archiveAuthoredSkill(id, version);
      toast.info($t('authored.toast.archived'));
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);

  function fmt(iso?: string): string {
    if (!iso) return '—';
    try {
      return new Date(iso).toLocaleString();
    } catch {
      return iso;
    }
  }

  function relativeFromNow(iso?: string): string {
    if (!iso) return $t('common.dash');
    try {
      const target = new Date(iso).getTime();
      const diffSec = Math.round((Date.now() - target) / 1000);
      const abs = Math.abs(diffSec);
      let value: string;
      if (abs < 60) value = `${abs}s`;
      else if (abs < 3600) value = `${Math.round(abs / 60)}m`;
      else if (abs < 86400) value = `${Math.round(abs / 3600)}h`;
      else value = `${Math.round(abs / 86400)}d`;
      return $t('authored.relative.ago', { value });
    } catch {
      return $t('common.dash');
    }
  }

  $: tabs = [
    { id: 'manifest', label: $t('authored.editor.manifest') },
    { id: 'files', label: $t('authored.editor.skillMd') },
    { id: 'versions', label: $t('authored.versions.title') }
  ];

  $: detailKV = detail
    ? [
        { label: $t('authored.col.status'), value: detail.status },
        { label: $t('authored.col.version'), value: detail.version, mono: true },
        { label: $t('authored.col.checksum'), value: detail.checksum, mono: true, full: true },
        { label: $t('authored.col.created'), value: fmt(detail.created_at) },
        { label: $t('authored.col.published'), value: fmt(detail.published_at) }
      ]
    : [];

  const versionCols = [
    { key: 'version', label: 'Version', mono: true, width: '110px' },
    { key: 'status', label: 'Status', width: '120px' },
    { key: 'created_at', label: 'Created' },
    { key: 'actions', label: '', align: 'right' as const, width: '180px' }
  ];

  type Tone = 'success' | 'warning' | 'neutral';
  function statusTone(s: string): Tone {
    if (s === 'published') return 'success';
    if (s === 'draft') return 'warning';
    return 'neutral';
  }

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh(),
      loading
    }
  ];

  $: publishedCount = history.filter((h) => h.status === 'published').length;
  $: latestPublished =
    history.find((h) => h.status === 'published')?.published_at;

  $: metrics = detail
    ? [
        {
          id: 'versions',
          label: $t('authoredDetail.metric.versions'),
          value: history.length.toString(),
          icon: IconLayers as ComponentType<any>,
          tone: 'brand' as const
        },
        {
          id: 'status',
          label: $t('authoredDetail.metric.status'),
          value: detail.status,
          icon: IconCheckCircle2 as ComponentType<any>,
          tone: detail.status === 'published' ? ('success' as const) : ('default' as const)
        },
        {
          id: 'published',
          label: $t('authoredDetail.metric.lastPublished'),
          value: latestPublished ? relativeFromNow(latestPublished) : $t('common.dash'),
          icon: IconClock as ComponentType<any>
        }
      ]
    : [];
</script>

<PageHeader title={detail?.title ?? id}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[
      { label: $t('nav.skills'), href: '/skills' },
      { label: $t('nav.authored'), href: '/skills/authored' },
      { label: id }
    ]}
  />
  <div slot="meta">
    <Badge tone="neutral" mono>{id}</Badge>
    {#if detail}
      <Badge tone={statusTone(detail.status)}>{detail.status}</Badge>
    {/if}
    {#if publishedCount > 0}
      <Badge tone="info">{$t('authoredDetail.badge.publishedCount', { n: publishedCount })}</Badge>
    {/if}
  </div>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if !detail && !loading}
  <EmptyState title={$t('authored.empty.title')} description={$t('authored.empty.description')} />
{:else if detail}
  <MetricStrip {metrics} compact label={$t('authoredDetail.metric.aria')} />
  <Tabs {tabs} bind:active={activeTab} />

  {#if activeTab === 'manifest'}
    <section class="card">
      <h4>{$t('authoredDetail.section.identity')}</h4>
      <KeyValueGrid items={detailKV} columns={2} />
    </section>
    <section class="card">
      <h4>{$t('authored.editor.manifest')}</h4>
      <CodeBlock language="json" code={JSON.stringify(detail.manifest, null, 2)} />
    </section>
  {:else if activeTab === 'files'}
    {#if detail.files.length === 0}
      <section class="card">
        <h4>{$t('authoredDetail.section.files')}</h4>
        <EmptyState title={$t('common.empty')} compact />
      </section>
    {:else}
      {#each detail.files as f (f.relpath)}
        <section class="card">
          <h4><code class="mono">{f.relpath}</code></h4>
          <CodeBlock
            language={f.mime_type === 'text/markdown' ? 'markdown' : 'plaintext'}
            code={f.body}
          />
        </section>
      {/each}
    {/if}
  {:else if activeTab === 'versions'}
    <section class="card">
      <h4>{$t('authored.versions.title')}</h4>
      <Table columns={versionCols} rows={history}>
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'status'}
            <Badge tone={statusTone(row.status)}>{row.status}</Badge>
          {:else if column.key === 'created_at'}
            <span class="muted">{fmt(row.created_at)}</span>
          {:else if column.key === 'actions'}
            {#if row.status === 'draft'}
              <Button size="sm" variant="primary" on:click={() => publish(row.version)}>
                <IconUpload slot="leading" size={14} />
                {$t('authored.action.publish')}
              </Button>
            {:else if row.status === 'published'}
              <Button size="sm" variant="ghost" on:click={() => archive(row.version)}>
                <IconArchive slot="leading" size={14} />
                {$t('authored.action.archive')}
              </Button>
            {:else}
              <span class="muted">{$t('common.dash')}</span>
            {/if}
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
      </Table>
    </section>
  {/if}
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
  }
</style>
