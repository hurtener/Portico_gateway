<script lang="ts">
  /**
   * Code Mode observability (Phase 13.5). The ROI surface: how many
   * executeToolCode runs happened, how many tokens Code Mode saved versus
   * shipping the full catalog, and the recent execution history. Reads
   * GET /api/code-mode/savings (rollup) and /api/code-mode/executions (list).
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable } from '$lib/api';
  import type { CodeModeExecution, CodeModeSavings } from '$lib/api';
  import {
    Badge,
    EmptyState,
    MetricStrip,
    PageHeader,
    SegmentedControl,
    Skeleton,
    Table
  } from '$lib/components';
  import type { Metric } from '$lib/components/MetricStrip.svelte';

  let loading = true;
  let unavailable = false;
  let error = '';

  let savings: CodeModeSavings = {
    executions: 0,
    tool_calls: 0,
    tokens_saved_est: 0,
    by_status: {}
  };
  let executions: CodeModeExecution[] = [];

  let range: '7d' | '30d' | 'all' = '30d';
  const rangeOptions = [
    { value: '7d', label: 'Last 7 days' },
    { value: '30d', label: 'Last 30 days' },
    { value: 'all', label: 'All time' }
  ];

  const columns = [
    { key: 'started_at', label: 'Started' },
    { key: 'session_id', label: 'Session' },
    { key: 'status', label: 'Status' },
    { key: 'tool_calls', label: 'Tool calls', align: 'right' as const },
    { key: 'tokens_saved_est', label: 'Tokens saved', align: 'right' as const },
    { key: 'snippet_sha', label: 'Snippet' }
  ];

  function sinceParam(): string | undefined {
    if (range === 'all') return undefined;
    const d = new Date();
    d.setUTCDate(d.getUTCDate() - (range === '7d' ? 6 : 29));
    return d.toISOString();
  }

  function setRange(v: string) {
    range = v as '7d' | '30d' | 'all';
    void load();
  }

  onMount(async () => {
    await load();
    loading = false;
  });

  async function load() {
    error = '';
    try {
      const [s, e] = await Promise.all([
        api.getCodeModeSavings(sinceParam()),
        api.listCodeModeExecutions(undefined, 100)
      ]);
      savings = s;
      executions = e ?? [];
      unavailable = false;
    } catch (err) {
      if (isFeatureUnavailable(err)) {
        unavailable = true;
      } else {
        error = err instanceof Error ? err.message : 'Failed to load Code Mode activity';
      }
    }
  }

  function fmtInt(n: number): string {
    return n.toLocaleString();
  }
  function statusTone(s: string): 'success' | 'danger' | 'warning' | 'neutral' {
    if (s === 'completed') return 'success';
    if (s === 'failed') return 'danger';
    if (s === 'awaiting_approval') return 'warning';
    return 'neutral';
  }

  $: metrics = [
    {
      id: 'saved',
      label: 'Tokens saved (est)',
      value: fmtInt(savings.tokens_saved_est),
      tone: 'brand'
    },
    { id: 'exec', label: 'Executions', value: fmtInt(savings.executions) },
    { id: 'calls', label: 'Tool calls', value: fmtInt(savings.tool_calls) },
    {
      id: 'done',
      label: 'Completed',
      value: fmtInt(savings.by_status?.completed ?? 0)
    }
  ] satisfies Metric[];

  $: statusRows = Object.entries(savings.by_status ?? {}).map(([status, count]) => ({
    status,
    count
  }));
</script>

<PageHeader
  title="Code Mode"
  description="Token savings and execution history for the sandboxed code-driven tool surface."
  compact
/>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="Code Mode not configured"
    description="The Code Mode execution store is not wired in this build."
  />
{:else if loading}
  <div class="stack">
    <Skeleton height="96px" />
    <Skeleton height="240px" />
  </div>
{:else}
  <div class="stack">
    <div class="toolbar">
      <SegmentedControl
        options={rangeOptions}
        value={range}
        ariaLabel="Code Mode date range"
        onChange={setRange}
      />
    </div>

    <MetricStrip {metrics} label="Code Mode savings summary" />

    {#if statusRows.length > 0}
      <section>
        <h3 class="section-title">Executions by status</h3>
        <div class="chips">
          {#each statusRows as s (s.status)}
            <Badge tone={statusTone(s.status)}>{s.status}: {fmtInt(s.count)}</Badge>
          {/each}
        </div>
      </section>
    {/if}

    <section>
      <h3 class="section-title">Recent executions</h3>
      {#if executions.length === 0}
        <EmptyState
          title="No executions yet"
          description="executeToolCode runs appear here once a Code Mode session executes a snippet."
        />
      {:else}
        <Table {columns} rows={executions} rowKeyField="execution_id">
          <svelte:fragment slot="cell" let:row let:column>
            {#if column.key === 'session_id'}
              <span class="mono">{row.session_id}</span>
            {:else if column.key === 'status'}
              <Badge tone={statusTone(row.status)}>{row.status}</Badge>
            {:else if column.key === 'tool_calls'}
              {fmtInt(row.tool_calls)}
            {:else if column.key === 'tokens_saved_est'}
              <span class="mono">{fmtInt(row.tokens_saved_est)}</span>
            {:else if column.key === 'snippet_sha'}
              <span class="mono sha">{row.snippet_sha?.slice(0, 12)}</span>
            {:else}
              {row[column.key]}
            {/if}
          </svelte:fragment>
        </Table>
      {/if}
    </section>
  </div>
{/if}

<style>
  .error {
    color: var(--color-danger-fg, var(--color-text));
    margin: var(--space-2) 0;
  }
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  .toolbar {
    display: flex;
  }
  .section-title {
    margin: 0 0 var(--space-3);
    font-size: var(--font-size-body-lg);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .sha {
    color: var(--color-text-muted);
  }
</style>
