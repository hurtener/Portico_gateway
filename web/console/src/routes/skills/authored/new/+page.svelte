<script lang="ts">
  /**
   * New authored skill — Phase 10.8 Step 5 form-page sub-vocabulary.
   *
   * The pre-10.8 page invented its own two-pane layout with a
   * bespoke "validation aside" that didn't share vocabulary with
   * the rest of the console. This rewrite reuses the same
   * `.layout.has-selection` two-column grid the list pages use, and
   * the right pane is a real `Inspector` primitive (with its own
   * tabs and close behaviour) instead of a hand-rolled `<aside>`.
   *
   * The Inspector "open" state is always true here — there's no
   * row-selection on this page; the validation panel is the
   * inspector's reason-for-being. Closing it is a no-op (no
   * close button is shown by the consumer-side handling — see the
   * Inspector primitive for details).
   *
   * Validation runs on every edit (debounced 500ms) and lands in
   * the Inspector's `validation` tab. The `schema` tab provides a
   * quick reference card so the operator doesn't need to leave the
   * page to remember the manifest shape.
   */
  import { onMount, onDestroy } from 'svelte';
  import { goto } from '$app/navigation';
  import { api, type SkillValidationResult } from '$lib/api';
  import {
    Badge,
    Breadcrumbs,
    Button,
    Inspector,
    PageActionGroup,
    PageHeader,
    Tabs,
    Textarea,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconCheck from 'lucide-svelte/icons/check';
  import IconAlertTriangle from 'lucide-svelte/icons/alert-triangle';
  import IconSave from 'lucide-svelte/icons/save';
  import IconUpload from 'lucide-svelte/icons/upload';

  const sampleManifest = `id: acme.example
title: Example Skill
version: 1.0.0
spec: skills/v1
description: An example authored skill
instructions: SKILL.md
binding:
  required_tools:
    - github.get_pull_request
`;

  let manifestBody = sampleManifest;
  let skillBody = '# Example skill\n\nDescribe how this skill is used here.\n';
  let promptBody = '';
  let activeTab = 'manifest';
  let inspectorTab = 'validation';
  /**
   * The inspector stays open by default — validation is the page's
   * reason for being. The operator may still click the close button
   * (collapse for a wider editor), and a "Validation" PageActionGroup
   * action toggles it back.
   */
  let inspectorOpen = true;
  let validation: SkillValidationResult | null = null;
  let validating = false;
  let validationDebounce: ReturnType<typeof setTimeout> | null = null;
  let saving = false;

  function buildRequest() {
    const files = [{ relpath: 'SKILL.md', mime_type: 'text/markdown', body: skillBody }];
    if (promptBody.trim()) {
      files.push({ relpath: 'prompts/main.md', mime_type: 'text/markdown', body: promptBody });
    }
    return { manifest: manifestBody, files };
  }

  async function runValidation() {
    validating = true;
    try {
      validation = await api.validateSkillManifest(buildRequest());
    } catch (e) {
      validation = {
        valid: false,
        violations: [{ pointer: '', reason: (e as Error).message }]
      };
    } finally {
      validating = false;
    }
  }

  function scheduleValidation() {
    if (validationDebounce) clearTimeout(validationDebounce);
    validationDebounce = setTimeout(runValidation, 500);
  }

  $: (manifestBody, skillBody, promptBody, scheduleValidation());

  async function saveDraft() {
    saving = true;
    try {
      const created = await api.createAuthoredSkill(buildRequest());
      toast.success($t('authored.toast.saved'));
      await goto(`/skills/authored/${encodeURIComponent(created.skill_id)}`);
    } catch (e) {
      toast.danger($t('common.save'), (e as Error).message);
    } finally {
      saving = false;
    }
  }

  async function publishNow() {
    saving = true;
    try {
      const created = await api.createAuthoredSkill(buildRequest());
      await api.publishAuthoredSkill(created.skill_id, created.version);
      toast.success($t('authored.toast.published'));
      await goto(`/skills/authored/${encodeURIComponent(created.skill_id)}`);
    } catch (e) {
      toast.danger($t('authored.editor.publish'), (e as Error).message);
    } finally {
      saving = false;
    }
  }

  onMount(runValidation);
  onDestroy(() => {
    if (validationDebounce) clearTimeout(validationDebounce);
  });

  const editorTabs = [
    { id: 'manifest', label: $t('authored.editor.manifest') },
    { id: 'skillmd', label: $t('authored.editor.skillMd') },
    { id: 'prompts', label: $t('authored.editor.prompts') }
  ];

  const inspectorTabs = [
    { id: 'validation', label: $t('authored.editor.validate') },
    { id: 'schema', label: $t('authoredNew.inspector.tab.schema') }
  ];

  $: pageActions = [
    ...(inspectorOpen
      ? []
      : [
          {
            label: $t('authoredNew.action.showValidation'),
            icon: IconCheck,
            variant: 'ghost' as const,
            onClick: () => {
              inspectorOpen = true;
            }
          }
        ]),
    {
      label: $t('authored.editor.savedraft'),
      icon: IconSave,
      variant: 'secondary' as const,
      onClick: saveDraft,
      loading: saving
    },
    {
      label: $t('authored.editor.publish'),
      icon: IconUpload,
      variant: 'primary' as const,
      onClick: publishNow,
      loading: saving
    }
  ];
</script>

<PageHeader title={$t('authored.editor.title')} description={$t('authored.editor.description')}>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[
      { label: $t('nav.skills'), href: '/skills' },
      { label: $t('nav.authored'), href: '/skills/authored' },
      { label: $t('authored.action.new') }
    ]}
  />
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

