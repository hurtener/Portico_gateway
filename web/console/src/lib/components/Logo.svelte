<script lang="ts">
  /**
   * Portico brand mark — the architectural arch in muted teal.
   * Pulled from /static/brand/portico-logo.svg so the SVG is editable.
   *
   * For the wordmark, render <Logo /> next to a styled <span class="wordmark">.
   */
  export let size: number | string = 32;
  export let alt = 'Portico';
  export let withWordmark = false;
</script>

<span class="logo-row">
  <img class="mark" src="/brand/portico-logo.svg" {alt} width={size} height={size} />
  {#if withWordmark}
    <span class="wordmark">Portico</span>
  {/if}
</span>

<style>
  .logo-row {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-text-primary);
  }
  .mark {
    display: block;
    flex-shrink: 0;
  }
  .wordmark {
    font-family: var(--font-sans);
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    letter-spacing: -0.005em;
    color: var(--color-text-primary);
  }
</style>
