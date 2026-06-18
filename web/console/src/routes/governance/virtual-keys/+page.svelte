<script lang="ts">
  /**
   * Governance · Virtual Keys (Phase 15.5). The headline credential screen:
   * pk-portico-* keys with scopes, provider/model/MCP allowlists, a budget
   * parent, and an optional Agent Profile. The secret is shown exactly once on
   * create/rotate (a non-dismissible modal). Reads/writes /api/governance/
   * virtual-keys (+ /rotate, /budget). VKs are not edited in place — they are
   * rotated (new secret) or revoked.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable } from '$lib/api';
  import type { VirtualKey, Team, Customer, BudgetLevelStatus } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    Input,
    Inspector,
    Modal,
    PageActionGroup,
    PageHeader,
    Select,
    Skeleton,
    Table,
    Textarea,
    toast
  } from '$lib/components';
  import IconPlus from 'lucide-svelte/icons/plus';

  let loading = true;
  let unavailable = false;
  let error = '';
  let vks: VirtualKey[] = [];
  let teams: Team[] = [];
  let customers: Customer[] = [];

  let draft: VirtualKey | null = null;
  let isNew = false;
  let saving = false;
  let budget: BudgetLevelStatus[] = [];

  // Allowlist/scope textareas (newline/comma separated).
  let scopesText = '';
  let providersText = '';
  let modelsText = '';
  let serversText = '';

  // Secret-once modal state.
  let secretToken = '';
  let secretOpen = false;

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'parent', label: 'Parent' },
    { key: 'scopes', label: 'Scopes', align: 'right' as const },
    { key: 'enabled', label: 'Status' }
  ];

  const parentKindOptions = [
    { value: 'none', label: 'none (standalone)' },
    { value: 'team', label: 'team' },
    { value: 'customer', label: 'customer' }
  ];

  $: parentIdOptions =
    draft?.parent_kind === 'team'
      ? teams.map((t) => ({ value: t.id, label: t.name }))
      : draft?.parent_kind === 'customer'
        ? customers.map((c) => ({ value: c.id, label: c.name }))
        : [];

  function linesToArr(s: string): string[] {
    return s
      .split(/[\n,]/)
      .map((x) => x.trim())
      .filter((x) => x.length > 0);
  }

  function emptyVK(): VirtualKey {
    return {
      id: '',
      name: '',
      parent_kind: 'none',
      scopes: [],
      provider_allowlist: [],
      model_allowlist: [],
      mcp_server_allowlist: [],
      enabled: true
    };
  }

  onMount(load);

  async function load() {
    error = '';
    try {
      vks = (await api.listVirtualKeys()) ?? [];
      teams = (await api.listTeams()) ?? [];
      customers = (await api.listCustomers()) ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) unavailable = true;
      else error = e instanceof Error ? e.message : 'Failed to load virtual keys';
    } finally {
      loading = false;
    }
  }

  function parentLabel(vk: VirtualKey): string {
    if (vk.parent_kind === 'none' || !vk.parent_id) return '—';
    return `${vk.parent_kind}:${vk.parent_id}`;
  }

  function openNew() {
    draft = emptyVK();
    isNew = true;
    budget = [];
    scopesText = '';
    providersText = '';
    modelsText = '';
    serversText = '';
  }

  async function openEdit(id: string) {
    try {
      draft = await api.getVirtualKey(id);
      isNew = false;
      scopesText = (draft.scopes ?? []).join('\n');
      providersText = (draft.provider_allowlist ?? []).join('\n');
      modelsText = (draft.model_allowlist ?? []).join('\n');
      serversText = (draft.mcp_server_allowlist ?? []).join('\n');
      budget = [];
      try {
        const b = await api.getVirtualKeyBudget(id);
        budget = b.levels ?? [];
      } catch {
        budget = [];
      }
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Failed to load virtual key');
    }
  }

  function closeInspector() {
    draft = null;
  }

  async function create() {
    if (!draft) return;
    if (!draft.name.trim()) {
      toast.danger('Name is required');
      return;
    }
    const payload: Partial<VirtualKey> = {
      name: draft.name.trim(),
      parent_kind: draft.parent_kind,
      parent_id: draft.parent_kind === 'none' ? '' : draft.parent_id,
      profile_id: draft.profile_id,
      scopes: linesToArr(scopesText),
      provider_allowlist: linesToArr(providersText),
      model_allowlist: linesToArr(modelsText),
      mcp_server_allowlist: linesToArr(serversText)
    };
    saving = true;
    try {
      const created = await api.createVirtualKey(payload);
      showSecret(created.token);
      closeInspector();
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Create failed');
    } finally {
      saving = false;
    }
  }

  async function rotate() {
    if (!draft || isNew) return;
    try {
      const r = await api.rotateVirtualKey(draft.id);
      showSecret(r.token);
      closeInspector();
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Rotate failed');
    }
  }

  async function revoke() {
    if (!draft || isNew) return;
    try {
      await api.revokeVirtualKey(draft.id);
      toast.success('Virtual key revoked');
      closeInspector();
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Revoke failed');
    }
  }

  function showSecret(token: string) {
    secretToken = token;
    secretOpen = true;
  }
  function closeSecret() {
    secretOpen = false;
    secretToken = '';
  }
  async function copySecret() {
    try {
      await navigator.clipboard.writeText(secretToken);
      toast.success('Copied to clipboard');
    } catch {
      toast.danger('Copy failed — select and copy manually');
    }
  }
</script>

<PageHeader
  title="Virtual Keys"
  description="pk-portico-* credentials with scopes, allowlists, budgets, and a profile."
  compact
>
  <div slot="actions">
    <PageActionGroup>
      <Button variant="primary" size="sm" on:click={openNew}>
        <IconPlus slot="leading" size={14} />
        Add virtual key
      </Button>
    </PageActionGroup>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="Governance not configured"
    description="Virtual keys are not wired in this build."
  />
{:else if loading}
  <Skeleton height="280px" />
{:else}
  <Table {columns} rows={vks} rowKeyField="id" on:rowclick={(e) => openEdit(e.detail.row.id)}>
    <svelte:fragment slot="cell" let:row let:column>
      {#if column.key === 'name'}
        <strong>{row.name}</strong>
      {:else if column.key === 'parent'}
        {parentLabel(row)}
      {:else if column.key === 'scopes'}
        {(row.scopes ?? []).length || '—'}
      {:else if column.key === 'enabled'}
        <Badge tone={row.enabled && !row.revoked_at ? 'success' : 'neutral'}>
          {row.revoked_at ? 'revoked' : row.enabled ? 'enabled' : 'disabled'}
        </Badge>
      {/if}
    </svelte:fragment>
    <svelte:fragment slot="empty">
      <EmptyState
        title="No virtual keys yet"
        description="Issue one to give an app a scoped, budgeted credential."
      >
        <svelte:fragment slot="actions">
          <Button variant="primary" size="sm" on:click={openNew}>
            <IconPlus slot="leading" size={14} />
            Add virtual key
          </Button>
        </svelte:fragment>
      </EmptyState>
    </svelte:fragment>
  </Table>

  <Inspector open={draft !== null} on:close={closeInspector}>
    {#if draft}
      <section class="card">
        <h4>{isNew ? 'New virtual key' : draft.name}</h4>
        <Input bind:value={draft.name} label="Name" block disabled={!isNew} />
        {#if isNew}
          <Select
            bind:value={draft.parent_kind}
            label="Budget parent"
            options={parentKindOptions}
          />
          {#if draft.parent_kind !== 'none'}
            <Select bind:value={draft.parent_id} label="Parent" options={parentIdOptions} />
          {/if}
          <Input bind:value={draft.profile_id} label="Agent Profile id (optional)" block />
        {:else}
          <p class="hint">
            Parent: {parentLabel(draft)}{draft.profile_id ? ` · profile ${draft.profile_id}` : ''}
          </p>
        {/if}
      </section>

      <section class="card">
        <h4>Scopes & allowlists</h4>
        <p class="hint">One per line. Empty allowlist = all allowed.</p>
        <Textarea bind:value={scopesText} label="Scopes" rows={2} disabled={!isNew} />
        <Textarea
          bind:value={providersText}
          label="Provider allowlist"
          rows={2}
          disabled={!isNew}
        />
        <Textarea bind:value={modelsText} label="Model allowlist" rows={2} disabled={!isNew} />
        <Textarea
          bind:value={serversText}
          label="MCP server allowlist"
          rows={2}
          disabled={!isNew}
        />
      </section>

      {#if !isNew && budget.length > 0}
        <section class="card">
          <h4>Budget headroom</h4>
          {#each budget as b (b.budget_id)}
            <div class="bar-row">
              <span class="bar-label">{b.level} · {b.metric}</span>
              <div class="bar">
                <div class="bar-fill" style={`width:${100 - b.headroom_pct}%`}></div>
              </div>
              <span class="bar-pct">{b.used.toFixed(2)}/{b.limit.toFixed(2)}</span>
            </div>
          {/each}
        </section>
      {/if}

      <div class="actions">
        {#if isNew}
          <Button variant="primary" on:click={create} disabled={saving}>
            {saving ? 'Creating…' : 'Create'}
          </Button>
        {:else}
          <Button variant="secondary" on:click={rotate}>Rotate</Button>
          <Button variant="ghost" on:click={revoke}>Revoke</Button>
        {/if}
      </div>
    {/if}
  </Inspector>
{/if}

<Modal
  open={secretOpen}
  title="Copy your virtual key now"
  dismissible={false}
  onClose={closeSecret}
>
  <p class="warn">
    This secret is shown <strong>once</strong>. Store it securely — it cannot be retrieved again.
  </p>
  <pre class="secret" data-testid="vk-secret">{secretToken}</pre>
  <div class="actions">
    <Button variant="secondary" on:click={copySecret}>Copy</Button>
    <Button variant="primary" on:click={closeSecret}>I've saved it</Button>
  </div>
</Modal>

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
  .warn {
    color: var(--color-warning-fg, var(--color-text));
    font-size: var(--font-size-sm);
  }
  .secret {
    background: var(--color-surface-sunken, var(--color-surface));
    padding: var(--space-3);
    border-radius: var(--radius-md);
    overflow-x: auto;
    font-size: var(--font-size-sm);
    user-select: all;
  }
  .actions {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-4);
  }
  .bar-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .bar-label {
    flex: 0 0 40%;
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
  }
  .bar {
    flex: 1;
    height: 8px;
    background: var(--color-surface-sunken, var(--color-border));
    border-radius: var(--radius-full, 999px);
    overflow: hidden;
  }
  .bar-fill {
    height: 100%;
    background: var(--color-accent, var(--color-text));
  }
  .bar-pct {
    flex: 0 0 auto;
    font-size: var(--font-size-sm);
    color: var(--color-text-muted);
  }
</style>
