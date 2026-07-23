package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestImageCacheRejectsTraversal proves a malicious TMDB path cannot escape the
// cache directory to write an arbitrary file.
func TestImageCacheRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	ic := NewImageCache(dir)
	for _, p := range []string{
		"/../../../etc/cron.d/x.jpg",
		"/../outside.jpg",
		"/..%2f..%2fx.jpg", // literal, but Join won't decode; still must stay contained
	} {
		if _, err := ic.Fetch(context.Background(), p, "w500"); err == nil {
			t.Errorf("path %q should be rejected", p)
		}
	}
	// Nothing was created outside the cache root.
	parent := filepath.Dir(dir)
	if _, err := os.Stat(filepath.Join(parent, "outside.jpg")); !os.IsNotExist(err) {
		t.Fatal("traversal wrote a file outside the cache dir")
	}
}
