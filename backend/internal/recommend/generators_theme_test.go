package recommend

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// seedLibraryWithThemes builds a library big enough for the vocabulary cutoffs
// to behave, then returns the engine, user, and a couple of ids of interest.
func themeLibrary(t *testing.T) (*Engine, int64) {
	t.Helper()
	e, _, uid := newTestEngine(t)
	// Fillers to give the library realistic size.
	for i := 0; i < 20; i++ {
		seedMovieKW(t, e, "Filler"+string(rune('A'+i)), []string{"Documentary"},
			[]string{"fillerkw" + string(rune('A'+i)), "topic" + string(rune('A'+i))}, villeneuve, 0)
	}
	return e, uid
}

func TestGenBecauseYouWatched(t *testing.T) {
	e, uid := themeLibrary(t)
	ctx := context.Background()

	watched := seedMovieKW(t, e, "Space Heist", []string{"Thriller"},
		[]string{"heist", "space", "crew"}, nolan, 0)
	// Two unwatched neighbors sharing themes with the watched movie.
	seedMovieKW(t, e, "Orbit Job", []string{"Thriller"}, []string{"heist", "space"}, villeneuve, 0)
	seedMovieKW(t, e, "Crew Cut", []string{"Thriller"}, []string{"space", "crew"}, villeneuve, 0)
	seedMovieKW(t, e, "Deep Space Team", []string{"Sci-Fi"}, []string{"space", "crew"}, nolan, 0)

	if err := e.RecordWatch(ctx, uid, model.KindMovie, watched, 120, 120); err != nil {
		t.Fatal(err)
	}
	e.InvalidateAll()

	rows, err := e.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	var row *Row
	for i := range rows {
		if rows[i].Key == "becausewatched-"+strconv.FormatInt(watched, 10) {
			row = &rows[i]
		}
	}
	if row == nil {
		t.Fatalf("no Because-You-Watched row for the seed; rows: %s", rowKeys(rows))
	}
	if !strings.Contains(row.Title, "Space Heist") {
		t.Fatalf("row title = %q, want it to name the seed", row.Title)
	}
	if row.Subtitle == "" {
		t.Fatalf("Because-You-Watched row should have a subtitle")
	}
	if len(row.Items) < minThemeRowItems {
		t.Fatalf("row has %d items, want >= %d", len(row.Items), minThemeRowItems)
	}
	// The watched movie itself must not appear.
	for _, it := range row.Items {
		if it.ID == watched {
			t.Fatal("seed movie should not appear in its own Because-You-Watched row")
		}
	}
}

func TestGenKeywordThemes(t *testing.T) {
	e, uid := themeLibrary(t)
	ctx := context.Background()

	// A cluster of "space" movies; watch one so "space" dominates taste.
	watched := seedMovieKW(t, e, "Space One", []string{"Sci-Fi"}, []string{"space", "orbit"}, nolan, 0)
	seedMovieKW(t, e, "Space Two", []string{"Sci-Fi"}, []string{"space", "station"}, villeneuve, 0)
	seedMovieKW(t, e, "Space Three", []string{"Sci-Fi"}, []string{"space", "planet"}, villeneuve, 0)
	seedMovieKW(t, e, "Space Four", []string{"Sci-Fi"}, []string{"space", "alien"}, villeneuve, 0)

	if err := e.RecordWatch(ctx, uid, model.KindMovie, watched, 120, 120); err != nil {
		t.Fatal(err)
	}
	e.InvalidateAll()

	rows, err := e.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	var themeRow *Row
	for i := range rows {
		if rows[i].Key == "theme-space" {
			themeRow = &rows[i]
		}
	}
	if themeRow == nil {
		t.Fatalf("no theme-space row; rows: %s", rowKeys(rows))
	}
	if !strings.EqualFold(themeRow.Title, "Movies About Space") {
		t.Fatalf("theme row title = %q, want 'Movies About Space'", themeRow.Title)
	}
	// The watched movie is excluded (it's a candidate filter), unwatched space
	// movies included.
	for _, it := range themeRow.Items {
		if it.ID == watched {
			t.Fatal("watched movie should be excluded from theme row")
		}
	}
	if len(themeRow.Items) < minThemeRowItems {
		t.Fatalf("theme row has %d items, want >= %d", len(themeRow.Items), minThemeRowItems)
	}
}

