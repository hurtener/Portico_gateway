/**
 * Toast queue store. Components subscribe; helper functions push.
 *
 * Drop-oldest beyond `MAX` so a noisy page can't pile up notifications.
 */
import { writable } from 'svelte/store';

export type ToastTone = 'info' | 'success' | 'warning' | 'danger';

export interface Toast {
  id: string;
  tone: ToastTone;
  title: string;
  description?: string;
  durationMs?: number;
}

const MAX = 5;
const DEFAULT_DURATION = 4500;

export const toasts = writable<Toast[]>([]);

let counter = 0;
function nextId() {
  counter += 1;
  return `t-${Date.now()}-${counter}`;
}

export function pushToast(t: Omit<Toast, 'id'>): string {
  const id = nextId();
  const toast: Toast = { id, durationMs: DEFAULT_DURATION, ...t };
  toasts.update((cur) => {
    const next = [...cur, toast];
    return next.length > MAX ? next.slice(next.length - MAX) : next;
  });
  if (toast.durationMs && toast.durationMs > 0) {
    setTimeout(() => dismiss(id), toast.durationMs);
  }
  return id;
}

export function dismiss(id: string) {
  toasts.update((cur) => cur.filter((t) => t.id !== id));
}

export function info(title: string, description?: string) {
  return pushToast({ tone: 'info', title, description });
}
export function success(title: string, description?: string) {
  return pushToast({ tone: 'success', title, description });
}
export function warning(title: string, description?: string) {
  return pushToast({ tone: 'warning', title, description });
}
export function danger(title: string, description?: string) {
  return pushToast({ tone: 'danger', title, description, durationMs: 7000 });
}
