<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  export let open = false;
  export let placement: 'bottom-start' | 'bottom-end' | 'top-start' | 'top-end' = 'bottom-start';
  export let onClose: (() => void) | null = null;

  let root: HTMLDivElement;

  function close() {
    open = false;
    onClose?.();
  }
  function toggle() {
    open = !open;
  }

  function onDocClick(e: MouseEvent) {
    if (!open) return;
    if (root && !root.contains(e.target as Node)) close();
  }
  function onKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') close();
  }

  onMount(() => {
    document.addEventListener('mousedown', onDocClick);
    document.addEventListener('keydown', onKey);
  });
  onDestroy(() => {
    document.removeEventListener('mousedown', onDocClick);
    document.removeEventListener('keydown', onKey);
  });
</script>

<div class="root" bind:this={root}>
  <slot name="trigger" {open} {close} {toggle} />
  {#if open}
    <div class="panel {placement}" role="dialog">
      <slot {close} {toggle} />
    </div>
  {/if}
</div>

<style>
  .root {
    position: relative;
    display: inline-block;
  }
  .panel {
    position: absolute;
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    padding: var(--space-2);
    z-index: var(--z-popover);
    min-width: 200px;
    animation: pop-in var(--motion-fast) var(--ease-default);
  }
  .bottom-start {
    top: calc(100% + 6px);
    left: 0;
  }
  .bottom-end {
    top: calc(100% + 6px);
    right: 0;
  }
  .top-start {
    bottom: calc(100% + 6px);
    left: 0;
  }
  .top-end {
    bottom: calc(100% + 6px);
    right: 0;
  }
  @keyframes pop-in {
    from {
      opacity: 0;
      transform: translateY(-2px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .panel {
      animation: none;
    }
  }
</style>
