package recommend

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

func TestHomePopulatesCache(t *testing.T) {
	e, d, uid := newTestEngine(t)
	seedMovie(t, d, "Inception", 2010, []string{"Sci-Fi"}, nolan, 0)

	if _, ok := e.cachedRows(uid); ok {
		t.Fatal("cache should be empty before first Home call")
	}
	if _, err := e.Home(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.cachedRows(uid); !ok {
		t.Fatal("Home should populate the cache for the user")
	}
}

func TestRecordWatchInvalidatesCache(t *testing.T) {
	e, d, uid := newTestEngine(t)
	id := seedMovie(t, d, "Inception", 2010, []string{"Sci-Fi"}, nolan, 0)

	if _, err := e.Home(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.cachedRows(uid); !ok {
		t.Fatal("expected populated cache")
	}
	// A watch changes the profile; the cached home must be dropped.
	if err := e.RecordWatch(context.Background(), uid, model.KindMovie, id, 100, 100); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.cachedRows(uid); ok {
		t.Fatal("RecordWatch should have invalidated the user's cache")
	}
}

func TestInvalidateAllClearsEveryUser(t *testing.T) {
	e, _, uid := newTestEngine(t)
	e.storeRows(uid, []Row{{Key: "x"}})
	e.storeRows(uid+1, []Row{{Key: "y"}})
	e.InvalidateAll()
	if _, ok := e.cachedRows(uid); ok {
		t.Error("user cache should be cleared")
	}
	if _, ok := e.cachedRows(uid + 1); ok {
		t.Error("other user cache should be cleared")
	}
}

func TestLoadMovieFeature_SingleMovie(t *testing.T) {
	e, d, _ := newTestEngine(t)
	id := seedMovie(t, d, "Inception", 2010, []string{"Sci-Fi", "Thriller"}, nolan, 0)

	mf, ok, err := e.db.LoadMovieFeature(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected the movie to be found")
	}
	if mf.Title != "Inception" || mf.Year != 2010 {
		t.Errorf("unexpected feature: %+v", mf)
	}
	if len(mf.Genres) != 2 {
		t.Errorf("expected 2 genres, got %v", mf.Genres)
	}
	foundDirector := false
	for _, dctr := range mf.Directors {
		if dctr.Name == "Christopher Nolan" {
			foundDirector = true
		}
	}
	if !foundDirector {
		t.Errorf("expected Nolan among directors, got %+v", mf.Directors)
	}
	if !mf.Playable {
		t.Error("seeded movie has a media file and should be playable")
	}

	// A missing id returns ok=false, not an error.
	if _, ok, err := e.db.LoadMovieFeature(context.Background(), 999999); err != nil || ok {
		t.Errorf("missing movie: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}
