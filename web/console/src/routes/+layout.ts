// SPA mode: prerender disabled, SSR off. The Go binary serves the static
// build output; client-side routing handles everything past index.html.
export const prerender = false;
export const ssr = false;
export const trailingSlash = 'never';
