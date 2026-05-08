<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    Badge,
    Button,
    CodeBlock,
    EmptyState,
    IdBadge,
    Modal,
    PageHeader,
    SchemaForm,
    SegmentedControl,
    Tabs,
    Textarea,
    Toaster
  } from '$lib/components';
  import { pushToast } from '$lib/components/toast';
  import {
    api,
    type PlaygroundSession,
    type PlaygroundCatalog,
    type CorrelationBundle,
    type SnapshotTool,
    type SnapshotResource,
    type SnapshotPrompt,
    type SnapshotSkill
  } from '$lib/api';
  import { t } from '$lib/i18n';
  import IconRefreshCw from 'lucide-svelte/icons/refresh-cw';
  import IconChevronDown from 'lucide-svelte/icons/chevron-down';
  import IconChevronRight from 'lucide-svelte/icons/chevron-right';

  const SESSION_STORAGE_KEY = 'portico.playground.session';

  let session: PlaygroundSession | null = null;
  let catalog: PlaygroundCatalog | null = null;
  let catalogLoading = false;
  let correlation: CorrelationBundle | null = null;
  let composerKind: 'tool_call' | 'resource_read' | 'prompt_get' = 'tool_call';
  let composerTarget = '';
  let composerArgsRaw = '{}';
  let composerArgsForm: Record<string, unknown> = {};
  let composerMode: 'form' | 'raw' = 'form';
  let activeSchema: Record<string, unknown> | null = null;
  let activePromptArgs: SnapshotPrompt['arguments'] | null = null;
  // Resource template variables — set when the selected URI contains
  // {placeholder} segments. Substituted into the URI on Run.
  let resourceTemplateVars: string[] = [];
  // Output model: render mode chosen by composerKind.
  type OutputView =
    | { kind: 'empty' }
    | { kind: 'error'; message: string }
    | { kind: 'tool_result'; raw: string; structured?: unknown; isError?: boolean }
    | { kind: 'resource'; contents: ResourceContent[] }
    | { kind: 'prompt'; description?: string; messages: PromptMessage[] }
    | { kind: 'raw'; text: string };
  type ResourceContent = {
    uri: string;
    mimeType?: string;
    text?: string;
    blob?: string;
  };
  type PromptMessage = {
    role: string;
    content: { type: string; text?: string; data?: string; mimeType?: string };
  };
  let outputView: OutputView = { kind: 'empty' };
  let outputRaw = '';
  let outputShowRaw = false;
  let skillToggleBusy: Record<string, boolean> = {};
  let activeTab: 'trace' | 'audit' | 'policy' | 'drift' = 'trace';
  let pollHandle: ReturnType<typeof setInterval> | null = null;
  let tabs: Array<{ id: 'trace' | 'audit' | 'policy' | 'drift'; label: string }> = [];
  // Catalog rail expansion state — server group is open by default.
  let serverOpen: Record<string, boolean> = {};
  let groupOpen: Record<'resources' | 'prompts' | 'skills', boolean> = {
    resources: true,
    prompts: true,
    skills: false
  };
  let catalogQuery = '';

  $: tabs = [
    { id: 'trace', label: $t('playground.tabs.trace') },
    { id: 'audit', label: $t('playground.tabs.audit') },
    { id: 'policy', label: $t('playground.tabs.policy') },
    { id: 'drift', label: $t('playground.tabs.drift') }
  ];

  $: composerOptions = [
    { value: 'form', label: $t('playground.composer.toolMode') },
    { value: 'raw', label: $t('playground.composer.rawMode') }
  ];

  // Keep raw and form views in sync — form changes overwrite raw, raw
  // changes are parsed back into form state when valid.
  function syncRawFromForm() {
    composerArgsRaw = JSON.stringify(composerArgsForm, null, 2);
  }
  function syncFormFromRaw() {
    try {
      const parsed = JSON.parse(composerArgsRaw || '{}');
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        composerArgsForm = parsed;
      }
    } catch {
      /* leave form state untouched on bad JSON */
    }
  }

  async function startSession(opts: { silent?: boolean } = {}) {
    try {
      session = await api.startPlaygroundSession({});
      try {
        sessionStorage.setItem(SESSION_STORAGE_KEY, session.id);
      } catch {
        /* storage may be disabled — degrade silently */
      }
      if (!opts.silent) {
        pushToast({ tone: 'success', title: $t('playground.session.start') });
      }
      pollCorrelation();
      pollHandle = setInterval(pollCorrelation, 5000);
      await refreshCatalog();
    } catch (err) {
      pushToast({ tone: 'danger', title: $t('playground.error.startFailed') });
      console.error(err);
    }
  }

  async function resumeOrStartSession() {
    let stored: string | null = null;
    try {
      stored = sessionStorage.getItem(SESSION_STORAGE_KEY);
    } catch {
      stored = null;
    }
    if (stored) {
      try {
        session = await api.getPlaygroundSession(stored);
        pollCorrelation();
        pollHandle = setInterval(pollCorrelation, 5000);
        await refreshCatalog();
        return;
      } catch {
        try {
          sessionStorage.removeItem(SESSION_STORAGE_KEY);
        } catch {
          /* noop */
        }
      }
    }
    await startSession();
  }

  async function endSession() {
    if (!session) return;
    try {
      await api.endPlaygroundSession(session.id);
    } finally {
      if (pollHandle) clearInterval(pollHandle);
      try {
        sessionStorage.removeItem(SESSION_STORAGE_KEY);
      } catch {
        /* noop */
      }
      session = null;
      catalog = null;
      correlation = null;
      composerTarget = '';
      composerArgsForm = {};
      composerArgsRaw = '{}';
    }
  }

  async function refreshCatalog() {
    if (!session) return;
    catalogLoading = true;
    try {
      catalog = await api.getPlaygroundCatalog(session.id);
      // Default-open every server group on first load.
      const newOpen: Record<string, boolean> = { ...serverOpen };
      for (const s of catalog.catalog.servers ?? []) {
        if (newOpen[s.id] === undefined) newOpen[s.id] = true;
      }
      serverOpen = newOpen;
    } catch (err) {
      console.error(err);
    } finally {
      catalogLoading = false;
    }
  }

  async function pollCorrelation() {
    if (!session) return;
    try {
      correlation = await api.getPlaygroundCorrelation(session.id);
    } catch (err) {
      console.error(err);
    }
  }

  function selectTool(tool: SnapshotTool) {
    composerKind = 'tool_call';
    composerTarget = tool.namespaced_name;
    activeSchema = (tool.input_schema as Record<string, unknown>) ?? null;
    activePromptArgs = null;
    resourceTemplateVars = [];
    composerArgsForm = {};
    syncRawFromForm();
    composerMode = activeSchema ? 'form' : 'raw';
    resetOutput();
  }

  function selectResource(res: SnapshotResource) {
    composerKind = 'resource_read';
    composerTarget = res.uri;
    activeSchema = null;
    activePromptArgs = null;
    composerArgsForm = {};
    composerArgsRaw = '{}';
    // Detect URI-template placeholders ({foo} or {+foo} per RFC 6570 simple).
    // When present, expose form fields so the operator doesn't have to hand-
    // edit the URI string.
    resourceTemplateVars = extractURITemplateVars(res.uri);
    composerMode = resourceTemplateVars.length > 0 ? 'form' : 'raw';
    if (resourceTemplateVars.length > 0) {
      const seed: Record<string, unknown> = {};
      for (const v of resourceTemplateVars) seed[v] = '';
      composerArgsForm = seed;
      // Synthesize a string-only schema so SchemaForm renders inputs.
      const props: Record<string, { type: string; description?: string }> = {};
      for (const v of resourceTemplateVars) {
        props[v] = { type: 'string', description: `Substituted into {${v}} in the URI` };
      }
      activeSchema = {
        type: 'object',
        properties: props,
        required: resourceTemplateVars
      } as Record<string, unknown>;
      syncRawFromForm();
    } else {
      activeSchema = null;
    }
    resetOutput();
  }

  function extractURITemplateVars(uri: string): string[] {
    const out: string[] = [];
    const seen = new Set<string>();
    const re = /\{[+#./;?&]?([A-Za-z0-9_]+)(?::\d+)?\*?\}/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(uri)) !== null) {
      const name = m[1];
      if (!seen.has(name)) {
        seen.add(name);
        out.push(name);
      }
    }
    return out;
  }

  function applyURITemplate(uri: string, vars: Record<string, unknown>): string {
    return uri.replace(/\{[+#./;?&]?([A-Za-z0-9_]+)(?::\d+)?\*?\}/g, (_, name) => {
      const v = vars[name];
      return v === undefined || v === null ? '' : encodeURIComponent(String(v));
    });
  }

  function selectPrompt(p: SnapshotPrompt) {
    composerKind = 'prompt_get';
    composerTarget = p.namespaced_name;
    // Synthesize a JSON Schema from the prompt arguments so the form
    // renders the same way a tool would.
    activePromptArgs = p.arguments ?? null;
    if (p.arguments && p.arguments.length > 0) {
      const props: Record<string, { type: string; description?: string }> = {};
      const required: string[] = [];
      for (const arg of p.arguments) {
        props[arg.name] = { type: 'string', description: arg.description };
        if (arg.required) required.push(arg.name);
      }
      activeSchema = { type: 'object', properties: props, required } as Record<string, unknown>;
    } else {
      activeSchema = null;
    }
    resourceTemplateVars = [];
    composerArgsForm = {};
    syncRawFromForm();
    composerMode = activeSchema ? 'form' : 'raw';
    resetOutput();
  }

  function resetOutput() {
    outputView = { kind: 'empty' };
    outputRaw = '';
  }

  async function toggleSkill(sk: SnapshotSkill) {
    if (!session) return;
    skillToggleBusy = { ...skillToggleBusy, [sk.id]: true };
    try {
      await api.setPlaygroundSkillEnabled(session.id, sk.id, !sk.enabled_for_session);
      await refreshCatalog();
      pushToast({
        tone: 'success',
        title: sk.enabled_for_session ? `Skill disabled: ${sk.id}` : `Skill enabled: ${sk.id}`
      });
    } catch (err) {
      pushToast({ tone: 'danger', title: `Could not toggle ${sk.id}` });
      console.error(err);
    } finally {
      skillToggleBusy = { ...skillToggleBusy, [sk.id]: false };
    }
  }

  async function runCall() {
    if (!session || !composerTarget) return;
    resetOutput();
    let argsParsed: unknown = composerArgsForm;
    if (composerMode === 'raw') {
      try {
        argsParsed = JSON.parse(composerArgsRaw || '{}');
      } catch {
        pushToast({ tone: 'danger', title: 'Invalid JSON in args' });
        return;
      }
    }
    let target = composerTarget;
    // resource_read with template vars: substitute and clear args so the
    // dispatcher receives a concrete URI.
    if (composerKind === 'resource_read' && resourceTemplateVars.length > 0) {
      target = applyURITemplate(composerTarget, (argsParsed as Record<string, unknown>) ?? {});
      argsParsed = {};
    }
    try {
      const env = await api.issuePlaygroundCall(session.id, {
        kind: composerKind,
        target,
        arguments: argsParsed
      });
      const url = `/api/playground/sessions/${encodeURIComponent(session.id)}/calls/${encodeURIComponent(env.call_id)}/stream`;
      const es = new EventSource(url);
      let chunkBuf = '';
      es.addEventListener('chunk', (e) => {
        chunkBuf += (e as MessageEvent).data + '\n';
      });
      es.addEventListener('error', (e) => {
        const msg = (e as MessageEvent).data;
        outputView = { kind: 'error', message: msg ? String(msg) : 'Call failed.' };
        outputRaw = msg ? String(msg) : '';
        es.close();
      });
      es.addEventListener('end', () => {
        es.close();
        outputRaw = chunkBuf;
        outputView = parseChunkOutput(chunkBuf, composerKind);
        pollCorrelation();
      });
    } catch (err) {
      pushToast({ tone: 'danger', title: $t('playground.error.callFailed') });
      console.error(err);
    }
  }

  // parseChunkOutput unwraps the {call_id, result} envelope the playground
  // adapter emits and routes to a kind-specific render mode. Falls back to
  // raw text when the JSON is unrecognised (defensive — protects against a
  // future adapter change).
  function parseChunkOutput(buf: string, kind: typeof composerKind): OutputView {
    const trimmed = buf.trim();
    if (!trimmed) return { kind: 'empty' };
    let envelope: { call_id?: string; result?: unknown };
    try {
      envelope = JSON.parse(trimmed);
    } catch {
      return { kind: 'raw', text: buf };
    }
    const result = envelope.result;
    if (!result || typeof result !== 'object') {
      return { kind: 'raw', text: buf };
    }
    if (kind === 'resource_read') {
      const r = result as { contents?: ResourceContent[] };
      return { kind: 'resource', contents: Array.isArray(r.contents) ? r.contents : [] };
    }
    if (kind === 'prompt_get') {
      const r = result as { description?: string; messages?: PromptMessage[] };
      return {
        kind: 'prompt',
        description: r.description,
        messages: Array.isArray(r.messages) ? r.messages : []
      };
    }
    // tool_call default
    const r = result as { content?: unknown; isError?: boolean; structuredContent?: unknown };
    return {
      kind: 'tool_result',
      raw: JSON.stringify(r.content ?? r, null, 2),
      structured: r.structuredContent,
      isError: r.isError
    };
  }

  // downloadBlob decodes a base64 blob and triggers a browser download.
  // Used by the binary branch of the resource view.
  function downloadBlob(content: ResourceContent) {
    if (!content.blob) return;
    try {
      const bin = atob(content.blob);
      const buf = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i);
      const blob = new Blob([buf], { type: content.mimeType ?? 'application/octet-stream' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filenameFromURI(content.uri) ?? 'resource.bin';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err) {
      pushToast({ tone: 'danger', title: 'Download failed' });
      console.error(err);
    }
  }

  function filenameFromURI(uri: string): string | undefined {
    const tail = uri.split('/').pop() ?? '';
    return tail || undefined;
  }

  function blobSize(blob: string): number {
    // base64 length → byte count, accounting for padding.
    const pad = blob.endsWith('==') ? 2 : blob.endsWith('=') ? 1 : 0;
    return Math.floor((blob.length * 3) / 4) - pad;
  }

  function fmtBytes(n: number): string {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }

  function mimeToLang(mime?: string): string {
    if (!mime) return 'text';
    if (mime.includes('json')) return 'json';
    if (mime.includes('yaml') || mime.includes('yml')) return 'yaml';
    if (mime.includes('html')) return 'html';
    if (mime.includes('javascript') || mime.includes('typescript')) return 'javascript';
    if (mime.includes('markdown')) return 'markdown';
    if (mime.includes('xml')) return 'xml';
    return 'text';
  }

  let saveModalOpen = false;
  let saveName = '';
  let saveDesc = '';
  async function saveAsCase() {
    if (!composerTarget) return;
    try {
      let argsParsed: unknown = composerArgsForm;
      if (composerMode === 'raw') {
        try {
          argsParsed = JSON.parse(composerArgsRaw || '{}');
        } catch {
          /* ignore — save raw text as-is below */
        }
      }
      await api.createPlaygroundCase({
        name: saveName || composerTarget,
        description: saveDesc,
        kind: composerKind,
        target: composerTarget,
        payload: argsParsed,
        tags: []
      });
      saveModalOpen = false;
      saveName = '';
      saveDesc = '';
      pushToast({ tone: 'success', title: $t('playground.composer.saveAsCase') });
    } catch (err) {
      pushToast({ tone: 'danger', title: 'Save failed' });
      console.error(err);
    }
  }

  function tabTone(status: string): 'success' | 'danger' | 'info' {
    if (status === 'ok') return 'success';
    if (status === 'error') return 'danger';
    return 'info';
  }

  function onComposerModeChange(v: string) {
    const next = v as 'form' | 'raw';
    if (next === 'raw' && composerMode === 'form') syncRawFromForm();
    if (next === 'form' && composerMode === 'raw') syncFormFromRaw();
    composerMode = next;
  }

  function toolsForServer(serverID: string): SnapshotTool[] {
    if (!catalog?.catalog.tools) return [];
    return catalog.catalog.tools.filter((t) => t.server_id === serverID);
  }

  $: filteredCatalog = (() => {
    const q = catalogQuery.trim().toLowerCase();
    if (!catalog?.catalog) return null;
    if (!q) return catalog.catalog;
    const c = catalog.catalog;
    return {
      ...c,
      servers: (c.servers ?? []).filter((s) =>
        `${s.id} ${s.display_name ?? ''}`.toLowerCase().includes(q)
      ),
      tools: (c.tools ?? []).filter((tool) =>
        `${tool.namespaced_name} ${tool.description ?? ''}`.toLowerCase().includes(q)
      ),
      resources: (c.resources ?? []).filter((r) => r.uri.toLowerCase().includes(q)),
      prompts: (c.prompts ?? []).filter((p) => p.namespaced_name.toLowerCase().includes(q))
    };
  })();

  $: hasCatalogContent = Boolean(
    filteredCatalog &&
      ((filteredCatalog.servers?.length ?? 0) > 0 ||
        (filteredCatalog.tools?.length ?? 0) > 0 ||
        (filteredCatalog.resources?.length ?? 0) > 0 ||
        (filteredCatalog.prompts?.length ?? 0) > 0)
  );

  onMount(() => {
    resumeOrStartSession();
  });

  onDestroy(() => {
    if (pollHandle) clearInterval(pollHandle);
  });
</script>

<PageHeader title={$t('playground.title')} description={$t('playground.subtitle')}>
  <svelte:fragment slot="actions">
    {#if session}
      <IdBadge value={session.id} label={$t('playground.session.id')} />
      <Button variant="secondary" on:click={endSession}>{$t('playground.session.end')}</Button>
    {:else}
      <Button on:click={() => startSession()}>{$t('playground.session.start')}</Button>
    {/if}
  </svelte:fragment>
</PageHeader>

{#if !session}
  <EmptyState title={$t('playground.title')} description={$t('playground.empty.pickTool')} />
{:else}
  <div class="grid">
    <aside
      class="catalog"
      data-testid="playground-catalog"
      data-server-count={filteredCatalog?.servers?.length ?? 0}
      data-tool-count={filteredCatalog?.tools?.length ?? 0}
    >
      <div class="catalog-head">
        <h2>{$t('playground.catalog.title')}</h2>
        <button
          class="icon-btn"
          on:click={refreshCatalog}
          aria-label={$t('playground.catalog.refresh')}
          disabled={catalogLoading}
        >
          <IconRefreshCw size={14} />
        </button>
      </div>
      <input
        class="catalog-search"
        type="search"
        bind:value={catalogQuery}
        placeholder={$t('playground.catalog.search')}
      />
      {#if !hasCatalogContent}
        <p class="muted small">{$t('playground.empty.noCatalog')}</p>
      {:else if filteredCatalog}
        <!-- Servers → tools -->
        {#if filteredCatalog.servers && filteredCatalog.servers.length > 0}
          <h3 class="group">{$t('playground.catalog.servers')}</h3>
          <ul class="tree">
            {#each filteredCatalog.servers as server (server.id)}
              {@const tools = toolsForServer(server.id)}
              <li>
                <button
                  type="button"
                  class="row toggle server-toggle"
                  on:click={() => (serverOpen = { ...serverOpen, [server.id]: !serverOpen[server.id] })}
                  title={server.id}
                >
                  {#if serverOpen[server.id]}
                    <IconChevronDown size={12} />
                  {:else}
                    <IconChevronRight size={12} />
                  {/if}
                  <span class="server-display">{server.display_name || server.id}</span>
                  <Badge tone="neutral" size="sm">{tools.length}</Badge>
                </button>
                {#if serverOpen[server.id]}
                  <ul class="tools">
                    {#each tools as tool (tool.namespaced_name)}
                      {@const bare = tool.namespaced_name.includes('.')
                        ? tool.namespaced_name.split('.').slice(1).join('.')
                        : tool.namespaced_name}
                      <li>
                        <button
                          type="button"
                          class="row tool-row tool-stack"
                          class:active={composerTarget === tool.namespaced_name}
                          on:click={() => selectTool(tool)}
                          title={tool.namespaced_name}
                        >
                          <span class="tool-head">
                            <span class="tool-name">{bare}</span>
                            {#if tool.requires_approval}
                              <Badge tone="warning" size="sm">approval</Badge>
                            {/if}
                          </span>
                          {#if tool.description}
                            <span class="tool-desc">{tool.description}</span>
                          {/if}
                        </button>
                      </li>
                    {:else}
                      <li class="muted small">{$t('playground.catalog.empty')}</li>
                    {/each}
                  </ul>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}

        <!-- Resources -->
        {#if filteredCatalog.resources && filteredCatalog.resources.length > 0}
          <h3 class="group">
            <button
              type="button"
              class="row toggle group-toggle"
              on:click={() => (groupOpen = { ...groupOpen, resources: !groupOpen.resources })}
            >
              {#if groupOpen.resources}
                <IconChevronDown size={12} />
              {:else}
                <IconChevronRight size={12} />
              {/if}
              <span>{$t('playground.catalog.resources')}</span>
              <Badge tone="neutral" size="sm">{filteredCatalog.resources.length}</Badge>
            </button>
          </h3>
          {#if groupOpen.resources}
            <ul class="tools">
              {#each filteredCatalog.resources as r (r.uri)}
                <li>
                  <button
                    type="button"
                    class="row tool-row"
                    class:active={composerTarget === r.uri}
                    on:click={() => selectResource(r)}
                  >
                    <span class="mono small">{r.uri}</span>
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        {/if}

        <!-- Prompts -->
        {#if filteredCatalog.prompts && filteredCatalog.prompts.length > 0}
          <h3 class="group">
            <button
              type="button"
              class="row toggle group-toggle"
              on:click={() => (groupOpen = { ...groupOpen, prompts: !groupOpen.prompts })}
            >
              {#if groupOpen.prompts}
                <IconChevronDown size={12} />
              {:else}
                <IconChevronRight size={12} />
              {/if}
              <span>{$t('playground.catalog.prompts')}</span>
              <Badge tone="neutral" size="sm">{filteredCatalog.prompts.length}</Badge>
            </button>
          </h3>
          {#if groupOpen.prompts}
            <ul class="tools">
              {#each filteredCatalog.prompts as p (p.namespaced_name)}
                <li>
                  <button
                    type="button"
                    class="row tool-row"
                    class:active={composerTarget === p.namespaced_name}
                    on:click={() => selectPrompt(p)}
                  >
                    <span class="mono small">{p.namespaced_name}</span>
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        {/if}

        <!-- Skills -->
        {#if filteredCatalog.skills && filteredCatalog.skills.length > 0}
          <h3 class="group">
            <button
              type="button"
              class="row toggle group-toggle"
              on:click={() => (groupOpen = { ...groupOpen, skills: !groupOpen.skills })}
            >
              {#if groupOpen.skills}
                <IconChevronDown size={12} />
              {:else}
                <IconChevronRight size={12} />
              {/if}
              <span>{$t('playground.catalog.skills')}</span>
              <Badge tone="neutral" size="sm">{filteredCatalog.skills.length}</Badge>
            </button>
          </h3>
          {#if groupOpen.skills}
            <ul class="tools">
              {#each filteredCatalog.skills as sk (sk.id)}
                {@const busy = skillToggleBusy[sk.id] === true}
                <li>
                  <button
                    type="button"
                    class="row tool-row skill-row"
                    class:enabled={sk.enabled_for_session}
                    on:click={() => toggleSkill(sk)}
                    disabled={busy}
                  >
                    <span class="mono small skill-id">{sk.id}</span>
                    <span class="muted small">@{sk.version}</span>
                    <Badge tone={sk.enabled_for_session ? 'success' : 'neutral'} size="sm">
                      {sk.enabled_for_session ? 'on' : 'off'}
                    </Badge>
                  </button>
                  {#if sk.missing_tools && sk.missing_tools.length > 0}
                    <p class="muted small skill-warn">
                      missing: {sk.missing_tools.join(', ')}
                    </p>
                  {/if}
                </li>
              {/each}
            </ul>
          {/if}
        {/if}
      {/if}
    </aside>

    <section class="composer" data-testid="playground-composer">
      <header class="composer-head">
        <h2>{$t('playground.composer.title')}</h2>
        {#if composerTarget}
          <Badge tone="info" mono>{composerTarget}</Badge>
        {/if}
      </header>

      {#if !composerTarget}
        <p class="muted">{$t('playground.composer.pickFromCatalog')}</p>
      {:else}
        <SegmentedControl
          options={composerOptions}
          value={composerMode}
          onChange={onComposerModeChange}
        />

        {#if composerMode === 'form'}
          <SchemaForm
            schema={activeSchema}
            bind:value={composerArgsForm}
            on:change={syncRawFromForm}
          />
        {:else}
          <Textarea
            label={$t('playground.composer.raw')}
            bind:value={composerArgsRaw}
            rows={8}
            mono
          />
        {/if}

        <div class="row">
          <Button on:click={runCall} disabled={!composerTarget}>
            {$t('playground.composer.run')}
          </Button>
          <Button
            variant="secondary"
            on:click={() => (saveModalOpen = true)}
            disabled={!composerTarget}
          >
            {$t('playground.composer.saveAsCase')}
          </Button>
        </div>
      {/if}
    </section>

    <section class="output" data-testid="playground-output">
      <header class="output-head">
        <h2>{$t('playground.output.title')}</h2>
        {#if outputView.kind !== 'empty'}
          <label class="raw-toggle">
            <input type="checkbox" bind:checked={outputShowRaw} />
            {$t('playground.output.raw')}
          </label>
        {/if}
      </header>

      {#if outputView.kind === 'empty'}
        <p class="muted">{$t('playground.output.empty')}</p>
      {:else if outputView.kind === 'error'}
        <CodeBlock language="json" code={outputView.message} />
      {:else if outputShowRaw}
        <CodeBlock language="json" code={outputRaw} />
      {:else if outputView.kind === 'tool_result'}
        {#if outputView.isError}
          <p class="error-banner">Tool returned isError=true</p>
        {/if}
        <CodeBlock language="json" code={outputView.raw} />
        {#if outputView.structured}
          <h3 class="block-sub">structuredContent</h3>
          <CodeBlock language="json" code={JSON.stringify(outputView.structured, null, 2)} />
        {/if}
      {:else if outputView.kind === 'resource'}
        {#if outputView.contents.length === 0}
          <p class="muted">No content returned.</p>
        {:else}
          {#each outputView.contents as c, i (i)}
            <article class="resource-content">
              <header class="resource-head">
                <span class="mono small">{c.uri}</span>
                {#if c.mimeType}
                  <Badge tone="neutral" size="sm">{c.mimeType}</Badge>
                {/if}
              </header>
              {#if c.text !== undefined}
                <CodeBlock language={mimeToLang(c.mimeType)} code={c.text} />
              {:else if c.blob}
                <div class="binary">
                  <p class="muted">Binary, {fmtBytes(blobSize(c.blob))}</p>
                  <Button variant="secondary" on:click={() => downloadBlob(c)}>Download</Button>
                </div>
              {:else}
                <p class="muted">Empty content.</p>
              {/if}
            </article>
          {/each}
        {/if}
      {:else if outputView.kind === 'prompt'}
        {#if outputView.description}
          <p class="prompt-desc">{outputView.description}</p>
        {/if}
        {#if outputView.messages.length === 0}
          <p class="muted">No messages.</p>
        {:else}
          <ol class="messages">
            {#each outputView.messages as m, i (i)}
              <li class="message" data-role={m.role}>
                <header class="message-head">
                  <Badge tone={m.role === 'assistant' ? 'success' : 'neutral'} size="sm">
                    {m.role}
                  </Badge>
                  {#if m.content.mimeType}
                    <span class="muted small">{m.content.mimeType}</span>
                  {/if}
                </header>
                {#if m.content.text}
                  <p class="message-text">{m.content.text}</p>
                {:else if m.content.data}
                  <p class="muted">Binary content ({m.content.type})</p>
                {:else}
                  <p class="muted">Empty</p>
                {/if}
              </li>
            {/each}
          </ol>
        {/if}
      {:else}
        <CodeBlock language="json" code={outputView.text} />
      {/if}
    </section>

    <aside class="rail">
      <Tabs {tabs} bind:active={activeTab} />
      {#if activeTab === 'trace'}
        {#if correlation?.spans && correlation.spans.length > 0}
          <ul>
            {#each correlation.spans as span (span.span_id)}
              <li>
                <strong>{span.name}</strong>
                <Badge tone={tabTone(span.status)}>{span.status}</Badge>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="muted">No spans yet.</p>
        {/if}
      {:else if activeTab === 'audit'}
        {#if correlation?.audits && correlation.audits.length > 0}
          <ul>
            {#each correlation.audits as ev, i (i)}
              <li>
                {ev.type} <span class="muted">{new Date(ev.occurred_at).toLocaleTimeString()}</span>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="muted">No audit events yet.</p>
        {/if}
      {:else if activeTab === 'policy'}
        {#if correlation?.policy && correlation.policy.length > 0}
          <ul>
            {#each correlation.policy as p, i (i)}
              <li><strong>{p.tool}</strong> → {p.decision} {p.reason ?? ''}</li>
            {/each}
          </ul>
        {:else}
          <p class="muted">No policy decisions yet.</p>
        {/if}
      {:else if correlation?.drift && correlation.drift.length > 0}
        <ul>
          {#each correlation.drift as d, i (i)}
            <li>{d.type}</li>
          {/each}
        </ul>
      {:else}
        <p class="muted">No drift events.</p>
      {/if}
    </aside>
  </div>
{/if}

<Modal bind:open={saveModalOpen} title={$t('playground.composer.saveAsCase')}>
  <label>
    Name <input bind:value={saveName} type="text" />
  </label>
  <label>
    Description <input bind:value={saveDesc} type="text" />
  </label>
  <svelte:fragment slot="footer">
    <Button on:click={saveAsCase}>{$t('common.save')}</Button>
  </svelte:fragment>
</Modal>

<Toaster />

<style>
  .grid {
    display: grid;
    grid-template-columns: 280px 1fr 360px;
    grid-template-rows: auto auto;
    gap: var(--space-4);
  }
  .catalog {
    grid-row: span 2;
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-3);
    overflow: auto;
    max-height: calc(100vh - 220px);
  }
  .catalog-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-2);
  }
  .catalog-head h2 {
    margin: 0;
    font-size: var(--font-size-title);
  }
  .icon-btn {
    background: transparent;
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-sm);
    padding: var(--space-1);
    color: var(--color-icon-default);
    cursor: pointer;
  }
  .icon-btn:hover {
    color: var(--color-text-primary);
    border-color: var(--color-border-strong);
  }
  .catalog-search {
    width: 100%;
    box-sizing: border-box;
    background: var(--color-bg-default);
    border: 1px solid var(--color-border-default);
    border-radius: var(--radius-sm);
    padding: var(--space-2);
    color: var(--color-text-primary);
    font-size: var(--font-size-body-sm);
    margin-bottom: var(--space-2);
  }
  .group {
    margin: var(--space-3) 0 var(--space-1);
    font-size: var(--font-size-label);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-text-tertiary);
    font-weight: var(--font-weight-medium);
  }
  .group .group-toggle {
    background: transparent;
    border: none;
    width: 100%;
    text-transform: uppercase;
    color: inherit;
    font-size: inherit;
    letter-spacing: inherit;
    padding: var(--space-1);
  }
  .tree,
  .tools {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  .tools {
    margin-left: var(--space-3);
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    background: transparent;
    border: none;
    color: var(--color-text-primary);
    text-align: left;
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .toggle {
    font-weight: var(--font-weight-medium);
  }
  .tool-row {
    font-size: var(--font-size-body-sm);
  }
  .tool-row:hover {
    background: var(--color-bg-default);
  }
  .tool-row.active {
    background: var(--color-bg-default);
    box-shadow: inset 0 0 0 1px var(--color-accent-primary);
  }
  .tool-row[disabled] {
    opacity: 0.6;
    cursor: progress;
  }
  .skill-row.enabled {
    background: var(--color-bg-default);
  }
  .skill-row .skill-id {
    flex: 1;
  }
  .server-display {
    flex: 1;
    color: var(--color-text-primary);
    font-weight: var(--font-weight-medium);
  }
  .tool-stack {
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    padding: var(--space-2);
  }
  .tool-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
  }
  .tool-name {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
    color: var(--color-text-primary);
    flex: 1;
  }
  .tool-desc {
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    line-height: 1.3;
    white-space: normal;
  }
  .skill-warn {
    margin: 0 0 var(--space-2) var(--space-4);
    color: var(--color-warning);
  }
  .composer,
  .output {
    background: var(--color-bg-elevated);
    padding: var(--space-4);
    border-radius: var(--radius-md);
    border: 1px solid var(--color-border-soft);
  }
  .composer-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    margin-bottom: var(--space-3);
  }
  .composer-head h2 {
    margin: 0;
    font-size: var(--font-size-title);
  }
  .row {
    display: flex;
    gap: var(--space-2);
  }
  .rail {
    grid-row: span 2;
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-soft);
    border-radius: var(--radius-md);
    padding: var(--space-3);
  }
  .rail ul {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .muted {
    color: var(--color-text-muted);
  }
  .small {
    font-size: var(--font-size-body-sm);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--font-size-mono-sm);
  }
  .output-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-2);
  }
  .output-head h2 {
    margin: 0;
    font-size: var(--font-size-title);
  }
  .raw-toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    cursor: pointer;
  }
  .resource-content {
    margin-bottom: var(--space-3);
    padding: var(--space-2) 0;
    border-top: 1px solid var(--color-border-soft);
  }
  .resource-content:first-of-type {
    border-top: none;
  }
  .resource-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-bottom: var(--space-2);
  }
  .binary {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: var(--space-3);
    border: 1px dashed var(--color-border-default);
    border-radius: var(--radius-sm);
    background: var(--color-bg-default);
  }
  .messages {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .message {
    padding: var(--space-3);
    border-radius: var(--radius-md);
    background: var(--color-bg-default);
    border: 1px solid var(--color-border-soft);
  }
  .message[data-role='assistant'] {
    background: var(--color-bg-elevated);
  }
  .message-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-bottom: var(--space-2);
  }
  .message-text {
    margin: 0;
    white-space: pre-wrap;
    font-family: var(--font-sans);
    font-size: var(--font-size-body-sm);
    color: var(--color-text-primary);
  }
  .prompt-desc {
    color: var(--color-text-secondary);
    font-style: italic;
    margin-bottom: var(--space-3);
  }
  .block-sub {
    margin: var(--space-3) 0 var(--space-1);
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .error-banner {
    color: var(--color-danger);
    font-size: var(--font-size-body-sm);
    margin-bottom: var(--space-2);
  }
</style>
