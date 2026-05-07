<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api, type Approval } from '$lib/api';
  import { Badge, Button, EmptyState, PageHeader, Table } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconCheck from 'lucide-svelte/icons/check';
  import IconX from 'lucide-svelte/icons/x';

  let approvals: Approval[] = [];
  let loading = true;
  let error = '';
  let timer: ReturnType<typeof setInterval> | null = null;

  type Tone = 'success' | 'warning' | 'danger' | 'info' | 'neutral' | 'accent';
  function riskTone(rc: string): Tone {
    const v = rc.toLowerCase();
    if (v === 'destructive' || v === 'sensitive_read') return 'danger';
    if (v === 'external_side_effect') return 'warning';
    if (v === 'idempotent_read') return 'info';
    return 'neutral';
  }

  async function refresh() {
    try {
      approvals = await api.listApprovals();
      error = '';
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function approve(id: string) {
    try {
      await api.approveApproval(id);
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  async function deny(id: string) {
    try {
      await api.denyApproval(id);
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(() => {
    refresh();
    timer = setInterval(refresh, 2000);
  });

  onDestroy(() => {
    if (timer !== null) clearInterval(timer);
  });

  function fmt(t: string): string {
    try {
      return new Date(t).toLocaleString();
    } catch {
      return t;
    }
  }

  const columns = [
    { key: 'tool', label: 'Tool', mono: true },
    { key: 'risk_class', label: 'Risk', width: '160px' },
    { key: 'session_id', label: 'Session', mono: true },
    { key: 'created_at', label: 'Created' },
    { key: 'expires_at', label: 'Expires' },
    { key: 'actions', label: '', align: 'right' as const, width: '180px' }
  ];
</script>

<PageHeader title={$t('approvals.title')} description={$t('approvals.description')}>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<Table {columns} rows={approvals} empty="No pending approvals.">
  <svelte:fragment slot="cell" let:row let:column>
    {#if column.key === 'risk_class'}
      <Badge tone={riskTone(row.risk_class)}>{row.risk_class}</Badge>
    {:else if column.key === 'session_id'}
      <code class="mono">{row.session_id}</code>
    {:else if column.key === 'created_at'}
      <span class="muted">{fmt(row.created_at)}</span>
    {:else if column.key === 'expires_at'}
      <span class="muted">{fmt(row.expires_at)}</span>
    {:else if column.key === 'actions'}
      <div class="actions">
        <Button size="sm" variant="primary" on:click={() => approve(row.id)}>
          <IconCheck slot="leading" size={14} />
          {$t('approvals.action.approve')}
        </Button>
        <Button size="sm" variant="destructive" on:click={() => deny(row.id)}>
          <IconX slot="leading" size={14} />
          {$t('approvals.action.deny')}
        </Button>
      </div>
    {:else}
      {row[column.key] ?? ''}
    {/if}
  </svelte:fragment>
  <svelte:fragment slot="empty">
    <EmptyState
      title={$t('approvals.empty.title')}
      description={$t('approvals.empty.description')}
      compact
    />
  </svelte:fragment>
</Table>

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
  }
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .actions {
    display: inline-flex;
    gap: var(--space-2);
    justify-content: flex-end;
  }
</style>
