package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// A duplicate movie (mkv + mp4) links one best copy; deleting the winner
// promotes the survivor; deleting all copies removes the title.
func TestReconcileDuplicatePromotionAndDeletion(t *testing.T) {
	srv := mockTMDB(t)
	sc, database := newTestScanner(t, srv.URL)
	ctx := context.Background()

	dir := t.TempDir()
	mkv := filepath.Join(dir, "Inception.2010.1080p.BluRay.x264-GROUP.mkv")
	mp4 := filepath.Join(dir, "Inception.2010.1080p.BluRay.x264-GROUP.mp4")
	touch(t, mkv)
	touch(t, mp4)

	if err := sc.Scan(ctx, []string{dir}, nil); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	movies, _ := database.ListMovies(ctx, 0, 0)
	if len(movies) != 1 {
		t.Fatalf("want 1 movie after dedup, got %d", len(movies))
	}
	if p := linkedPath(t, database); !strings.HasSuffix(p, ".mkv") {
		t.Fatalf("mkv should win, linked = %s", p)
	}

	// Delete the winner; the mp4 must be promoted on rescan.
	if err := os.Remove(mkv); err != nil {
		t.Fatal(err)
	}
	if err := sc.Scan(ctx, []string{dir}, nil); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	movies, _ = database.ListMovies(ctx, 0, 0)
	if len(movies) != 1 {
		t.Fatalf("want 1 movie after promotion, got %d", len(movies))
	}
	if p := linkedPath(t, database); !strings.HasSuffix(p, ".mp4") {
		t.Fatalf("mp4 should be promoted, linked = %s", p)
	}

	// Delete the last copy; the title must disappear.
	if err := os.Remove(mp4); err != nil {
		t.Fatal(err)
	}
	if err := sc.Scan(ctx, []string{dir}, nil); err != nil {
		t.Fatalf("scan 3: %v", err)
	}
	movies, _ = database.ListMovies(ctx, 0, 0)
	if len(movies) != 0 {
		t.Fatalf("want 0 movies after deleting all copies, got %d", len(movies))
	}
}

// linkedPath returns the path of the single media file currently in the DB.
func linkedPath(t *testing.T, database *db.DB) string {
	t.Helper()
	files, err := database.AllMediaFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("want exactly 1 media file, got %d", len(files))
	}
	return files[0].Path
}
