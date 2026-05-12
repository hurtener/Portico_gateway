<script lang="ts">
  /**
   * New server — Phase 10.8 Step 5 form-page sub-vocabulary.
   *
   * Thin wrapper around ServerForm; this rewrite adds Breadcrumbs
   * back to /servers and a compact PageHeader so the form page reads
   * the same as every other operator entry point. ServerForm itself
   * is out of scope (its own audit comes in a follow-up).
   */
  import { goto } from '$app/navigation';
  import { api, type ServerSpec } from '$lib/api';
  import { Breadcrumbs, PageHeader, ServerForm, toast } from '$lib/components';
  import { t } from '$lib/i18n';

  let saving = false;
  let error = '';

  async function onSubmit(e: CustomEvent<Partial<ServerSpec>>) {
    saving = true;
    error = '';
    const spec = e.detail;
    if (!spec.id) {
      error = 'Missing id';
      saving = false;
      return;
    }
    try {
      await api.upsertServer(spec);
      const id = spec.id as string;
      toast.success($t('crud.createdToast'), id);
      await waitForReady(id, 3000);
      void goto(`/servers/${encodeURIComponent(id)}`);
    } catch (err) {
      error = (err as Error).message;
    } finally {
      saving = false;
    }
  }

  async function waitForReady(id: string, timeoutMs: number): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      try {
        const h = await api.serverHealth(id);
        if (h.status === 'ready' || h.status === 'running' || h.status === 'healthy') {
          return;
        }
      } catch {
        // 503 or absent supervisor — fall through.
      }
      await new Promise((r) => setTimeout(r, 250));
    }
  }

  function onCancel() {
    void goto('/servers');
  }
</script>

<PageHeader title={$t('servers.new.title')} compact>
  <Breadcrumbs
    slot="breadcrumbs"
    items={[{ label: $t('nav.servers'), href: '/servers' }, { label: $t('servers.new.title') }]}
  />
</PageHeader>

<ServerForm mode="create" {saving} {error} on:submit={onSubmit} on:cancel={onCancel} />
