package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// uiFS holds the React SPA built by `make ui-build`. The Makefile copies
// web/dist into internal/api/dist before `go build`. The directive uses
// `all:dist` so dotfiles are included if any.
//
//go:embed all:dist
var uiFS embed.FS

// UIHandler returns a handler that serves the embedded SPA, falling back
// to index.html for any unknown path (so client-side routing works).
func UIHandler() http.Handler {
	sub, err := fs.Sub(uiFS, "dist")
	if err != nil {
		// dist may be absent during initial development; serve a hint.
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "UI bundle not built — run `make ui-build`", http.StatusNotFound)
		})
	}
	staticFS := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't fall back for /api or /healthz; those are handled elsewhere.
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/healthz") {
			http.NotFound(w, r)
			return
		}
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if _, err := fs.Stat(sub, clean); err == nil {
			staticFS.ServeHTTP(w, r)
			return
		}
		// SPA fallback: rewrite the URL path to / so FileServer hits index.html.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		staticFS.ServeHTTP(w, r2)
	})
}
