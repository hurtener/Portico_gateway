// Package ui mounts the Portico Console pages and embedded static assets.
//
// Phase 0 ships placeholder pages; later phases populate live data. The
// implementation uses stdlib html/template instead of Templ for Phase 0 —
// placeholder pages don't justify a code-generation step, and stdlib keeps
// the dependency surface minimal. Migration to Templ is a Phase 3+ concern
// when real UI components land.
package ui

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
)

//go:embed templates/*.html
var templateFS embed.FS

// Static assets embedded into the binary. Content lives at web/console/static/
// in the repo; we mirror it here so the embed.FS is rooted under the package.
//
//go:embed static/*
var staticFS embed.FS

// Deps is the projection of router-level deps the UI needs.
type Deps struct {
	Logger  *slog.Logger
	Version string
	DevMode bool
}

// Mount registers / and /static under the supplied router.
func Mount(r chi.Router, d Deps) {
	tmpls, err := loadTemplates()
	if err != nil {
		// Falling back to a hardcoded message keeps the gateway runnable even if
		// the embed bundle is malformed (e.g. during early bootstrap).
		d.Logger.Warn("ui: failed to parse embedded templates", "err", err)
		tmpls = nil
	}

	// Static assets — strip the package-prefix path so /static/foo.js maps to static/foo.js
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	r.Get("/", consolePage(d, tmpls, "home"))
	r.Get("/servers", consolePage(d, tmpls, "servers"))
	r.Get("/skills", consolePage(d, tmpls, "skills"))
	r.Get("/sessions", consolePage(d, tmpls, "sessions"))
}

func loadTemplates() (*template.Template, error) {
	t := template.New("portico")
	files, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		body, err := templateFS.ReadFile("templates/" + f.Name())
		if err != nil {
			return nil, err
		}
		if _, err := t.New(stripExt(f.Name())).Parse(string(body)); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func stripExt(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[:i]
		}
	}
	return name
}

type pageData struct {
	Title    string
	Page     string
	TenantID string
	Version  string
	DevMode  bool
}

func consolePage(d Deps, tmpls *template.Template, page string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := tenant.From(r.Context())
		data := pageData{
			Title:    titleFor(page),
			Page:     page,
			TenantID: id.TenantID,
			Version:  d.Version,
			DevMode:  d.DevMode,
		}
		if tmpls == nil {
			// Last-resort fallback so / responds 200 even without templates.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><title>Portico</title><h1>Portico Console</h1><p>Templates failed to load.</p>"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpls.ExecuteTemplate(w, "layout", data); err != nil {
			d.Logger.Warn("ui: template render error", "err", err, "page", page)
		}
	}
}

func titleFor(page string) string {
	switch page {
	case "home":
		return "Portico Console"
	case "servers":
		return "Servers · Portico"
	case "skills":
		return "Skills · Portico"
	case "sessions":
		return "Sessions · Portico"
	default:
		return "Portico"
	}
}
