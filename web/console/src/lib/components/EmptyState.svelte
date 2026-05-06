<script lang="ts">
  /**
   * EmptyState — uses the architectural arch motif as its default
   * illustration. Override with the `illustration` slot if a page wants
   * a different motif (linear diagrams, layered frames, routing paths).
   */
  export let title: string;
  export let description: string | undefined = undefined;
  export let compact = false;
</script>

<div class="es" class:compact>
  <div class="art" aria-hidden="true">
    <slot name="illustration">
      <!-- Default: simplified architectural arch -->
      <svg viewBox="0 0 160 120" width="160" height="120" fill="none" stroke="currentColor">
        <!-- Ground line -->
        <line x1="8" y1="106" x2="152" y2="106" stroke-width="1.25" />
        <!-- Left column -->
        <rect x="28" y="44" width="14" height="62" stroke-width="1.25" />
        <line x1="22" y1="44" x2="48" y2="44" stroke-width="1.25" />
        <line x1="20" y1="48" x2="50" y2="48" stroke-width="0.75" opacity="0.65" />
        <!-- Right column -->
        <rect x="118" y="44" width="14" height="62" stroke-width="1.25" />
        <line x1="112" y1="44" x2="138" y2="44" stroke-width="1.25" />
        <line x1="110" y1="48" x2="140" y2="48" stroke-width="0.75" opacity="0.65" />
        <!-- Architrave -->
        <rect x="24" y="32" width="112" height="10" stroke-width="1.25" />
        <!-- Pediment arch -->
        <path d="M30 32 Q80 4 130 32" stroke-width="1.25" />
        <!-- Subtle interior arches -->
        <path d="M48 90 Q80 60 112 90" stroke-width="0.75" opacity="0.55" />
        <line x1="80" y1="32" x2="80" y2="90" stroke-width="0.5" opacity="0.45" />
      </svg>
    </slot>
  </div>
  <div class="text">
    <h3 class="title">{title}</h3>
    {#if description}<p class="desc">{description}</p>{/if}
  </div>
  {#if $$slots.actions}
    <div class="actions"><slot name="actions" /></div>
  {/if}
</div>

<style>
  .es {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: var(--space-12) var(--space-6);
    gap: var(--space-4);
    color: var(--color-text-secondary);
  }
  .es.compact {
    padding: var(--space-8) var(--space-4);
  }
  .art {
    color: var(--color-accent-primary);
    opacity: 0.78;
  }
  .text {
    max-width: 48ch;
  }
  .title {
    font-family: var(--font-sans);
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    margin: 0 0 var(--space-2) 0;
  }
  .desc {
    margin: 0;
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-sm);
    line-height: var(--font-line-body-sm);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-2);
  }
</style>
