<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte';
  import { api, isFeatureUnavailable, type ServerSpec, type SecretRef } from '$lib/api';
  import Badge from './Badge.svelte';
  import Button from './Button.svelte';
  import Input from './Input.svelte';
  import Select from './Select.svelte';
  import { t } from '$lib/i18n';
  import IconPlus from 'lucide-svelte/icons/plus';
  import IconTrash from 'lucide-svelte/icons/trash-2';

  /**
   * The full server-registration form. Reused by /servers/new (creating)
   * and /servers/[id]/edit (editing). The component is "controlled":
   * the parent passes an initial spec; the parent receives `submit`
   * with the merged payload to send to the API.
   *
   * The form covers the Phase 9 plan's full surface:
   *   - id / display_name / transport / runtime_mode
   *   - stdio: command, args (quoted-string aware), env-table editor
   *   - http: url, auth_header, optional credential reference picker
   *     (drop-down sourced from /api/admin/secrets)
   */

  export let initial: Partial<ServerSpec> = {};
  export let mode: 'create' | 'edit' = 'create';
  export let saving = false;
  export let error = '';

  type EnvRow = { key: string; value: string };

  let id = initial.id ?? '';
  let displayName = initial.display_name ?? '';
  let transport: 'stdio' | 'http' = (initial.transport as 'stdio' | 'http') ?? 'stdio';
  let runtimeMode = initial.runtime_mode ?? 'shared_global';
  let command = initial.stdio?.command ?? '';
  let args: string[] = initial.stdio?.args ?? [];
  let argsRaw = args.join(' ');
  let env: EnvRow[] = parseEnv(initial.stdio?.env ?? []);
  let url = initial.http?.url ?? '';
  let authHeader = initial.http?.auth_header ?? '';
  let credentialRef = initial.auth?.secret_ref ?? '';

  let secretRefs: { value: string; label: string }[] = [];
  let secretsState: 'loading' | 'ready' | 'unavailable' = 'loading';

  const dispatch = createEventDispatcher<{ submit: Partial<ServerSpec>; cancel: void }>();

  // Load credentials once on mount so the picker is populated.
  onMount(async () => {
    try {
      const secrets: SecretRef[] = await api.listSecrets();
      // The picker stores only the secret name — the tenant is resolved
      // from the request identity at injection time.
      secretRefs = secrets.map((s) => ({ value: s.name, label: s.name }));
      secretsState = 'ready';
    } catch (e) {
      if (isFeatureUnavailable(e)) secretsState = 'unavailable';
      else secretsState = 'ready';
    }
  });

  // Quoted-string aware splitter so `--message "hello world"` parses to two
  // tokens, not three. Backslash-escapes inside quotes are preserved as-is.
  function splitArgs(s: string): string[] {
    const out: string[] = [];
    let buf = '';
    let inSingle = false;
    let inDouble = false;
    for (let i = 0; i < s.length; i++) {
      const ch = s[i];
      if (ch === '"' && !inSingle) {
        inDouble = !inDouble;
        continue;
      }
      if (ch === "'" && !inDouble) {
        inSingle = !inSingle;
        continue;
      }
      if (/\s/.test(ch) && !inSingle && !inDouble) {
        if (buf) {
          out.push(buf);
          buf = '';
        }
        continue;
      }
      buf += ch;
    }
    if (buf) out.push(buf);
    return out;
  }

  function parseEnv(rows: string[]): EnvRow[] {
    return rows.map((r) => {
      const i = r.indexOf('=');
      if (i < 0) return { key: r, value: '' };
      return { key: r.slice(0, i), value: r.slice(i + 1) };
    });
  }

  function envToList(rows: EnvRow[]): string[] {
    return rows.filter((r) => r.key.trim() !== '').map((r) => `${r.key}=${r.value}`);
  }

  function addEnvRow() {
    env = [...env, { key: '', value: '' }];
  }

  function removeEnvRow(i: number) {
    env = env.filter((_, idx) => idx !== i);
  }

  $: transportOptions = [
    { value: 'stdio', label: 'stdio' },
    { value: 'http', label: 'http' }
  ];

  $: runtimeModeOptions =
    transport === 'http'
      ? [{ value: 'remote_static', label: 'remote_static' }]
      : [
          { value: 'shared_global', label: 'shared_global' },
          { value: 'per_tenant', label: 'per_tenant' },
          { value: 'per_user', label: 'per_user' },
          { value: 'per_session', label: 'per_session' }
        ];

  // Reset runtime mode when switching transports — http forces remote_static.
  $: if (transport === 'http' && runtimeMode !== 'remote_static') {
    runtimeMode = 'remote_static';
  }
  $: if (transport === 'stdio' && runtimeMode === 'remote_static') {
    runtimeMode = 'shared_global';
  }

  $: credentialOptions = [{ value: '', label: $t('servers.field.credential.none') }, ...secretRefs];

  function handleSubmit() {
    const tokens = splitArgs(argsRaw);
    // The form emits a Partial<ServerSpec> — the storage layer fills in
    // status / enabled / updated_at on upsert. Type narrowed here so
    // strict mode doesn't flag the optional fields.
    const payload: Partial<ServerSpec> & { id: string; transport: 'stdio' | 'http' } = {
      id,
      display_name: displayName || id,
      transport,
      runtime_mode: runtimeMode
    };
    if (transport === 'stdio') {
      payload.stdio = {
        command,
        args: tokens,
        env: envToList(env)
      };
    } else {
      payload.http = { url };
      if (authHeader) payload.http.auth_header = authHeader;
    }
    if (credentialRef) {
      // The picker stores the vault entry name; the supervisor resolves
      // it under the request's tenant. `secret_reference` is the
      // injector strategy (internal/secrets/inject).
      payload.auth = { strategy: 'secret_reference', secret_ref: credentialRef };
    }
    dispatch('submit', payload);
  }
