/**
 * Theme management for the Portico Console.
 *
 * Two layers cooperate:
 *
 *   1. A pre-paint inline script in `app.html` reads the persisted theme
 *      (or matches `prefers-color-scheme`) and stamps `data-theme` on
 *      `<html>` *before* the first paint. That kills theme flash.
 *
 *   2. This module exposes a Svelte store for runtime toggling. When the
 *      store changes we update both `<html data-theme>` and localStorage
 *      so the next pre-paint sees the right value.
 *
 * Three modes:
 *   - 'light' — explicit user pick.
 *   - 'dark'  — explicit user pick.
 *   - 'system' — follow `prefers-color-scheme`. Default for new users.
 */

import { writable, type Writable } from 'svelte/store';
import { browser } from '$app/environment';

export type ThemeMode = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

const STORAGE_KEY = 'portico:theme';

function readPersisted(): ThemeMode {
  if (!browser) return 'system';
  const v = localStorage.getItem(STORAGE_KEY);
  if (v === 'light' || v === 'dark' || v === 'system') return v;
  return 'system';
}

function systemPrefersDark(): boolean {
  if (!browser) return false;
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

export function resolve(mode: ThemeMode): ResolvedTheme {
  if (mode === 'system') return systemPrefersDark() ? 'dark' : 'light';
  return mode;
}

function apply(mode: ThemeMode) {
  if (!browser) return;
  const resolved = resolve(mode);
  document.documentElement.setAttribute('data-theme', resolved);
  // Also keep `color-scheme` in sync so form controls + scrollbars match.
  document.documentElement.style.colorScheme = resolved;
}

function persist(mode: ThemeMode) {
  if (!browser) return;
  if (mode === 'system') {
    localStorage.removeItem(STORAGE_KEY);
  } else {
    localStorage.setItem(STORAGE_KEY, mode);
  }
}

/**
 * The reactive theme mode store. Subscribe in components; call
 * `setTheme(...)` to change.
 */
export const themeMode: Writable<ThemeMode> = writable<ThemeMode>(readPersisted());

themeMode.subscribe((m) => {
  apply(m);
  persist(m);
});

export function setTheme(mode: ThemeMode) {
  themeMode.set(mode);
}

/**
 * Two-state toggle. Resolves the current effective theme (honouring
 * 'system' if that's still active) and flips to the opposite. Once the
 * user picks via the toggle, the store leaves 'system' and stays on the
 * chosen mode.
 */
export function cycleTheme() {
  themeMode.update((m) => (resolve(m) === 'dark' ? 'light' : 'dark'));
}

/**
 * Re-evaluate when the OS theme changes and the user is on `system`.
 * Wire this once from the root layout's onMount.
 */
export function watchSystemPreference(): () => void {
  if (!browser) return () => {};
  const mq = window.matchMedia('(prefers-color-scheme: dark)');
  const handler = () => {
    let current: ThemeMode = 'system';
    themeMode.subscribe((m) => (current = m))();
    if (current === 'system') apply('system');
  };
  mq.addEventListener('change', handler);
  return () => mq.removeEventListener('change', handler);
}
