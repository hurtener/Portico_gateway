<script lang="ts">
  export let value: string = '';
  export let id: string | undefined = undefined;
  export let name: string | undefined = undefined;
  export let label: string | undefined = undefined;
  export let placeholder: string | undefined = undefined;
  export let hint: string | undefined = undefined;
  export let error: string | undefined = undefined;
  export let disabled = false;
  export let readonly = false;
  export let required = false;
  export let rows = 4;
  export let mono = false;
  export let block = true;

  $: hasError = Boolean(error);
</script>

<div class="field" class:block>
  {#if label}
    <label for={id} class="label">
      {label}
      {#if required}<span class="req" aria-hidden="true">*</span>{/if}
    </label>
  {/if}
  <textarea
    class="ta"
    class:mono
    class:err={hasError}
    {id}
    {name}
    {placeholder}
    {disabled}
    {readonly}
    {required}
    {rows}
    bind:value
    on:input
    on:change
    on:focus
    on:blur
    aria-invalid={hasError || undefined}
    aria-describedby={hint || error ? `${id}-msg` : undefined}
  ></textarea>
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
  .ta {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-default);
    border-radius: var(--radius-md);
    color: var(--color-text-primary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-md);
    padding: var(--space-3);
    resize: vertical;
    min-height: 96px;
    transition:
      border-color var(--motion-fast) var(--ease-default),
      box-shadow var(--motion-fast) var(--ease-default);
    width: 100%;
  }
  .ta:hover {
    border-color: var(--color-border-strong);
  }
  .ta:focus {
    outline: none;
    border-color: var(--color-accent-primary);
    box-shadow: var(--ring-focus);
  }
  .ta.err {
    border-color: var(--color-danger);
  }
  .ta.err:focus {
    box-shadow: 0 0 0 3px rgba(178, 74, 59, 0.18);
  }
  .ta::placeholder {
    color: var(--color-text-tertiary);
  }
  .ta.mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
  }
  .ta:disabled {
    cursor: not-allowed;
    color: var(--color-text-muted);
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
