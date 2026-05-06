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
    request<{ items: InstanceRecord[] }>(`/v1/servers/${encodeURIComponent(id)}/instances`)
};
