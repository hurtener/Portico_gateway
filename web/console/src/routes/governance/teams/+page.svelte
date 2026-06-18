<script lang="ts">
  /**
   * Governance · Teams (Phase 15.5). A group that optionally belongs to one
   * Customer; a budget parent for VKs. List + right-rail Inspector CRUD over
   * /api/governance/teams.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable } from '$lib/api';
  import type { Team, Customer } from '$lib/api';
  import {
    Button,
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
  let teams: Team[] = [];
  let customers: Customer[] = [];

  let draft: Team | null = null;
  let isNew = false;
  let saving = false;

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'customer_id', label: 'Customer' },
    { key: 'description', label: 'Description' }
  ];

  $: customerOptions = [
    { value: '', label: '— none (standalone) —' },
    ...customers.map((c) => ({ value: c.id, label: c.name }))
  ];

  function emptyTeam(): Team {
    return { id: '', name: '', customer_id: '', description: '' };
  }

  onMount(load);

  async function load() {
    error = '';
    try {
      teams = (await api.listTeams()) ?? [];
      customers = (await api.listCustomers()) ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) unavailable = true;
      else error = e instanceof Error ? e.message : 'Failed to load teams';
    } finally {
      loading = false;
    }
  }

  function customerName(id: string | undefined): string {
    if (!id) return '—';
    return customers.find((c) => c.id === id)?.name ?? id;
  }

  function openNew() {
    draft = emptyTeam();
    isNew = true;
  }
  async function openEdit(id: string) {
    try {
      draft = await api.getTeam(id);
      isNew = false;
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Failed to load team');
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
    const payload: Partial<Team> = {
      name: draft.name.trim(),
      customer_id: draft.customer_id,
      description: draft.description
    };
    saving = true;
    try {
      if (isNew) {
        await api.createTeam(payload);
        toast.success('Team created');
      } else {
        await api.updateTeam(draft.id, payload);
        toast.success('Team updated');
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
      await api.deleteTeam(draft.id);
      toast.success('Team deleted');
      closeInspector();
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Delete failed');
    }
  }
</script>

<PageHeader
  title="Teams"
  description="Groups under a customer; budget parents for virtual keys."
  compact
>
  <div slot="actions">
    <PageActionGroup>
      <Button variant="primary" size="sm" on:click={openNew}>
        <IconPlus slot="leading" size={14} />
        Add team
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
  <Table {columns} rows={teams} rowKeyField="id" on:rowclick={(e) => openEdit(e.detail.row.id)}>
    <svelte:fragment slot="cell" let:row let:column>
      {#if column.key === 'name'}
        <strong>{row.name}</strong>
      {:else if column.key === 'customer_id'}
        {customerName(row.customer_id)}
      {:else}
        {row[column.key] || '—'}
      {/if}
    </svelte:fragment>
    <svelte:fragment slot="empty">
      <EmptyState
        title="No teams yet"
        description="Create a team to attach virtual keys and budgets."
      >
        <svelte:fragment slot="actions">
          <Button variant="primary" size="sm" on:click={openNew}>
            <IconPlus slot="leading" size={14} />
            Add team
          </Button>
        </svelte:fragment>
      </EmptyState>
    </svelte:fragment>
  </Table>

  <Inspector open={draft !== null} on:close={closeInspector}>
    {#if draft}
      <section class="card">
        <h4>{isNew ? 'New team' : 'Edit team'}</h4>
        <Input bind:value={draft.name} label="Name" block />
        <Select bind:value={draft.customer_id} label="Customer" options={customerOptions} />
        <Input bind:value={draft.description} label="Description" block />
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
