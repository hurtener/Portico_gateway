<script lang="ts">
  import { goto } from '$app/navigation';
  import { api } from '$lib/api';
  import { Button, Input, PageHeader, Select, toast, Textarea } from '$lib/components';
  import { t } from '$lib/i18n';

  let id = '';
  let displayName = '';
  let transport: 'stdio' | 'http' = 'stdio';
  let command = '';
  let argsRaw = '';
  let envRaw = '';
  let url = '';
  let saving = false;
  let error = '';

  async function save() {
    saving = true;
    error = '';
    try {
      const args = argsRaw.split(/\s+/).filter(Boolean);
      const env = envRaw
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean);
      const payload: Record<string, unknown> = {
        id,
        display_name: displayName || id,
        transport,
        runtime_mode: 'shared_global'
      };
      if (transport === 'stdio') {
        payload.stdio = { command, args, env };
      } else {
        payload.http = { url };
      }
      // upsertServer hits /v1/servers — equivalent to /api/servers POST
      // for create; the back-end accepts the same payload on both paths.
      await api.upsertServer(payload as Record<string, never>);
      toast.success($t('crud.createdToast'), id);
      void goto(`/servers/${encodeURIComponent(id)}`);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      saving = false;
    }
  }

  $: transportOptions = [
    { value: 'stdio', label: 'stdio' },
    { value: 'http', label: 'http' }
  ];
</script>

<PageHeader title={$t('servers.new.title')} description={$t('servers.new.subtitle')} />

<form class="grid" on:submit|preventDefault={save}>
  <Input bind:value={id} label={$t('servers.field.id')} required block={false} />
  <Input bind:value={displayName} label={$t('servers.field.displayName')} block={false} />
  <Select bind:value={transport} label={$t('servers.field.transport')} options={transportOptions} />
  {#if transport === 'stdio'}
    <Input bind:value={command} label={$t('servers.field.command')} required block={false} />
    <Input bind:value={argsRaw} label={$t('servers.field.args')} block={false} />
    <Textarea bind:value={envRaw} label={$t('servers.field.env')} rows={4} />
  {:else}
    <Input bind:value={url} label={$t('servers.field.url')} required block={false} />
  {/if}
  {#if error}<p class="error">{error}</p>{/if}
  <div class="actions">
    <Button type="submit" loading={saving}>{$t('servers.action.saveStart')}</Button>
  </div>
</form>

<style>
  .grid {
    display: grid;
    gap: var(--space-3);
    max-width: 720px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    margin-top: var(--space-3);
  }
  .error {
    color: var(--color-danger);
    margin: 0;
  }
</style>
