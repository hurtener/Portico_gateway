<script lang="ts">
  // Table is intentionally untyped on row shape — it's a UI primitive that
  // works against any row-of-fields object. Slot consumers cast `row` to
  // their domain type themselves.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  type Row = any;

  type Column = {
    key: string;
    label: string;
    mono?: boolean;
    width?: string;
    align?: 'left' | 'right' | 'center';
    sortable?: boolean;
  };

  export let columns: Column[];
  export let rows: Row[] = [];
  export let onRowClick: ((row: Row) => void) | null = null;
  export let onSort: ((key: string, dir: 'asc' | 'desc') => void) | null = null;
  export let sortKey: string | null = null;
  export let sortDir: 'asc' | 'desc' = 'asc';
  export let empty: string = 'No items.';
  export let zebra = false;
  export let compact = false;
  /** When set, the row whose `rowKeyField` matches gets the selected state (Phase 10.6). */
  export let selectedKey: string | null = null;
  export let rowKeyField: string = 'id';

  function toggleSort(c: Column) {
    if (!c.sortable || !onSort) return;
    const dir: 'asc' | 'desc' = sortKey === c.key && sortDir === 'asc' ? 'desc' : 'asc';
    sortKey = c.key;
    sortDir = dir;
    onSort(c.key, dir);
  }
</script>

<div class="wrap">
  <table class:zebra class:compact>
    <thead>
      <tr>
        {#each columns as c (c.key)}
          <th
            class:sortable={c.sortable}
            style:text-align={c.align ?? 'left'}
            style:width={c.width ?? 'auto'}
            aria-sort={sortKey === c.key
              ? sortDir === 'asc'
                ? 'ascending'
                : 'descending'
              : 'none'}
          >
            {#if c.sortable}
              <button class="th-btn" type="button" on:click={() => toggleSort(c)}>
                {c.label}
                {#if sortKey === c.key}
                  <span class="caret">{sortDir === 'asc' ? '▲' : '▼'}</span>
                {/if}
              </button>
            {:else}
              {c.label}
            {/if}
          </th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#each rows as row, i (i)}
        <tr
          class:clickable={!!onRowClick}
          class:selected={selectedKey != null && row[rowKeyField] === selectedKey}
          on:click={() => onRowClick?.(row)}
          tabindex={onRowClick ? 0 : undefined}
          on:keydown={(e) => {
            if (onRowClick && (e.key === 'Enter' || e.key === ' ')) {
              e.preventDefault();
              onRowClick(row);
            }
          }}
        >
          {#each columns as c (c.key)}
            <td
              class:mono={c.mono}
              style:text-align={c.align ?? 'left'}
              style:width={c.width ?? 'auto'}
            >
              <slot name="cell" {row} column={c} value={row[c.key]}>
                {row[c.key] ?? '—'}
              </slot>
            </td>
          {/each}
        </tr>
      {:else}
        <tr>
          <td class="empty" colspan={columns.length}>
            <slot name="empty">{empty}</slot>
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .wrap {
    width: 100%;
    overflow-x: auto;
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    background: var(--color-bg-elevated);
  }
  table {
    width: 100%;
    border-collapse: separate;
    border-spacing: 0;
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
  }
  thead {
    background: var(--color-bg-subtle);
  }
  thead th {
    position: sticky;
    top: 0;
    background: var(--color-bg-subtle);
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-medium);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: var(--space-3) var(--space-4);
    border-bottom: 1px solid var(--color-border-soft);
    white-space: nowrap;
  }
  th.sortable {
    cursor: pointer;
  }
  .th-btn {
    appearance: none;
    background: transparent;
    border: none;
    color: inherit;
    font: inherit;
    text-transform: inherit;
    letter-spacing: inherit;
    cursor: pointer;
    padding: 0;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  .th-btn:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
    border-radius: var(--radius-xs);
  }
  .caret {
    font-size: 10px;
    color: var(--color-accent-primary);
  }
  tbody td {
    padding: var(--space-3) var(--space-4);
    border-bottom: 1px solid var(--color-border-soft);
    color: var(--color-text-primary);
    vertical-align: top;
  }
  tbody tr:last-child td {
    border-bottom: none;
  }
  tbody tr.clickable {
    cursor: pointer;
  }
  tbody tr.clickable:hover {
    background: var(--color-bg-subtle);
  }
  tbody tr.clickable:focus-visible {
    outline: none;
    background: var(--color-bg-subtle);
    box-shadow: inset 0 0 0 2px var(--color-accent-primary);
  }
  tbody tr.selected {
    background: var(--color-accent-primary-subtle);
  }
  tbody tr.selected td {
    border-bottom-color: var(--color-accent-primary-soft);
  }
  tbody tr.selected:hover {
    background: var(--color-accent-primary-subtle);
  }
  table.zebra tbody tr:nth-child(even) {
    background: var(--color-bg-subtle);
  }
  table.compact thead th,
  table.compact tbody td {
    padding: var(--space-2) var(--space-3);
  }
  td.mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
  }
  td.empty {
    color: var(--color-text-tertiary);
    text-align: center;
    padding: var(--space-12);
  }
</style>
