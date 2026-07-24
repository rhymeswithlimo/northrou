package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// fakeSubs records which files sidecar extraction was invoked for.
type fakeSubs struct {
	mu       sync.Mutex
	sidecars []string
}

func (f *fakeSubs) ExtractForFile(context.Context, int64, *model.MediaFile) error { return nil }
func (f *fakeSubs) ExtractSidecars(_ context.Context, _ int64, videoPath string) error {
	f.mu.Lock()
	f.sidecars = append(f.sidecars, videoPath)
	f.mu.Unlock()
	return nil
}
func (f *fakeSubs) calledFor(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.sidecars, path)
}

// Rescan (force) must refetch TMDB metadata for a file that a plain scan would
// skip as already-scanned-and-unchanged. Counting hits to the movie-details
// endpoint proves it: a second plain Scan does not hit it, a Rescan does.
func TestRescanRefetchesMetadata(t *testing.T) {
	var detailHits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":1,"results":[{"id":27205,"title":"Inception","release_date":"2010-07-16"}]}`))
	})
	mux.HandleFunc("/movie/27205", func(w http.ResponseWriter, r *http.Request) {
		detailHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":27205,"title":"Inception","release_date":"2010-07-16",
			"runtime":148,"original_language":"en","credits":{"cast":[],"crew":[]}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	sc, _ := newTestScanner(t, srv.URL)
	movieDir := t.TempDir()
	touch(t, filepath.Join(movieDir, "Inception.2010.1080p.BluRay.x264-GROUP.mkv"))
	ctx := context.Background()

	if err := sc.Scan(ctx, []string{movieDir}, nil); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if got := detailHits.Load(); got != 1 {
		t.Fatalf("first scan: want 1 detail fetch, got %d", got)
	}

	// A plain re-scan of the unchanged file must not refetch.
	if err := sc.Scan(ctx, []string{movieDir}, nil); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if got := detailHits.Load(); got != 1 {
		t.Fatalf("plain rescan should skip unchanged file, but detail fetches = %d", got)
	}

	// Rescan (force) must refetch even though nothing on disk changed.
	if err := sc.Rescan(ctx, []string{movieDir}, nil); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if got := detailHits.Load(); got != 2 {
		t.Fatalf("rescan should refetch metadata, want 2 total detail fetches, got %d", got)
	}

	// The force flag must not leak into a subsequent plain scan.
	if err := sc.Scan(ctx, []string{movieDir}, nil); err != nil {
		t.Fatalf("post-rescan scan: %v", err)
	}
	if got := detailHits.Load(); got != 2 {
		t.Fatalf("plain scan after rescan refetched (force leaked); detail fetches = %d", got)
	}
}

// A subtitle added next to an already-scanned video must be discovered on the
// next scan even though the video's size/mtime are unchanged (NeedsScan=false).
func TestRescanDiscoversLateSidecar(t *testing.T) {
	srv := mockTMDB(t)
	sc, _ := newTestScanner(t, srv.URL)
	fs := &fakeSubs{}
	sc.SetSubtitleExtractor(fs)

	movieDir := t.TempDir()
	video := filepath.Join(movieDir, "Inception.2010.1080p.BluRay.x264-GROUP.mkv")
	touch(t, video)

	ctx := context.Background()
	if err := sc.Scan(ctx, []string{movieDir}, nil); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if !fs.calledFor(video) {
		t.Fatal("first scan should have run sidecar extraction")
	}

	// Simulate downloading a subtitle later, then rescanning. The video is
	// unchanged, so this exercises the NeedsScan=false reconcile path.
	touch(t, filepath.Join(movieDir, "Inception.2010.1080p.BluRay.x264-GROUP.en.srt"))
	fs.sidecars = nil
	if err := sc.Scan(ctx, []string{movieDir}, nil); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if !fs.calledFor(video) {
		t.Fatal("rescan did not reconcile sidecars for an unchanged video")
	}
}
