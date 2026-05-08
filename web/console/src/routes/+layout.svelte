<script lang="ts">
  import '@fontsource-variable/inter/wght.css';
  import '@fontsource-variable/jetbrains-mono/wght.css';
  import '@fontsource-variable/newsreader/wght.css';
  import '$lib/tokens.css';
  import '$lib/globals.css';

  import { onMount, onDestroy } from 'svelte';
  import { watchSystemPreference } from '$lib/theme';
  import { CommandPalette, Sidebar, TopBar, Toaster } from '$lib/components';
  import {
    openCommandPalette,
    closeCommandPalette,
    commandPaletteOpen,
    toggleSidebar
  } from '$lib/ui';

  let stopWatch: () => void = () => {};

  function isMac(): boolean {
    if (typeof navigator === 'undefined') return false;
    return /Mac|iPhone|iPad|iPod/.test(navigator.platform);
  }

  function onGlobalKey(e: KeyboardEvent) {
    const mod = isMac() ? e.metaKey : e.ctrlKey;
    if (!mod) return;
    if (e.key.toLowerCase() === 'k') {
      e.preventDefault();
      let isOpen = false;
      const unsub = commandPaletteOpen.subscribe((v) => (isOpen = v));
      unsub();
      isOpen ? closeCommandPalette() : openCommandPalette();
    } else if (e.key.toLowerCase() === 'b') {
      e.preventDefault();
      toggleSidebar();
    }
  }

  onMount(() => {
    stopWatch = watchSystemPreference();
    document.addEventListener('keydown', onGlobalKey);
  });
  onDestroy(() => {
    stopWatch();
    if (typeof document !== 'undefined') {
      document.removeEventListener('keydown', onGlobalKey);
    }
  });
</script>

<div class="shell">
  <Sidebar />
  <div class="main">
    <TopBar />
    <main class="content">
      <slot />
    </main>
  </div>
  <Toaster />
  <CommandPalette />
</div>

<style>
  .shell {
    display: flex;
    min-height: 100vh;
    background: var(--color-bg-canvas);
  }
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .content {
    flex: 1;
    width: 100%;
    /* Console list / workspace pages are fluid so the right-rail
     * inspector has somewhere to live. Detail / form pages opt back
     * into a narrower column via `<div class="page-narrow">`. */
    margin: 0 auto;
    padding: var(--space-7) var(--space-8) var(--space-8);
  }
  @media (max-width: 880px) {
    .content {
      padding: var(--space-6) var(--space-4);
    }
  }
  /* Narrow column for detail / form / docs pages.
   * Apply via <div class="page-narrow">…</div> at the top of those routes. */
  :global(.page-narrow) {
    max-width: var(--layout-content-narrow-max-width);
    margin: 0 auto;
  }
</style>
