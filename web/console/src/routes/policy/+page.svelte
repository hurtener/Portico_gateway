<script lang="ts">
  /**
   * Policy rules — Phase 10.7c redesign.
   *
   * Composes the design vocabulary: PageHeader (compact) + KPI strip
   * (rule mix by action) + filter chip bar + rules table inside the
   * left main-col, sticky Inspector right rail with editor / dry-run
   * tabs when a rule is selected.
   *
   * The legacy three-column editor + dry-run aside collapses into a
   * single Inspector. The dry-run flow keeps a dedicated tab so the
   * operator can stay on the same selected rule while iterating.
   * "+ Add rule" creates an unsaved scratch rule and opens the
   * Inspector on the editor tab — same shape as picking an existing
   * rule, so save / cancel are the only escape hatches.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type PolicyDryRunResult, type PolicyRule, type PolicyToolCall } from '$lib/api';
  import {
    Badge,
    Button,
    Checkbox,
    EmptyState,
    FilterChipBar,
    IdentityCell,
    Input,
    Inspector,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    Select,
    Table,
    Textarea,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconShieldCheck from 'lucide-svelte/icons/shield-check';
  import IconShieldOff from 'lucide-svelte/icons/shield-off';
  import IconShieldAlert from 'lucide-svelte/icons/shield-alert';
  import IconList from 'lucide-svelte/icons/list';
  import IconTrash2 from 'lucide-svelte/icons/trash-2';
  import IconPlay from 'lucide-svelte/icons/play';
  import type { ComponentType } from 'svelte';

  // === Loading ========================================================

  let rules: PolicyRule[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    try {
      const set = await api.listPolicyRules();
      rules = set.rules ?? [];
      error = '';
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  // === URL state ======================================================

  let chip = '';
  let riskFilter = '';
  let search_q = '';
  let selectedId: string | null = null;
  let inspectorTab = 'editor';

  $: {
    const u = $page.url.searchParams;
    chip = u.get('action') || 'all';
    riskFilter = u.get('risk') || '';
    search_q = u.get('q') || '';
    selectedId = u.get('selected');
  }

  function pushUrl(updates: Record<string, string | null>) {
    if (typeof window === 'undefined') return;
    const params = new URLSearchParams($page.url.searchParams);
    for (const [k, v] of Object.entries(updates)) {
      if (v === null || v === '' || v === 'all') params.delete(k);
      else params.set(k, v);
    }
    const qs = params.toString();
    goto(qs ? `?${qs}` : '?', { replaceState: true, keepFocus: true, noScroll: true });
  }

  function onChipChange(e: CustomEvent<string>) {
    chip = e.detail;
    pushUrl({ action: chip });
  }
  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'risk') {
      riskFilter = e.detail.value;
      pushUrl({ risk: e.detail.value });
    }
  }
  function onSearchChange(e: CustomEvent<string>) {
    search_q = e.detail;
    pushUrl({ q: search_q });
  }
  function selectRow(row: PolicyRule) {
    draft = { ...row, conditions: cloneConditions(row.conditions), actions: { ...row.actions } };
    selectedId = row.id;
    inspectorTab = 'editor';
    dryResult = null;
    pushUrl({ selected: row.id });
  }
  function closeInspector() {
    selectedId = null;
    draft = null;
    dryResult = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    chip = 'all';
    riskFilter = '';
    search_q = '';
    pushUrl({ action: null, risk: null, q: null });
  }

  // === Substrate ======================================================

  type ActionKind = 'allow' | 'deny' | 'approval' | 'other';
  function actionOf(r: PolicyRule): ActionKind {
    if (r.actions.deny) return 'deny';
    if (r.actions.require_approval) return 'approval';
    if (r.actions.allow) return 'allow';
    return 'other';
  }

  type Tone = 'success' | 'danger' | 'warning' | 'neutral' | 'info' | 'accent';
  function actionTone(k: ActionKind): Tone {
    if (k === 'deny') return 'danger';
    if (k === 'approval') return 'warning';
    if (k === 'allow') return 'success';
    return 'neutral';
  }
  function actionLabel(k: ActionKind): string {
    if (k === 'deny') return $t('policy.actions.deny');
    if (k === 'approval') return $t('policy.actions.requireApproval');
    if (k === 'allow') return $t('policy.actions.allow');
    return '—';
  }

  $: filtered = rules.filter((r) => {
    const k = actionOf(r);
    if (chip === 'allow' && k !== 'allow') return false;
    if (chip === 'deny' && k !== 'deny') return false;
    if (chip === 'approval' && k !== 'approval') return false;
    if (chip === 'disabled' && r.enabled) return false;
    if (riskFilter && r.risk_class !== riskFilter) return false;
    if (search_q) {
      const needle = search_q.toLowerCase();
      const tools = (r.conditions.match.tools ?? []).join(' ');
      const servers = (r.conditions.match.servers ?? []).join(' ');
      const hay = `${r.id} ${r.notes ?? ''} ${tools} ${servers}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    let total = 0;
    let allow = 0;
    let deny = 0;
    let approval = 0;
    let disabled = 0;
    for (const r of rules) {
      total++;
      const k = actionOf(r);
      if (k === 'allow') allow++;
      else if (k === 'deny') deny++;
      else if (k === 'approval') approval++;
      if (!r.enabled) disabled++;
    }
    return { total, allow, deny, approval, disabled };
  })();

  $: riskOptions = (() => {
    const uniq = new Set<string>();
    for (const r of rules) if (r.risk_class) uniq.add(r.risk_class);
    return [
      { value: '', label: $t('policy.filter.any') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  // === Editor =========================================================

  /** Local working copy so save / discard semantics stay clean. */
  let draft: PolicyRule | null = null;
  let saving = false;
  let isNewRule = false;

  const RISK_OPTIONS = [
    { value: 'read', label: 'read' },
    { value: 'write', label: 'write' },
    { value: 'sensitive_read', label: 'sensitive_read' },
    { value: 'external_side_effect', label: 'external_side_effect' },
    { value: 'destructive', label: 'destructive' }
  ];

  function cloneConditions(c: PolicyRule['conditions']): PolicyRule['conditions'] {
    return {
      match: {
        ...c.match,
        tools: c.match.tools ? [...c.match.tools] : undefined,
        servers: c.match.servers ? [...c.match.servers] : undefined,
        tenants: c.match.tenants ? [...c.match.tenants] : undefined
      }
    };
  }

  function newRule() {
    const id = `rule-${Date.now()}`;
    draft = {
      id,
      priority: 100,
      enabled: true,
      risk_class: 'read',
      conditions: { match: {} },
      actions: { allow: true }
    };
    isNewRule = true;
    selectedId = id;
    inspectorTab = 'editor';
    dryResult = null;
    pushUrl({ selected: id });
  }

  async function saveRule() {
    if (!draft) return;
    saving = true;
    error = '';
    try {
      const exists = rules.some((r) => r.id === draft!.id);
      if (exists) {
        await api.updatePolicyRule(draft.id, draft);
      } else {
        await api.createPolicyRule(draft);
      }
      toast.success($t('crud.savedToast'), draft.id);
      isNewRule = false;
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
      if (selectedId === id) {
        selectedId = null;
        draft = null;
        pushUrl({ selected: null });
      }
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  function setToolsCSV(val: string) {
    if (!draft) return;
    const arr = val
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    draft.conditions.match.tools = arr.length ? arr : undefined;
    draft = { ...draft };
  }
  function setServersCSV(val: string) {
    if (!draft) return;
    const arr = val
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    draft.conditions.match.servers = arr.length ? arr : undefined;
    draft = { ...draft };
  }
  function onPriorityInput(e: Event) {
    const v = (e.target as HTMLInputElement | null)?.value ?? '';
    if (!draft) return;
    draft.priority = Number(v);
    draft = { ...draft };
  }
  function onToolsInput(e: Event) {
    const v = (e.target as HTMLInputElement | null)?.value ?? '';
    setToolsCSV(v);
  }
  function onServersInput(e: Event) {
    const v = (e.target as HTMLInputElement | null)?.value ?? '';
    setServersCSV(v);
  }

  $: toolsCSV = (draft?.conditions.match.tools ?? []).join(',');
  $: serversCSV = (draft?.conditions.match.servers ?? []).join(',');

  // === Dry-run ========================================================

  let dryServer = '';
  let dryTool = '';
  let dryArgsRaw = '';
  let dryResult: PolicyDryRunResult | null = null;
  let dryError = '';

  async function dryRun() {
    let args: Record<string, unknown> = {};
    if (dryArgsRaw.trim()) {
      try {
        args = JSON.parse(dryArgsRaw);
      } catch (e) {
        dryError = `args JSON: ${(e as Error).message}`;
        return;
      }
    }
    dryError = '';
    const call: PolicyToolCall = { server: dryServer, tool: dryTool, args };
    try {
      dryResult = await api.dryRunPolicy(call);
    } catch (e) {
      dryError = (e as Error).message;
    }
  }

  $: selected = filtered.find((r) => r.id === selectedId) ?? (isNewRule ? draft : null);

  $: inspectorTabs = [
    { id: 'editor', label: $t('policy.inspector.tab.editor') },
    { id: 'conditions', label: $t('policy.inspector.tab.conditions') },
    { id: 'dryrun', label: $t('policy.inspector.tab.dryrun') }
  ];

  // === Composition ====================================================

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh(),
      loading
    },
    {
      label: $t('policy.action.add'),
      icon: IconPlus,
      variant: 'primary' as const,
      onClick: newRule
    }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('policy.metric.total'),
      value: counts.total.toString(),
      helper: $t('policy.metric.total.helper'),
      icon: IconList as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'allow',
      label: $t('policy.metric.allow'),
      value: counts.allow.toString(),
      helper: $t('policy.metric.allow.helper'),
      icon: IconShieldCheck as ComponentType<any>
    },
    {
      id: 'deny',
      label: $t('policy.metric.deny'),
      value: counts.deny.toString(),
      helper: $t('policy.metric.deny.helper'),
      icon: IconShieldOff as ComponentType<any>,
      tone: 'danger' as const,
      attention: counts.deny > 0
    },
    {
      id: 'approval',
      label: $t('policy.metric.approval'),
      value: counts.approval.toString(),
      helper: $t('policy.metric.approval.helper'),
      icon: IconShieldAlert as ComponentType<any>
    }
  ];

  $: chips = [
    { id: 'all', label: $t('policy.filter.all'), count: counts.total },
    { id: 'allow', label: $t('policy.filter.allow'), count: counts.allow },
    { id: 'deny', label: $t('policy.filter.deny'), count: counts.deny },
    { id: 'approval', label: $t('policy.filter.approval'), count: counts.approval },
    { id: 'disabled', label: $t('policy.filter.disabled'), count: counts.disabled }
  ];

  $: dropdowns = [
    {
      id: 'risk',
      label: $t('policy.filter.risk'),
      value: riskFilter,
      options: riskOptions
    }
  ];

  $: columns = [
    { key: 'priority', label: $t('policy.col.priority'), width: '90px' },
    { key: 'id', label: $t('policy.col.rule') },
    { key: 'risk_class', label: $t('policy.col.risk'), width: '160px' },
    { key: 'action', label: $t('policy.col.action'), width: '160px' },
    ...(selected ? [] : [{ key: 'enabled', label: $t('policy.col.enabled'), width: '110px' }])
  ];
