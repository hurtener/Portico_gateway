<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type PolicyDryRunResult, type PolicyRule, type PolicyToolCall } from '$lib/api';
  import {
    Badge,
    Button,
    Checkbox,
    EmptyState,
    Input,
    PageHeader,
    Select,
    Textarea,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconShieldCheck from 'lucide-svelte/icons/shield-check';

  let rules: PolicyRule[] = [];
  let selected: PolicyRule | null = null;
  let saving = false;
  let error = '';

  // Dry-run sidebar inputs.
  let dryServer = '';
  let dryTool = '';
  let dryArgsRaw = '';
  let dryResult: PolicyDryRunResult | null = null;

  async function refresh() {
    try {
      const set = await api.listPolicyRules();
      rules = set.rules ?? [];
      if (rules.length > 0 && !selected) selected = { ...rules[0] };
    } catch (e) {
      error = (e as Error).message;
    }
  }

  function newRule() {
    selected = {
      id: `rule-${Date.now()}`,
      priority: 100,
      enabled: true,
      risk_class: 'read',
      conditions: { match: {} },
      actions: { allow: true }
    };
  }

  async function saveRule() {
    if (!selected) return;
    saving = true;
    error = '';
    try {
      const exists = rules.some((r) => r.id === selected!.id);
      if (exists) {
        await api.updatePolicyRule(selected.id, selected);
      } else {
        await api.createPolicyRule(selected);
      }
      toast.success($t('crud.savedToast'), selected.id);
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      saving = false;
    }
  }

  async function deleteRule(id: string) {
    if (!confirm($t('crud.confirmDelete'))) return;
    try {
      await api.deletePolicyRule(id);
      toast.info($t('crud.deletedToast'), id);
      if (selected?.id === id) selected = null;
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  async function dryRun() {
    let args: Record<string, unknown> = {};
    if (dryArgsRaw.trim()) {
      try {
        args = JSON.parse(dryArgsRaw);
      } catch (e) {
        error = `args JSON: ${(e as Error).message}`;
        return;
      }
    }
    const call: PolicyToolCall = { server: dryServer, tool: dryTool, args };
    try {
      dryResult = await api.dryRunPolicy(call);
    } catch (e) {
      error = (e as Error).message;
    }
  }

  $: riskOptions = [
    { value: 'read', label: 'read' },
    { value: 'write', label: 'write' },
    { value: 'sensitive_read', label: 'sensitive_read' },
    { value: 'external_side_effect', label: 'external_side_effect' },
    { value: 'destructive', label: 'destructive' }
  ];

  onMount(refresh);

  // Helpers to bind the structured tools list as a comma-separated input.
  $: toolsCSV = (selected?.conditions.match.tools ?? []).join(',');
  function syncToolsCSV(val: string) {
    if (!selected) return;
    const arr = val
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    selected.conditions.match.tools = arr;
    selected = { ...selected };
  }
  function onPriorityInput(e: Event) {
    const v = (e.target as HTMLInputElement | null)?.value ?? '';
    if (!selected) return;
    selected.priority = Number(v);
    selected = { ...selected };
  }
  function onToolsInput(e: Event) {
    const v = (e.target as HTMLInputElement | null)?.value ?? '';
    syncToolsCSV(v);
  }
</script>

<PageHeader title={$t('policy.title')} description={$t('policy.subtitle')}>
  <Button slot="actions" on:click={newRule}>
    <IconPlus slot="leading" size={14} />
    {$t('policy.action.add')}
  </Button>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<div class="layout">
  <aside class="rules">
    {#if rules.length === 0 && !selected}
      <EmptyState title={$t('policy.title')} description={$t('policy.empty')}>
        <span slot="illustration"><IconShieldCheck size={48} aria-hidden="true" /></span>
      </EmptyState>
    {:else}
      <ul>
        {#each rules as r (r.id)}
          <li class:active={selected?.id === r.id}>
            <button class="rowbtn" on:click={() => (selected = { ...r })}>
              <span class="prio">{r.priority}</span>
              <span class="rid">{r.id}</span>
              <Badge tone={r.actions.deny ? 'danger' : r.actions.allow ? 'success' : 'neutral'}>
                {r.actions.deny
                  ? $t('policy.actions.deny')
                  : r.actions.allow
                    ? $t('policy.actions.allow')
                    : '—'}
              </Badge>
            </button>
            <Button size="sm" variant="ghost" on:click={() => deleteRule(r.id)}>×</Button>
          </li>
        {/each}
      </ul>
    {/if}
  </aside>

  {#if selected}
    <section class="editor">
      <h2>{selected.id}</h2>
      <Input bind:value={selected.id} label={$t('policy.field.id')} block={false} />
      <Input
        value={String(selected.priority)}
        on:input={onPriorityInput}
        type="number"
        label={$t('policy.field.priority')}
        block={false}
      />
      <Checkbox bind:checked={selected.enabled} label={$t('policy.field.enabled')} />
      <Select
        bind:value={selected.risk_class}
        label={$t('policy.field.riskClass')}
        options={riskOptions}
      />
      <Input
        value={toolsCSV}
        on:input={onToolsInput}
        label={$t('policy.field.tools')}
        block={false}
      />
      <Textarea bind:value={selected.notes} label={$t('policy.field.notes')} rows={3} />
      <div class="actions">
        <Checkbox bind:checked={selected.actions.allow} label={$t('policy.actions.allow')} />
        <Checkbox bind:checked={selected.actions.deny} label={$t('policy.actions.deny')} />
        <Checkbox
          bind:checked={selected.actions.require_approval}
          label={$t('policy.actions.requireApproval')}
        />
      </div>
      <Button on:click={saveRule} loading={saving}>{$t('policy.action.save')}</Button>
    </section>
  {/if}

  <aside class="dryrun">
    <h3>{$t('policy.dryrun.title')}</h3>
    <p>{$t('policy.dryrun.subtitle')}</p>
    <Input bind:value={dryServer} label="server" block={false} />
    <Input bind:value={dryTool} label="tool" block={false} />
    <Textarea bind:value={dryArgsRaw} label="args (JSON)" rows={3} />
    <Button on:click={dryRun}>{$t('policy.dryrun.run')}</Button>
    {#if dryResult}
      <div class="result">
        <p>
          <strong>{$t('policy.dryrun.final')}:</strong>
          {JSON.stringify(dryResult.final_action)}
        </p>
        <p><strong>{$t('policy.dryrun.risk')}:</strong> {dryResult.final_risk}</p>
        <p><strong>{$t('policy.dryrun.matched')}:</strong></p>
        <ul>
          {#each dryResult.matched_rules as m (m.rule_id)}
            <li>{m.rule_id} — {m.reason}</li>
          {/each}
        </ul>
        {#if dryResult.losing_rules?.length}
          <p><strong>{$t('policy.dryrun.losing')}:</strong></p>
          <ul>
            {#each dryResult.losing_rules as m (m.rule_id)}
              <li>{m.rule_id} — {m.reason}</li>
            {/each}
          </ul>
        {/if}
      </div>
    {/if}
  </aside>
</div>

<style>
  .layout {
    display: grid;
    grid-template-columns: 280px 1fr 320px;
    gap: var(--space-4);
    margin-top: var(--space-3);
  }
  @media (max-width: 1100px) {
    .layout {
      grid-template-columns: 1fr;
    }
  }
  aside.rules ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: var(--space-1);
  }
  aside.rules li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .rowbtn {
    flex: 1;
    display: grid;
    grid-template-columns: 40px 1fr auto;
    gap: var(--space-2);
    align-items: center;
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-2) var(--space-3);
    cursor: pointer;
    color: var(--color-text-primary);
    text-align: left;
  }
  li.active .rowbtn {
    border-color: var(--color-accent);
  }
  .prio {
    font-family: var(--font-family-mono);
    color: var(--color-text-muted);
  }
  .rid {
    font-weight: var(--font-weight-medium);
  }
  section.editor {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    display: grid;
    gap: var(--space-3);
  }
  section.editor .actions {
    display: flex;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  aside.dryrun {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    display: grid;
    gap: var(--space-3);
    align-content: start;
  }
  aside.dryrun h3 {
    margin: 0;
    font-size: var(--font-size-title);
  }
  aside.dryrun p {
    margin: 0;
    font-size: var(--font-size-body-sm);
    color: var(--color-text-secondary);
  }
  .result {
    background: var(--color-bg-canvas);
    border-radius: var(--radius-sm);
    padding: var(--space-3);
    font-size: var(--font-size-body-sm);
  }
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-3) 0;
  }
</style>
