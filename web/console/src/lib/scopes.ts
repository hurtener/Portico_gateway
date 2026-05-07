/**
 * Scope constants — mirrored verbatim from
 * `internal/auth/scope/scope.go`. Keep both in sync (CLAUDE.md §4.5
 * + Phase 9 plan common pitfalls).
 *
 * The dev-mode tenant carries `admin` which, per the gateway's middleware,
 * implicitly grants every named write scope. So in local dev every page
 * renders unlocked.
 */

import { writable, derived, type Readable } from 'svelte/store';
import { browser } from '$app/environment';

export const SCOPE_ADMIN = 'admin';
export const SCOPE_SERVERS_WRITE = 'servers:write';
export const SCOPE_SECRETS_WRITE = 'secrets:write';
export const SCOPE_POLICY_WRITE = 'policy:write';
export const SCOPE_TENANTS_ADMIN = 'tenants:admin';

const STORAGE_KEY = 'portico:scopes';

function readPersisted(): string[] {
  if (!browser) return [SCOPE_ADMIN];
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [SCOPE_ADMIN];
    const parsed = JSON.parse(raw) as unknown;
    if (Array.isArray(parsed)) return parsed.map(String);
  } catch {
    // ignored — fall through to default
  }
  return [SCOPE_ADMIN];
}

/**
 * The active set of scopes. The auth middleware lands them on the JWT
 * (Phase 12 will replace this stub with a JWKS-driven flow).
 *
 * Dev mode synthesises `admin` so every page is reachable without a token.
 */
export const scopes = writable<string[]>(readPersisted());

scopes.subscribe((s) => {
  if (!browser) return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(s));
  } catch {
    // localStorage may be unavailable (private mode) — ignore.
  }
});

/**
 * Reactive helper. `$has(scope)` returns true when the active token
 * carries the named scope OR the umbrella admin scope.
 */
export const has: Readable<(scope: string) => boolean> = derived(scopes, ($s) => {
  return (scope: string) => {
    if (!scope) return true;
    if ($s.includes(scope)) return true;
    if (scope !== SCOPE_ADMIN && $s.includes(SCOPE_ADMIN)) return true;
    return false;
  };
});
