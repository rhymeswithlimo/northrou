package metadata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const imageBaseURL = "https://image.tmdb.org/t/p"

// ImageCache downloads and stores TMDB images on local disk so the frontend is
// served from the home server, never TMDB directly.
type ImageCache struct {
	dir  string
	http *http.Client
}

// NewImageCache stores images under dataDir/images.
func NewImageCache(dataDir string) *ImageCache {
	return &ImageCache{
		dir:  filepath.Join(dataDir, "images"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// Fetch downloads the image at the given TMDB path (e.g. "/abc.jpg") at the
// given size (e.g. "w500", "original") if not already cached, and returns the
// path relative to the cache dir (e.g. "w500/abc.jpg"), suitable for building a
// serving URL. An empty tmdbPath yields "" with no error.
func (ic *ImageCache) Fetch(ctx context.Context, tmdbPath, size string) (string, error) {
	if tmdbPath == "" {
		return "", nil
	}
	rel := filepath.ToSlash(filepath.Join(size, filepath.FromSlash(strings.TrimPrefix(tmdbPath, "/"))))
	dst := filepath.Join(ic.dir, filepath.FromSlash(rel))

	if _, err := os.Stat(dst); err == nil {
		return rel, nil // already cached
	}

	url := fmt.Sprintf("%s/%s%s", imageBaseURL, size, tmdbPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := ic.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch image %s: HTTP %d", url, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return rel, os.Rename(tmp, dst)
}

// Dir returns the root image cache directory (served read-only by the API).
func (ic *ImageCache) Dir() string { return ic.dir }
