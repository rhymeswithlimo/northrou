package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/metadata"
)

// mockTMDB serves canned search/details responses so scanner tests need no
// network or API key.
func mockTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":1,"results":[{"id":27205,"title":"Inception","release_date":"2010-07-16"}]}`))
	})
	mux.HandleFunc("/movie/27205", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":27205,"title":"Inception","release_date":"2010-07-16",
			"overview":"A thief who steals corporate secrets...","runtime":148,
			"original_language":"en","poster_path":"","backdrop_path":"",
			"genres":[{"id":28,"name":"Action"},{"id":878,"name":"Science Fiction"}],
			"credits":{"cast":[{"id":6193,"name":"Leonardo DiCaprio","character":"Cobb","order":0}],
			"crew":[{"id":525,"name":"Christopher Nolan","job":"Director","department":"Directing"}]}
		}`))
	})
	mux.HandleFunc("/search/tv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":1,"results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20"}]}`))
	})
	mux.HandleFunc("/tv/1396", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20",
			"overview":"A chemistry teacher...","original_language":"en","poster_path":"","backdrop_path":"",
			"genres":[{"id":18,"name":"Drama"}],"credits":{"cast":[],"crew":[]}}`))
	})
	mux.HandleFunc("/tv/1396/season/1/episode/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":62085,"name":"Pilot","overview":"...","episode_number":1,"season_number":1,"runtime":58}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestScanner(t *testing.T, baseURL string) (*Scanner, *db.DB) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	tmdb := metadata.NewClient("testkey", "en-US", metadata.WithBaseURL(baseURL))
	images := metadata.NewImageCache(t.TempDir())
	return New(database, tmdb, images, nil), database
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanMatchesMovieViaTMDB(t *testing.T) {
	srv := mockTMDB(t)
	sc, database := newTestScanner(t, srv.URL)

	movieDir := t.TempDir()
	touch(t, filepath.Join(movieDir, "Inception.2010.1080p.BluRay.x264-GROUP.mkv"))

	if err := sc.Scan(context.Background(), []string{movieDir}, nil); err != nil {
		t.Fatalf("scan: %v", err)
	}

	movies, err := database.ListMovies(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 1 {
		t.Fatalf("expected 1 movie, got %d", len(movies))
	}
	if movies[0].Title != "Inception" || movies[0].Year != 2010 {
		t.Errorf("unexpected movie: %+v", movies[0])
	}
	m, _ := database.GetMovie(context.Background(), movies[0].ID)
	if len(m.Genres) != 2 {
		t.Errorf("expected 2 genres, got %v", m.Genres)
	}
	if m.File == nil {
		t.Error("expected media file linked to movie")
	}

	p := sc.Progress()
	if p.Matched != 1 || p.Unmatched != 0 {
		t.Errorf("expected matched=1 unmatched=0, got %+v", p)
	}
}

func TestScanMatchesEpisode(t *testing.T) {
	srv := mockTMDB(t)
	sc, database := newTestScanner(t, srv.URL)

	showDir := t.TempDir()
	touch(t, filepath.Join(showDir, "Breaking Bad", "Season 1", "Breaking.Bad.S01E01.1080p.mkv"))

	if err := sc.Scan(context.Background(), nil, []string{showDir}); err != nil {
		t.Fatalf("scan: %v", err)
	}

	shows, err := database.ListShows(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(shows) != 1 || shows[0].Title != "Breaking Bad" {
		t.Fatalf("unexpected shows: %+v", shows)
	}
	full, _ := database.GetShow(context.Background(), shows[0].ID)
	if len(full.Seasons) != 1 || len(full.Seasons[0].Episodes) != 1 {
		t.Fatalf("expected 1 season/1 episode, got %+v", full.Seasons)
	}
	if full.Seasons[0].Episodes[0].Title != "Pilot" {
		t.Errorf("expected episode title Pilot, got %q", full.Seasons[0].Episodes[0].Title)
	}
}

func TestScanFlagsUnmatchedWithoutTMDBKey(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	// No API key => disabled client.
	tmdb := metadata.NewClient("", "en-US")
	sc := New(database, tmdb, metadata.NewImageCache(t.TempDir()), nil)

	dir := t.TempDir()
	touch(t, filepath.Join(dir, "Some.Movie.2021.1080p.mkv"))
	if err := sc.Scan(context.Background(), []string{dir}, nil); err != nil {
		t.Fatal(err)
	}
	un, _ := database.ListUnmatched(context.Background())
	if len(un) != 1 || !strings.Contains(un[0].Reason, "TMDB") {
		t.Fatalf("expected 1 unmatched with TMDB reason, got %+v", un)
	}
}
