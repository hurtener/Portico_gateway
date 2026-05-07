<script lang="ts">
  import IconChevronDown from 'lucide-svelte/icons/chevron-down';

  type Option = { value: string; label: string; disabled?: boolean };
  type Size = 'sm' | 'md' | 'lg';

  export let value: string = '';
  export let id: string | undefined = undefined;
  export let name: string | undefined = undefined;
  export let label: string | undefined = undefined;
  export let hint: string | undefined = undefined;
  export let error: string | undefined = undefined;
  export let disabled = false;
  export let required = false;
  export let options: Option[] = [];
  export let placeholder: string | undefined = undefined;
  export let size: Size = 'md';
  export let block = true;

  $: hasError = Boolean(error);

  // Auto-generate an id when a label is present but no id was given,
  // so getByLabel + screen readers can resolve the select.
  let _autoId = '';
  $: if (label && !id) {
    if (!_autoId) {
      _autoId = `sel-${Math.random().toString(36).slice(2, 9)}`;
    }
  }
  $: resolvedId = id ?? (label ? _autoId : undefined);
</script>

<div class="field" class:block>
  {#if label}
    <label for={resolvedId} class="label">
      {label}
      {#if required}<span class="req" aria-hidden="true">*</span>{/if}
    </label>
  {/if}
  <div class="wrap" class:err={hasError}>
    <select
      class="select {size}"
      id={resolvedId}
      {name}
      {disabled}
      {required}
      bind:value
      on:change
      on:focus
      on:blur
      aria-invalid={hasError || undefined}
      aria-describedby={hint || error ? `${resolvedId}-msg` : undefined}
    >
      {#if placeholder}
        <option value="" disabled selected={value === ''}>{placeholder}</option>
      {/if}
      {#each options as opt}
        <option value={opt.value} disabled={opt.disabled}>{opt.label}</option>
      {/each}
    </select>
    <span class="caret" aria-hidden="true"><IconChevronDown size={16} /></span>
  </div>
  {#if error}
    <p class="msg err-msg" id="{resolvedId}-msg">{error}</p>
  {:else if hint}
    <p class="msg hint" id="{resolvedId}-msg">{hint}</p>
  {/if}
</div>

<style>
  .field {
    display: inline-flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .field.block {
    display: flex;
    width: 100%;
  }
  .label {
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-secondary);
  }
  .req {
    color: var(--color-danger);
    margin-left: var(--space-1);
  }
  .wrap {
    position: relative;
    display: flex;
    align-items: center;
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-default);
    border-radius: var(--radius-md);
    transition:
      border-color var(--motion-fast) var(--ease-default),
      box-shadow var(--motion-fast) var(--ease-default);
  }
  .wrap:hover {
    border-color: var(--color-border-strong);
  }
  .wrap:focus-within {
    border-color: var(--color-accent-primary);
    box-shadow: var(--ring-focus);
  }
  .wrap.err {
    border-color: var(--color-danger);
  }
  .select {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--color-text-primary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-md);
    padding: 0 var(--space-8) 0 var(--space-3);
    appearance: none;
    -webkit-appearance: none;
    -moz-appearance: none;
    width: 100%;
    cursor: pointer;
  }
  .sm {
    height: 32px;
    font-size: var(--font-size-body-sm);
  }
  .md {
    height: 40px;
  }
  .lg {
    height: 48px;
    font-size: var(--font-size-body-lg);
  }
  .select:disabled {
    cursor: not-allowed;
    color: var(--color-text-muted);
  }
  .caret {
    position: absolute;
    right: var(--space-3);
    pointer-events: none;
    color: var(--color-icon-default);
    display: inline-flex;
    align-items: center;
  }
  .msg {
    font-size: var(--font-size-label);
    margin: 0;
  }
  .hint {
    color: var(--color-text-tertiary);
  }
  .err-msg {
    color: var(--color-danger);
  }
</style>
