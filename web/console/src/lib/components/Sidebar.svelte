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
  import IconPlug from 'lucide-svelte/icons/plug';
  import IconChevronLeft from 'lucide-svelte/icons/chevron-left';
  import IconChevronRight from 'lucide-svelte/icons/chevron-right';

  type Item = { labelKey: string; href: string; Icon: typeof IconHome };
  type Group = { labelKey?: string; items: Item[] };

  // Fixed in 10.6: previous version had two groups labelled
  // `nav.section.operations`, which rendered as duplicate "OPERATIONS"
  // headers. Policy collapses into the same Operations group so the
  // taxonomy stays flat.
  const groups: Group[] = [
    {
      items: [
        { labelKey: 'nav.overview', href: '/', Icon: IconHome },
        // Phase 10.9: connection facts + copy-paste client snippets.
        // Pinned just below Overview because "how do I connect?" is
        // the first question a new operator asks.
        { labelKey: 'nav.connect', href: '/connect', Icon: IconPlug }
      ]
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
        { labelKey: 'nav.policy', href: '/policy', Icon: IconScale },
        { labelKey: 'nav.audit', href: '/audit', Icon: IconHistory },
        { labelKey: 'nav.snapshots', href: '/snapshots', Icon: IconDatabase },
        { labelKey: 'nav.playground', href: '/playground', Icon: IconPlay }
      ]
    },
    {
      labelKey: 'nav.section.admin',
      items: [
        { labelKey: 'nav.secrets', href: '/admin/secrets', Icon: IconKey },
        { labelKey: 'nav.tenants', href: '/admin/tenants', Icon: IconUsers }
      ]
    }
  ];

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

  type Tone = 'neutral' | 'success' | 'warning' | 'danger';
  $: overallTone = ((): Tone => {
    if (healthOk === null && readyOk === null) return 'neutral';
    if (healthOk === false) return 'danger';
    if (readyOk === false) return 'warning';
    return 'success';
  })();
  $: statusCopy = ((): string => {
    if (overallTone === 'success') return $t('sidebar.allSystemsOperational');
    if (overallTone === 'warning') return $t('sidebar.partialHealth');
    if (overallTone === 'danger') return $t('sidebar.degraded');
    return $t('sidebar.unknown');
  })();
  $: statusTitle = `${$t('sidebar.health')}: ${
    healthOk === null ? '…' : healthOk ? $t('landing.status.ok') : $t('landing.status.down')
  } · ${$t('sidebar.ready')}: ${
    readyOk === null ? '…' : readyOk ? $t('landing.status.ok') : $t('landing.status.pending')
  }`;

  // Build-time injected; see vite.config.ts.
  const version = `v${typeof __APP_VERSION__ !== 'undefined' ? __APP_VERSION__ : '0.0.0'}`;
</script>

