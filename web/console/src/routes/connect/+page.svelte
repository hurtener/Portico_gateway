<script lang="ts">
  /**
   * Connect — Phase 10.9.
   *
   * The page that answers "how do I point an MCP client at this
   * gateway?" Surfaces the bind URL + auth requirement (from the
   * public /api/gateway/info endpoint), then turns those facts into
   * three copy-paste snippets:
   *   - Claude Desktop / generic MCP client config block
   *   - npx @modelcontextprotocol/inspector command
   *   - curl tools/list with the right headers
   *
   * The auth section reads dev-mode vs JWT-mode from the live response
   * and surfaces the issuer / audiences / JWKS URL so a key-management
   * person can verify what the gateway expects.
   */
  import { onMount } from 'svelte';
  import { api, type GatewayInfo, type ServerSummary } from '$lib/api';
  import {
    Badge,
    Button,
    EmptyState,
    KeyValueGrid,
    MetricStrip,
    PageActionGroup,
    PageHeader,
    toast
  } from '$lib/components';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconCopy from 'lucide-svelte/icons/copy';
  import IconPlug from 'lucide-svelte/icons/plug';
  import IconShield from 'lucide-svelte/icons/shield';
  import IconUser from 'lucide-svelte/icons/user';
  import IconServer from 'lucide-svelte/icons/server';
  import type { ComponentType } from 'svelte';

  let info: GatewayInfo | null = null;
  let servers: ServerSummary[] = [];
  let loading = true;
  let error = '';

  async function refresh() {
    loading = true;
    error = '';
    try {
      const [g, s] = await Promise.all([
        api.gatewayInfo(),
        api.listServers().catch(() => [] as ServerSummary[])
      ]);
      info = g;
      servers = s ?? [];
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  /**
   * The bind in portico.yaml is "host:port". For most local dev runs
   * it's "127.0.0.1:8080" — fine to drop straight into a URL. For
   * "0.0.0.0:8080" we substitute the operator's actual hostname so
   * the snippet is usable from another box. window.location.host is
   * the closest thing we have to "where the operator is reading this
   * page from", so it's a reasonable substitute.
   */
  function endpointURL(g: GatewayInfo | null): string {
    if (!g) return '';
    const bind = g.bind || '';
    let host = bind;
    if (host.startsWith('0.0.0.0:')) {
      const port = host.slice('0.0.0.0:'.length);
      const base = typeof window !== 'undefined' ? window.location.hostname : 'localhost';
      host = `${base}:${port}`;
    }
    return `http://${host}${g.mcp_path}`;
  }

  function endpointURLForServer(g: GatewayInfo | null): string {
    return endpointURL(g);
  }

  $: url = endpointURL(info);

  // === Snippet builders ==============================================

  function claudeDesktopSnippet(g: GatewayInfo | null, u: string): string {
    if (!g) return '';
    const headers: Record<string, string> = {};
    if (g.auth.mode === 'jwt') {
      headers['Authorization'] = 'Bearer <YOUR_JWT_HERE>';
    }
    const config = {
      mcpServers: {
        portico: {
          transport: 'http',
          url: u,
          ...(Object.keys(headers).length > 0 ? { headers } : {})
        }
      }
    };
    return JSON.stringify(config, null, 2);
  }

  function inspectorSnippet(u: string): string {
    return `npx @modelcontextprotocol/inspector --transport http ${u}`;
  }

  function curlSnippet(g: GatewayInfo | null, u: string): string {
    if (!g) return '';
    const lines: string[] = [];
    lines.push(`curl -X POST '${u}' \\`);
    lines.push(`  -H 'Content-Type: application/json' \\`);
    lines.push(`  -H 'Accept: application/json, text/event-stream' \\`);
    if (g.auth.mode === 'jwt') {
      lines.push(`  -H 'Authorization: Bearer <YOUR_JWT_HERE>' \\`);
    } else {
      lines.push(`  # dev mode: no Authorization header required (tenant=${g.dev_tenant ?? 'default'})`);
    }
    lines.push(
      `  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'`
    );
    return lines.join('\n');
  }

  $: claudeText = claudeDesktopSnippet(info, url);
  $: inspectorText = inspectorSnippet(url);
  $: curlText = curlSnippet(info, url);

  async function copy(text: string, what: string) {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      toast.success($t('connect.toast.copied', { what }));
    } catch {
      toast.danger($t('connect.toast.copyFailed'), '');
    }
  }

  // === KPI strip =====================================================

  $: pageActions = [
    {
      label: $t('common.refresh'),
      icon: IconRefreshCw,
      onClick: () => refresh(),
      loading
    }
  ];

  $: metrics = info
    ? [
        {
          id: 'endpoint',
          label: $t('connect.metric.endpoint'),
          value: info.bind,
          helper: info.mcp_path,
          icon: IconPlug as ComponentType<any>,
          tone: 'brand' as const
        },
        {
          id: 'auth',
          label: $t('connect.metric.auth'),
          value: info.auth.mode,
          helper: info.auth.mode === 'dev'
            ? $t('connect.metric.auth.devHelper')
            : $t('connect.metric.auth.jwtHelper'),
          icon: IconShield as ComponentType<any>,
          tone: info.auth.mode === 'dev' ? ('warning' as const) : ('success' as const),
          attention: info.auth.mode === 'dev'
        },
        {
          id: 'tenant',
          label: $t('connect.metric.tenant'),
          value: info.dev_mode
            ? info.dev_tenant ?? 'default'
            : info.auth.tenant_claim ?? 'tenant',
          helper: info.dev_mode
            ? $t('connect.metric.tenant.devHelper')
            : $t('connect.metric.tenant.jwtHelper'),
          icon: IconUser as ComponentType<any>
        },
        {
          id: 'servers',
          label: $t('connect.metric.servers'),
          value: servers.length.toString(),
          helper: $t('connect.metric.servers.helper'),
          icon: IconServer as ComponentType<any>,
          attention: servers.length === 0,
          onClick: () => {
            if (typeof window !== 'undefined') window.location.href = '/servers';
          }
        }
      ]
    : [];
</script>

<PageHeader title={$t('connect.title')} compact>
  <div slot="actions">
    <PageActionGroup actions={pageActions} />
  </div>
</PageHeader>

{#if error}<p class="error">{error}</p>{/if}

{#if !info && loading}
  <p class="muted">{$t('common.loading')}</p>
{:else if !info}
  <EmptyState
    title={$t('connect.error.title')}
    description={error || $t('connect.error.description')}
  />
{:else}
  <MetricStrip {metrics} compact label={$t('connect.metric.aria')} />

  <section id="quickstart" class="card">
    <header class="card-head">
      <h4>{$t('connect.section.quickstart')}</h4>
      <p class="muted body">{$t('connect.section.quickstartHelp')}</p>
    </header>

    <article class="snippet">
      <header class="snippet-head">
        <h5>{$t('connect.snippet.claude.title')}</h5>
        <Button variant="secondary" size="sm" on:click={() => copy(claudeText, $t('connect.snippet.claude.what'))}>
          <IconCopy slot="leading" size={14} />
          {$t('common.copy')}
        </Button>
      </header>
      <p class="muted body">{$t('connect.snippet.claude.help')}</p>
      <pre class="raw"><code>{claudeText}</code></pre>
    </article>

    <article class="snippet">
      <header class="snippet-head">
        <h5>{$t('connect.snippet.inspector.title')}</h5>
        <Button variant="secondary" size="sm" on:click={() => copy(inspectorText, $t('connect.snippet.inspector.what'))}>
          <IconCopy slot="leading" size={14} />
          {$t('common.copy')}
        </Button>
      </header>
      <p class="muted body">{$t('connect.snippet.inspector.help')}</p>
      <pre class="raw"><code>{inspectorText}</code></pre>
    </article>

    <article class="snippet">
      <header class="snippet-head">
        <h5>{$t('connect.snippet.curl.title')}</h5>
        <Button variant="secondary" size="sm" on:click={() => copy(curlText, $t('connect.snippet.curl.what'))}>
          <IconCopy slot="leading" size={14} />
          {$t('common.copy')}
        </Button>
      </header>
      <p class="muted body">{$t('connect.snippet.curl.help')}</p>
      <pre class="raw"><code>{curlText}</code></pre>
    </article>
  </section>

  <section id="auth" class="card">
    <h4>{$t('connect.section.auth')}</h4>
    {#if info.dev_mode}
      <div class="callout warn">
        <strong>{$t('connect.auth.devTitle')}</strong>
        <p class="body">
          {$t('connect.auth.devBody', { tenant: info.dev_tenant ?? 'default' })}
        </p>
      </div>
    {:else}
      <KeyValueGrid
        items={[
          { label: $t('connect.auth.mode'), value: 'JWT (asymmetric)' },
          { label: $t('connect.auth.issuer'), value: info.auth.issuer || '—' },
          {
            label: $t('connect.auth.audiences'),
            value: (info.auth.audiences ?? []).join(', ') || '—'
          },
          { label: $t('connect.auth.jwksUrl'), value: info.auth.jwks_url || '—' },
          { label: $t('connect.auth.tenantClaim'), value: info.auth.tenant_claim || 'tenant' },
          { label: $t('connect.auth.scopeClaim'), value: info.auth.scope_claim || 'scope' }
        ]}
        columns={1}
      />
      <p class="muted body">
        {$t('connect.auth.tenantsHint')}
        <a href="/admin/tenants">{$t('nav.tenants')}</a>.
      </p>
    {/if}
  </section>

  <section class="card">
    <h4>{$t('connect.section.headers')}</h4>
    <p class="muted body">{$t('connect.headers.description')}</p>
    <ul class="header-list">
      <li>
        <code class="mono">Origin</code> —
        {$t('connect.headers.origin')}
      </li>
      <li>
        <code class="mono">Authorization</code> —
        {$t('connect.headers.auth')}
      </li>
      <li>
        <code class="mono">Mcp-Session-Id</code> —
        {$t('connect.headers.session')}
      </li>
      <li>
        <code class="mono">Accept</code> —
        {$t('connect.headers.accept')}
      </li>
    </ul>
  </section>

  {#if servers.length === 0}
    <section class="card">
      <h4>{$t('connect.section.firstServer')}</h4>
      <p class="muted body">{$t('connect.firstServer.body')}</p>
      <div class="actions-row">
        <Button variant="primary" on:click={() => (window.location.href = '/servers/new')}>
          {$t('connect.firstServer.action')}
        </Button>
      </div>
    </section>
  {/if}
{/if}

<style>
  .error {
    color: var(--color-danger);
    margin: 0 0 var(--space-4) 0;
    font-size: var(--font-size-body-sm);
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
  .card {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-5);
    margin-top: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
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
  .card-head {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .snippet {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .snippet-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .snippet h5 {
    margin: 0;
    font-size: var(--font-size-body-sm);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
  }
  .raw {
    margin: 0;
    max-height: 360px;
    overflow: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    background: var(--color-bg-subtle);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: var(--space-3);
    color: var(--color-text-primary);
    white-space: pre;
    word-break: break-all;
  }
  .callout {
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border-soft);
    background: var(--color-bg-canvas);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .callout.warn {
    border-color: var(--color-warning);
    background: var(--color-warning-soft);
  }
  .callout.warn strong {
    color: var(--color-warning);
  }
  .callout p {
    margin: 0;
    color: var(--color-text-secondary);
  }
  .header-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    font-size: var(--font-size-body-sm);
    color: var(--color-text-secondary);
  }
  .header-list li {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-2);
  }
  .actions-row {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-2);
  }
</style>
