<script lang="ts">
  /**
   * Phase 10.6 design-vocabulary preview.
   *
   * Renders every new primitive in isolation so reviewers can scan the
   * shapes side-by-side without booting the full Servers / Skills pages,
   * and so the Phase 10.6 Playwright smoke can assert each primitive
   * renders without spinning up a real /servers context.
   */
  import {
    Button,
    FilterChipBar,
    IdentityCell,
    Inspector,
    MetricStrip,
    PageActionGroup,
    PageHeader
  } from '$lib/components';

  import IconServer from 'lucide-svelte/icons/server';
  import IconBox from 'lucide-svelte/icons/box';
  import IconShield from 'lucide-svelte/icons/shield';
  import IconShieldAlert from 'lucide-svelte/icons/shield-alert';
  import IconWorkflow from 'lucide-svelte/icons/workflow';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconUpload from 'lucide-svelte/icons/upload';

  // MetricStrip fixture
  const metrics = [
    {
      id: 'servers',
      label: 'Servers',
      value: '12',
      helper: '11 online · 1 offline',
      icon: IconServer,
      tone: 'brand' as const
    },
    {
      id: 'runtime',
      label: 'Runtime processes',
      value: '34',
      helper: '8 idle · 2 restarting',
      icon: IconWorkflow,
      tone: 'brand' as const
    },
    {
      id: 'caps',
      label: 'Capabilities',
      value: '184',
      helper: '39 resources · 22 prompts',
      icon: IconBox
    },
    {
      id: 'policy',
      label: 'Policies',
      value: '27',
      helper: '3 approval-gated',
      icon: IconShield
    },
    {
      id: 'drift',
      label: 'Catalog drift',
      value: '2',
      helper: 'Review required',
      icon: IconShieldAlert,
      attention: true
    }
  ];

  // FilterChipBar fixture
  let chip = 'all';
  let transport = '';
  let runtime = '';
  let search = '';
  const chips = [
    { id: 'all', label: 'All', count: 12 },
    { id: 'online', label: 'Online', count: 11 },
    { id: 'offline', label: 'Offline', count: 1 },
    { id: 'review', label: 'Needs review', count: 2 },
    { id: 'auth', label: 'Auth error' }
  ];
  const dropdowns = [
    {
      id: 'transport',
      label: 'Transport',
      value: '',
      options: [
        { value: '', label: 'Any' },
        { value: 'stdio', label: 'stdio' },
        { value: 'http', label: 'http' }
      ]
    },
    {
      id: 'runtime',
      label: 'Runtime',
      value: '',
      options: [
        { value: '', label: 'Any' },
        { value: 'shared_global', label: 'shared_global' },
        { value: 'per_session', label: 'per_session' },
        { value: 'per_user', label: 'per_user' },
        { value: 'per_tenant', label: 'per_tenant' }
      ]
    }
  ];

  // PageActionGroup fixture
  const actions = [
    { label: 'Import config', icon: IconUpload, onClick: () => {} },
    {
      label: 'Refresh catalog',
      icon: IconRefreshCw,
      onClick: () => {},
      dropdown: [
        { label: 'Refresh all', onSelect: () => {} },
        { label: 'Refresh selected', onSelect: () => {} }
      ]
    },
    {
      label: 'Add server',
      variant: 'primary' as const,
      icon: IconPlus,
      onClick: () => {},
      dropdown: [
        { label: 'Register stdio', onSelect: () => {} },
        { label: 'Register HTTP', onSelect: () => {} }
      ]
    }
  ];

  // Inspector fixture
  let inspectorOpen = true;
  let inspectorTab = 'overview';
  const tabs = [
    { id: 'overview', label: 'Overview' },
    { id: 'tools', label: 'Tools' },
    { id: 'resources', label: 'Resources' },
    { id: 'skills', label: 'Skills' }
  ];
</script>

