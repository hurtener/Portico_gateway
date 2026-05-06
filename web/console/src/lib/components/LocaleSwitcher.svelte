<script lang="ts">
  import { locale, setLocale, t, type Locale } from '$lib/i18n';

  const options: { value: Locale; label: string }[] = [
    { value: 'en', label: 'EN' },
    { value: 'es', label: 'ES' }
  ];
</script>

<div
  class="switcher"
  role="group"
  aria-label={$t('topbar.locale.en') + ' / ' + $t('topbar.locale.es')}
>
  {#each options as opt (opt.value)}
    <button
      type="button"
      class="opt"
      class:active={$locale === opt.value}
      aria-pressed={$locale === opt.value}
      title={opt.label}
      on:click={() => setLocale(opt.value)}
    >
      {opt.label}
    </button>
  {/each}
</div>

<style>
  .switcher {
    display: inline-flex;
    align-items: center;
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-pill);
    padding: 2px;
    gap: 2px;
  }
  .opt {
    appearance: none;
    border: none;
    background: transparent;
    cursor: pointer;
    color: var(--color-text-secondary);
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-medium);
    padding: 4px var(--space-3);
    border-radius: var(--radius-pill);
    height: 28px;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default);
  }
  .opt:hover {
    color: var(--color-text-primary);
  }
  .opt:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .opt.active {
    background: var(--color-bg-elevated);
    color: var(--color-accent-primary);
    box-shadow: var(--shadow-sm);
  }
</style>
