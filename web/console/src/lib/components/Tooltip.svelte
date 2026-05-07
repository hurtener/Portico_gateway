<script lang="ts">
  export let label: string;
  export let placement: 'top' | 'bottom' | 'left' | 'right' = 'top';
</script>

<span class="wrap">
  <slot />
  <span class="tip {placement}" role="tooltip">{label}</span>
</span>

<style>
  .wrap {
    position: relative;
    display: inline-flex;
  }
  .tip {
    position: absolute;
    background: var(--color-text-primary);
    color: var(--color-text-inverse);
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    padding: 4px var(--space-2);
    border-radius: var(--radius-xs);
    white-space: nowrap;
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--motion-fast) var(--ease-default);
    z-index: var(--z-popover);
  }
  .wrap:hover .tip,
  .wrap:focus-within .tip {
    opacity: 1;
  }
  .top {
    bottom: calc(100% + 6px);
    left: 50%;
    transform: translateX(-50%);
  }
  .bottom {
    top: calc(100% + 6px);
    left: 50%;
    transform: translateX(-50%);
  }
  .left {
    right: calc(100% + 6px);
    top: 50%;
    transform: translateY(-50%);
  }
  .right {
    left: calc(100% + 6px);
    top: 50%;
    transform: translateY(-50%);
  }
</style>
