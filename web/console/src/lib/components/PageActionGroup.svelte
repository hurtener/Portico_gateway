<script lang="ts" context="module">
  /**
   * Phase 10.6 design vocabulary.
   *
   * PageActionGroup is the right-aligned action cluster on a page header:
   * primary action ("Add server"), secondary actions ("Refresh
   * catalog"), and optional split-button dropdowns ("Refresh / Refresh
   * selected"). Replaces the ad-hoc Button rows scattered across list
   * pages so the spacing, ordering, and split-button semantics are
   * consistent.
   *
   * The component takes a flat array of actions; the consumer never
   * touches dropdown state. Actions render right-to-left in the order
   * provided (last item is the rightmost button — typically the primary
   * CTA).
   */
  import type { ComponentType, SvelteComponent } from 'svelte';

  export type ActionVariant = 'primary' | 'secondary' | 'ghost' | 'destructive';

  export interface DropdownChoice {
    label: string;
    onSelect: () => void;
    danger?: boolean;
    disabled?: boolean;
  }

  export interface PageAction {
    /** The visible label on the button half. */
    label: string;
    variant?: ActionVariant;
    /** Lucide icon component (or any Svelte component that takes a `size` prop). */
    icon?: ComponentType<SvelteComponent>;
    /** Click handler for the button half. Mutually exclusive with `href`. */
    onClick?: () => void;
    /** When provided, the button half renders as an `<a>` link. */
    href?: string;
    /** When provided, the action becomes a split-button (button + chevron). */
    dropdown?: DropdownChoice[];
    disabled?: boolean;
    loading?: boolean;
    /** ARIA label override for icon-only buttons. */
    ariaLabel?: string;
  }
</script>

<script lang="ts">
  import Button from './Button.svelte';
  import Dropdown from './Dropdown.svelte';
  import IconChevronDown from 'lucide-svelte/icons/chevron-down';

  export let actions: PageAction[] = [];

  // Pre-map dropdown choices to the Dropdown's tagged-union shape. Doing
  // this in the script (instead of inline in the template) keeps the
  // Svelte parser happy — it doesn't accept `as const` casts inside
  // template expressions.
  function asMenuItems(choices: DropdownChoice[]) {
    return choices.map((d) => ({
      kind: 'item' as const,
      label: d.label,
      onSelect: d.onSelect,
      danger: d.danger,
      disabled: d.disabled
    }));
  }
</script>

<div class="group" role="toolbar" aria-label="Page actions">
  {#each actions as a, i (i)}
    {#if a.dropdown && a.dropdown.length > 0}
      <span class="split" data-variant={a.variant ?? 'secondary'}>
        <Button
          variant={a.variant ?? 'secondary'}
          disabled={a.disabled}
          loading={a.loading}
          href={a.href ?? null}
          on:click={() => a.onClick?.()}
          ariaLabel={a.ariaLabel}
        >
          {#if a.icon}
            <svelte:component this={a.icon} slot="leading" size={14} />
          {/if}
          {a.label}
        </Button>
        <Dropdown items={asMenuItems(a.dropdown)} placement="bottom-end">
          <button
            slot="trigger"
            let:toggle
            type="button"
            class="chevron"
            data-variant={a.variant ?? 'secondary'}
            aria-label={`${a.label} options`}
            on:click={toggle}
          >
            <IconChevronDown size={14} />
          </button>
        </Dropdown>
      </span>
    {:else}
      <Button
        variant={a.variant ?? 'secondary'}
        disabled={a.disabled}
        loading={a.loading}
        href={a.href ?? null}
        on:click={() => a.onClick?.()}
        ariaLabel={a.ariaLabel}
      >
        {#if a.icon}
          <svelte:component this={a.icon} slot="leading" size={14} />
        {/if}
        {a.label}
      </Button>
    {/if}
  {/each}
</div>

<style>
  .group {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
  }
  .split {
    display: inline-flex;
    align-items: stretch;
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  /* Round only the outer corners so the button + chevron read as one unit. */
  .split :global(.btn) {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
  }
  .chevron {
    appearance: none;
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-default);
    border-left: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    padding: 0 var(--space-2);
    border-top-right-radius: var(--radius-md);
    border-bottom-right-radius: var(--radius-md);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default),
      border-color var(--motion-fast) var(--ease-default);
  }
  .chevron:hover {
    background: var(--color-bg-subtle);
    color: var(--color-text-primary);
    border-color: var(--color-border-strong);
  }
  .chevron:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  /* Primary split — chevron picks up the accent treatment. */
  .split[data-variant='primary'] .chevron {
    background: var(--color-accent-primary);
    color: var(--color-text-inverse);
    border-color: var(--color-accent-primary-active);
  }
  .split[data-variant='primary'] .chevron:hover {
    background: var(--color-accent-primary-hover);
  }
  .split[data-variant='destructive'] .chevron {
    background: transparent;
    color: var(--color-danger);
    border-color: var(--color-danger);
  }
  .split[data-variant='destructive'] .chevron:hover {
    background: var(--color-danger-soft);
  }
</style>
