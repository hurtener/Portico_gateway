<script lang="ts">
  type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'accent';
  export let tone: Tone = 'neutral';
  export let pulse = false;
  export let label: string | undefined = undefined;
</script>

<span
  class="dot {tone}"
  class:pulse
  aria-label={label || undefined}
  role={label ? 'img' : undefined}
></span>

<style>
  .dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 999px;
    flex-shrink: 0;
  }
  .neutral {
    background: var(--color-text-tertiary);
  }
  .success {
    background: var(--color-success);
  }
  .warning {
    background: var(--color-warning);
  }
  .danger {
    background: var(--color-danger);
  }
  .info {
    background: var(--color-info);
  }
  .accent {
    background: var(--color-accent-primary);
  }
  .pulse {
    box-shadow: 0 0 0 0 currentColor;
    animation: dot-pulse 1.6s var(--ease-default) infinite;
  }
  @keyframes dot-pulse {
    0% {
      box-shadow: 0 0 0 0 rgba(45, 111, 115, 0.35);
    }
    70% {
      box-shadow: 0 0 0 6px rgba(45, 111, 115, 0);
    }
    100% {
      box-shadow: 0 0 0 0 rgba(45, 111, 115, 0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pulse {
      animation: none;
    }
  }
</style>
