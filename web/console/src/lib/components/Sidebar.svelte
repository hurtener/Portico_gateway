<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Logo from './Logo.svelte';
  import StatusDot from './StatusDot.svelte';
  import { page } from '$app/stores';
  import { api } from '$lib/api';
  import { t } from '$lib/i18n';
  import { sidebarCollapsed, toggleSidebar } from '$lib/ui';
  import IconHome from 'lucide-svelte/icons/home';
  import IconServer from 'lucide-svelte/icons/server';
  import IconLayers from 'lucide-svelte/icons/layers';
  import IconFileText from 'lucide-svelte/icons/file-text';
  import IconBoxes from 'lucide-svelte/icons/boxes';
  import IconPackage from 'lucide-svelte/icons/package';
  import IconActivity from 'lucide-svelte/icons/activity';
  import IconShieldCheck from 'lucide-svelte/icons/shield-check';
  import IconHistory from 'lucide-svelte/icons/history';
  import IconDatabase from 'lucide-svelte/icons/database';
  import IconKey from 'lucide-svelte/icons/key';
  import IconUsers from 'lucide-svelte/icons/users';
  import IconScale from 'lucide-svelte/icons/scale';
  import IconPlay from 'lucide-svelte/icons/play';
  import IconChevronLeft from 'lucide-svelte/icons/chevron-left';
  import IconChevronRight from 'lucide-svelte/icons/chevron-right';

  type Item = { labelKey: string; href: string; Icon: typeof IconHome };
  type Group = { labelKey?: string; items: Item[] };

  const groups: Group[] = [
    {
      items: [{ labelKey: 'nav.overview', href: '/', Icon: IconHome }]
    },
    {
      labelKey: 'nav.section.catalog',
      items: [
        { labelKey: 'nav.servers', href: '/servers', Icon: IconServer },
        { labelKey: 'nav.resources', href: '/resources', Icon: IconLayers },
        { labelKey: 'nav.prompts', href: '/prompts', Icon: IconFileText },
        { labelKey: 'nav.apps', href: '/apps', Icon: IconBoxes },
        { labelKey: 'nav.skills', href: '/skills', Icon: IconPackage }
      ]
    },
    {
      labelKey: 'nav.section.operations',
      items: [
        { labelKey: 'nav.sessions', href: '/sessions', Icon: IconActivity },
        { labelKey: 'nav.approvals', href: '/approvals', Icon: IconShieldCheck },
        { labelKey: 'nav.audit', href: '/audit', Icon: IconHistory },
        { labelKey: 'nav.snapshots', href: '/snapshots', Icon: IconDatabase },
        { labelKey: 'nav.playground', href: '/playground', Icon: IconPlay }
      ]
    },
    {
      labelKey: 'nav.section.operations',
      items: [{ labelKey: 'nav.policy', href: '/policy', Icon: IconScale }]
    },
    {
      labelKey: 'nav.section.admin',
      items: [
        { labelKey: 'nav.secrets', href: '/admin/secrets', Icon: IconKey },
        { labelKey: 'nav.tenants', href: '/admin/tenants', Icon: IconUsers }
      ]
    }
  ];

  // Reactive `isActive` — Svelte's template tracker only sees variables it
  // reads in {...}. A plain function that closes over `pathname` doesn't
  // get re-evaluated on navigation, so we wrap it in $: which produces
  // a fresh closure whenever pathname changes.
  $: pathname = $page.url.pathname;
  $: isActive = (href: string): boolean => {
    if (href === '/') return pathname === '/';
    return pathname === href || pathname.startsWith(href + '/');
  };

  // Live gateway status — polled every 30s
  let healthOk: boolean | null = null;
  let readyOk: boolean | null = null;
  let pollHandle: ReturnType<typeof setInterval> | null = null;

  async function pollStatus() {
    try {
      const h = await api.health();
      healthOk = h.status === 'ok';
    } catch {
      healthOk = false;
    }
    try {
      const r = await api.ready();
      readyOk = r.status === 'ok' || r.status === 'ready';
    } catch {
      readyOk = false;
    }
  }

  onMount(() => {
    pollStatus();
    pollHandle = setInterval(pollStatus, 30_000);
  });
  onDestroy(() => {
    if (pollHandle !== null) clearInterval(pollHandle);
  });

  // Aggregated status — one indicator instead of two redundant green dots.
  type Tone = 'neutral' | 'success' | 'warning' | 'danger';
  $: overallTone = ((): Tone => {
    if (healthOk === null && readyOk === null) return 'neutral';
    if (healthOk === false) return 'danger';
    if (readyOk === false) return 'warning';
    return 'success';
  })();
  $: statusTitle = `${$t('sidebar.health')}: ${
    healthOk === null ? '…' : healthOk ? $t('landing.status.ok') : $t('landing.status.down')
  } · ${$t('sidebar.ready')}: ${
    readyOk === null ? '…' : readyOk ? $t('landing.status.ok') : $t('landing.status.pending')
  }`;
</script>

