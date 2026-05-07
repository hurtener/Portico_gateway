<script lang="ts" context="module">
  function iconSizeFor(size: 'sm' | 'md' | 'lg'): number {
    return size === 'sm' ? 14 : size === 'lg' ? 18 : 16;
  }
</script>

<script lang="ts">
  import IconLoader from 'lucide-svelte/icons/loader-2';

  type Variant = 'primary' | 'secondary' | 'ghost' | 'subtle' | 'destructive';
  type Size = 'sm' | 'md' | 'lg';

  export let variant: Variant = 'primary';
  export let size: Size = 'md';
  export let type: 'button' | 'submit' | 'reset' = 'button';
  export let href: string | null = null;
  export let disabled = false;
  export let loading = false;
  export let block = false;
  export let title: string | undefined = undefined;
  export let ariaLabel: string | undefined = undefined;
</script>

{#if href}
  <a
    {href}
    class="btn {variant} {size}"
    class:block
    class:loading
    aria-disabled={disabled || loading || undefined}
    aria-label={ariaLabel}
    {title}
    on:click
  >
    {#if loading}
      <span class="spin"><IconLoader size={iconSizeFor(size)} /></span>
    {:else}
      <slot name="leading" />
    {/if}
    <span class="label"><slot /></span>
    <slot name="trailing" />
  </a>
{:else}
  <button
    {type}
    class="btn {variant} {size}"
    class:block
    class:loading
    {disabled}
    aria-busy={loading || undefined}
    aria-label={ariaLabel}
    {title}
    on:click
  >
    {#if loading}
      <span class="spin"><IconLoader size={iconSizeFor(size)} /></span>
    {:else}
      <slot name="leading" />
    {/if}
    <span class="label"><slot /></span>
    <slot name="trailing" />
  </button>
{/if}

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    border-radius: var(--radius-md);
    font-family: var(--font-sans);
    font-weight: var(--font-weight-medium);
    cursor: pointer;
    text-decoration: none;
    border: 1px solid transparent;
    transition:
      background var(--motion-fast) var(--ease-default),
      border-color var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default),
      box-shadow var(--motion-fast) var(--ease-default);
    user-select: none;
    white-space: nowrap;
  }
  .btn:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .btn[disabled],
  .btn[aria-disabled='true'] {
    cursor: not-allowed;
    opacity: 0.55;
  }
  .btn.loading {
    cursor: progress;
  }
  .block {
    width: 100%;
  }

  /* Sizes */
  .sm {
    height: 32px;
    padding: 0 var(--space-3);
    font-size: var(--font-size-body-sm);
  }
  .md {
    height: 40px;
    padding: 0 var(--space-4);
    font-size: var(--font-size-body-md);
  }
  .lg {
    height: 48px;
    padding: 0 var(--space-5);
    font-size: var(--font-size-body-lg);
  }

  /* Variants */
  .primary {
    background: var(--color-accent-primary);
    color: var(--color-accent-on-primary);
  }
  .primary:hover:not([disabled]):not([aria-disabled='true']) {
    background: var(--color-accent-primary-hover);
  }
  .primary:active:not([disabled]):not([aria-disabled='true']) {
    background: var(--color-accent-primary-active);
  }

  .secondary {
    background: var(--color-bg-elevated);
    color: var(--color-text-primary);
    border-color: var(--color-border-default);
  }
  .secondary:hover:not([disabled]):not([aria-disabled='true']) {
    border-color: var(--color-border-strong);
    background: var(--color-bg-subtle);
  }

  .ghost {
    background: transparent;
    color: var(--color-text-secondary);
  }
  .ghost:hover:not([disabled]):not([aria-disabled='true']) {
    background: var(--color-bg-subtle);
    color: var(--color-text-primary);
  }

  .subtle {
    background: var(--color-accent-primary-subtle);
    color: var(--color-accent-primary);
  }
  .subtle:hover:not([disabled]):not([aria-disabled='true']) {
    background: var(--color-accent-primary-soft);
  }

  .destructive {
    background: var(--color-danger);
    color: var(--color-text-inverse);
  }
  .destructive:hover:not([disabled]):not([aria-disabled='true']) {
    filter: brightness(0.95);
  }

  .label {
    line-height: 1;
  }

  .spin {
    display: inline-flex;
    animation: btn-spin 700ms linear infinite;
  }
  @keyframes btn-spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .spin {
      animation-duration: 1s;
    }
  }
</style>
