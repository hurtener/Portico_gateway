<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { goto } from '$app/navigation';
  import { api, type SkillValidationResult } from '$lib/api';
  import { Badge, Breadcrumbs, Button, PageHeader, Tabs, Textarea, toast } from '$lib/components';
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

  const tabs = [
    { id: 'manifest', label: $t('authored.editor.manifest') },
    { id: 'skillmd', label: $t('authored.editor.skillMd') },
    { id: 'prompts', label: $t('authored.editor.prompts') }
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
    <Button variant="secondary" on:click={saveDraft} loading={saving}>
      <IconSave slot="leading" size={14} />
      {$t('authored.editor.savedraft')}
    </Button>
    <Button on:click={publishNow} loading={saving}>
      <IconUpload slot="leading" size={14} />
      {$t('authored.editor.publish')}
    </Button>
  </div>
</PageHeader>

<div class="layout">
  <section class="editor">
    <Tabs {tabs} bind:active={activeTab} />
    {#if activeTab === 'manifest'}
      <Textarea bind:value={manifestBody} rows={20} mono label={$t('authored.editor.manifest')} />
    {:else if activeTab === 'skillmd'}
      <Textarea bind:value={skillBody} rows={20} mono label={$t('authored.editor.skillMd')} />
    {:else if activeTab === 'prompts'}
      <Textarea bind:value={promptBody} rows={20} mono label={$t('authored.editor.prompts')} />
    {/if}
  </section>

  <aside class="validation">
    <h3>{$t('authored.editor.validate')}</h3>
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
              <Badge tone="neutral"
                >{$t('authored.validation.line', { line: v.line, col: v.col ?? 0 })}</Badge
              >
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </aside>
</div>

<style>
  .layout {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
    gap: var(--space-4);
    margin-top: var(--space-4);
  }
  .editor {
    background: var(--color-bg-elevated);
    border-radius: var(--radius-md);
    padding: var(--space-3);
  }
  .validation {
    background: var(--color-bg-elevated);
    border-radius: var(--radius-md);
    padding: var(--space-3);
    align-self: start;
  }
  .validation h3 {
    margin: 0 0 var(--space-3) 0;
    font-size: var(--font-size-body-sm);
    font-weight: 600;
    color: var(--color-text-secondary);
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
    margin: var(--space-3) 0 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .violations li {
    background: var(--color-bg-subtle);
    padding: var(--space-2);
    border-radius: var(--radius-sm);
    font-size: var(--font-size-body-sm);
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
  .muted {
    color: var(--color-text-tertiary);
  }
</style>