</script>

<form class="grid" on:submit|preventDefault={handleSubmit}>
  <div class="row">
    <Input
      bind:value={id}
      label={$t('servers.field.id')}
      placeholder={$t('servers.field.id.placeholder')}
      required
      disabled={mode === 'edit'}
      block={false}
    />
    <Input
      bind:value={displayName}
      label={$t('servers.field.displayName')}
      placeholder={$t('servers.field.displayName.placeholder')}
      block={false}
    />
  </div>
  <div class="row">
    <Select
      bind:value={transport}
      label={$t('servers.field.transport')}
      options={transportOptions}
    />
    <Select
      bind:value={runtimeMode}
      label={$t('servers.field.runtimeMode')}
      options={runtimeModeOptions}
    />
  </div>

  {#if transport === 'stdio'}
    <Input
      bind:value={command}
      label={$t('servers.field.command')}
      placeholder={$t('servers.field.command.placeholder')}
      required
      block
    />
    <Input
      bind:value={argsRaw}
      label={$t('servers.field.args')}
      placeholder={$t('servers.field.args.placeholder')}
      block
    />
    <p class="hint">{$t('servers.field.args.hint')}</p>

    <fieldset class="env-table">
      <legend class="env-title">
        {$t('servers.field.env')}
        <Badge tone="neutral" mono>{envToList(env).length}</Badge>
      </legend>
      {#if env.length === 0}
        <p class="hint">{$t('servers.field.env.empty')}</p>
      {/if}
      {#each env as row, i (i)}
        <div class="env-row">
          <Input
            bind:value={row.key}
            placeholder={$t('servers.field.env.key.placeholder')}
            block={false}
            label=""
          />
          <Input
            bind:value={row.value}
            placeholder={$t('servers.field.env.value.placeholder')}
            block={false}
            label=""
          />
          <Button
            type="button"
            size="sm"
            variant="ghost"
            on:click={() => removeEnvRow(i)}
            ariaLabel={$t('common.delete')}
          >
            <IconTrash slot="leading" size={14} />
          </Button>
        </div>
      {/each}
      <Button type="button" size="sm" variant="secondary" on:click={addEnvRow}>
        <IconPlus slot="leading" size={14} />
        {$t('servers.field.env.add')}
      </Button>
    </fieldset>
  {:else}
    <Input
      bind:value={url}
      label={$t('servers.field.url')}
      placeholder={$t('servers.field.url.placeholder')}
      required
      block
    />
    <Input
      bind:value={authHeader}
      label={$t('servers.field.authHeader')}
      placeholder={$t('servers.field.authHeader.placeholder')}
      block
    />
    <p class="hint">{$t('servers.field.authHeader.hint')}</p>
  {/if}

  <Select
    bind:value={credentialRef}
    label={$t('servers.field.credential')}
    options={credentialOptions}
    disabled={secretsState === 'unavailable'}
  />
  {#if secretsState === 'unavailable'}
    <p class="hint">{$t('servers.field.credential.unavailable')}</p>
  {:else if secretRefs.length === 0}
    <p class="hint">{$t('servers.field.credential.none.hint')}</p>
  {:else}
    <p class="hint">{$t('servers.field.credential.hint')}</p>
  {/if}

  {#if error}<p class="error" role="alert">{error}</p>{/if}

  <div class="actions">
    <Button type="button" variant="ghost" on:click={() => dispatch('cancel')}>
      {$t('common.cancel')}
    </Button>
    <Button type="submit" loading={saving}>
      {mode === 'create' ? $t('servers.action.saveStart') : $t('common.save')}
    </Button>
  </div>
</form>

<style>
  .grid {
    display: grid;
    gap: var(--space-4);
    max-width: 720px;
  }
  .row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
  }
  @media (max-width: 640px) {
    .row {
      grid-template-columns: 1fr;
    }
  }
  .env-table {
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-3);
    display: grid;
    gap: var(--space-2);
    margin: 0;
  }
  .env-title {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-2);
    font-size: var(--font-size-body-sm);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-secondary);
  }
  .env-row {
    display: grid;
    grid-template-columns: 1fr 1fr auto;
    gap: var(--space-2);
    align-items: end;
  }
  .hint {
    margin: 0;
    font-size: var(--font-size-body-sm);
    color: var(--color-text-tertiary);
  }
  .error {
    color: var(--color-danger);
    margin: 0;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
</style>
