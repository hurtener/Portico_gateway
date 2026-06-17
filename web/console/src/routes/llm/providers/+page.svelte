<script lang="ts">
  /**
   * LLM providers (Phase 13). Operator CRUD over the per-tenant LLM provider
   * registry + weighted keys. Built-in drivers (Bifrost natives) and a
   * custom_openai slot (operator-supplied base_url) split across a segmented
   * filter. Selecting a row or "+ Add provider" opens the Inspector right-rail
   * with a Settings editor + a Keys sub-panel, mirroring the rest of the Console.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable, type LLMProvider, type LLMProviderKey } from '$lib/api';
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

  // Bifrost native driver names + the custom_openai slot.
  const NATIVE_DRIVERS = [
    'openai',
    'azure',
    'anthropic',
    'bedrock',
    'cohere',
    'vertex',
    'mistral',
    'ollama',
    'groq',
    'gemini',
    'openrouter',
    'perplexity',
    'cerebras',
    'xai',
    'huggingface',
    'nebius'
  ];
  const ALL_DRIVERS = [...NATIVE_DRIVERS, 'custom_openai'];

  let providers: LLMProvider[] = [];
  let loading = true;
  let unavailable = false;
  let error = '';

  let filter: 'all' | 'builtin' | 'custom' = 'all';

  // Inspector state.
  let selected: LLMProvider | null = null;
  let creating = false;
  let inspectorTab = 'settings';
  let saving = false;

  // Editor form fields (decoupled from `selected` so cancel is clean).
  let fName = '';
  let fDriver = 'openai';
  let fBaseURL = '';
  let fHeaders = '';
  let fCredentialRef = '';
  let fEnabled = true;

  // Keys sub-panel.
  let keys: LLMProviderKey[] = [];
  let keysLoading = false;
  let kCredentialRef = '';
  let kWeight = '1';
  let kAllowlist = '';

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'driver', label: 'Driver' },
    { key: 'enabled', label: 'Status' }
  ];

  $: filtered = providers.filter((p) => {
    if (filter === 'builtin') return p.driver !== 'custom_openai';
    if (filter === 'custom') return p.driver === 'custom_openai';
    return true;
  });

  function setFilter(v: string) {
    filter = v as 'all' | 'builtin' | 'custom';
  }

  const filterOptions = [
    { value: 'all', label: 'All' },
    { value: 'builtin', label: 'Built-in' },
    { value: 'custom', label: 'Custom' }
  ];

  const inspectorTabs = [
    { id: 'settings', label: 'Settings' },
    { id: 'keys', label: 'Keys' }
  ];

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await api.listLLMProviders();
      providers = res.providers ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        unavailable = true;
      } else {
        error = e instanceof Error ? e.message : 'Failed to load providers';
      }
    } finally {
      loading = false;
    }
  }

  function isCustom(driver: string): boolean {
    return driver === 'custom_openai';
  }

  function openCreate() {
    creating = true;
    selected = { name: '', driver: 'openai', enabled: true };
    fName = '';
    fDriver = 'openai';
    fBaseURL = '';
    fHeaders = '';
    fCredentialRef = '';
    fEnabled = true;
    keys = [];
    inspectorTab = 'settings';
  }

  function selectRow(row: LLMProvider) {
    creating = false;
    selected = row;
    fName = row.name;
    fDriver = row.driver;
    fCredentialRef = row.credential_ref ?? '';
    fEnabled = row.enabled;
    const cfg = row.config ?? {};
    fBaseURL = typeof cfg.base_url === 'string' ? cfg.base_url : '';
    fHeaders = cfg.headers ? JSON.stringify(cfg.headers, null, 2) : '';
    inspectorTab = 'settings';
    void loadKeys(row.name);
  }

  function closeInspector() {
    selected = null;
    creating = false;
  }

  function buildConfig(): Record<string, unknown> | undefined {
    if (!isCustom(fDriver)) return undefined;
    const cfg: Record<string, unknown> = {};
    if (fBaseURL.trim()) cfg.base_url = fBaseURL.trim();
    if (fHeaders.trim()) {
      try {
        cfg.headers = JSON.parse(fHeaders);
      } catch {
        throw new Error('Headers must be valid JSON');
      }
    }
    return cfg;
  }

  async function save() {
    if (!fName.trim()) {
      toast.danger('Name is required');
      return;
    }
    saving = true;
    try {
      const body: LLMProvider = {
        name: fName.trim(),
        driver: fDriver,
        enabled: fEnabled,
        credential_ref: fCredentialRef.trim() || undefined,
        config: buildConfig()
      };
      if (creating) {
        await api.createLLMProvider(body);
        toast.success(`Provider "${body.name}" created`);
      } else {
        await api.updateLLMProvider(fName.trim(), body);
        toast.success(`Provider "${body.name}" updated`);
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
    if (!confirm(`Delete provider "${selected.name}"? This cannot be undone.`)) return;
    try {
      await api.deleteLLMProvider(selected.name);
      toast.success(`Provider "${selected.name}" deleted`);
      await load();
      closeInspector();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Delete failed');
    }
  }

  async function loadKeys(name: string) {
    keysLoading = true;
    try {
      const res = await api.listLLMProviderKeys(name);
      keys = res.keys ?? [];
    } catch {
      keys = [];
    } finally {
      keysLoading = false;
    }
  }

  async function addKey() {
    if (!selected || creating) {
      toast.danger('Save the provider before adding keys');
      return;
    }
    if (!kCredentialRef.trim()) {
      toast.danger('Credential ref is required');
      return;
    }
    try {
      const allowlist = kAllowlist
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      await api.addLLMProviderKey(selected.name, {
        credential_ref: kCredentialRef.trim(),
        weight: parseFloat(kWeight) || 1,
        model_allowlist: allowlist.length ? allowlist : undefined,
        enabled: true
      });
      kCredentialRef = '';
      kWeight = '1';
      kAllowlist = '';
      await loadKeys(selected.name);
      toast.success('Key added');
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Add key failed');
    }
  }

  async function removeKey(keyId: string) {
    if (!selected) return;
    try {
      await api.deleteLLMProviderKey(selected.name, keyId);
      await loadKeys(selected.name);
      toast.success('Key removed');
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Remove key failed');
    }
  }
</script>

<PageHeader
  title="LLM Providers"
  description="Configure the upstream model providers this tenant can route to."
  compact
>
  <div slot="actions">
    <Button on:click={openCreate}>
      <IconPlus slot="leading" size={14} />Add provider
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="LLM gateway not configured"
    description="The LLM provider store is not wired in this build."
  />
{:else}
  <div class="layout" class:has-selection={selected !== null}>
    <div class="main-col">
      <div class="toolbar">
        <SegmentedControl
          options={filterOptions}
          value={filter}
          ariaLabel="Filter providers"
          onChange={setFilter}
        />
      </div>

      {#if !loading && providers.length === 0}
        <EmptyState
          title="No providers yet"
          description="Add an OpenAI, Anthropic, or custom OpenAI-compatible provider to start routing models."
        >
          <svelte:fragment slot="actions">
            <Button on:click={openCreate}><IconPlus slot="leading" size={14} />Add provider</Button>
          </svelte:fragment>
        </EmptyState>
      {:else}
        <Table
          {columns}
          rows={filtered}
          empty="No providers match this filter."
          onRowClick={selectRow}
          selectedKey={selected?.name}
          rowKeyField="name"
        >
          <svelte:fragment slot="cell" let:row let:column>
            {#if column.key === 'name'}
              <IdentityCell primary={row.name} size="md" />
            {:else if column.key === 'driver'}
              <Badge tone={isCustom(row.driver) ? 'accent' : 'neutral'} mono>{row.driver}</Badge>
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
      emptyTitle="No provider selected"
      emptyDescription="Select a provider or add a new one."
      on:close={closeInspector}
    >
      <svelte:fragment slot="header">
        {#if selected}
          <IdentityCell primary={creating ? 'New provider' : selected.name} size="lg" />
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
        {#if inspectorTab === 'settings'}
          <section class="card form">
            <Input label="Name" bind:value={fName} placeholder="my-openai" disabled={!creating} />
            <Select label="Driver" bind:value={fDriver}>
              {#each ALL_DRIVERS as d}
                <option value={d}>{d}</option>
              {/each}
            </Select>
            {#if isCustom(fDriver)}
              <Input
                label="Base URL"
                bind:value={fBaseURL}
                type="url"
                placeholder="https://api.deepseek.com"
              />
              <Textarea
                label="Headers (JSON)"
                bind:value={fHeaders}
                rows={3}
                placeholder={'{"X-Org":"acme"}'}
              />
            {/if}
            <Input
              label="Default credential ref"
              bind:value={fCredentialRef}
              placeholder="vault key name (optional)"
            />
            <Checkbox label="Enabled" bind:checked={fEnabled} />
          </section>
        {:else if inspectorTab === 'keys'}
          <section class="card">
            <h4>Weighted keys</h4>
            {#if creating}
              <p class="muted">Save the provider first to manage its keys.</p>
            {:else if keysLoading}
              <p class="muted">Loading…</p>
            {:else if keys.length === 0}
              <p class="muted">No keys. Add one below.</p>
            {:else}
              <ul class="keys">
                {#each keys as k (k.key_id)}
                  <li>
                    <span class="mono">{k.credential_ref}</span>
                    <Badge tone="neutral">w={k.weight ?? 1}</Badge>
                    <Button variant="ghost" size="sm" on:click={() => removeKey(k.key_id ?? '')}>
                      <IconTrash slot="leading" size={14} />
                    </Button>
                  </li>
                {/each}
              </ul>
            {/if}
            {#if !creating}
              <div class="addkey">
                <Input bind:value={kCredentialRef} placeholder="vault credential ref" />
                <Input bind:value={kWeight} type="number" placeholder="weight" />
                <Input bind:value={kAllowlist} placeholder="model allowlist (comma-sep)" />
                <Button variant="secondary" size="sm" on:click={addKey}>Add key</Button>
              </div>
            {/if}
          </section>
        {/if}
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
  .muted {
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .keys {
    list-style: none;
    margin: 0 0 var(--space-3);
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .keys li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .addkey {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  @media (max-width: 900px) {
    .layout.has-selection {
      grid-template-columns: 1fr;
    }
  }
</style>
