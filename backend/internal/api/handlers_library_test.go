package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func TestImageHandler_SetsImmutableCacheControl(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "poster.jpg"), []byte("jpeg-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := New(Deps{ImagesDir: dir})

	req := httptest.NewRequest(http.MethodGet, "/api/images/poster.jpg", nil)
	rec := httptest.NewRecorder()
	a.imageHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=2592000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable 30-day", got)
	}
	if rec.Body.String() != "jpeg-bytes" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestImageHandler_DoesNotCacheMissingImage(t *testing.T) {
	a := New(Deps{ImagesDir: t.TempDir()})
	req := httptest.NewRequest(http.MethodGet, "/api/images/missing.jpg", nil)
	rec := httptest.NewRecorder()
	a.imageHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("a missing image must not be cached, got Cache-Control %q", got)
	}
}

// TestCompressMiddleware_PassesMediaThrough guards the "don't break streaming"
// constraint: the content-type-gated gzip middleware wraps every /api response,
// so media must still return 206 with correct partial bytes and no gzip.
func TestCompressMiddleware_PassesMediaThrough(t *testing.T) {
	content := []byte("0123456789abcdef")
	h := middleware.Compress(5, "application/json")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "movie.mp4", time.Time{}, bytes.NewReader(content))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/media/1/stream", nil)
	req.Header.Set("Range", "bytes=0-3")
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206 (range preserved through compress wrapper)", rec.Code)
	}
	if rec.Body.String() != "0123" {
		t.Errorf("partial body = %q, want first 4 bytes", rec.Body.String())
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("media must not be gzipped, got Content-Encoding %q", enc)
	}
	if cr := rec.Header().Get("Content-Range"); cr == "" {
		t.Error("expected Content-Range on a 206 media response")
	}
}

// TestCompressMiddleware_GzipsJSON confirms the same middleware still compresses
// JSON when the client asks for it.
func TestCompressMiddleware_GzipsJSON(t *testing.T) {
	h := middleware.Compress(5, "application/json")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(bytes.Repeat([]byte(`{"k":"v"},`), 100))
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/movies", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Errorf("JSON should be gzipped, got Content-Encoding %q", enc)
	}
}

func TestParsePaging(t *testing.T) {
	cases := []struct {
		query              string
		wantLimit, wantOff int
	}{
		{"", 0, 0},                     // no params => whole library
		{"limit=50", 50, 0},            // limit only
		{"limit=20&offset=40", 20, 40}, // paged
		{"limit=-5", 0, 0},             // non-positive limit ignored
		{"limit=abc", 0, 0},            // garbage ignored
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/movies?"+c.query, nil)
		limit, offset := parsePaging(req)
		if limit != c.wantLimit || offset != c.wantOff {
			t.Errorf("parsePaging(%q) = (%d,%d), want (%d,%d)", c.query, limit, offset, c.wantLimit, c.wantOff)
		}
	}
}
