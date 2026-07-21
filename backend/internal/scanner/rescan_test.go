package scanner

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
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
