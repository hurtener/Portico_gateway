<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type Snapshot } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    EmptyState,
    IdBadge,
    KeyValueGrid,
    PageHeader,
    Table
  } from '$lib/components';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';

  let snap: Snapshot | null = null;
  let loading = true;
  let error = '';

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

  function fmt(t: string): string {
    try {
      return new Date(t).toLocaleString();
    } catch {
      return t;
    }
  }

  $: meta = snap
    ? [
        { label: 'Tenant', value: snap.tenant_id },
        { label: 'Created', value: fmt(snap.created_at) }
      ]
    : [];

  const serverColumns = [
    { key: 'id', label: 'Server' },
    { key: 'transport', label: 'Transport' },
    { key: 'runtime_mode', label: 'Mode' },
    { key: 'schema_hash', label: 'Schema fingerprint' },
    { key: 'health', label: 'Health' }
  ];

  const toolColumns = [
    { key: 'namespaced_name', label: 'Name', mono: true },
    { key: 'risk_class', label: 'Risk' },
    { key: 'requires_approval', label: 'Approval' },
    { key: 'skill_id', label: 'Skill' },
    { key: 'hash', label: 'Fingerprint' }
  ];

  const skillColumns = [
    { key: 'id', label: 'Skill' },
    { key: 'version', label: 'Version', mono: true },
    { key: 'enabled_for_session', label: 'Session enabled' }
  ];
</script>

<PageHeader title={`Snapshot ${id}`}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: 'Snapshots', href: '/snapshots' }, { label: id }]}
  />
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if snap}
  <section class="panel">
    <KeyValueGrid items={meta} columns={2} />
    <div class="meta-ids">
      {#if snap.session_id}
        <IdBadge value={snap.session_id} label="Session" />
      {/if}
      <IdBadge value={snap.overall_hash} label="Fingerprint" chars={8} />
    </div>
  </section>

  {#if snap.warnings && snap.warnings.length > 0}
    <section class="warn">
      <h2 class="warn-title"><IconAlertTriangle size={16} aria-hidden="true" /> Warnings</h2>
      <ul>
        {#each snap.warnings as w (w)}<li>{w}</li>{/each}
      </ul>
    </section>
  {/if}

  <section class="block">
    <h2 class="section-title">Servers ({snap.servers.length})</h2>
    <Table columns={serverColumns} rows={snap.servers} empty="No servers in this snapshot.">
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
          <Badge tone={row.health === 'ready' || row.health === 'healthy' ? 'success' : 'neutral'}>
            {row.health}
          </Badge>
        {:else}
          {row[column.key] ?? ''}
        {/if}
      </svelte:fragment>
    </Table>
  </section>

  <section class="block">
    <h2 class="section-title">Tools ({snap.tools.length})</h2>
    <Table columns={toolColumns} rows={snap.tools} empty="No tools.">
      <svelte:fragment slot="cell" let:row let:column>
        {#if column.key === 'risk_class'}
          <Badge tone="neutral">{row.risk_class}</Badge>
        {:else if column.key === 'requires_approval'}
          <Badge tone={row.requires_approval ? 'warning' : 'neutral'}>
            {row.requires_approval ? 'yes' : 'no'}
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

  {#if snap.skills && snap.skills.length > 0}
    <section class="block">
      <h2 class="section-title">Skills ({snap.skills.length})</h2>
      <Table columns={skillColumns} rows={snap.skills} empty="No skills.">
        <svelte:fragment slot="cell" let:row let:column>
          {#if column.key === 'id'}
            <IdBadge value={row.id} />
          {:else if column.key === 'enabled_for_session'}
            <Badge tone={row.enabled_for_session ? 'success' : 'neutral'}>
              {row.enabled_for_session ? 'yes' : 'no'}
            </Badge>
          {:else}
            {row[column.key] ?? ''}
          {/if}
        </svelte:fragment>
      </Table>
    </section>
  {/if}
{:else if !loading}
  <EmptyState title="Snapshot not found" description={`No snapshot with id ${id}.`} />
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .panel {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-5);
    margin-bottom: var(--space-6);
  }
  .meta-ids {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-3);
    padding-top: var(--space-3);
    border-top: 1px solid var(--color-border-soft);
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
  .block {
    margin-bottom: var(--space-8);
  }
  .section-title {
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    margin: 0 0 var(--space-3) 0;
  }
  .warn {
    border: 1px solid var(--color-warning);
    background: var(--color-warning-soft);
    color: var(--color-warning);
    padding: var(--space-4) var(--space-5);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-6);
  }
  .warn-title {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0 0 var(--space-2) 0;
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
  }
  .warn ul {
    margin: 0;
    padding-left: var(--space-5);
  }
  .muted {
    color: var(--color-text-tertiary);
  }
</style>
