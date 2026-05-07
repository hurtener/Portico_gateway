/**
 * Lightweight in-house i18n.
 *
 * Why no library: 120 lines of code beat a 12 KB dependency for our scope
 * (≈ 200 keys, two locales). The tradeoffs we accept:
 *
 *   - No pluralisation rules beyond what's expressed inline via {n}.
 *   - No automatic locale negotiation against the browser; we honour an
 *     explicit `localStorage.getItem('portico:locale')` and fall back to
 *     English. Users switch via the LocaleSwitcher; persistence sticks.
 *   - Locale bags are eagerly imported at startup. Each bag is ~6 KB
 *     gzipped — adding a code-split path is a future optimisation.
 *
 * Pre-paint: `app.html` reads the persisted locale and stamps
 * `<html lang>` synchronously, so screen readers and assistive tech see
 * the right language before the first paint.
 */

import { writable, derived, type Readable } from 'svelte/store';
import { browser } from '$app/environment';

import en from './locales/en';
import es from './locales/es';

export type Locale = 'en' | 'es';
export const SUPPORTED_LOCALES: Locale[] = ['en', 'es'];

const STORAGE_KEY = 'portico:locale';

const BAGS: Record<Locale, Record<string, string>> = { en, es };

function readPersisted(): Locale {
  if (!browser) return 'en';
  const v = localStorage.getItem(STORAGE_KEY);
  return v === 'es' ? 'es' : 'en';
}

function applyHtmlLang(loc: Locale) {
  if (!browser) return;
  document.documentElement.setAttribute('lang', loc);
}

export const locale = writable<Locale>(readPersisted());

locale.subscribe((loc) => {
  if (!browser) return;
  applyHtmlLang(loc);
  if (loc === 'en') localStorage.removeItem(STORAGE_KEY);
  else localStorage.setItem(STORAGE_KEY, loc);
});

export type Translator = (key: string, vars?: Record<string, string | number>) => string;

/**
 * Reactive translator — subscribe in templates with `$t(...)`. Falls
 * back to English when the active bag misses a key, then to the key
 * itself so missing translations are visible (instead of showing
 * empty strings).
 */
export const t: Readable<Translator> = derived(locale, ($loc) => {
  const active = BAGS[$loc] ?? BAGS.en;
  const fallback = BAGS.en;
  return (key: string, vars: Record<string, string | number> = {}) => {
    const tpl = active[key] ?? fallback[key] ?? key;
    return tpl.replace(/\{(\w+)\}/g, (_, k) => {
      const v = vars[k];
      return v === undefined || v === null ? '' : String(v);
    });
  };
});

export function setLocale(next: Locale) {
  locale.set(next);
}

export function toggleLocale() {
  locale.update((cur) => (cur === 'en' ? 'es' : 'en'));
}

/**
 * Synchronous translator — useful for non-reactive contexts (e.g.
 * building an array of column descriptors during component init).
 * Reads the current locale snapshot once.
 */
export function tn(key: string, vars: Record<string, string | number> = {}): string {
  let current: Locale = 'en';
  locale.subscribe((v) => (current = v))();
  const active = BAGS[current] ?? BAGS.en;
  const fallback = BAGS.en;
  const tpl = active[key] ?? fallback[key] ?? key;
  return tpl.replace(/\{(\w+)\}/g, (_, k) => {
    const v = vars[k];
    return v === undefined || v === null ? '' : String(v);
  });
}
