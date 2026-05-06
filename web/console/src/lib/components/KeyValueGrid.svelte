<script lang="ts">
  type Item = { label: string; value: string | null | undefined; mono?: boolean; full?: boolean };
  export let items: Item[] = [];
  export let columns: 1 | 2 | 3 = 2;
</script>

<dl class="kv c{columns}">
  {#each items as it, i (i)}
    <div class="row" class:full={it.full}>
      <dt>{it.label}</dt>
      <dd class:mono={it.mono}>
        {#if it.value === null || it.value === undefined || it.value === ''}
          <span class="muted">—</span>
        {:else}
          {it.value}
        {/if}
      </dd>
    </div>
  {/each}
</dl>

<style>
  .kv {
    display: grid;
    gap: var(--space-3) var(--space-6);
    margin: 0;
  }
  .c1 {
    grid-template-columns: 1fr;
  }
  .c2 {
    grid-template-columns: 1fr 1fr;
  }
  .c3 {
    grid-template-columns: repeat(3, 1fr);
  }
  .row {
    min-width: 0;
  }
  .row.full {
    grid-column: 1 / -1;
  }
  dt {
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    margin-bottom: var(--space-1);
  }
  dd {
    margin: 0;
    color: var(--color-text-primary);
    font-size: var(--font-size-body-sm);
    word-break: break-word;
  }
  dd.mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-secondary);
  }
  .muted {
    color: var(--color-text-tertiary);
  }
</style>
