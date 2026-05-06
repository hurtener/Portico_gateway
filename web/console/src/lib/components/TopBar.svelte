<script lang="ts">
  import ThemeToggle from './ThemeToggle.svelte';
  import LocaleSwitcher from './LocaleSwitcher.svelte';
  import NotificationsPopover from './NotificationsPopover.svelte';
  import UserMenu from './UserMenu.svelte';
  import IconSearch from 'lucide-svelte/icons/search';
  import { openCommandPalette } from '$lib/ui';
  import { t } from '$lib/i18n';
</script>

<header class="topbar">
  <div class="left">
    <button class="search" type="button" on:click={openCommandPalette}>
      <span class="search-ico" aria-hidden="true"><IconSearch size={14} /></span>
      <span class="search-text">{$t('topbar.search')}</span>
      <kbd class="kbd">⌘K</kbd>
    </button>
    <slot name="left" />
  </div>
  <div class="right">
    <slot name="right" />
    <LocaleSwitcher />
    <ThemeToggle />
    <span class="divider" aria-hidden="true"></span>
    <NotificationsPopover />
    <UserMenu />
  </div>
</header>

<style>
  .topbar {
    height: var(--layout-topbar-height);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: 0 var(--space-6);
    border-bottom: 1px solid var(--color-border-soft);
    background: var(--color-bg-surface);
    position: sticky;
    top: 0;
    z-index: var(--z-topbar);
  }
  .left {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    flex: 1;
  }
  .right {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .divider {
    width: 1px;
    height: 20px;
    background: var(--color-border-soft);
    margin: 0 var(--space-1);
  }
  .search {
    appearance: none;
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: 6px var(--space-3);
    height: 32px;
    width: 360px;
    max-width: 100%;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    cursor: pointer;
    color: var(--color-text-tertiary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    transition:
      background var(--motion-fast) var(--ease-default),
      border-color var(--motion-fast) var(--ease-default);
  }
  .search:hover {
    border-color: var(--color-border-default);
  }
  .search:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .search-ico {
    display: inline-flex;
    color: var(--color-icon-default);
  }
  .search-text {
    flex: 1;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kbd {
    font-family: var(--font-mono);
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    padding: 1px var(--space-2);
    border-radius: var(--radius-xs);
  }
</style>
