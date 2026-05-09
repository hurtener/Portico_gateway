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

/**
 * Returns true when an error indicates the endpoint isn't wired in this
 * build (404/501/405 are all "feature absent" from the operator's
 * perspective). Pages use this to render a calm "not configured" empty
 * state instead of a raw error.
 */
export function isFeatureUnavailable(e: unknown): boolean {
  if (e instanceof HTTPError) {
    return e.status === 404 || e.status === 405 || e.status === 501;
  }
  const msg = (e as Error)?.message ?? '';
  return /\b(404|405|501)\b/.test(msg);
}

/**
 * Capability counts derived from the latest catalog snapshot for the
 * tenant. Zero across the board means no snapshot has been generated
 * yet — the UI renders an em-dash placeholder rather than "0 tools".
 */
export interface ServerCapabilities {
  tools: number;
  resources: number;
  prompts: number;
  apps: number;
}

export type ServerPolicyState = 'none' | 'enforced' | 'approval' | 'disabled';
export type ServerAuthState = 'none' | 'env' | 'header' | 'oauth' | 'vault_ref' | string;

export interface ServerSummary {
  id: string;
  display_name?: string;
  transport: 'stdio' | 'http';
  runtime_mode: string;
  enabled: boolean;
  status: string;
  status_detail?: string;
  updated_at: string;

  // Phase 10.6 substrate. All optional so older builds (or builds with
  // partial wiring — no snapshots service, no policy store) keep working
  // and the UI degrades to an em-dash for the missing dimension.
  capabilities?: ServerCapabilities;
  skills_count?: number;
  policy_state?: ServerPolicyState;
  auth_state?: ServerAuthState;
  last_seen?: string;
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

export interface AuthSpec {
  strategy?: string;
  secret_ref?: string;
  default_risk_class?: string;
  env?: string[];
  headers?: Record<string, string>;
}

export interface ServerSpec extends ServerSummary {
  stdio?: StdioSpec;
  http?: HTTPSpec;
  health?: HealthSpec;
  lifecycle?: LifecycleSpec;
  auth?: AuthSpec;
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

/**
 * Counts of prompts, resources, and embedded UI apps the skill carries.
 * Zero is meaningful (the skill genuinely has no prompts), so every
 * field is required — distinct from the server capabilities case.
 */
export interface SkillAssets {
  prompts: number;
  resources: number;
  apps: number;
}

export type SkillStatus = 'enabled' | 'disabled' | 'missing_tools' | 'draft' | 'review';

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

  // Phase 10.6 substrate. Always populated on new builds; older builds
  // omit, in which case the UI derives a fallback from existing fields.
  attached_server?: string;
  assets?: SkillAssets;
  last_updated?: string;
  status?: SkillStatus;
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

  listServers: () => request<ServerSummary[]>('/v1/servers'),
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
    request<InstanceRecord[]>(`/v1/servers/${encodeURIComponent(id)}/instances`),

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
    ),

