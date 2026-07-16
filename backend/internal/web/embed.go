// Package web serves Northrou's client: the Vite build of /frontend, embedded
// into the binary so a single file is the whole server.
//
// The assets are produced by `make frontend`, which runs the Vite build and
// copies frontend/dist here. They are not checked in - the build output is
// generated, and a diff full of hashed bundles helps nobody. Only .gitkeep is
// committed, so `go build` still works in a fresh clone; the server then reports
// that the client has not been built rather than serving a blank 404.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:assets
var assetsFS embed.FS

// built reports whether a real client build is embedded, as opposed to just the
// .gitkeep placeholder.
func built(sub fs.FS) bool {
	_, err := fs.Stat(sub, "index.html")
	return err == nil
}

// Handler serves the embedded client.
func Handler() http.Handler {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err) // embedded FS is known-good at build time
	}

	if !built(sub) {
		return http.HandlerFunc(notBuilt)
	}

	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hashed bundles are immutable by construction: their name changes when
		// their content does, so they can be cached hard. The HTML that points
		// at them must not be, or a client would keep loading the old bundles.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

const notBuiltPage = `<!doctype html>
<meta charset="utf-8">
<title>Northrou</title>
<style>
  body { font: 15px/1.6 system-ui, sans-serif; max-width: 34rem; margin: 12vh auto; padding: 0 1rem; }
  code { background: #8882; padding: .15rem .35rem; border-radius: 4px; }
</style>
<h1>Client not built</h1>
<p>The API is running, but no client is embedded in this binary.</p>
<p>Build it with <code>make frontend</code> (or <code>make build</code>, which
does both), then restart. During development, <code>npm run dev</code> in
<code>frontend/</code> serves the client on :5173 and proxies the API here.</p>
`

func notBuilt(w http.ResponseWriter, r *http.Request) {
	// The API still works; only the UI is missing. Say which, rather than
	// letting a bare 404 look like the server is broken.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(notBuiltPage))
}
