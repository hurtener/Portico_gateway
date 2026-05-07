<script lang="ts">
  import IconChevronRight from 'lucide-svelte/icons/chevron-right';

  type Crumb = { label: string; href?: string };
  export let items: Crumb[] = [];
</script>

<nav class="bc" aria-label="Breadcrumb">
  <ol>
    {#each items as it, i (i)}
      <li>
        {#if it.href && i < items.length - 1}
          <a href={it.href}>{it.label}</a>
        {:else}
          <span aria-current={i === items.length - 1 ? 'page' : undefined}>{it.label}</span>
        {/if}
        {#if i < items.length - 1}
          <span class="sep" aria-hidden="true"><IconChevronRight size={14} /></span>
        {/if}
      </li>
    {/each}
  </ol>
</nav>

<style>
  .bc {
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
  }
  ol {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
  }
  li {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  a {
    color: var(--color-text-secondary);
    text-decoration: none;
  }
  a:hover {
    color: var(--color-text-primary);
  }
  span[aria-current='page'] {
    color: var(--color-text-primary);
    font-weight: var(--font-weight-medium);
  }
  .sep {
    color: var(--color-icon-subtle);
    display: inline-flex;
  }
</style>
