<script lang="ts" context="module">
  /**
   * Phase 10.6 design vocabulary.
   *
   * IdentityCell renders the leftmost cell of a Server or Skill table row:
   * a glyph box + primary label + optional secondary description. Servers
   * use the sans-serif treatment ("filesystem" / "Local filesystem
   * access"); skills use the mono treatment for the id ("github.code-
   * review" / "GitHub Code Review").
   *
   * Glyphs default to a single-letter abstract square in the muted
   * neutral palette so we don't need licensing for product marks. A
   * future revision can swap in real brand glyphs server-by-server.
   */
  export type IdentitySize = 'sm' | 'md' | 'lg';

  function initial(s: string): string {
    if (!s) return '?';
    // Strip a namespace prefix so "github.code-review" → "C", not "G".
    const trimmed = s.includes('.') ? s.split('.').pop() ?? s : s;
    return trimmed.charAt(0).toUpperCase();
  }

  function glyphHue(seed: string): number {
    // Deterministic hue per id → distinct glyphs across rows without
    // bringing in product marks. Matches the muted palette by clamping
    // saturation and lightness in CSS.
    let h = 0;
    for (let i = 0; i < seed.length; i++) {
      h = (h * 31 + seed.charCodeAt(i)) % 360;
    }
    return h;
  }
</script>

<script lang="ts">
  export let primary: string;
  export let secondary: string | undefined = undefined;
  /** When true, the primary label uses the mono font (skill ids). */
  export let mono = false;
  /** Override the auto-generated single-letter glyph. */
  export let glyph: string | undefined = undefined;
  export let size: IdentitySize = 'md';
  /** When set, the cell renders as an `<a>` so the whole identity is clickable. */
  export let href: string | null = null;
  /** Override the seed used for the glyph hue (defaults to `primary`). */
  export let glyphSeed: string | undefined = undefined;

  $: letter = (glyph ?? initial(primary)).slice(0, 2);
  $: hue = glyphHue(glyphSeed ?? primary);
</script>

{#if href}
  <a class="cell {size}" {href} class:mono>
    <span class="glyph" style:--glyph-hue={hue} aria-hidden="true">{letter}</span>
    <span class="text">
      <span class="primary">{primary}</span>
      {#if secondary}<span class="secondary">{secondary}</span>{/if}
    </span>
  </a>
{:else}
  <span class="cell {size}" class:mono>
    <span class="glyph" style:--glyph-hue={hue} aria-hidden="true">{letter}</span>
    <span class="text">
      <span class="primary">{primary}</span>
      {#if secondary}<span class="secondary">{secondary}</span>{/if}
    </span>
  </span>
{/if}

<style>
  .cell {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    text-decoration: none;
    color: inherit;
    min-width: 0;
  }
  a.cell:hover .primary {
    color: var(--color-accent-primary);
  }
  a.cell:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
    border-radius: var(--radius-xs);
  }

  .glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    border: 1px solid var(--color-border-soft);
    background: hsl(var(--glyph-hue), 18%, 95%);
    color: hsl(var(--glyph-hue), 28%, 28%);
    font-family: var(--font-sans);
    font-weight: var(--font-weight-semibold);
    text-transform: uppercase;
  }
  :global([data-theme='dark']) .glyph {
    background: hsl(var(--glyph-hue), 18%, 18%);
    color: hsl(var(--glyph-hue), 35%, 78%);
    border-color: var(--color-border-soft);
  }
  .sm .glyph {
    width: 28px;
    height: 28px;
    border-radius: var(--radius-xs);
    font-size: 11px;
  }
  .md .glyph {
    width: 36px;
    height: 36px;
    border-radius: var(--radius-sm);
    font-size: 13px;
  }
  .lg .glyph {
    width: 48px;
    height: 48px;
    border-radius: var(--radius-md);
    font-size: 16px;
  }

  .text {
    display: inline-flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .primary {
    color: var(--color-text-primary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    font-weight: var(--font-weight-semibold);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mono .primary {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
  }
  .secondary {
    color: var(--color-text-tertiary);
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    line-height: 1.4;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .lg .primary {
    font-size: var(--font-size-title);
  }
  .lg .secondary {
    font-size: var(--font-size-body-sm);
  }
</style>
