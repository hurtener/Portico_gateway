// See https://kit.svelte.dev/docs/types#app
declare global {
  namespace App {
    // interface Error {}
    // interface Locals {}
    // interface PageData {}
    // interface Platform {}
  }

  // Vite-injected at build time; see vite.config.ts.
  // eslint-disable-next-line @typescript-eslint/naming-convention
  const __APP_VERSION__: string;
}

export {};