<aside class="sidebar" class:collapsed={$sidebarCollapsed} aria-label="Primary">
  <a class="brand" href="/" aria-label={$t('brand.name')}>
    <Logo size={28} variant="onDark" withWordmark={!$sidebarCollapsed} />
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
              <span class="ico"><it.Icon size={17} /></span>
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
    {#if !$sidebarCollapsed}
      <div class="status-card" title={statusTitle} aria-label={statusTitle}>
        <div class="status-card-head">
          <span class="card-title">{$t('sidebar.gateway')}</span>
          <span class="card-version">{version}</span>
        </div>
        <div class="status-card-body">
          <StatusDot tone={overallTone} />
          <span class="status-copy">{statusCopy}</span>
        </div>
      </div>
      <button
        type="button"
        class="collapse-btn"
        on:click={toggleSidebar}
        aria-label={$t('sidebar.collapse')}
        title={$t('sidebar.collapse')}
      >
        <IconChevronLeft size={14} />
      </button>
    {:else}
      <span class="status-row" title={statusTitle} aria-label={statusTitle}>
        <StatusDot tone={overallTone} />
      </span>
      <button
        type="button"
        class="collapse-btn"
        on:click={toggleSidebar}
        aria-label={$t('sidebar.expand')}
        title={$t('sidebar.expand')}
      >
        <IconChevronRight size={14} />
      </button>
    {/if}
  </div>
</aside>

<style>
  .sidebar {
    width: var(--layout-sidebar-width);
    flex-shrink: 0;
    background: var(--color-bg-sidebar);
    border-right: 1px solid var(--color-border-on-sidebar);
    display: flex;
    flex-direction: column;
    padding: var(--space-4) 0 var(--space-3);
    height: 100vh;
    position: sticky;
    top: 0;
    z-index: var(--z-sidebar);
    transition: width var(--motion-panel) var(--ease-default);
    color: var(--color-text-on-sidebar);
  }
  .sidebar.collapsed {
    width: var(--layout-sidebar-width-collapsed);
  }

  .brand {
    padding: 0 var(--space-4) var(--space-4);
    text-decoration: none;
    color: inherit;
    border-bottom: 1px solid var(--color-border-on-sidebar);
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
    /* tighter group rhythm to match the design spec */
    --nav-group-gap: var(--space-3);
  }
  .g-label {
    font-size: 11px;
    line-height: 14px;
    color: var(--color-text-on-sidebar-muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    padding: var(--space-3) var(--space-3) var(--space-1);
    font-weight: var(--font-weight-semibold);
    white-space: nowrap;
  }
  .g-rule {
    height: 1px;
    background: var(--color-border-on-sidebar);
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
    padding: 9px var(--space-3);
    border-radius: var(--radius-sm);
    color: var(--color-text-on-sidebar-soft);
    font-family: var(--font-sans);
    font-size: 13px;
    line-height: 20px;
    font-weight: var(--font-weight-medium);
    text-decoration: none;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default);
    white-space: nowrap;
  }
  .sidebar.collapsed a {
    justify-content: center;
    padding: 9px 0;
  }
  a:hover {
    background: var(--color-bg-sidebar-hover);
    color: var(--color-text-inverse);
  }
  a:focus-visible {
    outline: none;
    box-shadow: 0 0 0 3px rgba(216, 234, 232, 0.2);
  }
  a.active {
    background: var(--color-bg-sidebar-active);
    color: var(--color-text-inverse);
    font-weight: var(--font-weight-semibold);
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.08);
  }
  a.active::before {
    content: '';
    position: absolute;
    left: -2px;
    top: 6px;
    bottom: 6px;
    width: 3px;
    border-radius: 1px;
    background: var(--color-accent-primary);
  }
  .sidebar.collapsed a.active::before {
    left: 4px;
  }
  .ico {
    display: inline-flex;
    color: var(--color-text-on-sidebar-soft);
    flex-shrink: 0;
  }
  a:hover .ico {
    color: var(--color-text-inverse);
  }
  a.active .ico {
    color: var(--color-text-inverse);
  }
  .lbl {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .foot {
    padding: var(--space-3) var(--space-3) 0;
    border-top: 1px solid var(--color-border-on-sidebar);
    margin-top: var(--space-2);
    color: var(--color-text-on-sidebar-muted);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .sidebar.collapsed .foot {
    align-items: center;
    padding: var(--space-3) var(--space-2) 0;
  }
  .status-card {
    background: var(--color-bg-sidebar-elevated);
    border: 1px solid var(--color-border-on-sidebar);
    border-radius: var(--radius-md);
    padding: var(--space-3) var(--space-3);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .status-card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }
  .card-title {
    font-family: var(--font-sans);
    font-size: 12px;
    line-height: 16px;
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-on-sidebar);
    letter-spacing: 0.01em;
  }
  .card-version {
    font-family: var(--font-mono);
    font-size: 10px;
    line-height: 14px;
    color: var(--color-text-on-sidebar-muted);
    padding: 2px 6px;
    border-radius: var(--radius-pill);
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.06);
  }
  .status-card-body {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-text-on-sidebar-soft);
    font-size: 11px;
    line-height: 16px;
  }
  .status-copy {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .status-row {
    display: inline-flex;
    align-items: center;
  }
  .collapse-btn {
    appearance: none;
    background: transparent;
    border: 1px solid var(--color-border-on-sidebar);
    color: var(--color-text-on-sidebar-muted);
    cursor: pointer;
    width: 28px;
    height: 28px;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    align-self: flex-end;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default),
      border-color var(--motion-fast) var(--ease-default);
  }
  .sidebar.collapsed .collapse-btn {
    align-self: center;
  }
  .collapse-btn:hover {
    background: var(--color-bg-sidebar-hover);
    color: var(--color-text-inverse);
    border-color: rgba(255, 255, 255, 0.18);
  }
  .collapse-btn:focus-visible {
    outline: none;
    box-shadow: 0 0 0 3px rgba(216, 234, 232, 0.2);
  }
</style>
