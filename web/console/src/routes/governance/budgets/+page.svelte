<script lang="ts">
  /**
   * Governance · Budgets (Phase 15.5). Define a spend/usage cap on any
   * (scope_kind, scope_id, metric, period) tuple. List + right-rail Inspector
   * CRUD over /api/governance/budgets. The hierarchical enforcer (VK → team →
   * customer → tenant) consumes these.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable } from '$lib/api';
  import type { Budget } from '$lib/api';
  import {
    Badge,
    Button,
    Checkbox,
    EmptyState,
    Input,
    Inspector,
    PageActionGroup,
    PageHeader,
    Select,
    Skeleton,
    Table,
    toast
  } from '$lib/components';
  import IconPlus from 'lucide-svelte/icons/plus';

  let loading = true;
  let unavailable = false;
  let error = '';
  let budgets: Budget[] = [];

  let draft: Budget | null = null;
  let isNew = false;
  let saving = false;
  let limitText = '0'; // Input binds strings; limit_val is numeric.

  const columns = [
    { key: 'scope', label: 'Scope' },
    { key: 'metric', label: 'Metric' },
    { key: 'period', label: 'Period' },
    { key: 'limit_val', label: 'Limit', align: 'right' as const },
    { key: 'enabled', label: 'Status' }
  ];

  const scopeKindOptions = ['vk', 'team', 'customer', 'tenant'].map((v) => ({
    value: v,
    label: v
  }));
  const metricOptions = ['cost_usd', 'tokens', 'requests'].map((v) => ({ value: v, label: v }));
  const periodOptions = ['1m', '1h', '1d', '1w', '1M', '1Y'].map((v) => ({ value: v, label: v }));
  const alignmentOptions = ['rolling', 'calendar'].map((v) => ({ value: v, label: v }));

  function emptyBudget(): Budget {
    return {
      id: '',
      scope_kind: 'vk',
      scope_id: '',
      metric: 'cost_usd',
      period: '1d',
      alignment: 'rolling',
      limit_val: 0,
      enabled: true
    };
  }

  onMount(load);

  async function load() {
    error = '';
    try {
      budgets = (await api.listBudgets()) ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) unavailable = true;
      else error = e instanceof Error ? e.message : 'Failed to load budgets';
    } finally {
      loading = false;
    }
  }

  function openNew() {
    draft = emptyBudget();
    isNew = true;
    limitText = '0';
  }
  async function openEdit(id: string) {
    try {
      draft = await api.getBudget(id);
      isNew = false;
      limitText = String(draft.limit_val);
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Failed to load budget');
    }
  }
  function closeInspector() {
    draft = null;
  }

  async function save() {
    if (!draft) return;
    if (!draft.scope_id.trim()) {
      toast.danger('Scope id is required');
      return;
    }
    const payload: Partial<Budget> = {
      scope_kind: draft.scope_kind,
      scope_id: draft.scope_id.trim(),
      metric: draft.metric,
      period: draft.period,
      alignment: draft.alignment,
      limit_val: Number(limitText),
      enabled: draft.enabled
    };
    saving = true;
    try {
      if (isNew) {
        await api.createBudget(payload);
        toast.success('Budget created');
      } else {
        await api.updateBudget(draft.id, payload);
        toast.success('Budget updated');
      }
      closeInspector();
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Save failed');
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!draft || isNew) return;
    try {
      await api.deleteBudget(draft.id);
      toast.success('Budget deleted');
      closeInspector();
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Delete failed');
    }
  }
</script>

<PageHeader
  title="Budgets"
  description="Hierarchical spend/usage caps enforced VK → team → customer → tenant."
  compact
>
  <div slot="actions">
    <PageActionGroup>
      <Button variant="primary" size="sm" on:click={openNew}>
        <IconPlus slot="leading" size={14} />
        Add budget
      </Button>
    </PageActionGroup>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="Budgets not configured"
    description="The budget engine is not wired in this build."
  />
{:else if loading}
  <Skeleton height="280px" />
{:else}
  <Table {columns} rows={budgets} rowKeyField="id" on:rowclick={(e) => openEdit(e.detail.row.id)}>
    <svelte:fragment slot="cell" let:row let:column>
      {#if column.key === 'scope'}
        <strong>{row.scope_kind}</strong>:{row.scope_id}
      {:else if column.key === 'limit_val'}
        {row.limit_val}
      {:else if column.key === 'enabled'}
        <Badge tone={row.enabled ? 'success' : 'neutral'}
          >{row.enabled ? 'enabled' : 'disabled'}</Badge
        >
      {:else}
        {row[column.key]}
      {/if}
    </svelte:fragment>
    <svelte:fragment slot="empty">
      <EmptyState
        title="No budgets yet"
        description="Add a cap on a VK, team, customer, or the tenant."
      >
        <svelte:fragment slot="actions">
          <Button variant="primary" size="sm" on:click={openNew}>
            <IconPlus slot="leading" size={14} />
            Add budget
          </Button>
        </svelte:fragment>
      </EmptyState>
    </svelte:fragment>
  </Table>

  <Inspector open={draft !== null} on:close={closeInspector}>
    {#if draft}
      <section class="card">
        <h4>{isNew ? 'New budget' : 'Edit budget'}</h4>
        <Select bind:value={draft.scope_kind} label="Scope kind" options={scopeKindOptions} />
        <Input
          bind:value={draft.scope_id}
          label="Scope id (vk/team/customer id, or tenant id)"
          block
        />
        <Select bind:value={draft.metric} label="Metric" options={metricOptions} />
        <Input type="number" bind:value={limitText} label="Limit" block />
        <Select bind:value={draft.period} label="Period" options={periodOptions} />
        <Select bind:value={draft.alignment} label="Reset alignment" options={alignmentOptions} />
        <Checkbox bind:checked={draft.enabled} label="Enabled" />
      </section>
      <div class="actions">
        <Button variant="primary" on:click={save} disabled={saving}>
          {saving ? 'Saving…' : isNew ? 'Create' : 'Save'}
        </Button>
        {#if !isNew}
          <Button variant="ghost" on:click={remove}>Delete</Button>
        {/if}
      </div>
    {/if}
  </Inspector>
{/if}

<style>
  .error {
    color: var(--color-danger-fg, var(--color-text));
    margin: var(--space-2) 0;
  }
  .card {
    margin-bottom: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .card h4 {
    margin: 0 0 var(--space-1);
    font-size: var(--font-size-body-lg);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-4);
  }
</style>
