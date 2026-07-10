package db

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func sampleMovie(tmdbID int64, title string) *model.Movie {
	return &model.Movie{
		TMDBID: tmdbID, Title: title, Year: 2010, Runtime: 120, OriginalLang: "en",
		Genres: []string{"Sci-Fi", "Thriller"},
		Cast: []model.Credit{
			{PersonID: 1, Name: "Actor One", Role: "Cobb", Order: 0},
			{PersonID: 2, Name: "Actor Two", Role: "Arthur", Order: 1},
		},
		Crew: []model.Credit{{PersonID: 525, Name: "Christopher Nolan", Role: "Director"}},
	}
}

// TestUpsertMovie_WritesGenresAndCreditsAtomically verifies the transaction-
// wrapped upsert persists the movie row plus its genre and credit links.
func TestUpsertMovie_WritesGenresAndCreditsAtomically(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id, err := d.UpsertMovie(ctx, sampleMovie(1000, "Inception"))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	m, err := d.GetMovie(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(m.Genres) != 2 {
		t.Errorf("genres = %v, want 2", m.Genres)
	}

	feat, ok, err := d.LoadMovieFeature(ctx, id)
	if err != nil || !ok {
		t.Fatalf("feature load: ok=%v err=%v", ok, err)
	}
	if len(feat.Actors) != 2 {
		t.Errorf("actors = %d, want 2", len(feat.Actors))
	}
	if len(feat.Directors) != 1 || feat.Directors[0].Name != "Christopher Nolan" {
		t.Errorf("directors = %+v, want Nolan", feat.Directors)
	}

	// Re-upsert (idempotent): counts must not double.
	if _, err := d.UpsertMovie(ctx, sampleMovie(1000, "Inception")); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	feat, _, _ = d.LoadMovieFeature(ctx, id)
	if len(feat.Actors) != 2 {
		t.Errorf("after re-upsert actors = %d, want 2 (credits replaced, not appended)", len(feat.Actors))
	}
}

// TestConcurrentReadsDuringWrites drives a write-heavy scan alongside concurrent
// browse reads and asserts neither deadlocks nor returns SQLITE_BUSY. This is
// the scan-vs-browse contention the worst-case box faces.
func TestConcurrentReadsDuringWrites(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 3)

	// Writer: many transactional upserts (the scan write storm).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			m := sampleMovie(int64(2000+i), fmt.Sprintf("Movie %d", i))
			if _, err := d.UpsertMovie(ctx, m); err != nil {
				errs <- fmt.Errorf("write %d: %w", i, err)
				return
			}
		}
	}()

	// Two readers: repeated list + detail reads during the writes.
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if _, err := d.ListMovies(ctx, 50, 0); err != nil {
					errs <- fmt.Errorf("list: %w", err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
