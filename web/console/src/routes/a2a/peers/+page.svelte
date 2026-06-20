<script lang="ts">
  /**
   * A2A · Peers (Phase 16). Agent-to-Agent peer registry — list page with
   * right-rail Inspector CRUD over /api/a2a/peers. The agent_card_json field
   * is read-only (populated by auto-discovery) and shown in the inspector when
   * present. Operators can create/edit name, endpoint, egress_auth_ref, and
   * enabled; they cannot edit the discovered agent card.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable } from '$lib/api';
  import type { A2APeer } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    Input,
    Inspector,
    PageHeader,
    Skeleton,
    Table,
    Toggle,
    toast
  } from '$lib/components';
  import IconPlus from 'lucide-svelte/icons/plus';

  let loading = true;
  let unavailable = false;
  let error = '';
  let peers: A2APeer[] = [];

  let draft: A2APeer | null = null;
  let isNew = false;
  let saving = false;

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'endpoint', label: 'Endpoint' },
    { key: 'egress_auth_ref', label: 'Auth ref' },
    { key: 'enabled', label: 'Status' }
  ];

  function emptyPeer(): A2APeer {
    return {
      id: '',
      name: '',
      endpoint: '',
      egress_auth_ref: '',
      enabled: true,
      created_at: '',
      updated_at: ''
    };
  }

  onMount(load);

  async function load() {
    error = '';
    try {
      peers = (await api.listA2APeers()) ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) unavailable = true;
      else error = e instanceof Error ? e.message : 'Failed to load A2A peers';
    } finally {
      loading = false;
    }
  }

  function openNew() {
    draft = emptyPeer();
    isNew = true;
  }

  async function openEdit(id: string) {
    try {
      draft = await api.getA2APeer(id);
      isNew = false;
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Failed to load peer');
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
    if (!draft.endpoint.trim()) {
      toast.danger('Endpoint is required');
      return;
    }
    const payload: Partial<A2APeer> = {
      name: draft.name.trim(),
      endpoint: draft.endpoint.trim(),
      egress_auth_ref: draft.egress_auth_ref?.trim() || undefined,
      enabled: draft.enabled
    };
    saving = true;
    try {
      if (isNew) {
        await api.createA2APeer(payload);
        toast.success('Peer created');
      } else {
        await api.updateA2APeer(draft.id, payload);
        toast.success('Peer updated');
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
      await api.deleteA2APeer(draft.id);
      toast.success('Peer deleted');
      closeInspector();
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Delete failed');
    }
  }
</script>

<PageHeader
  title="A2A Peers"
  description="Remote Agent-to-Agent peers this gateway can route tasks to."
  compact
>
  <div slot="actions">
    <Button variant="primary" size="sm" on:click={openNew}>
      <IconPlus slot="leading" size={14} />
      Add peer
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="A2A not configured"
    description="The A2A peer store is not wired in this build."
  />
{:else if loading}
  <Skeleton height="280px" />
{:else}
  <Table {columns} rows={peers} rowKeyField="id" onRowClick={(row) => openEdit(row.id)}>
    <svelte:fragment slot="cell" let:row let:column>
      {#if column.key === 'name'}
        <strong>{row.name}</strong>
      {:else if column.key === 'endpoint'}
        <span class="mono">{row.endpoint}</span>
      {:else if column.key === 'egress_auth_ref'}
        {row.egress_auth_ref || '—'}
      {:else if column.key === 'enabled'}
        <Badge tone={row.enabled ? 'success' : 'neutral'}>
          {row.enabled ? 'enabled' : 'disabled'}
        </Badge>
      {/if}
    </svelte:fragment>
    <svelte:fragment slot="empty">
      <EmptyState
        title="No A2A peers yet"
        description="Register a remote agent peer to start routing tasks to it."
      >
        <svelte:fragment slot="actions">
          <Button variant="primary" size="sm" on:click={openNew}>
            <IconPlus slot="leading" size={14} />
            Add peer
          </Button>
        </svelte:fragment>
      </EmptyState>
    </svelte:fragment>
  </Table>

  <Inspector open={draft !== null} on:close={closeInspector}>
    {#if draft}
      <section class="card">
        <h4>{isNew ? 'New peer' : 'Edit peer'}</h4>
        <Input bind:value={draft.name} label="Name" block />
        <Input bind:value={draft.endpoint} label="Endpoint URL" block />
        <Input bind:value={draft.egress_auth_ref} label="Egress auth ref (optional)" block />
        <div class="toggle-row">
          <span class="toggle-label">Enabled</span>
          <Toggle bind:checked={draft.enabled} />
        </div>
      </section>

      {#if !isNew && draft.agent_card_json}
        <section class="card">
          <h4>Discovered agent card</h4>
          <p class="hint">Read-only — populated automatically via A2A handshake.</p>
          <pre class="card-code">{draft.agent_card_json}</pre>
        </section>
      {/if}

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
  .hint {
    margin: 0;
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-sm);
  }
  .card-code {
    background: var(--color-surface-sunken, var(--color-surface));
    padding: var(--space-3);
    border-radius: var(--radius-md);
    overflow-x: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-sm);
    white-space: pre-wrap;
    word-break: break-all;
  }
  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }
  .toggle-label {
    font-size: var(--font-size-sm);
    color: var(--color-text-secondary);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-4);
  }
</style>