</script>

<PageHeader title={$t('policy.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<div class="layout" class:has-selection={selected !== null}>
  <div class="main-col">
    <MetricStrip {metrics} label={$t('policy.title')} />
    <FilterChipBar
      searchValue={search_q}
      searchPlaceholder={$t('policy.filter.search')}
      {chips}
      activeChip={chip}
      {dropdowns}
      on:chipChange={onChipChange}
      on:dropdownChange={onDropdownChange}
      on:searchChange={onSearchChange}
    />

    <Table
      {columns}
      rows={filtered}
      empty={$t('policy.empty')}
      onRowClick={selectRow}
      selectedKey={selectedId}
      rowKeyField="id"
    >
      <svelte:fragment slot="cell" let:row let:column>
        {@const r = row}
        {#if column.key === 'priority'}
          <span class="prio">{r.priority}</span>
        {:else if column.key === 'id'}
          <IdentityCell primary={r.id} secondary={r.notes ?? ''} mono size="sm" />
        {:else if column.key === 'risk_class'}
          <Badge tone="neutral">{r.risk_class}</Badge>
        {:else if column.key === 'action'}
          <Badge tone={actionTone(actionOf(r))}>{actionLabel(actionOf(r))}</Badge>
        {:else if column.key === 'enabled'}
          {#if r.enabled}
            <Badge tone="success">{$t('policy.col.on')}</Badge>
          {:else}
            <Badge tone="neutral">{$t('policy.col.off')}</Badge>
          {/if}
        {:else}
          {r[column.key] ?? '—'}
        {/if}
      </svelte:fragment>
      <svelte:fragment slot="empty">
        {#if rules.length === 0}
          <EmptyState title={$t('policy.title')} description={$t('policy.empty')} compact>
            <span slot="illustration"><IconShieldCheck size={48} aria-hidden="true" /></span>
            <svelte:fragment slot="actions">
              <Button variant="primary" on:click={newRule}>
                <IconPlus slot="leading" size={14} />
                {$t('policy.action.add')}
              </Button>
            </svelte:fragment>
          </EmptyState>
        {:else}
          <EmptyState
            title={$t('policy.filter.empty.title')}
            description={$t('policy.filter.empty.description')}
            compact
          >
            <svelte:fragment slot="actions">
              <Button variant="secondary" on:click={clearFilters}>
                {$t('policy.filter.empty.action')}
              </Button>
            </svelte:fragment>
          </EmptyState>
        {/if}
      </svelte:fragment>
    </Table>
  </div>

  <Inspector
    open={selected !== null}
    tabs={inspectorTabs}
    bind:activeTab={inspectorTab}
    emptyTitle={$t('policy.inspector.empty.title')}
    emptyDescription={$t('policy.inspector.empty.description')}
    on:close={closeInspector}
  >
    <svelte:fragment slot="header">
      {#if draft}
        <IdentityCell
          primary={draft.id}
          secondary={`priority ${draft.priority} · ${draft.risk_class}`}
          mono
          size="lg"
        />
      {/if}
    </svelte:fragment>

    {#if draft}
      {#if inspectorTab === 'editor'}
        <section class="card">
          <h4>{$t('policy.inspector.section.identity')}</h4>
          <div class="grid">
            <Input bind:value={draft.id} label={$t('policy.field.id')} block />
            <Input
              value={String(draft.priority)}
              on:input={onPriorityInput}
              type="number"
              label={$t('policy.field.priority')}
              block
            />
          </div>
          <Select
            bind:value={draft.risk_class}
            label={$t('policy.field.riskClass')}
            options={RISK_OPTIONS}
          />
          <Checkbox bind:checked={draft.enabled} label={$t('policy.field.enabled')} />
        </section>

        <section class="card">
          <h4>{$t('policy.inspector.section.actions')}</h4>
          <div class="actions-grid">
            <Checkbox bind:checked={draft.actions.allow} label={$t('policy.actions.allow')} />
            <Checkbox bind:checked={draft.actions.deny} label={$t('policy.actions.deny')} />
            <Checkbox
              bind:checked={draft.actions.require_approval}
              label={$t('policy.actions.requireApproval')}
            />
          </div>
        </section>

        <section class="card">
          <h4>{$t('policy.inspector.section.notes')}</h4>
          <Textarea bind:value={draft.notes} label={$t('policy.field.notes')} rows={3} />
        </section>

        <section class="card decisions">
          <div class="decisions-row">
            <Button variant="primary" on:click={saveRule} loading={saving}>
              {$t('policy.action.save')}
            </Button>
            <Button variant="secondary" on:click={closeInspector}>
              {$t('common.cancel')}
            </Button>
            {#if !isNewRule && draft}
              <Button variant="destructive" on:click={() => draft && deleteRule(draft.id)}>
                <IconTrash2 slot="leading" size={14} />
                {$t('policy.action.delete')}
              </Button>
            {/if}
          </div>
        </section>
      {:else if inspectorTab === 'conditions'}
        <section class="card">
          <h4>{$t('policy.inspector.section.match')}</h4>
          <Input
            value={toolsCSV}
            on:input={onToolsInput}
            label={$t('policy.field.tools')}
            block
            placeholder={$t('policy.field.tools.placeholder')}
          />
          <Input
            value={serversCSV}
            on:input={onServersInput}
            label={$t('policy.field.servers')}
            block
            placeholder={$t('policy.field.servers.placeholder')}
          />
        </section>
        <section class="card">
          <h4>{$t('policy.inspector.section.matchSummary')}</h4>
          <KeyValueGrid
            items={[
              {
                label: $t('policy.field.tools'),
                value: (draft.conditions.match.tools ?? []).join(', ') || '—'
              },
              {
                label: $t('policy.field.servers'),
                value: (draft.conditions.match.servers ?? []).join(', ') || '—'
              }
            ]}
            columns={1}
          />
        </section>
      {:else if inspectorTab === 'dryrun'}
        <section class="card">
          <h4>{$t('policy.dryrun.title')}</h4>
          <p class="muted">{$t('policy.dryrun.subtitle')}</p>
          <Input bind:value={dryServer} label={$t('policy.dryrun.field.server')} block />
          <Input bind:value={dryTool} label={$t('policy.dryrun.field.tool')} block />
          <Textarea bind:value={dryArgsRaw} label={$t('policy.dryrun.field.args')} rows={3} />
          <Button variant="primary" on:click={dryRun}>
            <IconPlay slot="leading" size={14} />
            {$t('policy.dryrun.run')}
          </Button>
          {#if dryError}<p class="error">{dryError}</p>{/if}
        </section>
        {#if dryResult}
          <section class="card">
            <h4>{$t('policy.dryrun.outcome')}</h4>
            <KeyValueGrid
              items={[
                { label: $t('policy.dryrun.final'), value: JSON.stringify(dryResult.final_action) },
                { label: $t('policy.dryrun.risk'), value: dryResult.final_risk }
              ]}
              columns={1}
            />
          </section>
          <section class="card">
            <h4>{$t('policy.dryrun.matched')}</h4>
            {#if dryResult.matched_rules.length === 0}
              <p class="muted">{$t('policy.dryrun.noMatches')}</p>
            {:else}
              <ul class="match-list">
                {#each dryResult.matched_rules as m (m.rule_id)}
                  <li><code>{m.rule_id}</code> — {m.reason}</li>
                {/each}
              </ul>
            {/if}
          </section>
          {#if dryResult.losing_rules?.length}
            <section class="card">
              <h4>{$t('policy.dryrun.losing')}</h4>
              <ul class="match-list">
                {#each dryResult.losing_rules as m (m.rule_id)}
                  <li><code>{m.rule_id}</code> — {m.reason}</li>
                {/each}
              </ul>
            </section>
          {/if}
        {/if}
      {/if}
    {/if}
  </Inspector>
</div>

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-3) 0;
    font-size: var(--font-size-body-sm);
  }
  .layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-6);
    align-items: start;
  }
  .layout.has-selection {
    grid-template-columns: minmax(0, 1fr) 360px;
  }
  @media (max-width: 1279px) {
    .layout.has-selection {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .main-col {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .prio {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .card h4 {
    margin: 0;
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 100px;
    gap: var(--space-3);
    align-items: end;
  }
  .actions-grid {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .decisions-row {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .match-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    font-size: var(--font-size-body-sm);
    color: var(--color-text-secondary);
  }
  .match-list code {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
  }
</style>
