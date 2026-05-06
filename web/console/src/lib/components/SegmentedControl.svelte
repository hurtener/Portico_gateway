<script lang="ts">
  type Option = { value: string; label: string; disabled?: boolean };
  export let options: Option[] = [];
  export let value: string = '';
  export let onChange: ((v: string) => void) | null = null;
  export let ariaLabel: string | undefined = undefined;

  function pick(o: Option) {
    if (o.disabled) return;
    value = o.value;
    onChange?.(o.value);
  }
</script>

<div class="seg" role="group" aria-label={ariaLabel}>
  {#each options as opt}
    <button
      type="button"
      class="opt"
      class:active={value === opt.value}
      disabled={opt.disabled}
      aria-pressed={value === opt.value}
      on:click={() => pick(opt)}
    >
      {opt.label}
    </button>
  {/each}
</div>

<style>
  .seg {
    display: inline-flex;
    align-items: center;
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: 2px;
    gap: 2px;
  }
  .opt {
    appearance: none;
    background: transparent;
    border: none;
    cursor: pointer;
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    color: var(--color-text-secondary);
    padding: 6px var(--space-3);
    border-radius: var(--radius-sm);
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default);
  }
  .opt:hover:not([disabled]) {
    color: var(--color-text-primary);
  }
  .opt:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .opt.active {
    background: var(--color-bg-elevated);
    color: var(--color-accent-primary);
    box-shadow: var(--shadow-sm);
  }
  .opt[disabled] {
    opacity: 0.45;
    cursor: not-allowed;
  }
</style>
