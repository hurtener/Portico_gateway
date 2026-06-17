<script lang="ts">
  /**
   * LLM models (Phase 13). Operator CRUD over the per-tenant model alias
   * registry. An alias is the friendly name a client routes to (e.g.
   * "gpt-4o"); it maps to a configured provider + the provider's own model
   * id, with optional default params and capability tags. Mirrors the
   * providers screen: list + segmented filter + Inspector right-rail editor.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable, type LLMModel, type LLMProvider } from '$lib/api';
  import {
    Badge,
    Button,
    Checkbox,
    EmptyState,
    IdentityCell,
    Input,
    Inspector,
    PageHeader,
    SegmentedControl,
    Select,
    Table,
    Textarea,
    toast
  } from '$lib/components';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconTrash from 'lucide-svelte/icons/trash-2';

  // Common capability tags surfaced as quick-add chips; the field stays
  // free-form so operators can add provider-specific ones.
  const COMMON_CAPS = ['chat', 'completion', 'embedding', 'vision', 'tools', 'streaming'];

  let models: LLMModel[] = [];
  let providers: LLMProvider[] = [];
  let loading = true;
  let unavailable = false;
  let error = '';

  let filter: 'all' | 'enabled' | 'disabled' = 'all';

  // Inspector state.
  let selected: LLMModel | null = null;
  let creating = false;
  let inspectorTab = 'settings';
  let saving = false;

  // Editor form fields (decoupled from `selected` so cancel is clean).
  let fAlias = '';
  let fProvider = '';
  let fProviderModel = '';
  let fCapabilities = '';
  let fParams = '';
  let fEnabled = true;

  const columns = [
    { key: 'alias', label: 'Alias' },
    { key: 'provider', label: 'Provider' },
    { key: 'provider_model', label: 'Model' },
    { key: 'capabilities', label: 'Capabilities' },
    { key: 'enabled', label: 'Status' }
  ];

  $: filtered = models.filter((m) => {
    if (filter === 'enabled') return m.enabled;
    if (filter === 'disabled') return !m.enabled;
    return true;
  });

  // Select takes an options array ({value,label}), not slotted <option>s.
  $: providerOptions = providers.map((p) => ({ value: p.name, label: p.name }));

  function setFilter(v: string) {
    filter = v as 'all' | 'enabled' | 'disabled';
  }

  const filterOptions = [
    { value: 'all', label: 'All' },
    { value: 'enabled', label: 'Enabled' },
    { value: 'disabled', label: 'Disabled' }
  ];

  const inspectorTabs = [{ id: 'settings', label: 'Settings' }];

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      const [mRes, pRes] = await Promise.all([api.listLLMModels(), api.listLLMProviders()]);
      models = mRes.models ?? [];
      providers = pRes.providers ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        unavailable = true;
      } else {
        error = e instanceof Error ? e.message : 'Failed to load models';
      }
    } finally {
      loading = false;
    }
  }

  function openCreate() {
    creating = true;
    selected = { alias: '', provider_name: '', provider_model: '', enabled: true };
    fAlias = '';
    fProvider = providers[0]?.name ?? '';
    fProviderModel = '';
    fCapabilities = '';
    fParams = '';
    fEnabled = true;
    inspectorTab = 'settings';
  }

  function selectRow(row: LLMModel) {
    creating = false;
    selected = row;
    fAlias = row.alias;
    fProvider = row.provider_name;
    fProviderModel = row.provider_model;
    fCapabilities = (row.capabilities ?? []).join(', ');
    fParams = row.default_params ? JSON.stringify(row.default_params, null, 2) : '';
    fEnabled = row.enabled;
    inspectorTab = 'settings';
  }

  function closeInspector() {
    selected = null;
    creating = false;
  }

  function toggleCap(cap: string) {
    const set = new Set(
      fCapabilities
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
    );
    if (set.has(cap)) set.delete(cap);
    else set.add(cap);
    fCapabilities = [...set].join(', ');
  }

  function parseParams(): Record<string, unknown> | undefined {
    if (!fParams.trim()) return undefined;
    let parsed: unknown;
    try {
      parsed = JSON.parse(fParams);
    } catch {
      throw new Error('Default params must be valid JSON');
    }
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
      throw new Error('Default params must be a JSON object');
    }
    return parsed as Record<string, unknown>;
  }

  async function save() {
    if (!fAlias.trim()) {
      toast.danger('Alias is required');
      return;
    }
    if (!fProvider) {
      toast.danger('A provider is required');
      return;
    }
    if (!fProviderModel.trim()) {
      toast.danger('Provider model is required');
      return;
    }
    saving = true;
    try {
      const caps = fCapabilities
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      const body: LLMModel = {
        alias: fAlias.trim(),
        provider_name: fProvider,
        provider_model: fProviderModel.trim(),
        capabilities: caps.length ? caps : undefined,
        default_params: parseParams(),
        enabled: fEnabled
      };
      if (creating) {
        await api.createLLMModel(body);
        toast.success(`Model "${body.alias}" created`);
      } else {
        await api.updateLLMModel(fAlias.trim(), body);
        toast.success(`Model "${body.alias}" updated`);
      }
      await load();
      closeInspector();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Save failed');
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!selected) return;
    if (!confirm(`Delete model "${selected.alias}"? This cannot be undone.`)) return;
    try {
      await api.deleteLLMModel(selected.alias);
      toast.success(`Model "${selected.alias}" deleted`);
      await load();
      closeInspector();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Delete failed');
    }
  }
</script>

<PageHeader
  title="LLM Models"
  description="Map friendly aliases to a provider model, with default params and capabilities."
  compact
>
  <div slot="actions">
    <Button on:click={openCreate} disabled={providers.length === 0}>
      <IconPlus slot="leading" size={14} />Add model
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="LLM gateway not configured"
    description="The LLM model store is not wired in this build."
  />
{:else}
  <div class="layout" class:has-selection={selected !== null}>
    <div class="main-col">
      <div class="toolbar">
        <SegmentedControl
          options={filterOptions}
          value={filter}
          ariaLabel="Filter models"
          onChange={setFilter}
        />
      </div>

      {#if !loading && providers.length === 0}
        <EmptyState
          title="Add a provider first"
          description="Models map onto a configured provider. Create an LLM provider before adding model aliases."
        >
          <svelte:fragment slot="actions">
            <Button href="/llm/providers" variant="secondary">Go to Providers</Button>
          </svelte:fragment>
        </EmptyState>
      {:else if !loading && models.length === 0}
        <EmptyState
          title="No models yet"
          description="Add a model alias to expose a provider model to clients through the gateway."
        >
          <svelte:fragment slot="actions">
            <Button on:click={openCreate}><IconPlus slot="leading" size={14} />Add model</Button>
          </svelte:fragment>
        </EmptyState>
      {:else}
        <Table
          {columns}
          rows={filtered}
          empty="No models match this filter."
          onRowClick={selectRow}
          selectedKey={selected?.alias}
          rowKeyField="alias"
        >
          <svelte:fragment slot="cell" let:row let:column>
            {#if column.key === 'alias'}
              <IdentityCell primary={row.alias} size="md" />
            {:else if column.key === 'provider'}
              <Badge tone="neutral" mono>{row.provider_name}</Badge>
            {:else if column.key === 'provider_model'}
              <span class="mono">{row.provider_model}</span>
            {:else if column.key === 'capabilities'}
              <div class="caps">
                {#each row.capabilities ?? [] as cap}
                  <Badge tone="accent">{cap}</Badge>
                {:else}
                  <span class="muted">—</span>
                {/each}
              </div>
            {:else if column.key === 'enabled'}
              <Badge tone={row.enabled ? 'success' : 'neutral'}>
                {row.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            {/if}
          </svelte:fragment>
        </Table>
      {/if}
    </div>

    <Inspector
      open={selected !== null}
      tabs={inspectorTabs}
      bind:activeTab={inspectorTab}
      emptyTitle="No model selected"
      emptyDescription="Select a model or add a new one."
      on:close={closeInspector}
    >
      <svelte:fragment slot="header">
        {#if selected}
          <IdentityCell primary={creating ? 'New model' : selected.alias} size="lg" />
        {/if}
      </svelte:fragment>

      <svelte:fragment slot="actions">
        {#if selected}
          <Button variant="primary" size="sm" on:click={save} disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
          {#if !creating}
            <Button variant="ghost" size="sm" on:click={remove}>
              <IconTrash slot="leading" size={14} />Delete
            </Button>
          {/if}
        {/if}
      </svelte:fragment>

      {#if selected}
        <section class="card form">
          <Input label="Alias" bind:value={fAlias} placeholder="gpt-4o" disabled={!creating} />
          <Select label="Provider" bind:value={fProvider} options={providerOptions} />
          <Input
            label="Provider model"
            bind:value={fProviderModel}
            placeholder="gpt-4o-2024-08-06"
          />
          <div class="field">
            <Input
              label="Capabilities"
              bind:value={fCapabilities}
              placeholder="chat, tools, vision"
              hint="Comma-separated tags used for routing and the playground."
            />
            <div class="chips">
              {#each COMMON_CAPS as cap}
                <button type="button" class="chip" on:click={() => toggleCap(cap)}>+ {cap}</button>
              {/each}
            </div>
          </div>
          <Textarea
            label="Default params (JSON)"
            bind:value={fParams}
            rows={4}
            placeholder={'{"temperature": 0.7}'}
          />
          <Checkbox label="Enabled" bind:checked={fEnabled} />
        </section>
      {/if}
    </Inspector>
  </div>
{/if}

<style>
  .error {
    color: var(--color-danger-fg, var(--color-text));
    margin: var(--space-2) 0;
  }
  .layout {
    display: grid;
    grid-template-columns: 1fr;
    gap: var(--space-4);
  }
  .layout.has-selection {
    grid-template-columns: minmax(0, 1fr) minmax(320px, 420px);
  }
  .main-col {
    min-width: 0;
  }
  .toolbar {
    margin-bottom: var(--space-3);
  }
  .card {
    padding: var(--space-3);
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
  .chip {
    border: 1px solid var(--color-border);
    background: var(--color-surface-2, transparent);
    color: var(--color-text-muted);
    border-radius: var(--radius-full, 999px);
    padding: var(--space-1) var(--space-2);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }
  .chip:hover {
    color: var(--color-text);
    border-color: var(--color-accent);
  }
  .caps {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
  .muted {
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
  }
  .mono {
    font-family: var(--font-mono);
  }
  @media (max-width: 900px) {
    .layout.has-selection {
      grid-template-columns: 1fr;
    }
  }
</style>