  // Phase 5: approvals + audit + admin secrets.
  listApprovals: () => request<Approval[]>('/v1/approvals?status=pending'),
  approveApproval: (id: string, note = '') =>
    request<Approval>(`/v1/approvals/${encodeURIComponent(id)}/approve`, {
      method: 'POST',
      body: JSON.stringify({ note })
    }),
  denyApproval: (id: string, note = '') =>
    request<Approval>(`/v1/approvals/${encodeURIComponent(id)}/deny`, {
      method: 'POST',
      body: JSON.stringify({ note })
    }),
  queryAudit: (params: AuditQueryParams = {}) => {
    const q = new URLSearchParams();
    if (params.type) q.set('type', params.type);
    if (params.since) q.set('since', params.since);
    if (params.until) q.set('until', params.until);
    if (params.limit) q.set('limit', String(params.limit));
    if (params.cursor) q.set('cursor', params.cursor);
    const qs = q.toString();
    return request<{ events: AuditEvent[]; next_cursor: string }>(
      `/v1/audit/events${qs ? `?${qs}` : ''}`
    );
  },
  listSecrets: () => request<SecretRef[]>('/v1/admin/secrets'),
  putSecret: (tenant: string, name: string, value: string) =>
    request<void>(`/v1/admin/secrets/${encodeURIComponent(tenant)}/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify({ value })
    }),
  deleteSecret: (tenant: string, name: string) =>
    request<void>(`/v1/admin/secrets/${encodeURIComponent(tenant)}/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    }),

  // Phase 8: skill sources + authored skills.
  listSkillSources: () => request<{ items: SkillSource[] }>('/api/skill-sources'),
  getSkillSource: (name: string) =>
    request<SkillSource>(`/api/skill-sources/${encodeURIComponent(name)}`),
  upsertSkillSource: (s: Partial<SkillSource>) =>
    request<SkillSource>('/api/skill-sources', {
      method: 'POST',
      body: JSON.stringify(s)
    }),
  putSkillSource: (name: string, s: Partial<SkillSource>) =>
    request<SkillSource>(`/api/skill-sources/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify(s)
    }),
  deleteSkillSource: (name: string) =>
    request<void>(`/api/skill-sources/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  refreshSkillSource: (name: string) =>
    request<{ refreshed: string }>(`/api/skill-sources/${encodeURIComponent(name)}/refresh`, {
      method: 'POST',
      body: '{}'
    }),
  listSkillSourcePacks: (name: string) =>
    request<{ items: SourcePack[] }>(`/api/skill-sources/${encodeURIComponent(name)}/packs`),

  listAuthoredSkills: () => request<{ items: AuthoredSkillSummary[] }>('/api/skills/authored'),
  getAuthoredSkill: (id: string) =>
    request<AuthoredSkillDetail>(`/api/skills/authored/${encodeURIComponent(id)}`),
  authoredSkillVersions: (id: string) =>
    request<{ items: AuthoredSkillSummary[] }>(
      `/api/skills/authored/${encodeURIComponent(id)}/versions`
    ),
  getAuthoredSkillVersion: (id: string, v: string) =>
    request<AuthoredSkillDetail>(
      `/api/skills/authored/${encodeURIComponent(id)}/versions/${encodeURIComponent(v)}`
    ),
  createAuthoredSkill: (req: AuthoredSkillRequest) =>
    request<AuthoredSkillDetail>('/api/skills/authored', {
      method: 'POST',
      body: JSON.stringify(req)
    }),
  updateAuthoredSkillVersion: (id: string, v: string, req: AuthoredSkillRequest) =>
    request<AuthoredSkillDetail>(
      `/api/skills/authored/${encodeURIComponent(id)}/versions/${encodeURIComponent(v)}`,
      {
        method: 'PUT',
        body: JSON.stringify(req)
      }
    ),
  publishAuthoredSkill: (id: string, v: string) =>
    request<AuthoredSkillDetail>(
      `/api/skills/authored/${encodeURIComponent(id)}/versions/${encodeURIComponent(v)}/publish`,
      { method: 'POST', body: '{}' }
    ),
  archiveAuthoredSkill: (id: string, v: string) =>
    request<void>(
      `/api/skills/authored/${encodeURIComponent(id)}/versions/${encodeURIComponent(v)}/archive`,
      { method: 'POST', body: '{}' }
    ),
  deleteAuthoredSkillDraft: (id: string, v: string) =>
    request<void>(
      `/api/skills/authored/${encodeURIComponent(id)}/versions/${encodeURIComponent(v)}`,
      { method: 'DELETE' }
    ),
  validateSkillManifest: (req: AuthoredSkillRequest) =>
    request<SkillValidationResult>('/api/skills/validate', {
      method: 'POST',
      body: JSON.stringify(req)
    }),

  // Phase 6: snapshots + session inspector.
  listSnapshots: (params: { since?: string; cursor?: string; limit?: number } = {}) => {
    const q = new URLSearchParams();
    if (params.since) q.set('since', params.since);
    if (params.cursor) q.set('cursor', params.cursor);
    if (params.limit) q.set('limit', String(params.limit));
    const qs = q.toString();
    return request<{ snapshots: Snapshot[]; next_cursor: string }>(
      `/v1/catalog/snapshots${qs ? `?${qs}` : ''}`
    );
  },
  getSnapshot: (id: string) => request<Snapshot>(`/v1/catalog/snapshots/${encodeURIComponent(id)}`),
  diffSnapshots: (a: string, b: string) =>
    request<SnapshotDiff>(
      `/v1/catalog/snapshots/${encodeURIComponent(a)}/diff/${encodeURIComponent(b)}`
    ),
  getSessionSnapshot: (sessionId: string) =>
    request<Snapshot>(`/v1/sessions/${encodeURIComponent(sessionId)}/snapshot`),

  // ----- Phase 9: Console CRUD -------------------------------------------

  // Servers — phase-9 surface (PATCH, restart, health, activity).
  patchServer: (
    id: string,
    body: { enabled?: boolean; env_overrides?: Record<string, string>; reason?: string }
  ) =>
    request<ServerSpec>(`/api/servers/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(body)
    }),
  restartServer: (id: string, reason = '') =>
    request<ServerSpec>(`/api/servers/${encodeURIComponent(id)}/restart`, {
      method: 'POST',
      body: JSON.stringify({ reason })
    }),
  serverHealth: (id: string) =>
    request<ServerHealth>(`/api/servers/${encodeURIComponent(id)}/health`),
  serverActivity: (id: string, limit = 50) =>
    request<EntityActivityRow[]>(`/api/servers/${encodeURIComponent(id)}/activity?limit=${limit}`),

  // Tenants — full CRUD.
  listTenants: () => request<Tenant[]>('/api/admin/tenants'),
  getTenant: (id: string) => request<Tenant>(`/api/admin/tenants/${encodeURIComponent(id)}`),
  createTenant: (t: Partial<Tenant>) =>
    request<Tenant>('/api/admin/tenants', {
      method: 'POST',
      body: JSON.stringify(t)
    }),
  updateTenant: (id: string, t: Partial<Tenant>) =>
    request<Tenant>(`/api/admin/tenants/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(t)
    }),
  archiveTenant: (id: string) =>
    request<void>(`/api/admin/tenants/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  purgeTenant: (id: string) =>
    request<void>(`/api/admin/tenants/${encodeURIComponent(id)}/purge`, {
      method: 'POST',
      body: '{}'
    }),
  tenantActivity: (id: string, limit = 50) =>
    request<EntityActivityRow[]>(
      `/api/admin/tenants/${encodeURIComponent(id)}/activity?limit=${limit}`
    ),

  // Secrets — Phase 9 richer surface.
  listSecretsAPI: () => request<SecretMetadata[]>('/api/admin/secrets'),
  createSecretAPI: (body: { tenant_id?: string; name: string; value: string }) =>
    request<SecretMetadata>('/api/admin/secrets', {
      method: 'POST',
      body: JSON.stringify(body)
    }),
  getSecretMetadata: (name: string) =>
    request<SecretMetadata>(`/api/admin/secrets/${encodeURIComponent(name)}`),
  updateSecret: (name: string, value: string) =>
    request<SecretMetadata>(`/api/admin/secrets/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify({ value })
    }),
  deleteSecretAPI: (name: string) =>
    request<void>(`/api/admin/secrets/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  rotateSecret: (name: string) =>
    request<{ status: string }>(`/api/admin/secrets/${encodeURIComponent(name)}/rotate`, {
      method: 'POST',
      body: '{}'
    }),
  issueRevealToken: (name: string) =>
    request<RevealTokenResponse>(`/api/admin/secrets/${encodeURIComponent(name)}/reveal`, {
      method: 'POST',
      body: '{}'
    }),
  consumeRevealToken: (token: string) =>
    request<{ tenant_id: string; name: string; value: string }>(
      `/api/admin/secrets/reveal/${encodeURIComponent(token)}`
    ),
  secretActivity: (name: string, limit = 50) =>
    request<EntityActivityRow[]>(
      `/api/admin/secrets/${encodeURIComponent(name)}/activity?limit=${limit}`
    ),

  // Policy editor.
  listPolicyRules: () => request<{ rules: PolicyRule[] }>('/api/policy/rules'),
  replacePolicyRules: (rules: PolicyRule[]) =>
    request<{ rules: PolicyRule[] }>('/api/policy/rules', {
      method: 'PUT',
      body: JSON.stringify({ rules })
    }),
  createPolicyRule: (rule: PolicyRule) =>
    request<PolicyRule>('/api/policy/rules', {
      method: 'POST',
      body: JSON.stringify(rule)
    }),
  updatePolicyRule: (id: string, rule: PolicyRule) =>
    request<PolicyRule>(`/api/policy/rules/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(rule)
    }),
  deletePolicyRule: (id: string) =>
    request<void>(`/api/policy/rules/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  dryRunPolicy: (call: PolicyToolCall, rules?: PolicyRule[]) =>
    request<PolicyDryRunResult>('/api/policy/dry-run', {
      method: 'POST',
      body: JSON.stringify(rules ? { call, rules: { rules } } : { call })
    }),
  policyActivity: () => request<EntityActivityRow[]>('/api/policy/activity'),

  // ── Phase 10: Playground ────────────────────────────────────────────
  startPlaygroundSession: (req: PlaygroundStartSessionRequest = {}) =>
    request<PlaygroundSession>('/api/playground/sessions', {
      method: 'POST',
      body: JSON.stringify(req)
    }),
  getPlaygroundSession: (sid: string) =>
    request<PlaygroundSession>(`/api/playground/sessions/${encodeURIComponent(sid)}`),
  endPlaygroundSession: (sid: string) =>
    request<void>(`/api/playground/sessions/${encodeURIComponent(sid)}`, { method: 'DELETE' }),
  getPlaygroundCatalog: (sid: string) =>
    request<PlaygroundCatalog>(`/api/playground/sessions/${encodeURIComponent(sid)}/catalog`),
  issuePlaygroundCall: (sid: string, body: PlaygroundCallRequest) =>
    request<PlaygroundCallEnvelope>(`/api/playground/sessions/${encodeURIComponent(sid)}/calls`, {
      method: 'POST',
      body: JSON.stringify(body)
    }),
  getPlaygroundCorrelation: (sid: string, since?: string) =>
    request<CorrelationBundle>(
      `/api/playground/sessions/${encodeURIComponent(sid)}/correlation${
        since ? `?since=${encodeURIComponent(since)}` : ''
      }`
    ),
  setPlaygroundSkillEnabled: (sid: string, skillID: string, enabled: boolean) =>
    request<{ session_id: string; skill_id: string; enabled: boolean }>(
      `/api/playground/sessions/${encodeURIComponent(sid)}/skills/${encodeURIComponent(skillID)}/${enabled ? 'enable' : 'disable'}`,
      { method: 'POST' }
    ),
  listPlaygroundCases: () =>
    request<{ cases: PlaygroundCase[]; next_cursor?: string }>('/api/playground/cases'),
  createPlaygroundCase: (c: Partial<PlaygroundCase>) =>
    request<PlaygroundCase>('/api/playground/cases', {
      method: 'POST',
      body: JSON.stringify(c)
    }),
  getPlaygroundCase: (id: string) =>
    request<PlaygroundCase>(`/api/playground/cases/${encodeURIComponent(id)}`),
  updatePlaygroundCase: (id: string, c: Partial<PlaygroundCase>) =>
    request<PlaygroundCase>(`/api/playground/cases/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(c)
    }),
  deletePlaygroundCase: (id: string) =>
    request<void>(`/api/playground/cases/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  caseRuns: (id: string) =>
    request<{ runs: PlaygroundRun[]; next_cursor?: string }>(
      `/api/playground/cases/${encodeURIComponent(id)}/runs`
    ),
  replayPlaygroundCase: (id: string) =>
    request<PlaygroundRun>(`/api/playground/cases/${encodeURIComponent(id)}/replay`, {
      method: 'POST'
    }),
  getPlaygroundRun: (id: string) =>
    request<PlaygroundRun>(`/api/playground/runs/${encodeURIComponent(id)}`)
};

// ── Phase 10: Playground types ──────────────────────────────────────
export interface PlaygroundStartSessionRequest {
  tenant_id?: string;
  snapshot_id?: string;
  runtime_override?: string;
  scopes?: string[];
}

export interface PlaygroundSession {
  id: string;
  tenant_id: string;
  actor_id?: string;
  snapshot_id?: string;
  token: string;
  expires_at: string;
  created_at: string;
}

export interface PlaygroundCatalog {
  snapshot_id: string;
  // Catalog mirrors the Snapshot shape — every property the operator
  // needs to populate the rail is on it. Kept Partial because the
  // backend may shed fields in degraded boots (e.g. snapshots service
  // disabled).
  catalog: Partial<Snapshot>;
}

export interface PlaygroundCallRequest {
  kind: 'tool_call' | 'resource_read' | 'prompt_get';
  target: string;
  arguments?: unknown;
}

export interface PlaygroundCallEnvelope {
  call_id: string;
  session_id: string;
  status: string;
}

export interface PlaygroundCase {
  id: string;
  name: string;
  description?: string;
  kind: 'tool_call' | 'resource_read' | 'prompt_get';
  target: string;
  payload: unknown;
  snapshot_id?: string;
  tags: string[] | null;
  created_at: string;
  created_by?: string;
}

export interface PlaygroundRun {
  id: string;
  case_id?: string;
  session_id: string;
  snapshot_id: string;
  status: 'running' | 'ok' | 'error' | 'denied';
  drift_detected: boolean;
  summary?: string;
  started_at: string;
  ended_at?: string;
}

export interface SpanNode {
  span_id: string;
  parent_id?: string;
  name: string;
  started_at: string;
  ended_at?: string;
  status: string;
  attributes?: Record<string, string>;
}

export interface PolicyDecisionLite {
  tool?: string;
  decision: string;
  reason?: string;
  detail?: Record<string, unknown>;
  at: string;
}

export interface CorrelationBundle {
  session_id: string;
  spans: SpanNode[];
  audits: AuditEvent[];
  policy: PolicyDecisionLite[];
  drift: AuditEvent[];
  last_event_age?: string;
  generated_at: string;
}

export interface Snapshot {
  id: string;
  tenant_id: string;
  session_id?: string;
  created_at: string;
  overall_hash: string;
  servers: SnapshotServer[];
  tools: SnapshotTool[];
  resources: SnapshotResource[];
  prompts: SnapshotPrompt[];
  skills: SnapshotSkill[];
  policies: SnapshotPolicies;
  credentials: SnapshotCredential[];
  warnings?: string[];
}

export interface SnapshotServer {
  id: string;
  display_name?: string;
  transport: string;
  runtime_mode?: string;
  schema_hash: string;
  health: string;
}

export interface SnapshotTool {
  namespaced_name: string;
  server_id: string;
  description?: string;
  input_schema?: unknown;
  risk_class: string;
  requires_approval: boolean;
  skill_id?: string;
  hash: string;
}

export interface SnapshotResource {
  uri: string;
  upstream_uri?: string;
  server_id: string;
  mime_type?: string;
}

export interface SnapshotPrompt {
  namespaced_name: string;
  server_id: string;
  arguments?: Array<{ name: string; description?: string; required?: boolean }>;
}

export interface SnapshotSkill {
  id: string;
  version: string;
  enabled_for_session: boolean;
  missing_tools?: string[];
}

export interface SnapshotPolicies {
  allow_list?: string[];
  deny_list?: string[];
  approval_timeout?: number;
  default_risk_class?: string;
}

export interface SnapshotCredential {
  server_id: string;
  strategy?: string;
  secret_refs?: string[];
}

export interface SnapshotDiff {
  tools: { added?: string[]; removed?: string[]; modified?: ModifiedTool[] };
  resources: { added?: string[]; removed?: string[] };
  prompts: { added?: string[]; removed?: string[] };
  skills: { added?: string[]; removed?: string[] };
}

export interface ModifiedTool {
  name: string;
  fields_changed: string[];
  old_hash: string;
  new_hash: string;
}

export interface Approval {
  id: string;
  tenant_id: string;
  session_id: string;
  user_id?: string;
  tool: string;
  args_summary?: string;
  risk_class: string;
  status: string;
  created_at: string;
  decided_at?: string;
  expires_at: string;
  metadata?: Record<string, unknown>;
}

export interface AuditEvent {
  id?: string;
  type: string;
  tenant_id: string;
  session_id?: string;
  user_id?: string;
  occurred_at: string;
  trace_id?: string;
  span_id?: string;
  payload?: Record<string, unknown>;
}

export interface AuditQueryParams {
  type?: string;
  since?: string;
  until?: string;
  limit?: number;
  cursor?: string;
}

export interface SecretRef {
  tenant_id: string;
  name: string;
}

// Phase 8: skill sources + authored.
export interface SkillSource {
  name: string;
  driver: 'git' | 'http' | 'localdir' | 'authored' | string;
  config: Record<string, unknown>;
  credential_ref?: string;
  refresh_seconds?: number;
  priority?: number;
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
  last_refresh_at?: string;
  last_error?: string;
}

export interface SourcePack {
  id: string;
  version: string;
  loc?: string;
}

export interface AuthoredSkillSummary {
  skill_id: string;
  version: string;
  status: 'draft' | 'published' | 'archived' | string;
  title?: string;
  description?: string;
  checksum: string;
  author_user_id?: string;
  created_at: string;
  published_at?: string;
}

export interface AuthoredSkillFile {
  relpath: string;
  mime_type: string;
  body: string;
}

export interface AuthoredSkillDetail extends AuthoredSkillSummary {
  manifest: Record<string, unknown>;
  files: AuthoredSkillFile[];
}

export interface AuthoredSkillRequest {
  manifest: string;
  files?: AuthoredSkillFile[];
}

export interface SkillValidationViolation {
  pointer: string;
  line?: number;
  col?: number;
  reason: string;
  kind?: string;
}

export interface SkillValidationResult {
  valid: boolean;
  violations: SkillValidationViolation[];
  checksum?: string;
  validated?: string;
}

// ----- Phase 9 types ------------------------------------------------------

export interface ServerHealth {
  server_id: string;
  status: string;
  status_detail?: string;
  enabled: boolean;
  last_error?: string;
  updated_at: string;
}

export interface EntityActivityRow {
  event_id: string;
  occurred_at: string;
  actor_user_id?: string;
  summary: string;
  diff: Record<string, unknown>;
}

export interface Tenant {
  id: string;
  display_name: string;
  plan: string;
  runtime_mode?: string;
  max_concurrent_sessions?: number;
  max_requests_per_minute?: number;
  audit_retention_days?: number;
  jwt_issuer?: string;
  jwt_jwks_url?: string;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface SecretMetadata {
  tenant_id: string;
  name: string;
  version?: number;
  created_at?: string;
  updated_at?: string;
}

export interface RevealTokenResponse {
  token: string;
  expires_at: string;
}

export interface PolicyRuleConditions {
  match: {
    tools?: string[];
    servers?: string[];
    tenants?: string[];
    args_expr?: string;
    time_range?: { from?: string; to?: string };
  };
}

export interface PolicyRuleActions {
  allow?: boolean;
  deny?: boolean;
  require_approval?: boolean;
  log_level?: string;
  annotate?: string;
}

export interface PolicyRule {
  id: string;
  priority: number;
  enabled: boolean;
  risk_class: string;
  conditions: PolicyRuleConditions;
  actions: PolicyRuleActions;
  notes?: string;
  updated_at?: string;
  updated_by?: string;
}

export interface PolicyToolCall {
  tenant_id?: string;
  server: string;
  tool: string;
  args?: Record<string, unknown>;
  now?: string;
}

export interface PolicyDryRunMatch {
  rule_id: string;
  priority: number;
  reason: string;
}

export interface PolicyDryRunResult {
  matched_rules: PolicyDryRunMatch[];
  losing_rules?: PolicyDryRunMatch[];
  final_action: PolicyRuleActions;
  final_risk: string;
}
