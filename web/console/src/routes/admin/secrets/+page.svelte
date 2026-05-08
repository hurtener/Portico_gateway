<script lang="ts">
  /**
   * Secrets — Phase 10.7b redesign.
   *
   * Vault credential references. Composes the design vocabulary plus a
   * collapsible "Add secret" card that opens via the primary page
   * action — the form is high-frequency for operators so we keep the
   * one-click toggle, but the page is no longer dominated by it.
   *
   * The list endpoint returns only the (tenant, name) tuple — values
   * never come back. Reveal happens through a separate gated flow
   * (Phase 9; not exposed on this page yet).
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, isFeatureUnavailable, type SecretRef } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    FilterChipBar,
    IdentityCell,
    Input,
    Inspector,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    Table,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconLock from 'lucide-svelte/icons/lock';
  import IconKey from 'lucide-svelte/icons/key';
  import IconUsers from 'lucide-svelte/icons/users';
  import IconCopy from 'lucide-svelte/icons/copy';
  import IconTrash from 'lucide-svelte/icons/trash-2';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import type { ComponentType } from 'svelte';

  // === Loading + state ================================================

  type State = 'loading' | 'ready' | 'unavailable';

  let secrets: SecretRef[] = [];
  let state: State = 'loading';
  let error = '';
  let formOpen = false;
  let formTenant = '';
  let formName = '';
  let formValue = '';
  let saving = false;

  async function refresh() {
    try {
      secrets = await api.listSecrets();
      state = 'ready';
      error = '';
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        state = 'unavailable';
        return;
      }
      error = (e as Error).message;
      state = 'ready';
    }
  }

  async function createSecret() {
    if (!formTenant || !formName || !formValue) {
      error = $t('secrets.form.required');
      return;
    }
    saving = true;
    error = '';
    try {
      await api.putSecret(formTenant, formName, formValue);
      toast.success(
        $t('secrets.toast.saved.title'),
        $t('secrets.toast.saved.description', { tenant: formTenant, name: formName })
      );
      formValue = '';
      formOpen = false;
      await refresh();
    } catch (e) {
      const msg = (e as Error).message;
      error = msg;
      toast.danger($t('secrets.toast.saveFailed.title'), msg);
    } finally {
      saving = false;
    }
  }

  async function deleteSecret(s: SecretRef) {
    if (!confirm($t('secrets.confirmDelete', { tenant: s.tenant_id, name: s.name }))) return;
    try {
      await api.deleteSecret(s.tenant_id, s.name);
      toast.info(
        $t('secrets.toast.deleted.title'),
        $t('secrets.toast.deleted.description', { tenant: s.tenant_id, name: s.name })
      );
      // If the deleted secret was selected, clear the inspector.
      if (selectedKey === keyOf(s)) {
        selectedKey = null;
        pushUrl({ selected: null });
      }
      await refresh();
    } catch (e) {
      const msg = (e as Error).message;
      error = msg;
      toast.danger($t('secrets.toast.deleteFailed.title'), msg);
    }
  }

  onMount(refresh);

  // === URL state ======================================================

  let tenantFilter = '';
  let search = '';
  let selectedKey: string | null = null;
  let inspectorTab = 'overview';

  function keyOf(s: SecretRef): string {
    return `${s.tenant_id}/${s.name}`;
  }

  $: {
    const u = $page.url.searchParams;
    tenantFilter = u.get('tenant') || '';
    search = u.get('q') || '';
    selectedKey = u.get('selected');
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

  function onDropdownChange(e: CustomEvent<{ id: string; value: string }>) {
    if (e.detail.id === 'tenant') {
      tenantFilter = e.detail.value;
      pushUrl({ tenant: e.detail.value });
    }
  }
  function onSearchChange(e: CustomEvent<string>) {
    search = e.detail;
    pushUrl({ q: search });
  }
  function selectRow(row: SecretRow) {
    selectedKey = row.key;
    pushUrl({ selected: row.key });
  }
  function closeInspector() {
    selectedKey = null;
    pushUrl({ selected: null });
  }
  function clearFilters() {
    tenantFilter = '';
    search = '';
    pushUrl({ tenant: null, q: null });
  }

  function copyReference(s: SecretRef) {
    const ref = `{{secret:${s.name}}}`;
    if (typeof navigator !== 'undefined' && navigator.clipboard) {
      navigator.clipboard.writeText(ref).then(
        () => toast.success($t('secrets.reveal.copied')),
        () => toast.danger($t('secrets.toast.deleteFailed.title'))
      );
    }
  }

  // === Substrate ======================================================

  type SecretRow = SecretRef & { key: string };

  $: rows = secrets.map<SecretRow>((s) => ({ ...s, key: keyOf(s) }));

  $: filtered = rows.filter((r) => {
    if (tenantFilter && r.tenant_id !== tenantFilter) return false;
    if (search) {
      const needle = search.toLowerCase();
      const hay = `${r.name} ${r.tenant_id}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  $: counts = (() => {
    const tenants = new Set<string>();
    const namesByTenant = new Map<string, Set<string>>();
    const allNames = new Map<string, number>();
    for (const r of rows) {
      tenants.add(r.tenant_id);
      let s = namesByTenant.get(r.tenant_id);
      if (!s) {
        s = new Set();
        namesByTenant.set(r.tenant_id, s);
      }
      s.add(r.name);
      allNames.set(r.name, (allNames.get(r.name) ?? 0) + 1);
    }
    let shared = 0;
    for (const c of allNames.values()) if (c > 1) shared++;
    return { total: rows.length, tenants: tenants.size, shared };
  })();

  $: tenantOptions = (() => {
    const uniq = new Set<string>();
    for (const r of rows) uniq.add(r.tenant_id);
    return [
      { value: '', label: $t('secrets.filter.any') },
      ...Array.from(uniq)
        .sort()
        .map((v) => ({ value: v, label: v }))
    ];
  })();

  $: selected = filtered.find((r) => r.key === selectedKey) ?? null;

  $: inspectorTabs = [
    { id: 'overview', label: $t('secrets.inspector.tab.overview') },
    { id: 'binding', label: $t('secrets.inspector.tab.binding') }
  ];

  // === Composition ====================================================

  $: pageActions = [
    { label: $t('common.refresh'), icon: IconRefreshCw, onClick: refresh },
    {
      label: formOpen ? $t('secrets.form.toggle.cancel') : $t('secrets.form.toggle'),
      variant: 'primary' as const,
      icon: IconPlus,
      onClick: () => (formOpen = !formOpen)
    }
  ];

  $: metrics = [
    {
      id: 'total',
      label: $t('secrets.metric.total'),
      value: counts.total.toString(),
      helper: $t('secrets.metric.total.helper'),
      icon: IconKey as ComponentType<any>,
      tone: 'brand' as const
    },
    {
      id: 'tenants',
      label: $t('secrets.metric.tenants'),
      value: counts.tenants.toString(),
      helper: $t('secrets.metric.tenants.helper'),
      icon: IconUsers as ComponentType<any>
    },
    {
      id: 'shared',
      label: $t('secrets.metric.shared'),
      value: counts.shared.toString(),
      helper: $t('secrets.metric.shared.helper'),
      icon: IconCopy as ComponentType<any>
    }
  ];

  $: dropdowns = [
    {
      id: 'tenant',
      label: $t('secrets.filter.tenant'),
      value: tenantFilter,
      options: tenantOptions
    }
  ];

  $: columns = [
    { key: 'secret', label: $t('secrets.col.secret'), width: '280px' },
    { key: 'tenant', label: $t('secrets.col.tenant'), width: '140px' },
    ...(selected
      ? []
      : [{ key: 'actions', label: '', align: 'right' as const, width: '140px' }])
  ];
</script>

<PageHeader title={$t('secrets.title')} description={$t('secrets.description')} compact>
  <div slot="actions">
    {#if state !== 'unavailable'}
      <PageActionGroup actions={pageActions} />
    {/if}
  </div>
</PageHeader>

{#if state === 'unavailable'}
  <EmptyState
    title={$t('secrets.unavailable.title')}
    description={$t('secrets.unavailable.description')}
  >
    <span slot="illustration"><IconLock size={56} aria-hidden="true" /></span>
  </EmptyState>
{:else}
  {#if formOpen}
    <section class="form-card" data-region="secret-form">
      <h2 class="form-title">{$t('secrets.form.title')}</h2>
      <form on:submit|preventDefault={createSecret}>
        <Input
          bind:value={formTenant}
          label={$t('secrets.form.tenant')}
          placeholder={$t('secrets.form.tenant.placeholder')}
          required
          block={false}
        />
        <Input
          bind:value={formName}
          label={$t('secrets.form.name')}
          placeholder={$t('secrets.form.name.placeholder')}
          required
          block={false}
        />
        <Input
          bind:value={formValue}
          type="password"
          label={$t('secrets.form.value')}
          placeholder={$t('secrets.form.value.placeholder')}
          required
          block={false}
        />
        <Button type="submit" loading={saving}>
          <IconPlus slot="leading" size={14} />
          {$t('common.save')}
        </Button>
      </form>
    </section>
  {/if}

  {#if error}<p class="error">{error}</p>{/if}

  <div class="layout" class:has-selection={selected !== null}>
    <div class="main-col">
      <MetricStrip {metrics} label={$t('secrets.title')} />
      <FilterChipBar
        searchValue={search}
        searchPlaceholder={$t('secrets.filter.search')}
        chips={[]}
        activeChip=""
        {dropdowns}
        on:dropdownChange={onDropdownChange}
        on:searchChange={onSearchChange}
      />

      <Table
        {columns}
        rows={filtered}
        empty={$t('secrets.filter.empty.title')}
        onRowClick={selectRow}
        selectedKey={selectedKey}
        rowKeyField="key"
      >
        <svelte:fragment slot="cell" let:row let:column>
          {@const r = row}
          {#if column.key === 'secret'}
            <IdentityCell primary={r.name} secondary={r.tenant_id} mono size="md" />
          {:else if column.key === 'tenant'}
            <Badge tone="neutral" mono>{r.tenant_id}</Badge>
          {:else if column.key === 'actions'}
            <Button
              size="sm"
              variant="ghost"
              on:click={(e) => {
                e.stopPropagation();
                deleteSecret(r);
              }}
            >
              <IconTrash slot="leading" size={14} />
              {$t('common.delete')}
            </Button>
          {:else}
            {r[column.key] ?? '—'}
          {/if}
        </svelte:fragment>
        <svelte:fragment slot="empty">
          {#if rows.length === 0}
            <EmptyState
              title={$t('secrets.empty.title')}
              description={$t('secrets.empty.description')}
              compact
            />
          {:else}
            <EmptyState
              title={$t('secrets.filter.empty.title')}
              description={$t('secrets.filter.empty.description')}
              compact
            >
              <svelte:fragment slot="actions">
                <Button variant="secondary" on:click={clearFilters}>
                  {$t('secrets.filter.empty.action')}
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
      emptyTitle={$t('secrets.inspector.empty.title')}
      emptyDescription={$t('secrets.inspector.empty.description')}
      on:close={closeInspector}
    >
      <svelte:fragment slot="header">
        {#if selected}
          <IdentityCell primary={selected.name} secondary={selected.tenant_id} mono size="lg" />
        {/if}
      </svelte:fragment>

      <svelte:fragment slot="actions">
        {#if selected}
          <Button variant="secondary" size="sm" on:click={() => copyReference(selected)}>
            <IconCopy slot="leading" size={14} />
            {$t('secrets.action.copy')}
          </Button>
          <Button variant="ghost" size="sm" on:click={() => deleteSecret(selected)}>
            {$t('secrets.inspector.action.delete')}
          </Button>
        {/if}
      </svelte:fragment>

      {#if selected}
        {#if inspectorTab === 'overview'}
          <section class="card">
            <h4>{$t('secrets.inspector.section.identity')}</h4>
            <KeyValueGrid
              items={[
                { label: $t('secrets.col.secret'), value: selected.name },
                { label: $t('secrets.col.tenant'), value: selected.tenant_id }
              ]}
              columns={1}
            />
          </section>
        {:else if inspectorTab === 'binding'}
          <section class="card">
            <h4>{$t('secrets.inspector.section.binding')}</h4>
            <p class="prose">{$t('secrets.inspector.section.binding.description')}</p>
            <code class="ref">{`{{secret:${selected.name}}}`}</code>
          </section>
        {/if}
      {/if}
    </Inspector>
  </div>
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
  }
  .form-card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-5);
    margin-bottom: var(--space-6);
  }
  .form-title {
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
  }
  .form-card form {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr auto;
    gap: var(--space-3);
    align-items: end;
  }
  @media (max-width: 880px) {
    .form-card form {
      grid-template-columns: 1fr;
    }
  }
  .layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-6);
    align-items: start;
  }
  .layout.has-selection {
    grid-template-columns: minmax(0, 1fr) 304px;
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
  .prose {
    margin: 0;
    color: var(--color-text-primary);
    font-size: var(--font-size-body-sm);
    line-height: 1.55;
  }
  .ref {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    color: var(--color-accent-primary);
    word-break: break-all;
  }
</style>
