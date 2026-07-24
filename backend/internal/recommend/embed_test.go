package recommend

import (
	"math"
	"testing"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

func mf(id int64, keywords, genres []string) db.MovieFeature {
	return db.MovieFeature{ID: id, Title: "M", Keywords: keywords, Genres: genres, Playable: true}
}

func TestNormalizeKeyword(t *testing.T) {
	cases := map[string]string{
		"Slow Burn":  "slow-burn",
		"slow_burn":  "slow-burn",
		"slowburn":   "slow-burn",
		"  Heist  ":  "heist",
		"Time Travel Story": "time-travel",
		"dystopian":  "dystopia",
		"":           "",
	}
	for in, want := range cases {
		if got := normalizeKeyword(in); got != want {
			t.Errorf("normalizeKeyword(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVectorCosineOrdering(t *testing.T) {
	movies := []db.MovieFeature{
		mf(1, []string{"heist", "dream", "subconscious"}, nil),
		mf(2, []string{"heist", "dream", "city"}, nil),      // most like 1
		mf(3, []string{"romance", "wedding", "family"}, nil), // unlike 1
	}
	vs := buildVectorSpace(movies, nil)

	v1, ok := vs.vecOf(movieKey(1))
	if !ok {
		t.Fatal("movie 1 has no vector")
	}
	v2, _ := vs.vecOf(movieKey(2))
	v3, ok3 := vs.vecOf(movieKey(3))

	sim12 := cosine(v1, v2)
	if sim12 <= 0 {
		t.Fatalf("cosine(1,2) = %v, want > 0", sim12)
	}
	// Movie 3 shares no keywords with 1; its df=1 tokens are dropped, and it
	// shares none of 1's tokens, so cosine is 0.
	if ok3 {
		if s := cosine(v1, v3); s != 0 {
			t.Fatalf("cosine(1,3) = %v, want 0 (no shared themes)", s)
		}
	}
	if sim12 <= cosine(v1, v3) {
		t.Fatalf("expected 1 more similar to 2 than to 3")
	}
}

func TestVectorMinDFDropsSingletons(t *testing.T) {
	// "shared" appears on both (df=2, kept); each unique keyword is df=1, dropped.
	movies := []db.MovieFeature{
		mf(1, []string{"shared", "uniquea"}, nil),
		mf(2, []string{"shared", "uniqueb"}, nil),
		mf(3, []string{"lonelyx", "lonelyy"}, nil), // all df=1 -> empty vector
	}
	vs := buildVectorSpace(movies, nil)

	v1, _ := vs.vecOf(movieKey(1))
	v2, _ := vs.vecOf(movieKey(2))
	// Both reduce to the single "shared" token, so they're identical unit
	// vectors: cosine 1.
	if s := cosine(v1, v2); math.Abs(s-1) > 1e-9 {
		t.Fatalf("cosine(1,2) = %v, want 1 (only shared token survives)", s)
	}
	if _, ok := vs.vecOf(movieKey(3)); ok {
		t.Fatal("movie 3 (all singleton keywords) should have no vector")
	}
}

func TestVectorMaxDFDropsUbiquitous(t *testing.T) {
	// "common" is on all 4 (100% > 50% cutoff) and must be dropped, leaving the
	// paired distinctive keyword as the only signal.
	movies := []db.MovieFeature{
		mf(1, []string{"common", "noir"}, nil),
		mf(2, []string{"common", "noir"}, nil),
		mf(3, []string{"common", "comedy"}, nil),
		mf(4, []string{"common", "comedy"}, nil),
	}
	vs := buildVectorSpace(movies, nil)
	v1, _ := vs.vecOf(movieKey(1))
	v3, _ := vs.vecOf(movieKey(3))
	// 1 and 3 would be identical if "common" survived; with it dropped they
	// share nothing.
	if s := cosine(v1, v3); s != 0 {
		t.Fatalf("cosine(1,3) = %v, want 0 (ubiquitous 'common' dropped)", s)
	}
}

func TestGenreBackboneWeakerThanKeywords(t *testing.T) {
	// 1 and 2 share a keyword; 1 and 3 share only a genre. Keyword match should
	// score higher than genre match.
	movies := []db.MovieFeature{
		mf(1, []string{"heist", "dream"}, []string{"Thriller"}),
		mf(2, []string{"heist", "dream"}, []string{"Comedy"}),
		mf(3, []string{"romance", "wedding"}, []string{"Thriller"}),
	}
	vs := buildVectorSpace(movies, nil)
	v1, _ := vs.vecOf(movieKey(1))
	v2, _ := vs.vecOf(movieKey(2))
	v3, _ := vs.vecOf(movieKey(3))
	if cosine(v1, v2) <= cosine(v1, v3) {
		t.Fatalf("keyword match (1,2)=%v should beat genre-only match (1,3)=%v",
			cosine(v1, v2), cosine(v1, v3))
	}
	if cosine(v1, v3) <= 0 {
		t.Fatalf("genre backbone should give a small positive cosine, got %v", cosine(v1, v3))
	}
}

func TestTasteVectorWeighting(t *testing.T) {
	movies := []db.MovieFeature{
		mf(1, []string{"heist", "dream"}, nil),
		mf(2, []string{"romance", "wedding"}, nil),
		mf(10, []string{"heist", "caper"}, nil),   // near 1
		mf(11, []string{"romance", "family"}, nil), // near 2
	}
	vs := buildVectorSpace(movies, nil)

	// Taste dominated by movie 1 (heist/dream) should sit closer to 10 than 11.
	taste := vs.taste(map[titleKey]float64{movieKey(1): 1.0, movieKey(2): 0.1})
	v10, _ := vs.vecOf(movieKey(10))
	v11, _ := vs.vecOf(movieKey(11))
	if cosine(taste, v10) <= cosine(taste, v11) {
		t.Fatalf("heist-leaning taste should favor 10 (%v) over 11 (%v)",
			cosine(taste, v10), cosine(taste, v11))
	}
}

func TestTasteVectorFromHistory(t *testing.T) {
	movies := []db.MovieFeature{
		mf(1, []string{"heist", "dream"}, nil),
		mf(2, []string{"heist", "caper"}, nil),
	}
	vs := buildVectorSpace(movies, nil)
	now := time.Now()

	// A completed watch contributes; an abandoned one (low completion) does not.
	history := map[int64]db.WatchRow{
		1: {MediaID: 1, PositionSec: 100, DurationSec: 100, Completed: true, UpdatedAt: now},
		2: {MediaID: 2, PositionSec: 5, DurationSec: 100, UpdatedAt: now}, // abandoned
	}
	taste := tasteVector(vs, history, now)
	v1, _ := vs.vecOf(movieKey(1))
	if s := cosine(taste, v1); s <= 0 {
		t.Fatalf("taste from a completed watch of 1 should align with 1, got %v", s)
	}

	// No usable history -> empty taste.
	empty := tasteVector(vs, map[int64]db.WatchRow{}, now)
	if len(empty) != 0 {
		t.Fatalf("empty history should give empty taste, got %d dims", len(empty))
	}
}

func TestSharedKeywords(t *testing.T) {
	movies := []db.MovieFeature{
		mf(1, []string{"Heist", "Dream", "City"}, nil),
		mf(2, []string{"heist", "dream", "beach"}, nil),
	}
	vs := buildVectorSpace(movies, nil)
	shared := vs.sharedKeywords(movieKey(1), movieKey(2))
	// Normalized + sorted: dream, heist.
	if len(shared) != 2 || shared[0] != "dream" || shared[1] != "heist" {
		t.Fatalf("sharedKeywords = %v, want [dream heist]", shared)
	}
}
