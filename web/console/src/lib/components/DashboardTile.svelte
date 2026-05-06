<script lang="ts">
  import StatusDot from './StatusDot.svelte';

  type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'accent';

  export let label: string;
  export let value: string | number | null | undefined;
  export let subValue: string | null = null;
  export let tone: Tone = 'neutral';
  export let dot: Tone | null = null;
  export let href: string | null = null;
  export let loading = false;
</script>

{#if href}
  <a class="tile" {href}>
    <header class="hd">
      {#if dot}<StatusDot tone={dot} />{/if}
      <span class="lbl">{label}</span>
    </header>
    <div class="value tone-{tone}">
      {#if loading}
        <span class="skel" aria-hidden="true"></span>
      {:else}
        <span class="v">{value ?? '—'}</span>
        {#if subValue}<span class="sv">{subValue}</span>{/if}
      {/if}
    </div>
    {#if $$slots.foot}
      <footer class="ft"><slot name="foot" /></footer>
    {/if}
  </a>
{:else}
  <article class="tile">
    <header class="hd">
      {#if dot}<StatusDot tone={dot} />{/if}
      <span class="lbl">{label}</span>
    </header>
    <div class="value tone-{tone}">
      {#if loading}
        <span class="skel" aria-hidden="true"></span>
      {:else}
        <span class="v">{value ?? '—'}</span>
        {#if subValue}<span class="sv">{subValue}</span>{/if}
      {/if}
    </div>
    {#if $$slots.foot}
      <footer class="ft"><slot name="foot" /></footer>
    {/if}
  </article>
{/if}

<style>
  .tile {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-4) var(--space-5);
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    color: var(--color-text-primary);
    text-decoration: none;
    min-height: 110px;
    transition:
      border-color var(--motion-fast) var(--ease-default),
      box-shadow var(--motion-fast) var(--ease-default);
  }
  a.tile:hover {
    border-color: var(--color-border-default);
    box-shadow: var(--shadow-sm);
  }
  a.tile:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .hd {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .lbl {
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-weight: var(--font-weight-medium);
  }
  .value {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    font-family: var(--font-sans);
    font-size: var(--font-size-heading-2);
    font-weight: var(--font-weight-semibold);
    line-height: 1;
    letter-spacing: -0.01em;
  }
  .v {
    color: var(--color-text-primary);
  }
  .tone-success .v {
    color: var(--color-success);
  }
  .tone-warning .v {
    color: var(--color-warning);
  }
  .tone-danger .v {
    color: var(--color-danger);
  }
  .tone-info .v {
    color: var(--color-info);
  }
  .tone-accent .v {
    color: var(--color-accent-primary);
  }
  .sv {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-tertiary);
  }
  .ft {
    margin-top: auto;
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .skel {
    display: inline-block;
    width: 60%;
    height: 28px;
    border-radius: var(--radius-sm);
    background: linear-gradient(
      90deg,
      var(--color-bg-subtle) 0%,
      var(--color-bg-elevated) 40%,
      var(--color-bg-subtle) 80%
    );
    background-size: 200% 100%;
    animation: tile-shimmer 1.6s linear infinite;
  }
  @keyframes tile-shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .skel {
      animation: none;
    }
  }
</style>