<PageHeader
  title="Design vocabulary preview"
  description="Phase 10.6 primitives rendered in isolation. Light/dark and EN/ES toggles live in the topbar."
/>

<section class="block">
  <h2>MetricStrip</h2>
  <MetricStrip {metrics} />
</section>

<section class="block">
  <h2>FilterChipBar</h2>
  <FilterChipBar
    bind:searchValue={search}
    searchPlaceholder="Search servers…"
    {chips}
    activeChip={chip}
    {dropdowns}
    on:chipChange={(e) => (chip = e.detail)}
    on:dropdownChange={(e) => {
      if (e.detail.id === 'transport') transport = e.detail.value;
      if (e.detail.id === 'runtime') runtime = e.detail.value;
    }}
  />
  <p class="state">
    chip: <code>{chip}</code> · transport: <code>{transport || '—'}</code> · runtime: <code
      >{runtime || '—'}</code
    > · search: <code>{search || '—'}</code>
  </p>
</section>

<section class="block">
  <h2>IdentityCell</h2>
  <div class="row">
    <IdentityCell primary="filesystem" secondary="Local filesystem access" />
    <IdentityCell primary="github" secondary="GitHub API and repos" />
    <IdentityCell primary="postgres" secondary="PostgreSQL database" size="lg" />
  </div>
  <div class="row">
    <IdentityCell primary="github.code-review" secondary="Review pull requests" mono />
    <IdentityCell primary="postgres.sql-analyst" secondary="Run analytical queries" mono />
    <IdentityCell primary="linear.triage" mono />
  </div>
</section>

<section class="block">
  <h2>PageActionGroup</h2>
  <PageActionGroup {actions} />
</section>

<section class="block">
  <h2>Inspector</h2>
  <div class="inspector-demo">
    <div class="inspector-mock">
      <p class="hint">
        ← Imagine the table here. The Inspector sticks to the right rail at ≥1280px.
      </p>
      <Button variant="secondary" on:click={() => (inspectorOpen = !inspectorOpen)}>
        Toggle inspector
      </Button>
    </div>
    <Inspector
      bind:open={inspectorOpen}
      {tabs}
      bind:activeTab={inspectorTab}
      title="filesystem"
    >
      <svelte:fragment slot="header">
        <IdentityCell primary="filesystem" secondary="Local filesystem access" size="lg" />
      </svelte:fragment>
      {#if inspectorTab === 'overview'}
        <p>This is the Overview tab. Health gauge, runtime facts, attached skills.</p>
      {:else if inspectorTab === 'tools'}
        <p>12 tools. List would render here.</p>
      {:else if inspectorTab === 'resources'}
        <p>4 resources.</p>
      {:else if inspectorTab === 'skills'}
        <p>2 attached skills: filesystem.search, file.read.</p>
      {/if}
      <svelte:fragment slot="actions">
        <Button variant="secondary" size="sm">View details</Button>
        <Button variant="ghost" size="sm">Restart</Button>
      </svelte:fragment>
    </Inspector>
  </div>
</section>

<style>
  .block {
    margin: var(--space-7) 0;
  }
  .block h2 {
    margin: 0 0 var(--space-3);
    font-family: var(--font-sans);
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-6);
    padding: var(--space-4);
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-3);
  }
  .state {
    margin: var(--space-2) 0 0;
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .state code {
    font-family: var(--font-mono);
    background: var(--color-bg-subtle);
    padding: 1px var(--space-2);
    border-radius: var(--radius-xs);
    color: var(--color-text-secondary);
  }
  .inspector-demo {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 304px;
    gap: var(--space-6);
  }
  .inspector-mock {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-6);
    min-height: 240px;
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    align-items: flex-start;
  }
  .hint {
    margin: 0;
    color: var(--color-text-tertiary);
  }
  @media (max-width: 1279px) {
    .inspector-demo {
      grid-template-columns: 1fr;
    }
  }
</style>
