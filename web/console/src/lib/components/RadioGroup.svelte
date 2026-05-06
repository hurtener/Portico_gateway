<script lang="ts">
  type Option = { value: string; label: string; hint?: string; disabled?: boolean };
  export let options: Option[] = [];
  export let value: string = '';
  export let name: string;
  export let label: string | undefined = undefined;
  export let layout: 'vertical' | 'horizontal' = 'vertical';
</script>

<fieldset class="rg" class:horizontal={layout === 'horizontal'}>
  {#if label}
    <legend class="legend">{label}</legend>
  {/if}
  {#each options as opt (opt.value)}
    <label class="row" class:disabled={opt.disabled}>
      <input
        type="radio"
        {name}
        value={opt.value}
        checked={value === opt.value}
        disabled={opt.disabled}
        on:change={() => (value = opt.value)}
      />
      <span class="bullet" class:on={value === opt.value}></span>
      <span class="text">
        <span class="lbl">{opt.label}</span>
        {#if opt.hint}<span class="hint">{opt.hint}</span>{/if}
      </span>
    </label>
  {/each}
</fieldset>

<style>
  .rg {
    border: 0;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .rg.horizontal {
    flex-direction: row;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
  .legend {
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-secondary);
    margin-bottom: var(--space-1);
  }
  .row {
    display: inline-flex;
    align-items: flex-start;
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
    pointer-events: none;
    width: 0;
    height: 0;
  }
  .bullet {
    display: inline-flex;
    width: 18px;
    height: 18px;
    border-radius: 999px;
    border: 1px solid var(--color-border-strong);
    background: var(--color-bg-elevated);
    margin-top: 2px;
    flex-shrink: 0;
    transition:
      background var(--motion-fast) var(--ease-default),
      border-color var(--motion-fast) var(--ease-default);
  }
  input:focus-visible + .bullet {
    box-shadow: var(--ring-focus);
  }
  .bullet.on {
    border-color: var(--color-accent-primary);
    background: radial-gradient(
      circle,
      var(--color-accent-primary) 35%,
      var(--color-bg-elevated) 38%
    );
  }
  .text {
    display: flex;
    flex-direction: column;
  }
  .lbl {
    font-size: var(--font-size-body-md);
    color: var(--color-text-primary);
  }
  .hint {
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
  }
</style>
