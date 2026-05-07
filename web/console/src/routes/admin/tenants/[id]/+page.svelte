<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api, type EntityActivityRow, type Tenant } from '$lib/api';
  import { Badge, Button, KeyValueGrid, PageHeader, Tabs, toast } from '$lib/components';
  import { t } from '$lib/i18n';

  let tenant: Tenant | null = null;
  let activity: EntityActivityRow[] = [];
  let error = '';
  let archiving = false;
  let activeTab: 'overview' | 'activity' = 'overview';

  $: id = ($page.params.id ?? '') as string;

  async function refresh() {
    if (!id) return;
    try {
      tenant = await api.getTenant(id);
      activity = await api.tenantActivity(id);
    } catch (e) {
      error = (e as Error).message;
    }
  }

  async function archive() {
    if (!id || !confirm($t('crud.confirmDelete'))) return;
    archiving = true;
    try {
      await api.archiveTenant(id);
      toast.info($t('crud.deletedToast'), id);
      void goto('/admin/tenants');
    } catch (e) {
      error = (e as Error).message;
    } finally {
      archiving = false;
    }
  }

  $: tabs = [
    { id: 'overview', label: $t('servers.tabs.overview') },
    { id: 'activity', label: $t('servers.tabs.activity') }
  ];

  $: items = tenant
    ? [
        { label: $t('tenants.field.id'), value: tenant.id },
        { label: $t('tenants.field.displayName'), value: tenant.display_name },
        { label: $t('tenants.field.plan'), value: tenant.plan },
        { label: $t('tenants.field.runtimeMode'), value: tenant.runtime_mode ?? '' },
        {
          label: $t('tenants.field.maxSessions'),
          value: String(tenant.max_concurrent_sessions ?? '')
        },
        { label: $t('tenants.field.maxRpm'), value: String(tenant.max_requests_per_minute ?? '') },
        { label: $t('tenants.field.retention'), value: String(tenant.audit_retention_days ?? '') },
        { label: $t('tenants.field.jwtIssuer'), value: tenant.jwt_issuer ?? '' },
        { label: $t('tenants.field.jwtJwks'), value: tenant.jwt_jwks_url ?? '' }
      ]
    : [];

  onMount(refresh);
</script>

<PageHeader title={tenant?.display_name ?? id} description={tenant?.id ?? ''}>
  <div slot="actions" class="actions">
    {#if tenant?.status !== 'archived'}
      <Button variant="ghost" on:click={archive} loading={archiving}>{$t('crud.archive')}</Button>
    {:else}
      <Badge tone="warning">{$t('tenants.status.archived')}</Badge>
    {/if}
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

<Tabs {tabs} bind:active={activeTab} />

{#if activeTab === 'overview'}
  {#if tenant}
    <KeyValueGrid {items} />
  {/if}
{:else}
  <ul class="activity">
    {#each activity as a (a.event_id)}
      <li>
        <span class="when">{a.occurred_at}</span>
        <span class="who">{a.actor_user_id ?? '—'}</span>
        <span>{a.summary}</span>
      </li>
    {:else}
      <li class="empty">{$t('common.empty')}</li>
    {/each}
  </ul>
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-3);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
  }
  .activity {
    list-style: none;
    padding: 0;
    margin: var(--space-3) 0 0;
    display: grid;
    gap: var(--space-2);
  }
  .activity li {
    display: grid;
    grid-template-columns: 220px 160px 1fr;
    gap: var(--space-3);
    color: var(--color-text-secondary);
    font-size: var(--font-size-body-sm);
  }
  .activity li.empty {
    grid-template-columns: 1fr;
    color: var(--color-text-muted);
  }
  .when {
    font-family: var(--font-family-mono);
  }
  .who {
    color: var(--color-text-primary);
  }
</style>
