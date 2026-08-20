package service

import (
	"io/fs"
	"net/http"
)

// mountUI serves the embedded control SPA under /, /css/, and /js/.
// More specific /api, /ai, /v1, and /healthz routes keep precedence.
func mountUI(mux *http.ServeMux, fsys fs.FS) {
	if mux == nil || fsys == nil {
		return
	}
	files := http.FileServer(http.FS(fsys))
	mux.Handle("GET /{$}", files)
	mux.Handle("GET /index.html", http.RedirectHandler("/", http.StatusFound))
	mux.Handle("GET /css/", files)
	mux.Handle("GET /js/", files)
}
