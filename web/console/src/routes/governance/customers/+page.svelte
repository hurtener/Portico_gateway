<script lang="ts">
  /**
   * Governance · Customers (Phase 15.5). A tenant-scoped grouping that owns
   * Teams + budgets. List + right-rail Inspector CRUD over /api/governance/
   * customers. Customers are budget parents (a VK can belong to one).
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable } from '$lib/api';
  import type { Customer } from '$lib/api';
  import {
    Button,
    EmptyState,
    Input,
    Inspector,
    PageActionGroup,
    PageHeader,
    Skeleton,
    Table,
    toast
  } from '$lib/components';
  import IconPlus from 'lucide-svelte/icons/plus';

  let loading = true;
  let unavailable = false;
  let error = '';
  let customers: Customer[] = [];

  let draft: Customer | null = null;
  let isNew = false;
  let saving = false;

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'description', label: 'Description' },
    { key: 'webhook_url', label: 'Webhook' }
  ];

  function emptyCustomer(): Customer {
    return { id: '', name: '', description: '', webhook_url: '' };
  }

  onMount(load);

  async function load() {
    error = '';
    try {
      customers = (await api.listCustomers()) ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) unavailable = true;
      else error = e instanceof Error ? e.message : 'Failed to load customers';
    } finally {
      loading = false;
    }
  }

  function openNew() {
    draft = emptyCustomer();
    isNew = true;
  }
  async function openEdit(id: string) {
    try {
      draft = await api.getCustomer(id);
      isNew = false;
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Failed to load customer');
    }
  }
  function closeInspector() {
    draft = null;
  }

  async function save() {
    if (!draft) return;
    if (!draft.name.trim()) {
      toast.danger('Name is required');
      return;
    }
    const payload: Partial<Customer> = {
      name: draft.name.trim(),
      description: draft.description,
      webhook_url: draft.webhook_url
    };
    saving = true;
    try {
      if (isNew) {
        await api.createCustomer(payload);
        toast.success('Customer created');
      } else {
        await api.updateCustomer(draft.id, payload);
        toast.success('Customer updated');
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
      await api.deleteCustomer(draft.id);
      toast.success('Customer deleted');
      closeInspector();
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Delete failed');
    }
  }
</script>

<PageHeader
  title="Customers"
  description="Tenant-scoped billing/grouping accounts that own teams and budgets."
  compact
>
  <div slot="actions">
    <PageActionGroup>
      <Button variant="primary" size="sm" on:click={openNew}>
        <IconPlus slot="leading" size={14} />
        Add customer
      </Button>
    </PageActionGroup>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="Governance not configured"
    description="The governance store is not wired in this build."
  />
{:else if loading}
  <Skeleton height="280px" />
{:else}
  <Table {columns} rows={customers} rowKeyField="id" on:rowclick={(e) => openEdit(e.detail.row.id)}>
    <svelte:fragment slot="cell" let:row let:column>
      {#if column.key === 'name'}
        <strong>{row.name}</strong>
      {:else}
        {row[column.key] || '—'}
      {/if}
    </svelte:fragment>
    <svelte:fragment slot="empty">
      <EmptyState title="No customers yet" description="Create one to group teams and budgets.">
        <svelte:fragment slot="actions">
          <Button variant="primary" size="sm" on:click={openNew}>
            <IconPlus slot="leading" size={14} />
            Add customer
          </Button>
        </svelte:fragment>
      </EmptyState>
    </svelte:fragment>
  </Table>

  <Inspector open={draft !== null} on:close={closeInspector}>
    {#if draft}
      <section class="card">
        <h4>{isNew ? 'New customer' : 'Edit customer'}</h4>
        <Input bind:value={draft.name} label="Name" block />
        <Input bind:value={draft.description} label="Description" block />
        <Input bind:value={draft.webhook_url} label="Budget-alert webhook URL" block />
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
