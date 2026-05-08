<script lang="ts">
  import IconCopy from 'lucide-svelte/icons/copy';
  import IconCheck from 'lucide-svelte/icons/check';

  /**
   * IdBadge renders a machine id (snap_*, psn_*, ULID, hash) as a
   * compact, copyable badge. The full id stays available as a tooltip
   * + click-to-copy; the visible label is a 6-char short prefix so
   * the operator sees something stable but isn't asked to memorise it.
   *
   * Use this instead of raw `<code>{id}</code>` anywhere the id is
   * shown for reference (lists, detail headers, audit columns) rather
   * than as a primary identifier.
   */
  export let value: string;
  // Optional human-friendly label that takes precedence visually.
  export let label: string | undefined = undefined;
  // How many chars to show after the optional prefix (snap_, psn_, etc.).
  export let chars = 6;
  // Visual size — defaults to small to fit table cells.
  export let size: 'sm' | 'md' = 'sm';
  // Render hint after the badge (e.g. "snapshot id", "session id").
  export let hint: string | undefined = undefined;

  let copied = false;

  $: short = shortenId(value, chars);

  function shortenId(id: string, n: number): string {
    if (!id) return '';
    // Preserve the type prefix (snap_, psn_, call_) so the operator
    // still sees what kind of id it is.
    const m = id.match(/^([a-z]+_)(.+)$/);
    if (m) {
      return m[1] + m[2].slice(0, n) + '…';
    }
    if (id.length > n + 3) {
      return id.slice(0, n) + '…';
    }
    return id;
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      copied = true;
      setTimeout(() => (copied = false), 1200);
    } catch {
      /* clipboard may be unavailable in non-secure contexts; degrade silently */
    }
  }
</script>

<span class="id-badge {size}" title={value} aria-label={label ? `${label} (${value})` : value}>
  {#if label}
    <span class="label">{label}</span>
  {/if}
  <button
    type="button"
    class="chip"
    on:click={copy}
    aria-label={copied ? 'Copied' : `Copy ${value}`}
  >
    <code>{short}</code>
    {#if copied}
      <IconCheck size={10} />
    {:else}
      <IconCopy size={10} />
    {/if}
  </button>
  {#if hint}
    <span class="hint">{hint}</span>
  {/if}
</span>

<style>
  .id-badge {
    display: inline-flex;
    align-items: baseline;
    gap: var(--space-2);
    font-size: var(--font-size-label);
  }
  .id-badge.md {
    font-size: var(--font-size-body-sm);
  }
  .label {
    color: var(--color-text-primary);
    font-weight: var(--font-weight-medium);
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: var(--color-bg-default);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: 1px 6px;
    color: var(--color-text-secondary);
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
  }
  .chip:hover {
    background: var(--color-bg-elevated);
    color: var(--color-text-primary);
  }
  .chip code {
    font-family: inherit;
    font-size: inherit;
  }
  .hint {
    color: var(--color-text-tertiary);
    font-style: italic;
  }
</style>
