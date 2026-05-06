<script lang="ts">
  type Tab = { id: string; label: string; disabled?: boolean };
  export let tabs: Tab[] = [];
  export let active: string;
  export let onChange: ((id: string) => void) | null = null;
  export let variant: 'underline' | 'pill' = 'underline';

  function pick(t: Tab) {
    if (t.disabled) return;
    active = t.id;
    onChange?.(t.id);
  }

  function key(e: KeyboardEvent, idx: number) {
    let next = idx;
    if (e.key === 'ArrowRight') next = (idx + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') next = (idx - 1 + tabs.length) % tabs.length;
    else return;
    e.preventDefault();
    while (tabs[next]?.disabled && next !== idx) next = (next + 1) % tabs.length;
    pick(tabs[next]);
    const el = document.getElementById(`tab-${tabs[next].id}`);
    el?.focus();
  }
</script>

<div class="tabs {variant}" role="tablist">
  {#each tabs as t, i (t.id)}
    <button
      type="button"
      role="tab"
      id="tab-{t.id}"
      class="tab"
      class:active={active === t.id}
      aria-selected={active === t.id}
      aria-disabled={t.disabled || undefined}
      tabindex={active === t.id ? 0 : -1}
      disabled={t.disabled}
      on:click={() => pick(t)}
      on:keydown={(e) => key(e, i)}
    >
      {t.label}
    </button>
  {/each}
</div>

<style>
  .tabs {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  .tabs.underline {
    border-bottom: 1px solid var(--color-border-soft);
    width: 100%;
    gap: var(--space-2);
  }
  .tabs.pill {
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-pill);
    padding: 2px;
  }
  .tab {
    appearance: none;
    background: transparent;
    border: none;
    cursor: pointer;
    color: var(--color-text-secondary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    font-weight: var(--font-weight-medium);
    padding: var(--space-2) var(--space-3);
    transition:
      color var(--motion-fast) var(--ease-default),
      background var(--motion-fast) var(--ease-default),
      border-color var(--motion-fast) var(--ease-default);
  }
  .tab:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
    border-radius: var(--radius-sm);
  }
  .tab[disabled] {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .underline .tab {
    padding: var(--space-3) var(--space-3);
    margin-bottom: -1px;
    border-bottom: 2px solid transparent;
  }
  .underline .tab:hover {
    color: var(--color-text-primary);
  }
  .underline .tab.active {
    color: var(--color-accent-primary);
    border-bottom-color: var(--color-accent-primary);
  }

  .pill .tab {
    border-radius: var(--radius-pill);
    padding: 6px var(--space-3);
  }
  .pill .tab:hover {
    color: var(--color-text-primary);
  }
  .pill .tab.active {
    background: var(--color-bg-elevated);
    color: var(--color-accent-primary);
    box-shadow: var(--shadow-sm);
  }
</style>
