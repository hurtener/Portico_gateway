<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type SecretRef } from '$lib/api';

  let secrets: SecretRef[] = [];
  let loading = true;
  let error = '';
  let formTenant = '';
  let formName = '';
  let formValue = '';
  let saving = false;

  async function refresh() {
    loading = true;
    try {
      secrets = await api.listSecrets();
      error = '';
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  async function createSecret() {
    if (!formTenant || !formName || !formValue) {
      error = 'tenant, name, and value are all required';
      return;
    }
    saving = true;
    try {
      await api.putSecret(formTenant, formName, formValue);
      formValue = '';
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      saving = false;
    }
  }

  async function deleteSecret(s: SecretRef) {
    if (!confirm(`Delete ${s.tenant_id}/${s.name}? This cannot be undone.`)) return;
    try {
      await api.deleteSecret(s.tenant_id, s.name);
      await refresh();
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(refresh);
</script>

<header class="page-head">
  <h1>Vault secrets (admin)</h1>
</header>

<p class="muted">
  Values are never displayed. The vault stores AES-256-GCM-encrypted payloads with HKDF-derived
  per-value keys; only references (tenant + name) are listed here.
</p>

{#if error}
  <p class="error">{error}</p>
{/if}

<section class="form-card">
  <h2>Add secret</h2>
  <form on:submit|preventDefault={createSecret}>
    <label>
      <span>Tenant</span>
      <input type="text" bind:value={formTenant} placeholder="acme" required />
    </label>
    <label>
      <span>Name</span>
      <input type="text" bind:value={formName} placeholder="github_token" required />
    </label>
    <label>
      <span>Value</span>
      <input type="password" bind:value={formValue} placeholder="•••••••••" required />
    </label>
    <button class="btn" type="submit" disabled={saving}>Save</button>
  </form>
</section>

<section>
  <h2>Existing secrets</h2>
  {#if loading && secrets.length === 0}
    <p class="muted">Loading…</p>
  {:else if secrets.length === 0}
    <p class="muted">No secrets stored.</p>
  {:else}
    <table>
      <thead>
        <tr>
          <th>Tenant</th>
          <th>Name</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each secrets as s, i (i)}
          <tr>
            <td>{s.tenant_id}</td>
            <td><code>{s.name}</code></td>
            <td>
              <button class="btn btn-danger" on:click={() => deleteSecret(s)}>Delete</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</section>

<style>
  .page-head {
    margin-bottom: var(--space-4);
  }
  .form-card {
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    margin-bottom: var(--space-6);
  }
  .form-card form {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr auto;
    gap: var(--space-3);
    align-items: end;
  }
  .form-card label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .form-card label span {
    font-size: var(--font-sm);
    color: var(--color-text-muted);
  }
  input {
    padding: var(--space-1) var(--space-2);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  th,
  td {
    padding: var(--space-2) var(--space-3);
    text-align: left;
    border-bottom: 1px solid var(--color-border);
  }
  .muted {
    color: var(--color-text-muted);
  }
  .error {
    color: var(--color-danger);
  }
  .btn {
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    cursor: pointer;
  }
  .btn-danger {
    background: var(--color-danger);
    color: var(--color-on-danger);
    border-color: var(--color-danger);
  }
</style>
