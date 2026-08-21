package ui

import "embed"

// FS is the embedded control SPA (same-origin with /api/* and /ai/*).
//
//go:embed index.html css js
var FS embed.FS
