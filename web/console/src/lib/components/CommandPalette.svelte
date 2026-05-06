<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { goto } from '$app/navigation';
  import IconSearch from 'lucide-svelte/icons/search';
  import IconArrowRight from 'lucide-svelte/icons/arrow-right';
  import IconSun from 'lucide-svelte/icons/sun';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconHistory from 'lucide-svelte/icons/history';
  import IconHome from 'lucide-svelte/icons/home';
  import IconServer from 'lucide-svelte/icons/server';
  import IconLayers from 'lucide-svelte/icons/layers';
  import IconFileText from 'lucide-svelte/icons/file-text';
  import IconBoxes from 'lucide-svelte/icons/boxes';
  import IconPackage from 'lucide-svelte/icons/package';
  import IconActivity from 'lucide-svelte/icons/activity';
  import IconShieldCheck from 'lucide-svelte/icons/shield-check';
  import IconDatabase from 'lucide-svelte/icons/database';
  import IconKey from 'lucide-svelte/icons/key';
  import IconLanguages from 'lucide-svelte/icons/languages';

  import { commandPaletteOpen, closeCommandPalette } from '$lib/ui';
  import { t, toggleLocale } from '$lib/i18n';
  import { cycleTheme } from '$lib/theme';

  type Cmd = {
    id: string;
    sectionKey: string;
    labelKey: string;
    Icon: typeof IconSearch;
    run: () => void;
  };

  let input: HTMLInputElement | undefined;
  let panel: HTMLDivElement | undefined;
  let query = '';
  let cursor = 0;

  $: items = buildItems();
  $: filtered = filterItems(items, query);

  function buildItems(): Cmd[] {
    type NavSpec = { id: string; labelKey: string; href: string; Icon: typeof IconHome };
    const navSpecs: NavSpec[] = [
      { id: 'nav.overview', labelKey: 'nav.overview', href: '/', Icon: IconHome },
      { id: 'nav.servers', labelKey: 'nav.servers', href: '/servers', Icon: IconServer },
      { id: 'nav.resources', labelKey: 'nav.resources', href: '/resources', Icon: IconLayers },
      { id: 'nav.prompts', labelKey: 'nav.prompts', href: '/prompts', Icon: IconFileText },
      { id: 'nav.apps', labelKey: 'nav.apps', href: '/apps', Icon: IconBoxes },
      { id: 'nav.skills', labelKey: 'nav.skills', href: '/skills', Icon: IconPackage },
      { id: 'nav.sessions', labelKey: 'nav.sessions', href: '/sessions', Icon: IconActivity },
      {
        id: 'nav.approvals',
        labelKey: 'nav.approvals',
        href: '/approvals',
        Icon: IconShieldCheck
      },
      { id: 'nav.audit', labelKey: 'nav.audit', href: '/audit', Icon: IconHistory },
      { id: 'nav.snapshots', labelKey: 'nav.snapshots', href: '/snapshots', Icon: IconDatabase },
      { id: 'nav.secrets', labelKey: 'nav.secrets', href: '/admin/secrets', Icon: IconKey }
    ];
    const nav: Cmd[] = navSpecs.map((n) => ({
      id: n.id,
      sectionKey: 'cmdk.section.navigate',
      labelKey: n.labelKey,
      Icon: n.Icon,
      run: () => {
        goto(n.href);
        closeCommandPalette();
      }
    }));

    const actions: Cmd[] = [
      {
        id: 'action.toggleTheme',
        sectionKey: 'cmdk.section.actions',
        labelKey: 'cmdk.action.toggleTheme',
        Icon: IconSun,
        run: () => {
          cycleTheme();
          closeCommandPalette();
        }
      },
      {
        id: 'action.toggleLocale',
        sectionKey: 'cmdk.section.actions',
        labelKey: 'cmdk.action.toggleLocale',
        Icon: IconLanguages,
        run: () => {
          toggleLocale();
          closeCommandPalette();
        }
      },
      {
        id: 'action.refresh',
        sectionKey: 'cmdk.section.actions',
        labelKey: 'cmdk.action.refresh',
        Icon: IconRefreshCw,
        run: () => {
          location.reload();
        }
      },
      {
        id: 'action.openAudit',
        sectionKey: 'cmdk.section.actions',
        labelKey: 'nav.audit',
        Icon: IconHistory,
        run: () => {
          goto('/audit');
          closeCommandPalette();
        }
      }
    ];

    return [...nav, ...actions];
  }

  // Subsequence fuzzy match. Cheap; works well at this scale.
  function fuzzyScore(label: string, q: string): number {
    if (!q) return 1;
    const l = label.toLowerCase();
    const s = q.toLowerCase();
    let score = 0;
    let li = 0;
    for (let si = 0; si < s.length; si++) {
      const c = s[si];
      const found = l.indexOf(c, li);
      if (found < 0) return 0;
      score += 1 / (1 + found - li);
      li = found + 1;
    }
    return score;
  }

  function filterItems(all: Cmd[], q: string): Cmd[] {
    if (!q.trim()) return all;
    return all
      .map((it) => ({ it, score: fuzzyScore($t(it.labelKey), q) }))
      .filter((x) => x.score > 0)
      .sort((a, b) => b.score - a.score)
      .map((x) => x.it);
  }

  // Group filtered items by section for rendering
  $: grouped = filtered.reduce(
    (acc, it) => {
      const k = it.sectionKey;
      acc[k] = acc[k] ?? [];
      acc[k].push(it);
      return acc;
    },
    {} as Record<string, Cmd[]>
  );

  function move(delta: number) {
    if (filtered.length === 0) return;
    cursor = (cursor + delta + filtered.length) % filtered.length;
  }

  function activate() {
    if (filtered.length === 0) return;
    filtered[cursor]?.run();
  }

  function onKey(e: KeyboardEvent) {
    if (!$commandPaletteOpen) return;
    if (e.key === 'Escape') {
      e.preventDefault();
      closeCommandPalette();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      move(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      move(-1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      activate();
    }
  }

  $: if ($commandPaletteOpen) {
    cursor = 0;
    query = '';
    tick().then(() => input?.focus());
  }

  onMount(() => document.addEventListener('keydown', onKey));
  onDestroy(() => document.removeEventListener('keydown', onKey));
</script>

{#if $commandPaletteOpen}
  <button
    class="overlay"
    type="button"
    aria-label={$t('common.close')}
    tabindex="-1"
    on:click={closeCommandPalette}
  ></button>
  <div
    class="palette"
    role="dialog"
    aria-modal="true"
    aria-label={$t('common.search')}
    bind:this={panel}
  >
    <div class="search">
      <span class="search-ico" aria-hidden="true"><IconSearch size={16} /></span>
      <input
        bind:this={input}
        bind:value={query}
        type="text"
        placeholder={$t('cmdk.placeholder')}
        on:input={() => (cursor = 0)}
      />
      <kbd>esc</kbd>
    </div>
    <div class="list" role="listbox">
      {#if filtered.length === 0}
        <p class="empty">{$t('cmdk.empty')}</p>
      {:else}
        {#each Object.entries(grouped) as [sectionKey, items] (sectionKey)}
          <div class="section">{$t(sectionKey)}</div>
          {#each items as cmd, _idx (cmd.id)}
            {@const overallIdx = filtered.indexOf(cmd)}
            <button
              type="button"
              class="row"
              class:cursor={cursor === overallIdx}
              on:click={cmd.run}
              on:mouseenter={() => (cursor = overallIdx)}
              role="option"
              aria-selected={cursor === overallIdx}
            >
              <span class="row-ico" aria-hidden="true"><cmd.Icon size={14} /></span>
              <span class="row-label">{$t(cmd.labelKey)}</span>
              <span class="row-cta" aria-hidden="true"><IconArrowRight size={14} /></span>
            </button>
          {/each}
        {/each}
      {/if}
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(15, 23, 42, 0.36);
    backdrop-filter: blur(2px);
    z-index: var(--z-modal);
    border: none;
    cursor: default;
    padding: 0;
    animation: fade-in var(--motion-default) var(--ease-default);
  }
  .overlay:focus {
    outline: none;
  }
  .palette {
    position: fixed;
    top: 14vh;
    left: 50%;
    transform: translateX(-50%);
    width: min(640px, calc(100vw - 32px));
    max-height: 60vh;
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    z-index: calc(var(--z-modal) + 1);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    animation: slide-down var(--motion-panel) var(--ease-default);
  }
  .search {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-bottom: 1px solid var(--color-border-soft);
  }
  .search-ico {
    color: var(--color-icon-default);
    display: inline-flex;
  }
  .search input {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--color-text-primary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-md);
  }
  .search input::placeholder {
    color: var(--color-text-tertiary);
  }
  kbd {
    font-family: var(--font-mono);
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    padding: 2px var(--space-2);
    border-radius: var(--radius-xs);
  }
  .list {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-2);
  }
  .section {
    padding: var(--space-2) var(--space-3) 4px;
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    font-weight: var(--font-weight-medium);
  }
  .row {
    appearance: none;
    background: transparent;
    border: none;
    width: 100%;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    color: var(--color-text-primary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    text-align: left;
  }
  .row.cursor {
    background: var(--color-accent-primary-subtle);
    color: var(--color-accent-primary);
  }
  .row.cursor .row-ico {
    color: var(--color-accent-primary);
  }
  .row-ico {
    color: var(--color-icon-default);
    display: inline-flex;
    flex-shrink: 0;
  }
  .row-label {
    flex: 1;
  }
  .row-cta {
    color: var(--color-icon-subtle);
    display: inline-flex;
    flex-shrink: 0;
  }
  .empty {
    text-align: center;
    color: var(--color-text-tertiary);
    padding: var(--space-8) var(--space-4);
    margin: 0;
  }
  @keyframes fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
  @keyframes slide-down {
    from {
      transform: translate(-50%, -8px);
      opacity: 0;
    }
    to {
      transform: translate(-50%, 0);
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .palette {
      animation: none;
    }
  }
</style>
