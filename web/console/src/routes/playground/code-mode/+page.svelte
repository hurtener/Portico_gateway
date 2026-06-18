<script lang="ts">
  /**
   * Code Mode playground (Phase 13.5). The interactive surface: browse the
   * virtual .pyi stub file system, read a stub, write a Starlark snippet, and
   * run it through the hardened sandbox against a synthetic Console code-mode
   * session. Reads GET /api/code-mode/files + /files/read; runs POST
   * /api/code-mode/run. (Editor is a plain textarea for V1 — a Monaco upgrade is
   * a follow-up; the value is the run loop, not syntax highlighting.)
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable, HTTPError } from '$lib/api';
  import type { CodeModeRunResult } from '$lib/api';
  import { Badge, Button, EmptyState, PageHeader, Skeleton, toast } from '$lib/components';

  let loading = true;
  let unavailable = false;

  let files: string[] = [];
  let openFile = '';
  let openContent = '';

  let code = `# Write Starlark. Assign the final value to \`result\`.\n# Call tools via their server module, e.g. github.list_issues(repo="owner/repo").\nresult = 1 + 1`;
  let running = false;
  let result: CodeModeRunResult | null = null;
  let runError = '';

  onMount(async () => {
    try {
      const res = await api.listCodeModeFiles();
      files = (res.files ?? []).slice().sort();
    } catch (e) {
      if (isFeatureUnavailable(e)) unavailable = true;
    }
    loading = false;
  });

  async function openStub(path: string) {
    openFile = path;
    openContent = '';
    try {
      const res = await api.readCodeModeFile(path);
      openContent = res.content;
    } catch (e) {
      openContent = e instanceof Error ? `# error: ${e.message}` : '# error';
    }
  }

  function insertStub() {
    if (!openContent) return;
    code = `${openContent.trimEnd()}\n\n${code}`;
    toast.success(`Inserted ${openFile}`);
  }

  async function run() {
    running = true;
    runError = '';
    result = null;
    try {
      result = await api.runCodeMode(code);
    } catch (e) {
      if (e instanceof HTTPError) {
        runError = e.detail ?? e.message;
      } else {
        runError = e instanceof Error ? e.message : 'Run failed';
      }
    } finally {
      running = false;
    }
  }

  function resultJSON(r: CodeModeRunResult): string {
    return JSON.stringify(r.result ?? null, null, 2);
  }
</script>

<PageHeader
  title="Code Mode Playground"
  description="Browse the tool stubs, write a Starlark snippet, and run it in the sandbox."
  compact
/>

{#if unavailable}
  <EmptyState
    title="Code Mode not available"
    description="The Code Mode runtime is not wired in this build."
  />
{:else if loading}
  <Skeleton height="320px" />
{:else}
  <div class="layout">
    <aside class="tree" aria-label="Tool stub files">
      <h3 class="panel-title">Tool files</h3>
      {#if files.length === 0}
        <p class="muted">No stub files in the current snapshot.</p>
      {:else}
        <ul>
          {#each files as f (f)}
            <li>
              <button
                class="file"
                class:active={f === openFile}
                on:click={() => openStub(f)}
                type="button"
              >
                {f}
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      {#if openFile}
        <div class="stub">
          <div class="stub-head">
            <Badge tone="neutral" mono>{openFile}</Badge>
            <Button variant="ghost" size="sm" on:click={insertStub}>Insert</Button>
          </div>
          <pre class="code-block">{openContent}</pre>
        </div>
      {/if}
    </aside>

    <section class="editor">
      <div class="editor-head">
        <h3 class="panel-title">Snippet</h3>
        <Button variant="primary" size="sm" on:click={run} disabled={running}>
          {running ? 'Running…' : 'Run'}
        </Button>
      </div>
      <textarea
        class="code-input"
        bind:value={code}
        spellcheck="false"
        aria-label="Starlark snippet"
      ></textarea>

      {#if runError}
        <div class="run-error">
          <Badge tone="danger">{runError}</Badge>
        </div>
      {/if}

      {#if result}
        {#if result.status === 'approval_required'}
          <div class="run-pane">
            <h4 class="panel-title">Approval required</h4>
            <p class="muted">
              A tool call needs operator approval. Approve it, then resume with the continuation
              token.
            </p>
            <pre
              class="code-block">{`approval_id: ${result.approval_id}\ncontinuation_token: ${result.continuation_token}`}</pre>
          </div>
        {:else}
          <div class="run-pane">
            <div class="run-stats">
              <Badge tone="success">{result.tokens_saved_est ?? 0} tokens saved</Badge>
              <Badge tone="neutral">{result.tool_calls ?? 0} tool calls</Badge>
              <Badge tone="neutral">{result.duration_ms ?? 0} ms</Badge>
            </div>
            <h4 class="panel-title">Result</h4>
            <pre class="code-block">{resultJSON(result)}</pre>
            {#if result.output}
              <h4 class="panel-title">Output{result.output_truncated ? ' (truncated)' : ''}</h4>
              <pre class="code-block">{result.output}</pre>
            {/if}
          </div>
        {/if}
      {/if}
    </section>
  </div>
{/if}

<style>
  .layout {
    display: grid;
    grid-template-columns: minmax(220px, 320px) 1fr;
    gap: var(--space-5);
    align-items: start;
  }
  .panel-title {
    margin: 0 0 var(--space-3);
    font-size: var(--font-size-body-lg);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
  }
  .tree ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .file {
    width: 100%;
    text-align: left;
    font-family: var(--font-mono);
    font-size: var(--font-size-sm);
    color: var(--color-text-secondary);
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    padding: var(--space-1) var(--space-2);
    cursor: pointer;
  }
  .file:hover {
    background: var(--color-surface-raised);
    color: var(--color-text-primary);
  }
  .file.active {
    background: var(--color-surface-raised);
    border-color: var(--color-border);
    color: var(--color-text-primary);
  }
  .stub {
    margin-top: var(--space-4);
  }
  .stub-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-2);
  }
  .editor-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-2);
  }
  .code-input {
    width: 100%;
    min-height: 240px;
    font-family: var(--font-mono);
    font-size: var(--font-size-sm);
    line-height: var(--line-height-relaxed, 1.6);
    color: var(--color-text-primary);
    background: var(--color-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    padding: var(--space-3);
    resize: vertical;
  }
  .code-block {
    font-family: var(--font-mono);
    font-size: var(--font-size-sm);
    color: var(--color-text-secondary);
    background: var(--color-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    padding: var(--space-3);
    margin: 0 0 var(--space-3);
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .run-pane {
    margin-top: var(--space-4);
  }
  .run-stats {
    display: flex;
    gap: var(--space-2);
    margin-bottom: var(--space-3);
  }
  .run-error {
    margin: var(--space-3) 0;
  }
  .muted {
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
  }
</style>
