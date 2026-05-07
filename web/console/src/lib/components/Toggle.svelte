<script lang="ts">
  export let checked = false;
  export let disabled = false;
  export let id: string | undefined = undefined;
  export let label: string | undefined = undefined;
  export let ariaLabel: string | undefined = undefined;
</script>

<label class="row" class:disabled>
  <button
    type="button"
    role="switch"
    aria-checked={checked}
    aria-label={ariaLabel || label}
    {id}
    {disabled}
    on:click={() => (checked = !checked)}
    class="track"
    class:on={checked}
  >
    <span class="thumb" class:on={checked}></span>
  </button>
  {#if label}
    <span class="lbl">{label}</span>
  {/if}
</label>

<style>
  .row {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    cursor: pointer;
    user-select: none;
  }
  .row.disabled {
    cursor: not-allowed;
    opacity: 0.55;
  }
  .lbl {
    font-family: var(--font-sans);
    font-size: var(--font-size-body-md);
    color: var(--color-text-primary);
  }
  .track {
    position: relative;
    width: 36px;
    height: 20px;
    border-radius: 999px;
    background: var(--color-border-default);
    border: none;
    cursor: inherit;
    padding: 2px;
    transition: background var(--motion-fast) var(--ease-default);
  }
  .track.on {
    background: var(--color-accent-primary);
  }
  .track:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .thumb {
    display: block;
    width: 16px;
    height: 16px;
    border-radius: 999px;
    background: var(--color-bg-elevated);
    box-shadow: var(--shadow-sm);
    transform: translateX(0);
    transition: transform var(--motion-fast) var(--ease-default);
  }
  .thumb.on {
    transform: translateX(16px);
  }
</style>
