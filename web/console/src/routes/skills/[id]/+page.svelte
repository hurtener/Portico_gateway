<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type SkillDetail } from '$lib/api';
  import { Badge, Breadcrumbs, Button, CodeBlock, PageHeader } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';

  let detail: SkillDetail | null = null;
  let loading = true;
  let error = '';

  $: id = $page.params.id ?? '';

  async function refresh() {
    if (!id) return;
    loading = true;
    error = '';
    try {
      detail = await api.getSkill(id);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function toggle() {
    if (!detail) return;
    try {
      if (detail.enabled_for_tenant) {
        await api.disableSkill(detail.id);
      } else {
        await api.enableSkill(detail.id);
      }
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);

  $: manifestJson = detail ? JSON.stringify(detail.manifest, null, 2) : '';
</script>

<PageHeader title={detail?.title || id} description={detail?.description}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: $t('nav.skills'), href: '/skills' }, { label: id }]}
  />
  <div slot="meta">
    <Badge tone="neutral" mono>{id}</Badge>
    {#if detail?.version}<Badge tone="neutral" mono>v{detail.version}</Badge>{/if}
    {#if detail}
      {#if detail.enabled_for_tenant}
        <Badge tone="success">enabled</Badge>
      {:else}
        <Badge tone="neutral">disabled</Badge>
      {/if}
    {/if}
  </div>
  <div slot="actions">
    <Button variant="secondary" on:click={refresh} {loading}>
      <IconRefreshCw slot="leading" size={14} />
      {$t('common.refresh')}
    </Button>
    {#if detail}
      <Button on:click={toggle}>
        {detail.enabled_for_tenant
          ? $t('skillDetail.action.disableForTenant')
          : $t('skillDetail.action.enableForTenant')}
      </Button>
    {/if}
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if detail}
  {#if detail.warnings && detail.warnings.length > 0}
    <section class="warn">
      <h2 class="warn-title">
        <IconAlertTriangle size={16} aria-hidden="true" />
        {$t('skillDetail.warnings')}
      </h2>
      <ul>
        {#each detail.warnings as w (w)}
          <li>{w}</li>
        {/each}
      </ul>
    </section>
  {/if}

  <section>
    <h2 class="section-title">{$t('skillDetail.manifest')}</h2>
    <CodeBlock code={manifestJson} language="json" filename="skill.yaml" />
  </section>
{:else if !loading}
  <p class="muted">{$t('skillDetail.notFound')}</p>
{/if}

<style>
  .error {
    color: var(--color-danger);
    font-size: var(--font-size-body-sm);
    margin: 0 0 var(--space-4) 0;
  }
  .muted {
    color: var(--color-text-tertiary);
  }
  .warn {
    border: 1px solid var(--color-warning);
    background: var(--color-warning-soft);
    color: var(--color-warning);
    padding: var(--space-4) var(--space-5);
    border-radius: var(--radius-md);
    margin-bottom: var(--space-6);
  }
  .warn-title {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0 0 var(--space-2) 0;
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
  }
  .warn ul {
    margin: 0;
    padding-left: var(--space-5);
  }
  .section-title {
    font-size: var(--font-size-title);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
    margin: 0 0 var(--space-3) 0;
  }
</style>
