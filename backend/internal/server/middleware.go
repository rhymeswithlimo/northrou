package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// maxRequestBody caps the size of any request body the API will read. Every
// handler consumes small JSON; media is read from disk, never uploaded, so a
// generous 1 MiB ceiling protects the (deliberately weak) box from an unbounded
// body OOM without constraining anything legitimate. Response streaming is
// unaffected (MaxBytesReader limits reads, not writes).
const maxRequestBody = 1 << 20

// slogLogger logs each request via slog with method, path, status, and latency.
func slogLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Debug("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"dur", time.Since(start).String(),
			"reqid", middleware.GetReqID(r.Context()),
		)
	})
}

// maxBodyBytes wraps every request body in http.MaxBytesReader so an oversized
// body is rejected rather than buffered. The tunnel path is already bounded by
// the 16 MiB frame cap; this covers the direct HTTP path, which otherwise had no
// limit.
func maxBodyBytes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

// secureHeaders sets conservative response headers. The API returns JSON, VTT,
// images, and media; nosniff stops content-type confusion, and the frame/
// referrer/permissions headers harden the served client pages. No CSP is set
// here because the embedded client and the Tauri shell define their own.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// allowedOrigin reports whether an Origin may make credentialed cross-origin
// requests. Northrou's clients are: the Tauri desktop/mobile shell (custom
// scheme), the dev server (http://localhost:PORT), and the embedded web client
// (same-origin, which browsers send with no Origin or a matching one). Anything
// else — an arbitrary website a LAN user happens to visit — must NOT be able to
// read API responses, which previously it could because the old policy reflected
// any Origin with credentials enabled.
func allowedOrigin(origin string) bool {
	if origin == "" {
		return true // same-origin / non-browser client
	}
	// Tauri v2 app origins: tauri://localhost (macOS/iOS) and
	// http(s)://tauri.localhost (Windows/Android).
	switch origin {
	case "tauri://localhost", "https://tauri.localhost", "http://tauri.localhost":
		return true
	}
	// Local dev and same-machine browsers: http(s)://localhost[:port] and
	// http(s)://127.0.0.1[:port].
	for _, p := range []string{"http://localhost", "https://localhost", "http://127.0.0.1", "https://127.0.0.1"} {
		if origin == p || strings.HasPrefix(origin, p+":") {
			return true
		}
	}
	return false
}

// cors applies a strict, allowlisted CORS policy for the Tauri/web frontend and
// local dev. It replaces an earlier policy that reflected ANY Origin while
// sending Access-Control-Allow-Credentials: true, which let any website script
// credentialed requests to the box and read the responses (drive-by pairing /
// admin from a LAN victim's browser). Media streaming still needs Range and the
// standard preflight headers.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigin(origin) {
			h := w.Header()
			if origin == "" {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range")
			h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges")
		}
		if r.Method == http.MethodOptions {
			// Reflect the preflight outcome: allowed origins got the headers
			// above; disallowed ones get a bare 204 with no ACAO (the browser
			// then blocks the real request).
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