<aside class="sidebar" class:collapsed={$sidebarCollapsed} aria-label="Primary">
  <a class="brand" href="/" aria-label={$t('brand.name')}>
    <Logo size={28} withWordmark={!$sidebarCollapsed} />
  </a>

  <nav class="nav" aria-label="Sections">
    {#each groups as g, gi (gi)}
      {#if g.labelKey && !$sidebarCollapsed}
        <div class="g-label">{$t(g.labelKey)}</div>
      {:else if g.labelKey && $sidebarCollapsed}
        <div class="g-rule" aria-hidden="true"></div>
      {/if}
      <ul class="g-list">
        {#each g.items as it (it.href)}
          <li>
            <a
              href={it.href}
              class:active={isActive(it.href)}
              aria-current={isActive(it.href) ? 'page' : undefined}
              title={$sidebarCollapsed ? $t(it.labelKey) : undefined}
            >
              <span class="ico"><it.Icon size={16} /></span>
              {#if !$sidebarCollapsed}
                <span class="lbl">{$t(it.labelKey)}</span>
              {/if}
            </a>
          </li>
        {/each}
      </ul>
    {/each}
  </nav>

  <div class="foot">
    <span class="status-row" title={statusTitle} aria-label={statusTitle}>
      <StatusDot tone={overallTone} />
      {#if !$sidebarCollapsed}
        <span class="env-badge">{$t('topbar.envBadge')}</span>
      {/if}
    </span>
    <button
      type="button"
      class="collapse-btn"
      on:click={toggleSidebar}
      aria-label={$sidebarCollapsed ? $t('sidebar.expand') : $t('sidebar.collapse')}
      title={$sidebarCollapsed ? $t('sidebar.expand') : $t('sidebar.collapse')}
    >
      {#if $sidebarCollapsed}
        <IconChevronRight size={14} />
      {:else}
        <IconChevronLeft size={14} />
      {/if}
    </button>
  </div>
</aside>

<style>
  .sidebar {
    width: var(--layout-sidebar-width);
    flex-shrink: 0;
    background: var(--color-bg-surface);
    border-right: 1px solid var(--color-border-soft);
    display: flex;
    flex-direction: column;
    padding: var(--space-4) 0;
    height: 100vh;
    position: sticky;
    top: 0;
    z-index: var(--z-sidebar);
    transition: width var(--motion-panel) var(--ease-default);
  }
  .sidebar.collapsed {
    width: var(--layout-sidebar-width-collapsed);
  }

  .brand {
    padding: 0 var(--space-4) var(--space-4);
    text-decoration: none;
    color: inherit;
    border-bottom: 1px solid var(--color-border-soft);
    margin-bottom: var(--space-3);
    display: flex;
    align-items: center;
    overflow: hidden;
  }
  .sidebar.collapsed .brand {
    padding: 0 0 var(--space-4);
    justify-content: center;
  }
  .brand:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
    border-radius: var(--radius-xs);
  }

  .nav {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 0 var(--space-2);
  }
  .g-label {
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    padding: var(--space-3) var(--space-3) var(--space-1);
    font-weight: var(--font-weight-medium);
    white-space: nowrap;
  }
  .g-rule {
    height: 1px;
    background: var(--color-border-soft);
    margin: var(--space-3) var(--space-2) var(--space-1);
  }
  .g-list {
    list-style: none;
    padding: 0;
    margin: 0 0 var(--space-3) 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  a {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 8px var(--space-3);
    border-radius: var(--radius-sm);
    color: var(--color-text-secondary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    text-decoration: none;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default);
    white-space: nowrap;
  }
  .sidebar.collapsed a {
    justify-content: center;
    padding: 8px 0;
  }
  a:hover {
    background: var(--color-bg-subtle);
    color: var(--color-text-primary);
  }
  a:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  a.active {
    background: var(--color-accent-primary-subtle);
    color: var(--color-accent-primary);
    font-weight: var(--font-weight-medium);
  }
  a.active::before {
    content: '';
    position: absolute;
    left: 0;
    top: 6px;
    bottom: 6px;
    width: 2px;
    border-radius: 1px;
    background: var(--color-accent-primary);
  }
  .sidebar.collapsed a.active::before {
    left: 4px;
  }
  .ico {
    display: inline-flex;
    color: var(--color-icon-default);
    flex-shrink: 0;
  }
  a.active .ico {
    color: var(--color-accent-primary);
  }
  .lbl {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .foot {
    padding: var(--space-3) var(--space-3) 0;
    border-top: 1px solid var(--color-border-soft);
    margin-top: var(--space-2);
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }
  .sidebar.collapsed .foot {
    flex-direction: column;
    padding: var(--space-3) var(--space-2) 0;
    gap: var(--space-2);
  }
  .status-row {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }
  .env-badge {
    font-family: var(--font-mono);
    color: var(--color-text-tertiary);
    padding: 2px var(--space-2);
    border-radius: var(--radius-pill);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    font-size: var(--font-size-label);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .collapse-btn {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--color-icon-default);
    cursor: pointer;
    width: 28px;
    height: 28px;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default);
  }
  .collapse-btn:hover {
    background: var(--color-bg-subtle);
    color: var(--color-text-primary);
  }
  .collapse-btn:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
</style>
