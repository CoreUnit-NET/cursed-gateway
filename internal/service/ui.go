package service

import (
	"io/fs"
	"net/http"
)

// mountUI serves the control SPA under /, /css/, and /js/ when uiFS is provided.
// More specific /api, /ai, /v1, and /healthz routes keep precedence.
func mountUI(mux *http.ServeMux, fsys fs.FS) {
	if mux == nil || fsys == nil {
		return
	}
	fsrv := http.FileServer(http.FS(fsys))
	// Wrap the FileServer to add security headers
	files := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CSP: only allow resources from the same origin for the UI
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self';")
		// Prevent framing of the UI
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		fsrv.ServeHTTP(w, r)
	})

	mux.Handle("GET /{$}", files)
	mux.Handle("GET /index.html", http.RedirectHandler("/", http.StatusFound))
	mux.Handle("GET /css/", files)
	mux.Handle("GET /js/", files)
}
