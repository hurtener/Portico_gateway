<script lang="ts">
  import Popover from './Popover.svelte';
  import { t } from '$lib/i18n';
  import IconLogOut from 'lucide-svelte/icons/log-out';
  import IconShieldCheck from 'lucide-svelte/icons/shield-check';

  // Placeholder identity. Phase 12 wires this to the JWT subject.
  const displayName = 'Admin';
  const role = 'local';
  const initials = displayName
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase() ?? '')
    .join('');

  let open = false;
</script>

<Popover placement="bottom-end" bind:open>
  <span slot="trigger" let:toggle>
    <button
      type="button"
      class="avatar"
      aria-label={$t('topbar.profile.role.admin')}
      aria-haspopup="menu"
      on:click={toggle}
    >
      <span class="initials" aria-hidden="true">{initials}</span>
    </button>
  </span>

  <div class="panel">
    <div class="hd">
      <span class="name">{displayName}</span>
      <span class="role">{role}</span>
    </div>
    <div class="rule" role="separator"></div>
    <div class="rbac" title={$t('topbar.profile.signOut.disabled')}>
      <span class="rbac-ico" aria-hidden="true"><IconShieldCheck size={14} /></span>
      <span class="rbac-msg">{$t('topbar.profile.signOut.disabled')}</span>
    </div>
    <button type="button" class="item disabled" disabled aria-disabled="true">
      <IconLogOut size={14} />
      <span>{$t('topbar.profile.signOut')}</span>
    </button>
  </div>
</Popover>

<style>
  .avatar {
    appearance: none;
    background: var(--color-accent-primary-subtle);
    border: 1px solid var(--color-accent-primary-soft);
    color: var(--color-accent-primary);
    width: 32px;
    height: 32px;
    border-radius: 999px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition:
      background var(--motion-fast) var(--ease-default),
      box-shadow var(--motion-fast) var(--ease-default);
  }
  .avatar:hover {
    background: var(--color-accent-primary-soft);
  }
  .avatar:focus-visible {
    outline: none;
    box-shadow: var(--ring-focus);
  }
  .initials {
    font-family: var(--font-sans);
    font-size: var(--font-size-label);
    font-weight: var(--font-weight-semibold);
    letter-spacing: 0.02em;
  }
  .panel {
    width: 240px;
    padding: var(--space-2);
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .hd {
    display: flex;
    flex-direction: column;
    padding: var(--space-2) var(--space-3) var(--space-1);
  }
  .name {
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
  }
  .role {
    font-family: var(--font-mono);
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
  }
  .rule {
    height: 1px;
    background: var(--color-border-soft);
    margin: var(--space-1) 0;
  }
  .rbac {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
    line-height: var(--font-line-label);
  }
  .rbac-ico {
    display: inline-flex;
    margin-top: 2px;
    color: var(--color-icon-subtle);
    flex-shrink: 0;
  }
  .rbac-msg {
    flex: 1;
  }
  .item {
    appearance: none;
    background: transparent;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
    color: var(--color-text-primary);
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    text-align: left;
  }
  .item:hover:not([disabled]) {
    background: var(--color-bg-subtle);
  }
  .item.disabled {
    color: var(--color-text-muted);
    cursor: not-allowed;
  }
</style>
