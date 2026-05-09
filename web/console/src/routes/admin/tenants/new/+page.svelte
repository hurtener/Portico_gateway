<script lang="ts">
  /**
   * New tenant — Phase 10.8 Step 5 form-page sub-vocabulary.
   *
   * Replaces the bespoke <ul class="steps"> step indicator with the
   * standard SegmentedControl, fixes a labelling bug (the legacy
   * page rendered "Cancel" on the back button mid-flow), and adds a
   * fourth Review step using KeyValueGrid so the operator can
   * confirm before submit. Each step body becomes a .card with the
   * <h4> SECTION-LABEL header pattern.
   */
  import { goto } from '$app/navigation';
  import { api, type Tenant } from '$lib/api';
  import {
    Breadcrumbs,
    Button,
    Input,
    KeyValueGrid,
    PageHeader,
    SegmentedControl,
    Select,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';

  type Step = 'identity' | 'runtime' | 'auth' | 'review';
  let step: Step = 'identity';
  let saving = false;
  let error = '';

  let id = '';
  let displayName = '';
  let plan = 'free';
  let runtimeMode = 'shared_global';
  let maxSessions = '16';
  let maxRpm = '600';
  let retention = '30';
  let issuer = '';
  let jwks = '';

  $: planOptions = [
    { value: 'free', label: 'free' },
    { value: 'pro', label: 'pro' },
    { value: 'enterprise', label: 'enterprise' }
  ];
  $: runtimeOptions = [
    { value: 'shared_global', label: 'shared_global' },
    { value: 'per_tenant', label: 'per_tenant' },
    { value: 'per_user', label: 'per_user' },
    { value: 'per_session', label: 'per_session' }
  ];

  $: stepOptions = [
    { value: 'identity', label: $t('tenants.new.step.identity') },
    { value: 'runtime', label: $t('tenants.new.step.runtime') },
    { value: 'auth', label: $t('tenants.new.step.auth') },
    { value: 'review', label: $t('tenants.new.step.review') }
  ];

  const ORDER: Step[] = ['identity', 'runtime', 'auth', 'review'];

  function next() {
    const i = ORDER.indexOf(step);
    if (i < ORDER.length - 1) step = ORDER[i + 1];
  }
  function back() {
    const i = ORDER.indexOf(step);
    if (i > 0) step = ORDER[i - 1];
  }
  function cancel() {
    void goto('/admin/tenants');
  }

  /**
   * Per-step validation. Identity is the only step with required
   * fields the operator must supply; runtime + auth have working
   * defaults so the wizard advances cleanly even on a minimal
   * config. Reactive ($:) so the Next button enables synchronously
   * with the input bind — calling a method inside `disabled={...}`
   * tracks the function's call expression but Svelte's compiler
   * doesn't always see fields read inside the body, leading to
   * stale-disabled behaviour during e2e fills.
   */
  $: canAdvance = step !== 'identity' || id.trim().length > 0;

  async function save() {
    saving = true;
    error = '';
    try {
      const payload: Partial<Tenant> = {
        id,
        display_name: displayName || id,
        plan,
        runtime_mode: runtimeMode,
        max_concurrent_sessions: Number(maxSessions),
        max_requests_per_minute: Number(maxRpm),
        audit_retention_days: Number(retention),
        jwt_issuer: issuer,
        jwt_jwks_url: jwks
      };
      await api.createTenant(payload);
      toast.success($t('crud.createdToast'), id);
      void goto(`/admin/tenants/${encodeURIComponent(id)}`);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      saving = false;
    }
  }

  $: reviewKV = [
    { label: $t('tenants.field.id'), value: id, mono: true },
    { label: $t('tenants.field.displayName'), value: displayName || id },
    { label: $t('tenants.field.plan'), value: plan },
    { label: $t('tenants.field.runtimeMode'), value: runtimeMode },
    { label: $t('tenants.field.maxSessions'), value: maxSessions },
    { label: $t('tenants.field.maxRpm'), value: maxRpm },
    { label: $t('tenants.field.retention'), value: retention },
    { label: $t('tenants.field.jwtIssuer'), value: issuer || '—' },
    { label: $t('tenants.field.jwtJwks'), value: jwks || '—' }
  ];
</script>

<PageHeader title={$t('tenants.new.title')}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[
      { label: $t('nav.tenants'), href: '/admin/tenants' },
      { label: $t('tenants.new.title') }
    ]}
  />
</PageHeader>

<form on:submit|preventDefault={save} class="form">
  <SegmentedControl
    options={stepOptions}
    bind:value={step}
    ariaLabel={$t('tenants.new.steps.aria')}
  />

  {#if step === 'identity'}
    <section class="card">
      <h4>{$t('tenants.new.step.identity')}</h4>
      <Input bind:value={id} label={$t('tenants.field.id')} required block />
      <Input bind:value={displayName} label={$t('tenants.field.displayName')} block />
      <Select bind:value={plan} label={$t('tenants.field.plan')} options={planOptions} />
    </section>
  {:else if step === 'runtime'}
    <section class="card">
      <h4>{$t('tenants.new.step.runtime')}</h4>
      <Select
        bind:value={runtimeMode}
        label={$t('tenants.field.runtimeMode')}
        options={runtimeOptions}
      />
      <Input
        bind:value={maxSessions}
        type="number"
        label={$t('tenants.field.maxSessions')}
        block
      />
      <Input bind:value={maxRpm} type="number" label={$t('tenants.field.maxRpm')} block />
      <Input
        bind:value={retention}
        type="number"
        label={$t('tenants.field.retention')}
        block
      />
    </section>
  {:else if step === 'auth'}
    <section class="card">
      <h4>{$t('tenants.new.step.auth')}</h4>
      <Input bind:value={issuer} label={$t('tenants.field.jwtIssuer')} block />
      <Input bind:value={jwks} label={$t('tenants.field.jwtJwks')} block />
    </section>
  {:else if step === 'review'}
    <section class="card">
      <h4>{$t('tenants.new.step.review')}</h4>
      <p class="muted">{$t('tenants.new.review.description')}</p>
      <KeyValueGrid items={reviewKV} columns={2} />
    </section>
  {/if}

  {#if error}<p class="error">{error}</p>{/if}

  <div class="actions">
    {#if step === 'identity'}
      <Button variant="ghost" type="button" on:click={cancel}>{$t('common.cancel')}</Button>
    {:else}
      <Button variant="ghost" type="button" on:click={back}>{$t('common.back')}</Button>
    {/if}
    {#if step === 'review'}
      <Button type="submit" variant="primary" loading={saving}>{$t('crud.create')}</Button>
    {:else}
      <Button
        type="button"
        variant="primary"
        on:click={next}
        disabled={!canAdvance}
      >
        {$t('common.next')}
      </Button>
    {/if}
  </div>
</form>

<style>
  .form {
    display: grid;
    gap: var(--space-4);
    max-width: 720px;
    margin-top: var(--space-4);
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .card h4 {
    margin: 0;
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-body-sm);
    margin: 0;
    line-height: 1.5;
  }
  .actions {
    display: flex;
    gap: var(--space-2);
    justify-content: flex-end;
  }
  .error {
    color: var(--color-danger);
    margin: 0;
    font-size: var(--font-size-body-sm);
  }
</style>
