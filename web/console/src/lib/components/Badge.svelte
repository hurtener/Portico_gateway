<script lang="ts">
  type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'accent' | 'warm';
  type Size = 'sm' | 'md';

  export let tone: Tone = 'neutral';
  export let size: Size = 'sm';
  export let pill = false;
  export let mono = false;
</script>

<span class="badge {tone} {size}" class:pill class:mono>
  <slot name="leading" />
  <slot />
  <slot name="trailing" />
</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    border-radius: var(--radius-xs);
    border: 1px solid transparent;
    font-family: var(--font-sans);
    font-weight: var(--font-weight-medium);
    line-height: 1;
    white-space: nowrap;
  }
  .pill {
    border-radius: var(--radius-pill);
  }
  .mono {
    font-family: var(--font-mono);
  }

  .sm {
    padding: 3px var(--space-2);
    font-size: var(--font-size-label);
  }
  .md {
    padding: 4px var(--space-3);
    font-size: var(--font-size-body-sm);
  }

  .neutral {
    background: var(--color-bg-subtle);
    color: var(--color-text-secondary);
    border-color: var(--color-border-soft);
  }
  .success {
    background: var(--color-success-soft);
    color: var(--color-success);
  }
  .warning {
    background: var(--color-warning-soft);
    color: var(--color-warning);
  }
  .danger {
    background: var(--color-danger-soft);
    color: var(--color-danger);
  }
  .info {
    background: var(--color-info-soft);
    color: var(--color-info);
  }
  .accent {
    background: var(--color-accent-primary-subtle);
    color: var(--color-accent-primary);
  }
  .warm {
    background: var(--color-accent-warm-subtle);
    color: var(--color-accent-warm);
  }
</style>
