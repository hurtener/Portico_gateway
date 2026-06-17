<script lang="ts">
  /**
   * LLM quotas (Phase 13). A single per-tenant row of rate/usage limits the
   * gateway enforces across every LLM request. Unlike providers/models this
   * is not a list — there is exactly one quota per tenant — so the screen is
   * a single settings form: load the current values (or built-in defaults),
   * edit, save (PUT /api/llm/quota). A zero in any field means "unlimited".
   */
  import { onMount } from 'svelte';
  import { api, isFeatureUnavailable, type LLMQuota } from '$lib/api';
  import { Button, EmptyState, Input, PageHeader, Skeleton, toast } from '$lib/components';
  import IconSave from 'lucide-svelte/icons/save';
  import IconRotate from 'lucide-svelte/icons/rotate-ccw';

  // Mirrors ifaces.DefaultLLMQuota so "Reset to defaults" matches the
  // server-side fallback exactly.
  const DEFAULTS: LLMQuota = {
    requests_per_minute: 600,
    tokens_per_minute: 200000,
    tokens_per_day: 4000000,
    cost_usd_per_day: 100
  };

  let loading = true;
  let saving = false;
  let unavailable = false;
  let error = '';
  let updatedAt = '';

  // String-backed form fields (Input emits strings); parsed on save.
  let fRpm = '';
  let fTpm = '';
  let fTpd = '';
  let fCost = '';

  onMount(load);

  function apply(q: LLMQuota) {
    fRpm = String(q.requests_per_minute ?? 0);
    fTpm = String(q.tokens_per_minute ?? 0);
    fTpd = String(q.tokens_per_day ?? 0);
    fCost = String(q.cost_usd_per_day ?? 0);
    updatedAt = q.updated_at ?? '';
  }

  async function load() {
    loading = true;
    error = '';
    try {
      apply(await api.getLLMQuota());
      unavailable = false;
    } catch (e) {
      if (isFeatureUnavailable(e)) {
        unavailable = true;
      } else {
        error = e instanceof Error ? e.message : 'Failed to load quota';
      }
    } finally {
      loading = false;
    }
  }

  function parseField(label: string, raw: string, integer: boolean): number {
    const n = integer ? parseInt(raw, 10) : parseFloat(raw);
    if (Number.isNaN(n) || n < 0) {
      throw new Error(`${label} must be a non-negative number`);
    }
    return n;
  }

  async function save() {
    saving = true;
    try {
      const body: LLMQuota = {
        requests_per_minute: parseField('Requests per minute', fRpm, true),
        tokens_per_minute: parseField('Tokens per minute', fTpm, true),
        tokens_per_day: parseField('Tokens per day', fTpd, true),
        cost_usd_per_day: parseField('Cost per day', fCost, false)
      };
      apply(await api.updateLLMQuota(body));
      toast.success('Quota updated');
    } catch (e) {
      toast.danger(e instanceof Error ? e.message : 'Save failed');
    } finally {
      saving = false;
    }
  }

  function resetDefaults() {
    apply(DEFAULTS);
    toast.info('Defaults loaded — save to apply.');
  }
</script>

<PageHeader
  title="LLM Quotas"
  description="Per-tenant rate and usage limits the gateway enforces on every LLM request."
  compact
>
  <div slot="actions">
    <Button variant="ghost" on:click={resetDefaults} disabled={loading || unavailable}>
      <IconRotate slot="leading" size={14} />Reset to defaults
    </Button>
    <Button on:click={save} disabled={saving || loading || unavailable}>
      <IconSave slot="leading" size={14} />{saving ? 'Saving…' : 'Save'}
    </Button>
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if unavailable}
  <EmptyState
    title="LLM gateway not configured"
    description="The LLM quota store is not wired in this build."
  />
{:else if loading}
  <div class="grid">
    <Skeleton height="180px" />
    <Skeleton height="180px" />
  </div>
{:else}
  <div class="grid">
    <section class="card">
      <header>
        <h3>Rate limits</h3>
        <p class="muted">Rolling per-minute windows. Set 0 to disable a limit.</p>
      </header>
      <Input
        label="Requests per minute"
        type="number"
        bind:value={fRpm}
        hint="Max LLM requests accepted per minute for this tenant."
      />
      <Input
        label="Tokens per minute"
        type="number"
        bind:value={fTpm}
        hint="Combined prompt + completion tokens per minute."
      />
    </section>

    <section class="card">
      <header>
        <h3>Daily caps</h3>
        <p class="muted">Resets each day. Set 0 to disable a cap.</p>
      </header>
      <Input
        label="Tokens per day"
        type="number"
        bind:value={fTpd}
        hint="Combined prompt + completion tokens per day."
      />
      <Input
        label="Cost (USD) per day"
        type="number"
        bind:value={fCost}
        hint="Estimated upstream spend ceiling per day."
      />
    </section>
  </div>

  {#if updatedAt}
    <p class="muted updated">Last updated {new Date(updatedAt).toLocaleString()}</p>
  {/if}
{/if}

<style>
  .error {
    color: var(--color-danger-fg, var(--color-text));
    margin: var(--space-2) 0;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--space-4);
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4);
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-default);
    border-radius: var(--radius-lg);
  }
  .card header {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .card h3 {
    margin: 0;
    font-size: var(--font-size-body-lg);
    font-weight: var(--font-weight-medium);
    color: var(--color-text-primary);
  }
  .muted {
    color: var(--color-text-muted);
    font-size: var(--font-size-sm);
    margin: 0;
  }
  .updated {
    margin-top: var(--space-3);
  }
  @media (max-width: 760px) {
    .grid {
      grid-template-columns: 1fr;
    }
  }
</style>
