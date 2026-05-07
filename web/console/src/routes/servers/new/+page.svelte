<script lang="ts">
  import { goto } from '$app/navigation';
  import { api, type ServerSpec } from '$lib/api';
  import { PageHeader, ServerForm, toast } from '$lib/components';
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
      // upsertServer hits /v1/servers; the back-end accepts the same
      // payload on /api/servers POST when the spec carries an `id`.
      await api.upsertServer(spec);
      const id = spec.id as string;
      toast.success($t('crud.createdToast'), id);
      // Best-effort: poll health for up to 3 s so the operator sees the
      // supervisor's acknowledgement before navigating away.
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
        // 503 or absent supervisor — fall through; the detail page will
        // surface the actual status. Best-effort.
      }
      await new Promise((r) => setTimeout(r, 250));
    }
  }

  function onCancel() {
    void goto('/servers');
  }
</script>

<PageHeader title={$t('servers.new.title')} description={$t('servers.new.subtitle')} />

<ServerForm mode="create" {saving} {error} on:submit={onSubmit} on:cancel={onCancel} />
