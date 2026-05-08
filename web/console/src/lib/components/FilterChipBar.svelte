<script lang="ts" context="module">
  /**
   * Phase 10.6 design vocabulary.
   *
   * FilterChipBar is the horizontal control row directly above a list
   * table: text search on the left, mutually-exclusive chips
   * ("All / Online / Offline / Needs review"), and grouped facet
   * dropdowns ("Transport: Any / stdio / http"). Replaces the bare
   * `<input>` we shipped on /servers + /skills.
   *
   * Search input is debounced (250ms by default). Chips fire immediately.
   * Dropdowns fire on selection. Parents own the filter state; this
   * component is a controlled view + event source.
   */

  export interface FilterChip {
    id: string;
    label: string;
    /** Optional small badge after the label, e.g. "All 12". */
    count?: number | string;
  }

  export interface DropdownOption {
    /** Stored value. Empty string is the "any" option. */
    value: string;
    label: string;
  }

  export interface FilterDropdown {
    id: string;
    label: string;
    /** First option should usually be the "Any" reset. */
    options: DropdownOption[];
    value: string;
  }
</script>

<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import IconSearch from 'lucide-svelte/icons/search';

  export let searchValue = '';
  export let searchPlaceholder = 'Search…';
  /** Hide the search input entirely (some filter bars only need chips). */
  export let showSearch = true;
  /** Debounce window for search updates in ms. */
  export let debounceMs = 250;
  export let chips: FilterChip[] = [];
  /** Currently active chip id. Empty string = none active. */
  export let activeChip = '';
  export let dropdowns: FilterDropdown[] = [];

  const dispatch = createEventDispatcher<{
    searchChange: string;
    chipChange: string;
    dropdownChange: { id: string; value: string };
  }>();

  let debounceHandle: ReturnType<typeof setTimeout> | null = null;
  let pendingValue = searchValue;
  // Keep the rendered input synced when the parent sets searchValue
  // imperatively (e.g. clearFilters).
  $: pendingValue = searchValue;

  function onSearchInput(e: Event) {
    const v = (e.target as HTMLInputElement).value;
    pendingValue = v;
    if (debounceHandle !== null) clearTimeout(debounceHandle);
    debounceHandle = setTimeout(() => {
      searchValue = v;
      dispatch('searchChange', v);
    }, debounceMs);
  }
  function onSearchSubmit() {
    if (debounceHandle !== null) clearTimeout(debounceHandle);
    searchValue = pendingValue;
    dispatch('searchChange', pendingValue);
  }

  function pickChip(id: string) {
    activeChip = id;
    dispatch('chipChange', id);
  }
  function pickDropdown(d: FilterDropdown, e: Event) {
    const v = (e.target as HTMLSelectElement).value;
    d.value = v;
    dispatch('dropdownChange', { id: d.id, value: v });
  }
</script>

<div class="bar" role="search" data-region="filters">
  {#if showSearch}
    <form class="search" on:submit|preventDefault={onSearchSubmit}>
      <span class="search-icon" aria-hidden="true"><IconSearch size={14} /></span>
      <input
        type="text"
        class="search-input"
        placeholder={searchPlaceholder}
        value={pendingValue}
        on:input={onSearchInput}
        aria-label="Search"
      />
    </form>
  {/if}
  {#if chips.length > 0}
    <div class="chips" role="group" aria-label="Filter">
      {#each chips as c (c.id)}
        <button
          type="button"
          class="chip"
          class:active={activeChip === c.id}
          aria-pressed={activeChip === c.id}
          on:click={() => pickChip(c.id)}
        >
          <span class="chip-label">{c.label}</span>
          {#if c.count !== undefined}
            <span class="chip-count">{c.count}</span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}
  {#if dropdowns.length > 0}
    <div class="dropdowns">
      {#each dropdowns as d (d.id)}
        <label class="dropdown">
          <span class="dropdown-label">{d.label}</span>
          <select value={d.value} on:change={(e) => pickDropdown(d, e)}>
            {#each d.options as opt (opt.value)}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        </label>
      {/each}
    </div>
  {/if}
  {#if $$slots.trailing}
    <div class="trailing"><slot name="trailing" /></div>
  {/if}
</div>

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    margin-bottom: var(--space-3);
  }

  .search {
    position: relative;
    display: inline-flex;
    align-items: center;
    width: 220px;
    flex-shrink: 0;
  }
  .search-icon {
    position: absolute;
    left: var(--space-3);
    color: var(--color-icon-subtle);
    pointer-events: none;
    display: inline-flex;
  }
  .search-input {
    width: 100%;
    height: 34px;
    padding: 0 var(--space-3) 0 calc(var(--space-3) + 18px);
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border-soft);
    background: var(--color-bg-elevated);
    color: var(--color-text-primary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    transition:
      border-color var(--motion-fast) var(--ease-default),
      box-shadow var(--motion-fast) var(--ease-default);
  }
  .search-input::placeholder {
    color: var(--color-text-tertiary);
  }
  .search-input:hover {
    border-color: var(--color-border-default);
  }
  .search-input:focus {
    outline: none;
    border-color: var(--color-accent-primary);
    box-shadow: var(--ring-focus);
  }

  .chips {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-wrap: wrap;
  }
  .chip {
    appearance: none;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: 34px;
    padding: 0 var(--space-3);
    border-radius: var(--radius-sm);
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    color: var(--color-text-secondary);
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-medium);
    cursor: pointer;
    transition:
      background var(--motion-fast) var(--ease-default),
      color var(--motion-fast) var(--ease-default),
      border-color var(--motion-fast) var(--ease-default);
  }
  .chip:hover {
    border-color: var(--color-border-default);
    color: var(--color-text-primary);
  }
  .chip:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .chip.active {
    background: var(--color-accent-primary-subtle);
    border-color: var(--color-accent-primary);
    color: var(--color-accent-primary);
  }
  .chip-count {
    color: var(--color-text-tertiary);
    font-variant-numeric: tabular-nums;
    font-size: 11px;
    padding: 1px var(--space-2);
    border-radius: var(--radius-pill);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
  }
  .chip.active .chip-count {
    color: var(--color-accent-primary);
    background: var(--color-bg-elevated);
    border-color: var(--color-accent-primary-subtle);
  }

  .dropdowns {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .dropdown {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: 34px;
    padding: 0 var(--space-2) 0 var(--space-3);
    border-radius: var(--radius-sm);
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    color: var(--color-text-secondary);
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    transition:
      border-color var(--motion-fast) var(--ease-default);
    cursor: pointer;
  }
  .dropdown:hover {
    border-color: var(--color-border-default);
  }
  .dropdown:focus-within {
    border-color: var(--color-accent-primary);
    box-shadow: var(--ring-focus);
  }
  .dropdown-label {
    color: var(--color-text-tertiary);
    font-weight: var(--font-weight-medium);
  }
  .dropdown select {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--color-text-primary);
    font-family: inherit;
    font-size: inherit;
    font-weight: var(--font-weight-medium);
    padding-right: var(--space-3);
    cursor: pointer;
  }
  .dropdown select:focus {
    outline: none;
  }
  /* Custom chevron via gradient */
  .dropdown {
    background-image: linear-gradient(
        45deg,
        transparent 50%,
        var(--color-icon-subtle) 50%
      ),
      linear-gradient(135deg, var(--color-icon-subtle) 50%, transparent 50%);
    background-position:
      calc(100% - 12px) center,
      calc(100% - 8px) center;
    background-size:
      4px 4px,
      4px 4px;
    background-repeat: no-repeat;
    padding-right: var(--space-5);
  }

  .trailing {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }
</style>
