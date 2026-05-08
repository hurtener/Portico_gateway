<script lang="ts" context="module">
  /**
   * Phase 10.6 design vocabulary.
   *
   * Inspector is the 304px sticky right rail on list pages: header
   * (identity), tabs (Overview / Tools / etc), tab body (slotted by the
   * page). Holds selection in URL state via `?selected=<id>` so reload
   * preserves the inspector's content.
   *
   * Below 1280px the rail folds to a drawer (overlay sheet). Above, it
   * sits sticky below the topbar and the page renders inside a
   * `inspector-grid` two-column layout.
   *
   * Renders nothing when neither `open` nor any header/body slot is
   * provided — the consumer can drop the component in unconditionally
   * and it stays out of the way until something is selected.
   */
  export interface InspectorTab {
    id: string;
    label: string;
    disabled?: boolean;
  }
</script>

<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import Tabs from './Tabs.svelte';
  import IconX from 'lucide-svelte/icons/x';

  /** When false the rail is hidden (collapsed empty state) and a placeholder is shown. */
  export let open = false;
  /** Tab definitions; if omitted, the body slot renders on its own. */
  export let tabs: InspectorTab[] = [];
  /** The currently active tab id; bound by parent so the page owns URL state. */
  export let activeTab = '';
  /** Title above the tabs when no `header` slot is provided. */
  export let title = '';
  /** Empty-state copy shown when `open` is false. */
  export let emptyTitle = 'Nothing selected';
  export let emptyDescription = 'Select a row to inspect it here.';
  /** Aria label for the panel. */
  export let label = 'Inspector';

  const dispatch = createEventDispatcher<{ close: void; tabChange: string }>();

  function close() {
    open = false;
    dispatch('close');
  }
  function onTabChange(id: string) {
    activeTab = id;
    dispatch('tabChange', id);
  }
</script>

<aside class="inspector" class:open aria-label={label} data-region="inspector">
  {#if !open}
    <div class="empty">
      <h3 class="empty-title">{emptyTitle}</h3>
      <p class="empty-desc">{emptyDescription}</p>
    </div>
  {:else}
    <div class="head">
      <div class="head-content">
        {#if $$slots.header}
          <slot name="header" />
        {:else if title}
          <h3 class="title">{title}</h3>
        {/if}
      </div>
      <button
        type="button"
        class="close"
        on:click={close}
        aria-label="Close inspector"
        title="Close"
      >
        <IconX size={14} />
      </button>
    </div>
    {#if tabs.length > 0}
      <div class="tabs">
        <Tabs {tabs} active={activeTab} variant="underline" onChange={onTabChange} />
      </div>
    {/if}
    <div class="body">
      <slot />
    </div>
    {#if $$slots.actions}
      <div class="actions">
        <slot name="actions" />
      </div>
    {/if}
  {/if}
</aside>

<style>
  .inspector {
    width: 304px;
    flex-shrink: 0;
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-sm);
    /* Sticky below the topbar. The 12px breathing room above prevents
     * the inspector from grafting directly onto the topbar's bottom border. */
    position: sticky;
    top: calc(var(--layout-topbar-height) + var(--space-3));
    max-height: calc(100vh - var(--layout-topbar-height) - var(--space-6));
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .empty {
    padding: var(--space-6);
    text-align: center;
    color: var(--color-text-tertiary);
    font-family: var(--font-sans);
  }
  .empty-title {
    margin: 0 0 var(--space-2);
    font-size: var(--font-size-body-md);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
  }
  .empty-desc {
    margin: 0;
    font-size: var(--font-size-body-sm);
    line-height: 1.5;
  }

  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-4);
    border-bottom: 1px solid var(--color-border-soft);
  }
  .head-content {
    flex: 1;
    min-width: 0;
  }
  .title {
    margin: 0;
    font-family: var(--font-sans);
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .close {
    appearance: none;
    background: transparent;
    border: 1px solid transparent;
    color: var(--color-icon-default);
    cursor: pointer;
    width: 28px;
    height: 28px;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default);
  }
  .close:hover {
    background: var(--color-bg-subtle);
    color: var(--color-text-primary);
  }
  .close:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }

  .tabs {
    padding: 0 var(--space-4);
    border-bottom: 1px solid var(--color-border-soft);
  }
  .body {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-4);
    /* Stack the slotted cards. Pages compose KeyValueGrid + cards into
     * the body — gap stays consistent across consumers. */
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .actions {
    border-top: 1px solid var(--color-border-soft);
    padding: var(--space-3) var(--space-4);
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
  }

  /* Below 1280px the inspector turns into a drawer. The list page is
   * responsible for wrapping the inspector in a Drawer when its grid
   * collapses; this stylesheet just makes the standalone inspector
   * shrink gracefully if used outside the grid. */
  @media (max-width: 1279px) {
    .inspector {
      width: 100%;
      position: static;
      max-height: none;
    }
  }
</style>
