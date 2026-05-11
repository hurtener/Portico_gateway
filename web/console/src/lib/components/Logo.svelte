<script lang="ts">
  /**
   * Portico brand mark — the architectural arch.
   * Inlined via Vite `?raw` so `currentColor` flows from the wrapping
   * element. Variant prop maps to a CSS color so the same SVG can render
   * dark teal on parchment, off-white on the dark sidebar, or accent on
   * empty-state illustrations without a second asset.
   */
  import logoSvg from '$lib/brand/portico-logo.svg?raw';

  export let size: number | string = 32;
  export let alt = 'Portico';
  export let withWordmark = false;
  export let variant: 'default' | 'onDark' | 'subtle' | 'accent' | 'inverse' = 'default';

  $: dim = typeof size === 'number' ? `${size}px` : size;
</script>

<span class="logo-row" data-variant={variant} role="img" aria-label={alt}>
  <span class="mark" style:--logo-size={dim}>
    <!-- eslint-disable-next-line svelte/no-at-html-tags -->
    {@html logoSvg}
  </span>
  {#if withWordmark}
    <span class="wordmark">Portico</span>
  {/if}
</span>

<style>
  .logo-row {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }
  .logo-row[data-variant='default'] {
    color: #0f5b57;
  }
  .logo-row[data-variant='onDark'] {
    color: var(--color-text-on-sidebar);
  }
  .logo-row[data-variant='subtle'] {
    color: var(--color-text-tertiary);
  }
  .logo-row[data-variant='accent'] {
    color: var(--color-accent-primary);
  }
  .logo-row[data-variant='inverse'] {
    color: var(--color-text-inverse);
  }
  .mark {
    display: inline-flex;
    flex-shrink: 0;
    width: var(--logo-size);
    height: var(--logo-size);
  }
  .mark :global(svg) {
    width: 100%;
    height: 100%;
    display: block;
  }
  .wordmark {
    font-family: var(--font-sans);
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    letter-spacing: -0.005em;
    color: inherit;
  }
  .logo-row[data-variant='onDark'] .wordmark,
  .logo-row[data-variant='inverse'] .wordmark {
    color: inherit;
  }
</style>
