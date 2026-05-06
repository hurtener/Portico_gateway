// Typed REST client for the Portico Console.
//
// All component-level data fetching MUST go through this module
// (CLAUDE.md §4.5 forbids hand-rolled fetch in .svelte files). The
// seam centralises base URL resolution, tenant header injection, and
// JSON-error parsing.
//
// The client is generated against the live REST surface in
// `internal/server/api/handlers_servers.go`; mismatches are caught
// at compile time by `svelte-check`.

const isBrowser = typeof window !== 'undefined';

export interface APIError extends Error {
  status: number;
  code?: string;
  detail?: string;
}

export class HTTPError extends Error implements APIError {
  status: number;
  code?: string;
  detail?: string;
  constructor(status: number, message: string, code?: string, detail?: string) {
    super(message);
    this.name = 'HTTPError';
    this.status = status;
    this.code = code;
    this.detail = detail;
  }
}

export interface ServerSummary {
  id: string;
  display_name?: string;
  transport: 'stdio' | 'http';
  runtime_mode: string;
  enabled: boolean;
  status: string;
  status_detail?: string;
  updated_at: string;
}

export interface StdioSpec {
  command: string;
  args?: string[];
  env?: string[];
  cwd?: string;
  start_timeout?: string;
}

export interface HTTPSpec {
  url: string;
  auth_header?: string;
  timeout?: string;
}

export interface HealthSpec {
  ping_interval?: string;
  ping_timeout?: string;
  startup_grace?: string;
}

export interface LifecycleSpec {
  idle_timeout?: string;
  max_restarts?: number;
  restart_window?: string;
}

export interface ServerSpec extends ServerSummary {
  stdio?: StdioSpec;
  http?: HTTPSpec;
  health?: HealthSpec;
  lifecycle?: LifecycleSpec;
}

export interface InstanceRecord {
  instance_key: string;
  server_id: string;
  state: string;
  pid?: number;
  started_at?: string;
  last_heartbeat?: string;
}

export interface Resource {
  uri: string;
  name?: string;
  description?: string;
  mimeType?: string;
  size?: number;
  _meta?: Record<string, unknown>;
}

export interface ResourceTemplate {
  uriTemplate: string;
  name?: string;
  description?: string;
  mimeType?: string;
}

export interface PromptArgument {
  name: string;
  description?: string;
  required?: boolean;
}

export interface Prompt {
  name: string;
  description?: string;
  arguments?: PromptArgument[];
}

export interface AppEntry {
  uri: string;
  upstreamUri: string;
  serverId: string;
  name?: string;
  description?: string;
  mimeType?: string;
  discoveredAt: string;
}

export interface SkillIndexEntry {
  id: string;
  version: string;
  title: string;
  description?: string;
  spec: string;
  required_servers: string[];
  required_tools: string[];
  optional_tools?: string[];
  manifest_uri: string;
  instructions_uri: string;
  ui_resource_uri?: string;
  enabled_for_tenant: boolean;
  enabled_for_session: boolean;
  missing_tools?: string[];
  warnings?: string[];
}

export interface SkillsIndex {
  version: number;
  tenant_id: string;
  session_id?: string;
  generated_at: string;
  skills: SkillIndexEntry[];
}

export interface SkillDetail {
  id: string;
  version: string;
  title: string;
  description?: string;
  manifest: Record<string, unknown>;
  warnings?: string[];
  enabled_for_tenant: boolean;
}

function baseURL(): string {
  // In the browser, same-origin (the Go binary serves both API and SPA).
  // In tests/SSR (which we don't ship), assume localhost dev server.
  if (isBrowser) return '';
  return 'http://127.0.0.1:8080';
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const url = baseURL() + path;
  const res = await fetch(url, {
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...(init?.headers ?? {})
    },
    ...init
  });

  if (res.status === 204) {
    return undefined as T;
  }

  const text = await res.text();
  let body: unknown = undefined;
  if (text.length > 0) {
    try {
      body = JSON.parse(text);
    } catch {
      body = text;
    }
  }

  if (!res.ok) {
    const err = (
      body && typeof body === 'object' ? (body as Record<string, unknown>).error : undefined
    ) as { code?: string; message?: string; detail?: string } | undefined;
    throw new HTTPError(res.status, err?.message ?? `HTTP ${res.status}`, err?.code, err?.detail);
  }
  return body as T;
}

export const api = {
  health: () => request<{ status: string }>('/healthz'),
  ready: () => request<{ status: string }>('/readyz'),

  listServers: () => request<{ items: ServerSummary[] }>('/v1/servers'),
  getServer: (id: string) => request<ServerSpec>(`/v1/servers/${encodeURIComponent(id)}`),
  upsertServer: (spec: Partial<ServerSpec>) =>
    request<ServerSpec>('/v1/servers', {
      method: 'POST',
      body: JSON.stringify(spec)
    }),
  putServer: (id: string, spec: Partial<ServerSpec>) =>
    request<ServerSpec>(`/v1/servers/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(spec)
    }),
  deleteServer: (id: string) =>
    request<void>(`/v1/servers/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  reloadServer: (id: string) =>
    request<void>(`/v1/servers/${encodeURIComponent(id)}/reload`, { method: 'POST' }),
  enableServer: (id: string) =>
    request<ServerSpec>(`/v1/servers/${encodeURIComponent(id)}/enable`, { method: 'POST' }),
  disableServer: (id: string) =>
    request<ServerSpec>(`/v1/servers/${encodeURIComponent(id)}/disable`, { method: 'POST' }),
  listInstances: (id: string) =>
    request<{ items: InstanceRecord[] }>(`/v1/servers/${encodeURIComponent(id)}/instances`),

  listResources: (cursor = '') =>
    request<{ resources: Resource[]; nextCursor?: string }>(
      `/v1/resources${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''}`
    ),
  listResourceTemplates: () =>
    request<{ resourceTemplates: ResourceTemplate[]; nextCursor?: string }>(
      '/v1/resources/templates'
    ),
  readResource: (uri: string) =>
    request<{ contents: Array<{ uri: string; mimeType?: string; text?: string; blob?: string }> }>(
      `/v1/resources/${encodeURI(uri)}`
    ),

  listPrompts: () => request<{ prompts: Prompt[]; nextCursor?: string }>('/v1/prompts'),
  getPrompt: (name: string, args: Record<string, string> = {}) =>
    request<{
      description?: string;
      messages: Array<{ role: string; content: { type: string; text?: string } }>;
    }>(`/v1/prompts/${encodeURIComponent(name)}`, {
      method: 'POST',
      body: JSON.stringify({ arguments: args })
    }),

  listApps: () => request<{ items: AppEntry[] }>('/v1/apps'),

  listSkills: () => request<SkillsIndex>('/v1/skills'),
  getSkill: (id: string) => request<SkillDetail>(`/v1/skills/${encodeURIComponent(id)}`),
  enableSkill: (id: string) =>
    request<{ skill_id: string; enabled: boolean }>(`/v1/skills/${encodeURIComponent(id)}/enable`, {
      method: 'POST',
      body: '{}'
    }),
  disableSkill: (id: string) =>
    request<{ skill_id: string; enabled: boolean }>(
      `/v1/skills/${encodeURIComponent(id)}/disable`,
      {
        method: 'POST',
        body: '{}'
      }
    )
};
