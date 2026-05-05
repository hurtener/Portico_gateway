package api

import "net/http"

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func readyzHandler(d Deps) http.HandlerFunc {
	version := d.Version
	if version == "" {
		version = "v0.0.0"
	}
	commit := d.BuildCommit
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ready",
			"version": version,
			"commit":  commit,
		})
	}
}
