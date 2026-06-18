<script lang="ts">
  /**
   * Agent Profiles (Phase 14). The headline consumer-binding screen: one object
   * per logical agent describing which MCP servers/tools, Skills, and LLM models
   * it may use, plus the JWT subjects bound to it. List + a right-rail Inspector
   * with the full surface (basics → MCP → skills → models → scopes → bindings).
   * Reads/writes /api/agent-profiles + its /bindings sub-resource.
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable } from '$lib/api';
  import type { AgentProfile } from '$lib/api';
  import {
    Badge,
    Button,
    Checkbox,
    EmptyState,
    Input,
    Inspector,
    PageActionGroup,
    PageHeader,
    Skeleton,
    Table,
    Textarea,
    toast
  } from '$lib/components';
  import IconPlus from 'lucide-svelte/icons/plus';

  let loading = true;
  let unavailable = false;
  let error = '';
  let profiles: AgentProfile[] = [];

  // The draft is the editable profile in the Inspector. isNew distinguishes
  // create from edit. bindSub is the subject in the bindings sub-form.
  let draft: AgentProfile | null = null;
  let isNew = false;
  let saving = false;
  let bindSub = '';

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'allowed_mcp_servers', label: 'Servers', align: 'right' as const },
    { key: 'allowed_tools', label: 'Tools', align: 'right' as const },
    { key: 'allowed_skills', label: 'Skills', align: 'right' as const },
    { key: 'allowed_model_aliases', label: 'Models', align: 'right' as const },
    { key: 'enabled', label: 'Status' }
  ];

  function emptyProfile(): AgentProfile {
    return {
      id: '',
      name: '',
      description: '',
      allowed_mcp_servers: [],
      allowed_tools: [],
      allowed_skills: [],
      allowed_model_aliases: [],
      scopes: [],
      enabled: true
    };
  }

  function linesToArr(s: string): string[] {
    return s
      .split(/[\n,]/)
      .map((x) => x.trim())
      .filter((x) => x.length > 0);
  }
  function arrToLines(a: string[] | undefined): string {
    return (a ?? []).join('\n');
  }

  // Textarea-bound mirrors of the allowlists (newline/comma separated).
  let serversText = '';
  let toolsText = '';
  let skillsText = '';
  let modelsText = '';
  let scopesText = '';

  function syncTextFromDraft(p: AgentProfile) {
    serversText = arrToLines(p.allowed_mcp_servers);
    toolsText = arrToLines(p.allowed_tools);
    skillsText = arrToLines(p.allowed_skills);
    modelsText = arrToLines(p.allowed_model_aliases);
    scopesText = arrToLines(p.scopes);
  }

  onMount(load);

  async function load() {
    error = '';
    try {
      profiles = (await api.listAgentProfiles()) ?? [];
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) unavailable = true;
      else error = e instanceof Error ? e.message : 'Failed to load agent profiles';
    } finally {
      loading = false;
    }
  }

  function openNew() {
    draft = emptyProfile();
    isNew = true;
    bindSub = '';
    syncTextFromDraft(draft);
  }

  async function openEdit(id: string) {
    try {
      draft = await api.getAgentProfile(id);
      isNew = false;
      bindSub = '';
      syncTextFromDraft(draft);
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Failed to load profile');
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
    const payload: Partial<AgentProfile> = {
      name: draft.name.trim(),
      description: draft.description,
      enabled: draft.enabled,
      allowed_mcp_servers: linesToArr(serversText),
      allowed_tools: linesToArr(toolsText),
      allowed_skills: linesToArr(skillsText),
      allowed_model_aliases: linesToArr(modelsText),
      scopes: linesToArr(scopesText)
    };
    saving = true;
    try {
      if (isNew) {
        await api.createAgentProfile(payload);
        toast.success('Agent profile created');
      } else {
        await api.updateAgentProfile(draft.id, payload);
        toast.success('Agent profile updated');
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
      await api.deleteAgentProfile(draft.id);
      toast.success('Agent profile deleted');
      closeInspector();
      await load();
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Delete failed');
    }
  }

  async function bind() {
    if (!draft || isNew || !bindSub.trim()) return;
    try {
      await api.bindAgentProfile(draft.id, bindSub.trim());
      toast.success(`Bound ${bindSub.trim()} to this profile`);
      bindSub = '';
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Bind failed');
    }
  }

  async function unbind() {
    if (!draft || isNew || !bindSub.trim()) return;
    try {
      await api.unbindAgentProfile(draft.id, bindSub.trim());
      toast.success(`Unbound ${bindSub.trim()}`);
      bindSub = '';
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Unbind failed');
    }
  }
</script>

<PageHeader
  title="Agent Profiles"
  description="One object per agent: the MCP servers, tools, Skills, and models it may use."
  compact
>
  <div slot="actions">
    <PageActionGroup>
      <Button variant="primary" size="sm" on:click={openNew}>
        <IconPlus slot="leading" size={14} />
        Add profile
      </Button>
    </PageActionGroup>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="Agent profiles not configured"
    description="The agent profile store is not wired in this build."
  />
{:else if loading}
  <Skeleton height="280px" />
{:else}
  <Table {columns} rows={profiles} rowKeyField="id" on:rowclick={(e) => openEdit(e.detail.row.id)}>
    <svelte:fragment slot="cell" let:row let:column>
      {#if column.key === 'name'}
        <strong>{row.name}</strong>
      {:else if column.key === 'enabled'}
        <Badge tone={row.enabled ? 'success' : 'neutral'}
          >{row.enabled ? 'enabled' : 'disabled'}</Badge
        >
      {:else if column.key.startsWith('allowed_')}
        {(row[column.key] ?? []).length || '—'}
      {:else}
        {row[column.key]}
      {/if}
    </svelte:fragment>
    <svelte:fragment slot="empty">
      <EmptyState
        title="No agent profiles yet"
        description="Create one to scope an agent to a subset of your servers, tools, skills, and models."
      >
        <svelte:fragment slot="actions">
          <Button variant="primary" size="sm" on:click={openNew}>
            <IconPlus slot="leading" size={14} />
            Add profile
          </Button>
        </svelte:fragment>
      </EmptyState>
    </svelte:fragment>
  </Table>

  <Inspector open={draft !== null} on:close={closeInspector}>
    {#if draft}
      <section class="card">
        <h4>Basics</h4>
        <Input bind:value={draft.name} label="Name" block />
        <Input bind:value={draft.description} label="Description" block />
        <Checkbox bind:checked={draft.enabled} label="Enabled" />
      </section>

      <section class="card">
        <h4>MCP surface</h4>
        <p class="hint">One per line. Tools empty = all tools in the allowed servers.</p>
        <Textarea bind:value={serversText} label="Allowed MCP servers" rows={3} />
        <Textarea bind:value={toolsText} label="Allowed tools (server.tool)" rows={3} />
      </section>

      <section class="card">
        <h4>Skills & models</h4>
        <Textarea bind:value={skillsText} label="Allowed Skill Packs" rows={2} />
        <Textarea bind:value={modelsText} label="Allowed model aliases" rows={2} />
      </section>

      <section class="card">
        <h4>Scopes</h4>
        <Textarea bind:value={scopesText} label="Scopes" rows={2} />
      </section>

      {#if !isNew}
        <section class="card">
          <h4>Bindings</h4>
          <p class="hint">Bind a JWT subject to this profile (its requests resolve here).</p>
          <Input bind:value={bindSub} label="JWT subject" block />
          <div class="row">
            <Button variant="secondary" size="sm" on:click={bind} disabled={!bindSub.trim()}
              >Bind</Button
            >
            <Button variant="ghost" size="sm" on:click={unbind} disabled={!bindSub.trim()}
              >Unbind</Button
            >
          </div>
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
  .row {
    display: flex;
    gap: var(--space-2);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-4);
  }
</style>
