<script lang="ts">
  import IconCopy from 'lucide-svelte/icons/copy';
  import IconCheck from 'lucide-svelte/icons/check';

  export let code: string = '';
  export let language: string | undefined = undefined;
  export let filename: string | undefined = undefined;
  export let maxHeight: string | undefined = undefined;
  export let wrap = false;

  let copied = false;
  let timer: ReturnType<typeof setTimeout> | null = null;

  async function copy() {
    try {
      await navigator.clipboard.writeText(code);
      copied = true;
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => (copied = false), 1600);
    } catch {
      // ignore — clipboard may be blocked in some contexts
    }
  }
</script>

<div class="cb">
  {#if filename || language}
    <header class="head">
      <span class="meta">
        {#if filename}<span class="fn">{filename}</span>{/if}
        {#if language}<span class="lang">{language}</span>{/if}
      </span>
      <button type="button" class="copy" aria-label="Copy" on:click={copy}>
        {#if copied}<IconCheck size={14} /><span>Copied</span>{:else}<IconCopy size={14} /><span
            >Copy</span
          >{/if}
      </button>
    </header>
  {/if}
  <pre class:wrap style:max-height={maxHeight}><code>{code}</code></pre>
  {#if !filename && !language}
    <button type="button" class="copy floating" aria-label="Copy" on:click={copy}>
      {#if copied}<IconCheck size={14} />{:else}<IconCopy size={14} />{/if}
    </button>
  {/if}
</div>

<style>
  .cb {
    position: relative;
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    background: var(--color-bg-subtle);
    overflow: hidden;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-2) var(--space-3);
    border-bottom: 1px solid var(--color-border-soft);
    background: var(--color-bg-elevated);
  }
  .meta {
    display: flex;
    gap: var(--space-2);
    align-items: center;
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .fn {
    font-family: var(--font-mono);
    color: var(--color-text-secondary);
  }
  .lang {
    font-family: var(--font-sans);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  pre {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    line-height: var(--font-line-mono-sm);
    color: var(--color-text-primary);
    overflow: auto;
  }
  pre.wrap {
    white-space: pre-wrap;
    word-break: break-word;
  }
  code {
    background: transparent;
    color: inherit;
  }
  .copy {
    appearance: none;
    background: transparent;
    border: 1px solid var(--color-border-default);
    color: var(--color-text-secondary);
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    padding: 4px var(--space-2);
    border-radius: var(--radius-xs);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  .copy:hover {
    color: var(--color-text-primary);
    background: var(--color-bg-subtle);
  }
  .copy:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .copy.floating {
    position: absolute;
    top: var(--space-2);
    right: var(--space-2);
    background: var(--color-bg-elevated);
  }
</style>
