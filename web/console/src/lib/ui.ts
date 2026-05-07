/**
 * Console-wide UI state — sidebar collapse, command-palette visibility,
 * notifications-popover visibility. Persists what makes sense across
 * reloads (sidebar collapse) and keeps ephemeral overlays in memory.
 */

import { writable } from 'svelte/store';
import { browser } from '$app/environment';

const SIDEBAR_KEY = 'portico:sidebarCollapsed';

function readSidebarCollapsed(): boolean {
  if (!browser) return false;
  return localStorage.getItem(SIDEBAR_KEY) === '1';
}

export const sidebarCollapsed = writable<boolean>(readSidebarCollapsed());

sidebarCollapsed.subscribe((v) => {
  if (!browser) return;
  if (v) {
    localStorage.setItem(SIDEBAR_KEY, '1');
    document.documentElement.setAttribute('data-sidebar', 'collapsed');
  } else {
    localStorage.removeItem(SIDEBAR_KEY);
    document.documentElement.removeAttribute('data-sidebar');
  }
});

export function toggleSidebar() {
  sidebarCollapsed.update((v) => !v);
}

/**
 * Command palette open state. Toggled by Cmd/Ctrl+K and the TopBar
 * search button.
 */
export const commandPaletteOpen = writable<boolean>(false);

export function openCommandPalette() {
  commandPaletteOpen.set(true);
}
export function closeCommandPalette() {
  commandPaletteOpen.set(false);
}
