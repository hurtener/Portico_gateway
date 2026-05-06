<script lang="ts">
  import IconCheck from 'lucide-svelte/icons/check';
  export let checked = false;
  export let disabled = false;
  export let id: string | undefined = undefined;
  export let label: string | undefined = undefined;
  export let ariaLabel: string | undefined = undefined;
</script>

<label class="row" class:disabled>
  <span class="box" class:on={checked}>
    {#if checked}<IconCheck size={12} />{/if}
  </span>
  <input type="checkbox" bind:checked {id} {disabled} aria-label={ariaLabel || label} on:change />
  {#if label}
    <span class="lbl">{label}</span>
  {/if}
</label>

<style>
  .row {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    cursor: pointer;
    user-select: none;
  }
  .row.disabled {
    cursor: not-allowed;
    opacity: 0.55;
  }
  input {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
    pointer-events: none;
  }
  .box {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: var(--radius-xs);
    border: 1px solid var(--color-border-strong);
    background: var(--color-bg-elevated);
    color: transparent;
    transition:
      background var(--motion-fast) var(--ease-default),
      border-color var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default);
  }
  .row:focus-within .box {
    box-shadow: var(--ring-focus);
  }
  .box.on {
    background: var(--color-accent-primary);
    border-color: var(--color-accent-primary);
    color: var(--color-accent-on-primary);
  }
  .lbl {
    font-family: var(--font-sans);
    font-size: var(--font-size-body-md);
    color: var(--color-text-primary);
  }
</style>
