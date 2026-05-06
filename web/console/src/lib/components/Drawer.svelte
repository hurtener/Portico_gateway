<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import IconX from 'lucide-svelte/icons/x';

  export let open = false;
  export let side: 'right' | 'left' = 'right';
  export let title: string | undefined = undefined;
  export let onClose: (() => void) | null = null;
  export let width = '420px';
  export let dismissible = true;

  function close() {
    if (!dismissible) return;
    open = false;
    onClose?.();
  }

  function onKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') close();
  }

  onMount(() => document.addEventListener('keydown', onKey));
  onDestroy(() => document.removeEventListener('keydown', onKey));
</script>

{#if open}
  <button class="overlay" type="button" aria-label="Close drawer" tabindex="-1" on:click={close}
  ></button>
  <aside class="drawer {side}" role="dialog" aria-modal="true" aria-label={title} style:width>
    {#if title || dismissible}
      <header class="head">
        {#if title}<h2 class="title">{title}</h2>{/if}
        {#if dismissible}
          <button class="close" aria-label="Close" on:click={close}><IconX size={16} /></button>
        {/if}
      </header>
    {/if}
    <div class="body">
      <slot />
    </div>
    {#if $$slots.footer}
      <footer class="foot"><slot name="footer" /></footer>
    {/if}
  </aside>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(15, 23, 42, 0.36);
    z-index: var(--z-modal);
    border: none;
    cursor: default;
    padding: 0;
    animation: fade-in var(--motion-default) var(--ease-default);
  }
  .overlay:focus {
    outline: none;
  }
  .drawer {
    position: fixed;
    top: 0;
    bottom: 0;
    background: var(--color-bg-elevated);
    border-left: 1px solid var(--color-border-soft);
    box-shadow: var(--shadow-lg);
    display: flex;
    flex-direction: column;
    max-width: 90vw;
    z-index: calc(var(--z-modal) + 1);
  }
  .right {
    right: 0;
    animation: slide-in-right var(--motion-panel) var(--ease-default);
  }
  .left {
    left: 0;
    border-left: none;
    border-right: 1px solid var(--color-border-soft);
    animation: slide-in-left var(--motion-panel) var(--ease-default);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-4) var(--space-5);
    border-bottom: 1px solid var(--color-border-soft);
  }
  .title {
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    margin: 0;
  }
  .close {
    appearance: none;
    background: transparent;
    border: none;
    cursor: pointer;
    color: var(--color-icon-default);
    padding: var(--space-1);
    border-radius: var(--radius-xs);
  }
  .close:hover {
    background: var(--color-bg-subtle);
    color: var(--color-text-primary);
  }
  .close:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .body {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-5);
  }
  .foot {
    border-top: 1px solid var(--color-border-soft);
    padding: var(--space-4) var(--space-5);
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    background: var(--color-bg-subtle);
  }
  @keyframes fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
  @keyframes slide-in-right {
    from {
      transform: translateX(100%);
    }
    to {
      transform: translateX(0);
    }
  }
  @keyframes slide-in-left {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .drawer {
      animation: none;
    }
  }
</style>