<div class="layout" class:has-selection={inspectorOpen}>
  <div class="main-col">
    <section class="card">
      <Tabs tabs={editorTabs} bind:active={activeTab} />
      {#if activeTab === 'manifest'}
        <Textarea
          bind:value={manifestBody}
          rows={20}
          mono
          label={$t('authored.editor.manifest')}
        />
      {:else if activeTab === 'skillmd'}
        <Textarea bind:value={skillBody} rows={20} mono label={$t('authored.editor.skillMd')} />
      {:else if activeTab === 'prompts'}
        <Textarea bind:value={promptBody} rows={20} mono label={$t('authored.editor.prompts')} />
      {/if}
    </section>
  </div>

  <Inspector
    bind:open={inspectorOpen}
    tabs={inspectorTabs}
    bind:activeTab={inspectorTab}
    title={$t('authored.editor.validate')}
    emptyTitle={$t('authoredNew.inspector.empty.title')}
    emptyDescription={$t('authoredNew.inspector.empty.description')}
  >
    {#if inspectorTab === 'validation'}
      <section class="card">
        <h4>{$t('authored.editor.validate')}</h4>
        {#if validating}
          <p class="muted">{$t('common.loading')}</p>
        {:else if !validation}
          <p class="muted">{$t('common.dash')}</p>
        {:else if validation.valid}
          <div class="ok">
            <IconCheck size={16} />
            <span>{$t('authored.validation.valid')}</span>
          </div>
        {:else}
          <div class="warn">
            <IconAlertTriangle size={16} />
            <span>{$t('authored.validation.errors', { n: validation.violations.length })}</span>
          </div>
          <ul class="violations">
            {#each validation.violations as v}
              <li>
                <code class="ptr">{v.pointer || '/'}</code>
                <span class="reason">{v.reason}</span>
                {#if v.line && v.line > 0}
                  <Badge tone="neutral">
                    {$t('authored.validation.line', { line: v.line, col: v.col ?? 0 })}
                  </Badge>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {:else if inspectorTab === 'schema'}
      <section class="card">
        <h4>{$t('authoredNew.inspector.section.schema')}</h4>
        <p class="muted body">{$t('authoredNew.inspector.section.schemaHelp')}</p>
        <ul class="schema-list">
          <li><code class="mono">id</code> — {$t('authoredNew.schema.id')}</li>
          <li><code class="mono">title</code> — {$t('authoredNew.schema.title')}</li>
          <li><code class="mono">version</code> — {$t('authoredNew.schema.version')}</li>
          <li><code class="mono">spec</code> — {$t('authoredNew.schema.spec')}</li>
          <li><code class="mono">description</code> — {$t('authoredNew.schema.description')}</li>
          <li><code class="mono">instructions</code> — {$t('authoredNew.schema.instructions')}</li>
          <li>
            <code class="mono">binding.required_tools</code> — {$t('authoredNew.schema.tools')}
          </li>
          <li>
            <code class="mono">binding.required_servers</code> — {$t('authoredNew.schema.servers')}
          </li>
        </ul>
      </section>
    {/if}
  </Inspector>
</div>

<style>
  .layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-6);
    align-items: start;
    margin-top: var(--space-4);
  }
  .layout.has-selection {
    grid-template-columns: minmax(0, 1fr) 320px;
  }
  @media (max-width: 1279px) {
    .layout.has-selection {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .main-col {
    min-width: 0;
    display: flex;
    flex-direction: column;
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
    font-size: var(--font-size-label);
  }
  .body {
    line-height: 1.5;
    font-size: var(--font-size-body-sm);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
  }
  .ok {
    color: var(--color-success);
    display: flex;
    gap: var(--space-2);
    align-items: center;
  }
  .warn {
    color: var(--color-warning);
    display: flex;
    gap: var(--space-2);
    align-items: center;
  }
  .violations {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .violations li {
    background: var(--color-bg-canvas);
    padding: var(--space-2);
    border-radius: var(--radius-sm);
    font-size: var(--font-size-body-sm);
    border: 1px solid var(--color-border-soft);
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .ptr {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-tertiary);
  }
  .reason {
    color: var(--color-text-primary);
  }
  .schema-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    font-size: var(--font-size-body-sm);
    color: var(--color-text-secondary);
  }
</style>
