// Package web embeds Northrou's minimal built-in static assets: the first-run
// setup wizard. This is a placeholder served by the backend until the full
// Tauri frontend under /frontend is linked in. It is intentionally tiny and
// dependency-free.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets/*
var assetsFS embed.FS

// Handler returns an http.Handler that serves the embedded assets, falling
// back to index.html for the root.
func Handler() http.Handler {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err) // embedded FS is known-good at build time
	}
	return http.FileServer(http.FS(sub))
}
