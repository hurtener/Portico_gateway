<script lang="ts" context="module">
  /**
   * Phase 10.6 design vocabulary.
   *
   * MetricStrip renders the KPI row above a list-page table. Five cards
   * on desktop ≥1280px, three at 960–1279px, two below. Each card shows
   * a label, large metric, helper line, optional icon, and an `attention`
   * tone that swaps the surface to a warning tint (mockup uses this for
   * "Catalog Drift: 2 — review required").
   *
   * The Sparkline slot is reserved per the design spec but not populated
   * in this phase — Phase 11's metrics rollup will fill it. Card layout
   * stays stable when sparkline is absent.
   */
  import type { ComponentType, SvelteComponent } from 'svelte';

  export type MetricTone = 'default' | 'brand' | 'warning' | 'info' | 'danger' | 'success';

  export interface Metric {
    id: string;
    label: string;
    /**
     * Primary metric. Strings let us render "—" for unknown / not-yet-
     * measured. Numbers are rendered as-is.
     */
    value: string | number;
    /** Optional tail: "11 online · 1 offline", "Review required", etc. */
    helper?: string;
    icon?: ComponentType<SvelteComponent>;
    tone?: MetricTone;
    /** Highlights the card with a soft tint when the metric needs operator attention. */
    attention?: boolean;
    /** When provided, clicking the card navigates here. */
    href?: string;
    /** Optional click handler (mutually exclusive with href). */
    onClick?: () => void;
  }
</script>

<script lang="ts">
  export let metrics: Metric[] = [];
  /** Optional ARIA label for the overall strip. */
  export let label = 'Page metrics';
  /**
   * Compact variant for detail-page mini-KPI strips. Halves the card
   * padding and drops the value type from 28px → 22px. The helper line
   * is hidden in compact mode (truncated below 22px gets unreadable);
   * use the default variant when helper text is load-bearing.
   */
  export let compact = false;
</script>

<section
  class="strip"
  class:compact
  aria-label={label}
  data-region="kpi"
  data-variant={compact ? 'compact' : 'default'}
>
  {#each metrics as m (m.id)}
    {@const tone = m.tone ?? 'default'}
    {@const interactive = m.href || m.onClick}
    <svelte:element
      this={m.href ? 'a' : interactive ? 'button' : 'div'}
      class="card"
      role={interactive ? (m.href ? 'link' : 'button') : 'group'}
      aria-label={m.label}
      data-tone={tone}
      data-attention={m.attention ? 'true' : undefined}
      data-interactive={interactive ? 'true' : undefined}
      href={m.href ?? undefined}
      type={!m.href && interactive ? 'button' : undefined}
      on:click={() => m.onClick?.()}
    >
      <div class="head">
        {#if m.icon}
          <span class="icon" aria-hidden="true">
            <svelte:component this={m.icon} size={compact ? 14 : 18} />
          </span>
        {/if}
        <span class="label">{m.label}</span>
      </div>
      <div class="value">{m.value}</div>
      {#if m.helper && !compact}
        <div class="helper">{m.helper}</div>
      {/if}
    </svelte:element>
  {/each}
</section>

<style>
  .strip {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: var(--space-3);
    margin-bottom: var(--space-7);
  }
  .strip.compact {
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--space-2);
    margin-bottom: var(--space-5);
  }
  @media (max-width: 1279px) {
    .strip {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .strip.compact {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }
  @media (max-width: 960px) {
    .strip {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .strip.compact {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (max-width: 600px) {
    .strip,
    .strip.compact {
      grid-template-columns: 1fr;
    }
  }

  .card {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: var(--space-2);
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-5);
    box-shadow: var(--shadow-sm);
    text-decoration: none;
    color: inherit;
    text-align: left;
    transition:
      border-color var(--motion-fast) var(--ease-default),
      box-shadow var(--motion-fast) var(--ease-default),
      background var(--motion-fast) var(--ease-default);
    /* Reset button defaults */
    appearance: none;
    font: inherit;
    cursor: default;
  }
  .strip.compact .card {
    padding: var(--space-3);
    gap: var(--space-1);
    box-shadow: none;
  }
  .card[data-interactive='true'] {
    cursor: pointer;
  }
  .card[data-interactive='true']:hover {
    border-color: var(--color-border-default);
    box-shadow: var(--shadow-md);
  }
  .card:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .card[data-attention='true'] {
    background: var(--color-warning-soft);
    border-color: var(--color-warning);
  }
  .card[data-attention='true'] .label {
    color: var(--color-warning);
  }

  .head {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-text-secondary);
  }
  .icon {
    display: inline-flex;
    color: var(--color-icon-default);
  }
  .card[data-tone='brand'] .icon {
    color: var(--color-accent-primary);
  }
  .card[data-tone='warning'] .icon,
  .card[data-attention='true'] .icon {
    color: var(--color-warning);
  }
  .card[data-tone='danger'] .icon {
    color: var(--color-danger);
  }
  .card[data-tone='success'] .icon {
    color: var(--color-success);
  }
  .card[data-tone='info'] .icon {
    color: var(--color-info);
  }

  .label {
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
    letter-spacing: 0.01em;
    text-transform: uppercase;
  }
  .value {
    font-family: var(--font-sans);
    font-size: 28px;
    line-height: 34px;
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    letter-spacing: -0.02em;
  }
  .strip.compact .value {
    font-size: 22px;
    line-height: 28px;
  }
  .strip.compact .label {
    font-size: var(--font-size-label-sm, var(--font-size-label));
  }
  .helper {
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    line-height: 1.4;
  }
</style>
