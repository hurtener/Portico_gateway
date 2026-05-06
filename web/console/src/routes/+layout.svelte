<script lang="ts">
  import '$lib/tokens.css';
  import { page } from '$app/stores';

  $: pathname = $page.url.pathname;
  $: isActive = (prefix: string) =>
    prefix === '/' ? pathname === '/' : pathname.startsWith(prefix);
</script>

<div class="app">
  <header class="header">
    <a class="brand" href="/">Portico</a>
    <nav>
      <a href="/servers" class:active={isActive('/servers')}>Servers</a>
      <a href="/resources" class:active={isActive('/resources')}>Resources</a>
      <a href="/prompts" class:active={isActive('/prompts')}>Prompts</a>
      <a href="/apps" class:active={isActive('/apps')}>Apps</a>
      <a href="/skills" class:active={isActive('/skills')}>Skills</a>
      <a href="/sessions" class:active={isActive('/sessions')}>Sessions</a>
      <a href="/approvals" class:active={isActive('/approvals')}>Approvals</a>
      <a href="/audit" class:active={isActive('/audit')}>Audit</a>
      <a href="/admin/secrets" class:active={isActive('/admin/secrets')}>Secrets</a>
    </nav>
    <div class="meta">
      <span class="badge">v0.5</span>
    </div>
  </header>

  <main>
    <slot />
  </main>

  <footer>Portico Console</footer>
</div>

<style>
  :global(html, body) {
    margin: 0;
    padding: 0;
    background: var(--color-bg);
    color: var(--color-text);
    font-family: var(--font-sans);
    font-size: var(--text-base);
    line-height: var(--line-normal);
  }

  :global(*, *::before, *::after) {
    box-sizing: border-box;
  }

  :global(a) {
    color: var(--color-brand);
    text-decoration: none;
  }
  :global(a:hover) {
    color: var(--color-brand-hover);
  }

  .app {
    display: flex;
    flex-direction: column;
    min-height: 100vh;
  }

  .header {
    display: flex;
    align-items: center;
    gap: var(--space-6);
    padding: 0 var(--space-6);
    height: var(--layout-header-height);
    border-bottom: 1px solid var(--color-border);
    background: var(--color-surface);
  }

  .brand {
    font-weight: var(--weight-semibold);
    font-size: var(--text-lg);
    color: var(--color-text);
  }

  nav {
    display: flex;
    gap: var(--space-4);
    flex: 1;
  }
  nav a {
    color: var(--color-text-muted);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
  }
  nav a.active {
    color: var(--color-text);
    background: var(--color-surface-2);
  }

  .meta {
    display: flex;
    gap: var(--space-2);
    align-items: center;
  }
  .badge {
    font-size: var(--text-xs);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-pill);
    background: var(--color-warning-soft);
    color: var(--color-warning);
    font-weight: var(--weight-medium);
  }

  main {
    flex: 1;
    width: 100%;
    max-width: var(--layout-max-width);
    margin: 0 auto;
    padding: var(--space-8) var(--space-6);
  }

  footer {
    border-top: 1px solid var(--color-border);
    padding: var(--space-4) var(--space-6);
    color: var(--color-text-subtle);
    font-size: var(--text-sm);
  }
</style>
