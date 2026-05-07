<script lang="ts">
  import { themeMode, resolve, setTheme } from '$lib/theme';
  import IconSun from 'lucide-svelte/icons/sun';
  import IconMoon from 'lucide-svelte/icons/moon';
  import { t } from '$lib/i18n';

  // Two-state binary toggle. The underlying store still supports
  // 'system' (used on first load to honour prefers-color-scheme), but
  // the user-facing UI is light vs dark only.
  $: resolved = resolve($themeMode);
  $: isDark = resolved === 'dark';

  function toggle() {
    setTheme(isDark ? 'light' : 'dark');
  }
</script>

<button
  type="button"
  class="toggle"
  on:click={toggle}
  aria-label={isDark ? $t('topbar.theme.light') : $t('topbar.theme.dark')}
  aria-pressed={isDark}
  title={isDark ? $t('topbar.theme.light') : $t('topbar.theme.dark')}
>
  {#if isDark}
    <IconMoon size={16} aria-hidden="true" />
  {:else}
    <IconSun size={16} aria-hidden="true" />
  {/if}
</button>

<style>
  .toggle {
    appearance: none;
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    color: var(--color-icon-default);
    width: 32px;
    height: 32px;
    border-radius: var(--radius-pill);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default),
      border-color var(--motion-fast) var(--ease-default);
  }
  .toggle:hover {
    background: var(--color-bg-elevated);
    color: var(--color-text-primary);
    border-color: var(--color-border-default);
  }
  .toggle:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
</style>
