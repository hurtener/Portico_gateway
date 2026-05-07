<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import IconX from 'lucide-svelte/icons/x';

  export let open = false;
  export let title: string | undefined = undefined;
  export let description: string | undefined = undefined;
  export let onClose: (() => void) | null = null;
  export let size: 'sm' | 'md' | 'lg' = 'md';
  export let dismissible = true;

  let dialog: HTMLDivElement | undefined;
  let previousFocus: HTMLElement | null = null;

  function close() {
    if (!dismissible) return;
    open = false;
    onClose?.();
  }

  function onKey(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === 'Escape') {
      e.stopPropagation();
      close();
    } else if (e.key === 'Tab' && dialog) {
      // Focus trap
      const focusables = dialog.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
      );
      if (focusables.length === 0) return;
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }

  $: if (open) {
    previousFocus = document.activeElement as HTMLElement | null;
    queueMicrotask(() => {
      dialog?.querySelector<HTMLElement>('[autofocus]')?.focus() ??
        dialog
          ?.querySelector<HTMLElement>('a[href], button:not([disabled]), input:not([disabled])')
          ?.focus();
    });
  } else if (previousFocus) {
    previousFocus.focus();
    previousFocus = null;
  }

  onMount(() => {
    document.addEventListener('keydown', onKey);
  });
  onDestroy(() => {
    document.removeEventListener('keydown', onKey);
  });
</script>

{#if open}
  <button class="overlay" type="button" aria-label="Close dialog" tabindex="-1" on:click={close}
  ></button>
  <div class="overlay-layer">
    <div
      bind:this={dialog}
      class="dialog {size}"
      role="dialog"
      aria-modal="true"
      aria-labelledby={title ? 'modal-title' : undefined}
      aria-describedby={description ? 'modal-desc' : undefined}
    >
      {#if title || dismissible}
        <header class="head">
          <div class="titles">
            {#if title}<h2 id="modal-title" class="title">{title}</h2>{/if}
            {#if description}<p id="modal-desc" class="desc">{description}</p>{/if}
          </div>
          {#if dismissible}
            <button class="close" type="button" aria-label="Close" on:click={close}>
              <IconX size={16} />
            </button>
          {/if}
        </header>
      {/if}
      <div class="body">
        <slot />
      </div>
      {#if $$slots.footer}
        <footer class="foot"><slot name="footer" /></footer>
      {/if}
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(15, 23, 42, 0.42);
    backdrop-filter: blur(2px);
    z-index: var(--z-modal);
    border: none;
    cursor: default;
    padding: 0;
    animation: fade-in var(--motion-default) var(--ease-default);
  }
  .overlay:focus {
    outline: none;
  }
  .overlay-layer {
    position: fixed;
    inset: 0;
    z-index: calc(var(--z-modal) + 1);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-6);
    pointer-events: none;
  }
  .overlay-layer > .dialog {
    pointer-events: auto;
  }
  .dialog {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    display: flex;
    flex-direction: column;
    width: 100%;
    max-height: calc(100vh - var(--space-12));
    overflow: hidden;
    animation: slide-up var(--motion-panel) var(--ease-default);
  }
  .sm {
    max-width: 420px;
  }
  .md {
    max-width: 600px;
  }
  .lg {
    max-width: 880px;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-5) var(--space-6);
    border-bottom: 1px solid var(--color-border-soft);
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  .title {
    font-size: var(--font-size-heading-4);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    margin: 0;
    line-height: var(--font-line-heading-4);
  }
  .desc {
    margin: var(--space-1) 0 0 0;
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-sm);
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
    padding: var(--space-5) var(--space-6);
    overflow-y: auto;
  }
  .foot {
    border-top: 1px solid var(--color-border-soft);
    padding: var(--space-4) var(--space-6);
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
  @keyframes slide-up {
    from {
      transform: translateY(8px);
      opacity: 0;
    }
    to {
      transform: translateY(0);
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .dialog {
      animation: none;
    }
  }
</style>
