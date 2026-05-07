<script lang="ts">
  export let title: string;
  export let description: string | undefined = undefined;
  export let eyebrow: string | undefined = undefined;
</script>

<header class="ph">
  <div class="left">
    {#if $$slots.breadcrumbs}
      <div class="bc"><slot name="breadcrumbs" /></div>
    {/if}
    {#if eyebrow}<div class="eyebrow">{eyebrow}</div>{/if}
    <h1 class="title">{title}</h1>
    {#if description}<p class="desc">{description}</p>{/if}
    {#if $$slots.meta}
      <div class="meta"><slot name="meta" /></div>
    {/if}
  </div>
  {#if $$slots.actions}
    <div class="actions"><slot name="actions" /></div>
  {/if}
</header>

<style>
  .ph {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-6);
    padding-bottom: var(--space-6);
    margin-bottom: var(--space-6);
    border-bottom: 1px solid var(--color-border-soft);
  }
  .left {
    flex: 1;
    min-width: 0;
  }
  .bc {
    margin-bottom: var(--space-2);
  }
  .eyebrow {
    font-size: var(--font-size-label);
    color: var(--color-accent-primary);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    font-weight: var(--font-weight-medium);
    margin-bottom: var(--space-1);
  }
  .title {
    font-family: var(--font-sans);
    font-size: var(--font-size-heading-2);
    line-height: var(--font-line-heading-2);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    margin: 0;
    letter-spacing: -0.01em;
  }
  .desc {
    margin: var(--space-2) 0 0 0;
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-md);
    max-width: 64ch;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    align-items: center;
    margin-top: var(--space-3);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
    flex-shrink: 0;
    align-items: center;
  }
</style>
