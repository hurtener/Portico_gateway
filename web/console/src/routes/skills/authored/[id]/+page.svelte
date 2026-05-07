<script lang="ts">
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
    PageHeader,
    Table,
    Tabs,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconUpload from 'lucide-svelte/icons/upload';
  import IconArchive from 'lucide-svelte/icons/archive';

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
      // active version may not exist yet — fall through to versions list
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

  $: tabs = [
    { id: 'manifest', label: $t('authored.editor.manifest') },
    { id: 'files', label: $t('authored.editor.skillMd') },
    { id: 'versions', label: $t('authored.versions.title') }
  ];

  $: detailKV = detail
    ? [
        { label: 'Status', value: detail.status },
        { label: 'Version', value: detail.version, mono: true },
        { label: 'Checksum', value: detail.checksum, mono: true, full: true },
        { label: 'Created', value: detail.created_at },
        { label: 'Published', value: detail.published_at ?? '—' }
      ]
    : [];

  const versionCols = [
    { key: 'version', label: 'Version', mono: true },
    { key: 'status', label: 'Status', width: '120px' },
    { key: 'created_at', label: 'Created' },
    { key: 'actions', label: '', align: 'right' as const, width: '180px' }
  ];

  function statusTone(s: string): 'success' | 'warning' | 'neutral' {
    if (s === 'published') return 'success';
    if (s === 'draft') return 'warning';
    return 'neutral';
  }
</script>

<PageHeader title={detail?.title ?? id} description={detail?.description}>
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
  </div>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if !detail && !loading}
  <EmptyState title={$t('authored.empty.title')} description={$t('authored.empty.description')} />
{:else if detail}
  <Tabs {tabs} bind:active={activeTab} />

  {#if activeTab === 'manifest'}
    <section class="card">
      <KeyValueGrid items={detailKV} />
      <CodeBlock language="json" code={JSON.stringify(detail.manifest, null, 2)} />
    </section>
  {:else if activeTab === 'files'}
    <section class="card">
      {#if detail.files.length === 0}
        <EmptyState title={$t('common.empty')} compact />
      {:else}
        {#each detail.files as f}
          <h3><code>{f.relpath}</code></h3>
          <CodeBlock
            language={f.mime_type === 'text/markdown' ? 'markdown' : 'plaintext'}
            code={f.body}
          />
        {/each}
      {/if}
    </section>
  {:else if activeTab === 'versions'}
    <section class="card">
      <Table columns={versionCols} rows={history}>
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'status'}
            <Badge tone={statusTone(row.status)}>{row.status}</Badge>
          {:else if column.key === 'actions'}
            {#if row.status === 'draft'}
              <Button size="sm" on:click={() => publish(row.version)}>
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
  .card {
    margin-top: var(--space-4);
    background: var(--color-surface-1);
    border-radius: var(--radius-md);
    padding: var(--space-4);
  }
  h3 {
    margin: var(--space-3) 0 var(--space-2) 0;
    font-size: var(--font-size-body-sm);
    font-weight: 600;
    color: var(--color-text-secondary);
  }
  .muted {
    color: var(--color-text-tertiary);
  }
</style>
