<script lang="ts">
  type Size = 'sm' | 'md' | 'lg';

  export let value: string = '';
  export let type: 'text' | 'email' | 'password' | 'search' | 'url' | 'number' = 'text';
  export let placeholder: string | undefined = undefined;
  export let id: string | undefined = undefined;
  export let name: string | undefined = undefined;
  export let label: string | undefined = undefined;

  // Auto-generate an id when a label is present but no id was given,
  // so screen readers + tests using getByLabel can resolve the input.
  let _autoId = '';
  $: if (label && !id) {
    if (!_autoId) {
      _autoId = `inp-${Math.random().toString(36).slice(2, 9)}`;
    }
  }
  $: resolvedId = id ?? (label ? _autoId : undefined);
  export let hint: string | undefined = undefined;
  export let error: string | undefined = undefined;
  export let disabled = false;
  export let readonly = false;
  export let required = false;
  export let autocomplete: string | undefined = undefined;
  export let size: Size = 'md';
  export let mono = false;
  export let block = true;

  $: hasError = Boolean(error);

  function handleInput(event: Event) {
    const target = event.currentTarget as HTMLInputElement;
    value = target.value;
  }
</script>

<div class="field" class:block>
  {#if label}
    <label for={resolvedId} class="label">
      {label}
      {#if required}<span class="req" aria-hidden="true">*</span>{/if}
    </label>
  {/if}
  <div class="wrap" class:err={hasError}>
    {#if $$slots.leading}
      <span class="adorn"><slot name="leading" /></span>
    {/if}
    <input
      class="input {size}"
      class:mono
      id={resolvedId}
      {name}
      {type}
      {placeholder}
      {disabled}
      {readonly}
      {required}
      {autocomplete}
      {value}
      on:input={handleInput}
      on:input
      on:change
      on:focus
      on:blur
      on:keydown
      aria-invalid={hasError || undefined}
      aria-describedby={hint || error ? `${id}-msg` : undefined}
    />
    {#if $$slots.trailing}
      <span class="adorn"><slot name="trailing" /></span>
    {/if}
  </div>
  {#if error}
    <p class="msg err-msg" id="{id}-msg">{error}</p>
  {:else if hint}
    <p class="msg hint" id="{id}-msg">{hint}</p>
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
  .wrap.err:focus-within {
    box-shadow: 0 0 0 3px rgba(178, 74, 59, 0.18);
  }
  .input {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--color-text-primary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-md);
    width: 100%;
    padding: 0 var(--space-3);
  }
  .input.mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
  }
  .input::placeholder {
    color: var(--color-text-tertiary);
  }
  .input:disabled {
    cursor: not-allowed;
    color: var(--color-text-muted);
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
  .adorn {
    display: inline-flex;
    align-items: center;
    color: var(--color-icon-default);
    padding: 0 var(--space-3);
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
