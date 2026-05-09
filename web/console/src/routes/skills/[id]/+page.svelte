<script lang="ts">
  /**
   * Skill detail — Phase 10.8 detail-page sub-vocabulary.
   *
   * The pre-10.8 page was effectively a JSON dump of the manifest.
   * This rewrite tabbed-splits Overview / Manifest / Bindings:
   *   - Overview: KeyValueGrid of high-fidelity fields (id, version,
   *     title, description, enablement) + warnings card if any.
   *   - Manifest: the full manifest as a CodeBlock (parity with the
   *     legacy view).
   *   - Bindings: required tools and servers parsed from the manifest
   *     so the operator can see what this skill needs without reading
   *     YAML.
   *
   * Manifest fields are heterogeneous; we narrow the types defensively.
   */
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api, type SkillDetail } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    CodeBlock,
    EmptyState,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    Tabs
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import IconLayers from 'lucide-svelte/icons/layers';
  import IconWrench from 'lucide-svelte/icons/wrench';
  import IconServer from 'lucide-svelte/icons/server';
  import IconCheckCircle2 from 'lucide-svelte/icons/check-circle-2';
  import IconCircleSlash from 'lucide-svelte/icons/circle-slash';
  import type { ComponentType } from 'svelte';

  let detail: SkillDetail | null = null;
  let loading = true;
  let error = '';
  let activeTab = 'overview';

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

  // === Manifest derivations ===========================================

  /**
   * Extract a string array from a manifest field. Handles both flat
   * arrays (`required_tools: [a, b]`) and tools-with-objects shapes
   * (`tools: [{name: a}, {name: b}]`). Returns [] when neither.
   */
  function extractStrings(field: unknown): string[] {
    if (!Array.isArray(field)) return [];
    return field
      .map((v) => {
        if (typeof v === 'string') return v;
        if (v && typeof v === 'object' && 'name' in v) {
          const n = (v as { name?: unknown }).name;
          return typeof n === 'string' ? n : '';
        }
        return '';
      })
      .filter(Boolean);
  }

  $: requiredTools = detail
    ? extractStrings(detail.manifest?.required_tools).concat(
        extractStrings(detail.manifest?.tools)
      )
    : [];
  $: requiredServers = detail
    ? extractStrings(detail.manifest?.required_servers).concat(
        extractStrings(detail.manifest?.servers)
      )
    : [];

  $: tabs = [
    { id: 'overview', label: $t('skillDetail.tab.overview') },
    { id: 'manifest', label: $t('skillDetail.tab.manifest') },
    { id: 'bindings', label: $t('skillDetail.tab.bindings') }
  ];

  $: identityKV = detail
    ? [
        { label: $t('skillDetail.field.id'), value: detail.id, mono: true },
        { label: $t('skillDetail.field.version'), value: detail.version, mono: true },
        { label: $t('skillDetail.field.title'), value: detail.title || '—' },
        {
          label: $t('skillDetail.field.enabled'),
          value: detail.enabled_for_tenant ? $t('common.yes') : $t('common.no')
        }
      ]
    : [];

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh(),
      loading
    },
    ...(detail
      ? [
          {
            label: detail.enabled_for_tenant
              ? $t('skillDetail.action.disableForTenant')
              : $t('skillDetail.action.enableForTenant'),
            icon: detail.enabled_for_tenant ? IconCircleSlash : IconCheckCircle2,
            variant: 'primary' as const,
            onClick: toggle
          }
        ]
      : [])
  ];

  $: metrics = detail
    ? [
        {
          id: 'version',
          label: $t('skillDetail.metric.version'),
          value: `v${detail.version}`,
          icon: IconLayers as ComponentType<any>,
          tone: 'brand' as const
        },
        {
          id: 'tools',
          label: $t('skillDetail.metric.tools'),
          value: requiredTools.length.toString(),
          icon: IconWrench as ComponentType<any>
        },
        {
          id: 'servers',
          label: $t('skillDetail.metric.servers'),
          value: requiredServers.length.toString(),
          icon: IconServer as ComponentType<any>
        }
      ]
    : [];

  $: manifestJson = detail ? JSON.stringify(detail.manifest, null, 2) : '';
</script>

<PageHeader title={detail?.title || id}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: $t('nav.skills'), href: '/skills' }, { label: id }]}
  />
  <div slot="meta">
    <Badge tone="neutral" mono>{id}</Badge>
    {#if detail?.version}<Badge tone="neutral" mono>v{detail.version}</Badge>{/if}
    {#if detail}
      {#if detail.enabled_for_tenant}
        <Badge tone="success">{$t('skillDetail.badge.enabled')}</Badge>
      {:else}
        <Badge tone="neutral">{$t('skillDetail.badge.disabled')}</Badge>
      {/if}
    {/if}
  </div>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if detail}
  <MetricStrip {metrics} compact label={$t('skillDetail.metric.aria')} />
  <Tabs {tabs} bind:active={activeTab} />

  {#if activeTab === 'overview'}
    <section class="card">
      <h4>{$t('skillDetail.section.identity')}</h4>
      <KeyValueGrid items={identityKV} columns={2} />
    </section>
    {#if detail.description}
      <section class="card">
        <h4>{$t('skillDetail.section.description')}</h4>
        <p class="body">{detail.description}</p>
      </section>
    {/if}
    {#if detail.warnings && detail.warnings.length > 0}
      <section class="card warn">
        <h4>
          <IconAlertTriangle size={14} aria-hidden="true" />
          {$t('skillDetail.warnings')}
        </h4>
        <ul>
          {#each detail.warnings as w (w)}
            <li>{w}</li>
          {/each}
        </ul>
      </section>
    {/if}
  {:else if activeTab === 'manifest'}
    <section class="card">
      <h4>{$t('skillDetail.manifest')}</h4>
      <CodeBlock code={manifestJson} language="json" filename="skill.yaml" />
    </section>
  {:else if activeTab === 'bindings'}
    <section class="card">
      <h4>{$t('skillDetail.section.requiredTools')}</h4>
      {#if requiredTools.length === 0}
        <p class="muted">{$t('skillDetail.section.noTools')}</p>
      {:else}
        <ul class="binding-list">
          {#each requiredTools as tool (tool)}
            <li><code class="mono">{tool}</code></li>
          {/each}
        </ul>
      {/if}
    </section>
    <section class="card">
      <h4>{$t('skillDetail.section.requiredServers')}</h4>
      {#if requiredServers.length === 0}
        <p class="muted">{$t('skillDetail.section.noServers')}</p>
      {:else}
        <ul class="binding-list">
          {#each requiredServers as server (server)}
            <li><code class="mono">{server}</code></li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}
{:else if !loading}
  <EmptyState title={$t('skillDetail.notFound')} />
{/if}

<style>
  .error {
    color: var(--color-danger);
    font-size: var(--font-size-body-sm);
    margin: 0 0 var(--space-4) 0;
  }
  .muted {
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
  }
  .body {
    color: var(--color-text-secondary);
    line-height: 1.5;
    margin: 0;
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
  }
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    margin-top: var(--space-4);
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
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  .card.warn {
    border-color: var(--color-warning);
    background: var(--color-warning-soft);
  }
  .card.warn h4 {
    color: var(--color-warning);
  }
  .card.warn ul {
    margin: 0;
    padding-left: var(--space-5);
    color: var(--color-warning);
  }
  .binding-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .binding-list li {
    background: var(--color-bg-canvas);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
  }
</style>
