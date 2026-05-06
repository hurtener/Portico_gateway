// Package ui mounts the Portico Console — a SvelteKit SPA built with
// adapter-static and embedded into the Go binary at compile time.
//
// The handler serves three classes of request:
//   - "/_app/*"   — SvelteKit's hashed asset bundle, served verbatim.
//   - "/<file>"   — any concrete file in the build root (favicon.svg, etc.).
//   - "/<route>"  — unknown paths fall back to index.html so client-side
//     routing (SvelteKit) can resolve them in the browser.
//
// The handler is mounted at the root of the auth group so the Console
// inherits the same tenant context as the REST API.
package ui

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	console "github.com/hurtener/Portico_gateway/web/console"
)

// Deps is the projection of router-level deps the UI needs.
type Deps struct {
	Logger  *slog.Logger
	Version string
	DevMode bool
}

// Mount installs the Console on the supplied router. When the SvelteKit
// build is populated the handler serves the SPA; otherwise it falls
// back to a placeholder page that tells the operator to run
// `npm run build`. Either way, requests under known API prefixes are
// passed through to the chi 404 envelope.
func Mount(r chi.Router, d Deps) {
	buildFS, err := fs.Sub(console.Build, "build")
	if err != nil {
		d.Logger.Warn("ui: console build subfs unavailable", "err", err)
		buildFS = nil
	}

	hasReal := buildFS != nil && hasIndex(buildFS)
	if !hasReal {
		d.Logger.Warn("ui: console build is empty — run `npm --prefix web/console run build`")
	}

	var handler http.Handler
	if hasReal {
		handler = spaHandler(buildFS, d.Logger)
	} else {
		handler = placeholderRouter(d)
	}

	r.Method("GET", "/", handler)
	r.Method("GET", "/*", handler)
}

func hasIndex(fsys fs.FS) bool {
	f, err := fsys.Open("index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// apiPrefixes lists URL prefixes the SPA must NEVER swallow. Requests
// that fall through the REST/MCP routes for these prefixes should reach
// the chi 404 handler so callers get a JSON error envelope.
var apiPrefixes = []string{
	"/v1/",
	"/mcp",
	"/healthz",
	"/readyz",
}

func isAPIPath(p string) bool {
	for _, pre := range apiPrefixes {
		if p == pre || strings.HasPrefix(p, pre+"/") || strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// spaHandler serves files from the SvelteKit build output and falls
// back to index.html for unknown paths so the browser router can
// resolve them. Paths under known API prefixes are passed through to
// the chi 404 envelope instead of being shadowed by the SPA.
func spaHandler(buildFS fs.FS, logger *slog.Logger) http.Handler {
	fileServer := http.FileServer(http.FS(buildFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urlPath := r.URL.Path
		if isAPIPath(urlPath) {
			writeJSONNotFound(w, r)
			return
		}

		if urlPath == "" || urlPath == "/" {
			serveIndex(w, r, buildFS, logger)
			return
		}

		// Normalise and resolve against the embed root.
		clean := strings.TrimPrefix(path.Clean(urlPath), "/")
		if clean == "" || clean == "." {
			serveIndex(w, r, buildFS, logger)
			return
		}

		f, err := buildFS.Open(clean)
		if err != nil {
			// Unknown path → SPA fallback. SvelteKit owns routing past
			// index.html.
			serveIndex(w, r, buildFS, logger)
			return
		}
		info, statErr := f.Stat()
		_ = f.Close()
		if statErr != nil || info.IsDir() {
			serveIndex(w, r, buildFS, logger)
			return
		}

		// Hashed assets under /_app/immutable/ are content-addressable; cache aggressively.
		if strings.HasPrefix(clean, "_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// writeJSONNotFound emits the same JSON 404 envelope the chi NotFound
// handler produces, so SPA-shadowed API paths look identical to a
// genuine miss.
func writeJSONNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	body := fmt.Sprintf(`{"error":"not_found","message":"no route","details":{"path":%q}}`, r.URL.Path)
	_, _ = w.Write([]byte(body))
}

func serveIndex(w http.ResponseWriter, r *http.Request, buildFS fs.FS, logger *slog.Logger) {
	body, err := fs.ReadFile(buildFS, "index.html")
	if err != nil {
		logger.Warn("ui: failed to read embedded index.html", "err", err)
		http.Error(w, "console unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		logger.Debug("ui: index write error", "err", err)
	}
	_ = r // keep parity with handler signature
}

// placeholderRouter renders a minimal HTML page when the SvelteKit
// build is missing. API-path requests still pass through to chi's 404
// envelope, and a tiny stand-in favicon keeps clients quiet.
func placeholderRouter(d Deps) http.Handler {
	body := fmt.Sprintf(`<!doctype html>
<meta charset="utf-8">
<title>Portico Console</title>
<style>
  body { font-family: system-ui, -apple-system, sans-serif; max-width: 36rem; margin: 4rem auto; padding: 0 1rem; color: #0f172a; }
  code { background: #f1f5f9; padding: 0.125rem 0.375rem; border-radius: 0.25rem; }
  .badge { display: inline-block; background: #fef9c3; color: #854d0e; padding: 0.125rem 0.5rem; border-radius: 999px; font-size: 0.75rem; }
</style>
<h1>Portico Console</h1>
<p class="badge">build pending</p>
<p>The SvelteKit build output is empty. Run <code>npm --prefix web/console run build</code> and rebuild the binary to populate this page.</p>
<p style="color:#64748b;font-size:0.875rem;">Portico %s · dev_mode=%t</p>
`, d.Version, d.DevMode)

	const placeholderFavicon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" width="64" height="64"><rect width="64" height="64" rx="12" fill="#2563eb"/><text x="32" y="42" font-family="ui-sans-serif,system-ui,sans-serif" font-size="28" font-weight="700" text-anchor="middle" fill="#ffffff">P</text></svg>`

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			writeJSONNotFound(w, r)
			return
		}
		if r.URL.Path == "/favicon.svg" {
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(placeholderFavicon))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(body))
	})
}
