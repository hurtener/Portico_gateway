<script lang="ts">
  import { toasts, dismiss } from './toast';
  import IconCheckCircle from 'lucide-svelte/icons/check-circle-2';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import IconAlertCircle from 'lucide-svelte/icons/alert-circle';
  import IconInfo from 'lucide-svelte/icons/info';
  import IconX from 'lucide-svelte/icons/x';
</script>

<div class="stack" role="region" aria-live="polite" aria-label="Notifications">
  {#each $toasts as t (t.id)}
    <div class="toast {t.tone}" role="status">
      <span class="ico" aria-hidden="true">
        {#if t.tone === 'success'}
          <IconCheckCircle size={18} />
        {:else if t.tone === 'warning'}
          <IconAlertTriangle size={18} />
        {:else if t.tone === 'danger'}
          <IconAlertCircle size={18} />
        {:else}
          <IconInfo size={18} />
        {/if}
      </span>
      <div class="body">
        <div class="title">{t.title}</div>
        {#if t.description}<div class="desc">{t.description}</div>{/if}
      </div>
      <button class="close" type="button" aria-label="Dismiss" on:click={() => dismiss(t.id)}>
        <IconX size={14} />
      </button>
    </div>
  {/each}
</div>

<style>
  .stack {
    position: fixed;
    bottom: var(--space-6);
    right: var(--space-6);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    z-index: var(--z-toast);
    pointer-events: none;
  }
  .toast {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-default);
    border-left: 3px solid var(--color-accent-primary);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    padding: var(--space-3) var(--space-4);
    min-width: 280px;
    max-width: 420px;
    pointer-events: auto;
    animation: toast-in var(--motion-panel) var(--ease-default);
  }
  .toast.success {
    border-left-color: var(--color-success);
  }
  .toast.warning {
    border-left-color: var(--color-warning);
  }
  .toast.danger {
    border-left-color: var(--color-danger);
  }
  .toast.info {
    border-left-color: var(--color-info);
  }
  .ico {
    display: inline-flex;
    margin-top: 1px;
    color: var(--color-icon-default);
  }
  .toast.success .ico {
    color: var(--color-success);
  }
  .toast.warning .ico {
    color: var(--color-warning);
  }
  .toast.danger .ico {
    color: var(--color-danger);
  }
  .toast.info .ico {
    color: var(--color-info);
  }
  .body {
    flex: 1;
    min-width: 0;
  }
  .title {
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
  }
  .desc {
    font-size: var(--font-size-label);
    color: var(--color-text-secondary);
    margin-top: 2px;
  }
  .close {
    appearance: none;
    background: transparent;
    border: none;
    cursor: pointer;
    color: var(--color-icon-default);
    padding: 2px;
    border-radius: var(--radius-xs);
  }
  .close:hover {
    color: var(--color-text-primary);
    background: var(--color-bg-subtle);
  }
  .close:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  @keyframes toast-in {
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
    .toast {
      animation: none;
    }
  }
</style>
