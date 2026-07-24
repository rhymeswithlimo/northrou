package recommend

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// TestThemeFitMovesRecommendationRanking is the gate on the core upgrade: with
// every classic affinity term held equal (same genre, decade, director), the
// keyword-theme term alone must change which unwatched movie ranks higher. If
// this can't separate, the thematic signal isn't actually influencing
// recommendations.
func TestThemeFitMovesRecommendationRanking(t *testing.T) {
	e, _, uid := newTestEngine(t)
	ctx := context.Background()

	// Fillers give the library a realistic size so the vocabulary cutoffs behave
	// as they would in production (on a 5-movie library, maxKeywordDFFrac prunes
	// almost everything). Their keywords are unique, so they don't interfere.
	for i := 0; i < 20; i++ {
		seedMovieKW(t, e, "Filler"+string(rune('A'+i)), []string{"Documentary"},
			[]string{"fillerkw" + string(rune('A'+i)), "topic" + string(rune('A'+i))}, villeneuve, 0)
	}

	// These three share genre + decade + director, so genre/decade/director/
	// actor/language/runtime affinity terms are identical across them. They
	// differ only in keywords. "heist"/"dream" appear on watched+near only
	// (df=2), so they survive the cutoffs and carry the thematic signal.
	watched := seedMovieKW(t, e, "Watched Heist", []string{"Thriller"},
		[]string{"heist", "dream"}, nolan, 0)
	near := seedMovieKW(t, e, "Near Heist", []string{"Thriller"},
		[]string{"heist", "dream"}, nolan, 0)
	farUnrelated := seedMovieKW(t, e, "Wedding", []string{"Thriller"},
		[]string{"romance", "wedding"}, nolan, 0)

	// Watch the heist movie to completion: taste leans heist/dream.
	if err := e.RecordWatch(ctx, uid, model.KindMovie, watched, 120, 120); err != nil {
		t.Fatal(err)
	}
	e.InvalidateAll() // rebuild catalog/vectors including the watch's effect

	rows, err := e.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	items := forYouItems(t, rows)

	nearIdx := indexOfItem(items, near)
	farIdx := indexOfItem(items, farUnrelated)
	if nearIdx < 0 || farIdx < 0 {
		t.Fatalf("expected both candidates in for-you row; near=%d far=%d in %v", nearIdx, farIdx, items)
	}
	if nearIdx >= farIdx {
		t.Fatalf("thematically-near movie ranked %d, unrelated ranked %d; theme term did not move ranking",
			nearIdx, farIdx)
	}
}

func forYouItems(t *testing.T, rows []Row) []Item {
	t.Helper()
	for _, r := range rows {
		if r.Key == "for-you" {
			return r.Items
		}
	}
	t.Fatalf("no for-you row in %d rows", len(rows))
	return nil
}

func indexOfItem(items []Item, id int64) int {
	for i, it := range items {
		if it.ID == id {
			return i
		}
	}
	return -1
}
