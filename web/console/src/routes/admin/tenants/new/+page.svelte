<script lang="ts">
  import { goto } from '$app/navigation';
  import { api, type Tenant } from '$lib/api';
  import { Button, Input, PageHeader, Select, toast } from '$lib/components';
  import { t } from '$lib/i18n';

  let step = 1;
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

  function next() {
    if (step < 3) step += 1;
  }
  function prev() {
    if (step > 1) step -= 1;
  }

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
</script>

<PageHeader title={$t('tenants.new.title')} />

<form class="grid" on:submit|preventDefault={save}>
  <ul class="steps">
    <li class:active={step === 1}>{$t('tenants.new.step.identity')}</li>
    <li class:active={step === 2}>{$t('tenants.new.step.runtime')}</li>
    <li class:active={step === 3}>{$t('tenants.new.step.auth')}</li>
  </ul>

  {#if step === 1}
    <Input bind:value={id} label={$t('tenants.field.id')} required block={false} />
    <Input bind:value={displayName} label={$t('tenants.field.displayName')} block={false} />
    <Select bind:value={plan} label={$t('tenants.field.plan')} options={planOptions} />
  {:else if step === 2}
    <Select
      bind:value={runtimeMode}
      label={$t('tenants.field.runtimeMode')}
      options={runtimeOptions}
    />
    <Input
      bind:value={maxSessions}
      type="number"
      label={$t('tenants.field.maxSessions')}
      block={false}
    />
    <Input bind:value={maxRpm} type="number" label={$t('tenants.field.maxRpm')} block={false} />
    <Input
      bind:value={retention}
      type="number"
      label={$t('tenants.field.retention')}
      block={false}
    />
  {:else}
    <Input bind:value={issuer} label={$t('tenants.field.jwtIssuer')} block={false} />
    <Input bind:value={jwks} label={$t('tenants.field.jwtJwks')} block={false} />
  {/if}

  {#if error}<p class="error">{error}</p>{/if}

  <div class="actions">
    {#if step > 1}
      <Button variant="ghost" type="button" on:click={prev}>{$t('common.cancel')}</Button>
    {/if}
    {#if step < 3}
      <Button type="button" on:click={next}>{$t('common.save')}</Button>
    {:else}
      <Button type="submit" loading={saving}>{$t('crud.create')}</Button>
    {/if}
  </div>
</form>

<style>
  .grid {
    display: grid;
    gap: var(--space-3);
    max-width: 720px;
  }
  .steps {
    display: flex;
    gap: var(--space-3);
    list-style: none;
    padding: 0;
    margin: 0 0 var(--space-3);
  }
  .steps li {
    color: var(--color-text-muted);
    font-size: var(--font-size-body-sm);
  }
  .steps li.active {
    color: var(--color-text-primary);
    font-weight: var(--font-weight-semibold);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
  .error {
    color: var(--color-danger);
    margin: 0;
  }
</style>
