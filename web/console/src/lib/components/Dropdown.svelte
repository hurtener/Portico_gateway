<script lang="ts">
  import Popover from './Popover.svelte';

  type Item =
    | { kind: 'item'; label: string; onSelect: () => void; danger?: boolean; disabled?: boolean }
    | { kind: 'separator' }
    | { kind: 'header'; label: string };

  export let items: Item[] = [];
  export let placement: 'bottom-start' | 'bottom-end' = 'bottom-start';

  let open = false;
</script>

<Popover {placement} bind:open>
  <div slot="trigger">
    <slot name="trigger" {open} toggle={() => (open = !open)} />
  </div>
  <ul class="menu" role="menu">
    {#each items as it, i (i)}
      {#if it.kind === 'separator'}
        <li class="sep" role="separator"></li>
      {:else if it.kind === 'header'}
        <li class="hdr" role="presentation">{it.label}</li>
      {:else}
        <li role="none">
          <button
            type="button"
            role="menuitem"
            class="mi"
            class:danger={it.danger}
            disabled={it.disabled}
            on:click={() => {
              if (!it.disabled) {
                it.onSelect();
                open = false;
              }
            }}
          >
            {it.label}
          </button>
        </li>
      {/if}
    {/each}
  </ul>
</Popover>

<style>
  .menu {
    list-style: none;
    padding: 0;
    margin: 0;
    min-width: 180px;
  }
  .mi {
    appearance: none;
    background: transparent;
    border: none;
    width: 100%;
    text-align: left;
    cursor: pointer;
    padding: var(--space-2) var(--space-3);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    color: var(--color-text-primary);
    border-radius: var(--radius-xs);
  }
  .mi:hover:not([disabled]) {
    background: var(--color-bg-subtle);
  }
  .mi:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .mi[disabled] {
    color: var(--color-text-muted);
    cursor: not-allowed;
  }
  .mi.danger {
    color: var(--color-danger);
  }
  .sep {
    height: 1px;
    background: var(--color-border-soft);
    margin: var(--space-1) 0;
  }
  .hdr {
    padding: var(--space-2) var(--space-3) var(--space-1);
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
</style>
