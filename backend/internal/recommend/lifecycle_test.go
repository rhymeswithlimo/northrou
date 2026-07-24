package recommend

import (
	"context"
	"testing"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

func TestFatiguePenaltyMonotonicAndCapped(t *testing.T) {
	rc := &rowContext{impressions: map[int64]int{
		1: 0, 2: 1, 3: 2, 4: 5, 5: 1000,
	}}
	// 0 and 1 prior serves: no penalty (one free showing).
	if p := rc.fatiguePenalty(1); p != 0 {
		t.Fatalf("penalty at 0 serves = %v, want 0", p)
	}
	if p := rc.fatiguePenalty(2); p != 0 {
		t.Fatalf("penalty at 1 serve = %v, want 0", p)
	}
	// Monotonic increase after that.
	if rc.fatiguePenalty(3) <= 0 || rc.fatiguePenalty(4) <= rc.fatiguePenalty(3) {
		t.Fatalf("penalty should increase with serves: 2=%v 5=%v",
			rc.fatiguePenalty(3), rc.fatiguePenalty(4))
	}
	// Capped.
	if p := rc.fatiguePenalty(5); p != fatigueMax {
		t.Fatalf("penalty at 1000 serves = %v, want cap %v", p, fatigueMax)
	}
}

func TestStrategyOf(t *testing.T) {
	cases := map[string]string{
		"becausewatched-12": "becausewatched",
		"theme-space":       "theme",
		"director-525":      "director",
		"decgenre-2010-Action": "decgenre",
		"collection-77":     "collection",
		"for-you":           "for-you",           // unique, uncapped
		"cold-acclaimed-films": "cold-acclaimed-films", // cold rows never grouped
		"cold-toprated-tv":  "cold-toprated-tv",
		"rewatch":           "rewatch",
		"contrast":          "contrast",
	}
	for in, want := range cases {
		if got := strategyOf(in); got != want {
			t.Errorf("strategyOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyLifecycleDormancyAndEngagement(t *testing.T) {
	now := time.Now()
	rows := []Row{
		{Key: "a", Confidence: 1.0},
		{Key: "b", Confidence: 1.0}, // dormant, still resting -> dropped
		{Key: "c", Confidence: 1.0}, // dormant, cooldown passed -> revived
		{Key: "d", Confidence: 1.0}, // high engagement -> boosted
	}
	reg := map[string]db.HomeCollectionRow{
		"b": {Key: "b", State: "dormant", DormantUntil: now.Add(time.Hour)},
		"c": {Key: "c", State: "dormant", DormantUntil: now.Add(-time.Hour)},
		"d": {Key: "d", State: "active", ServedCount: 10, ClickCount: 10},
	}
	kept, revive := applyLifecycle(rows, reg, now)

	keys := map[string]float64{}
	for _, r := range kept {
		keys[r.Key] = r.Confidence
	}
	if _, ok := keys["b"]; ok {
		t.Fatal("resting dormant row b should be dropped")
	}
	if _, ok := keys["c"]; !ok {
		t.Fatal("rested-out dormant row c should be kept")
	}
	if len(revive) != 1 || revive[0] != "c" {
		t.Fatalf("revive = %v, want [c]", revive)
	}
	if keys["d"] <= 1.0 {
		t.Fatalf("high-engagement row d should be boosted, got %v", keys["d"])
	}
	if keys["a"] != 1.0 {
		t.Fatalf("never-served row a should be unchanged, got %v", keys["a"])
	}
}

func TestRecordServeRetirementGatedOnEngagement(t *testing.T) {
	e, _, uid := newTestEngine(t)
	ctx := context.Background()
	now := time.Now()
	rows := []Row{{Key: "for-you", Title: "For You", Items: []Item{{Kind: "movie", ID: 1}}}}

	// Case 1: no engagement anywhere. Even far past the serve threshold, nothing
	// retires - the home screen must never empty itself for a user who simply
	// doesn't play from home.
	reg := map[string]db.HomeCollectionRow{
		"for-you": {Key: "for-you", ServedCount: retireServedThreshold + 5, ClickCount: 0, State: "active"},
	}
	if err := e.recordServe(ctx, uid, rows, reg, now); err != nil {
		t.Fatal(err)
	}
	got, _ := e.db.GetHomeCollections(ctx, uid)
	if got["for-you"].State == "dormant" {
		t.Fatal("row retired despite no engagement evidence anywhere")
	}

	// Case 2: a non-foundational row, served past the threshold with zero clicks
	// while the profile has engaged elsewhere -> dormant.
	themeRow := []Row{{Key: "theme-heist", Title: "Movies About Heist", Items: []Item{{Kind: "movie", ID: 1}}}}
	reg = map[string]db.HomeCollectionRow{
		"theme-heist": {Key: "theme-heist", ServedCount: retireServedThreshold, ClickCount: 0, State: "active"},
		"other":       {Key: "other", ServedCount: 3, ClickCount: 2, State: "active"},
	}
	if err := e.recordServe(ctx, uid, themeRow, reg, now); err != nil {
		t.Fatal(err)
	}
	got, _ = e.db.GetHomeCollections(ctx, uid)
	if got["theme-heist"].State != "dormant" {
		t.Fatalf("theme row should be dormant after threshold serves with clicks elsewhere, got %q", got["theme-heist"].State)
	}

	// Case 3: the flagship for-you row is exempt - it never rests, even past the
	// threshold with zero clicks and engagement elsewhere.
	reg = map[string]db.HomeCollectionRow{
		"for-you": {Key: "for-you", ServedCount: retireServedThreshold + 10, ClickCount: 0, State: "active"},
		"other":   {Key: "other", ServedCount: 3, ClickCount: 2, State: "active"},
	}
	if err := e.recordServe(ctx, uid, rows, reg, now); err != nil {
		t.Fatal(err)
	}
	got, _ = e.db.GetHomeCollections(ctx, uid)
	if got["for-you"].State == "dormant" {
		t.Fatal("for-you is foundational and must never be retired")
	}
}
