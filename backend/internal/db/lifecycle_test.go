package db

import (
	"context"
	"testing"
	"time"
)

func TestItemImpressionsIncrement(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	uid, err := d.CreateProfile(ctx, "Viewer", "")
	if err != nil {
		t.Fatal(err)
	}

	items := []ImpressionRef{{Kind: "movie", ID: 1}, {Kind: "movie", ID: 2}, {Kind: "show", ID: 9}}
	if err := d.RecordItemImpressions(ctx, uid, items); err != nil {
		t.Fatal(err)
	}
	if err := d.RecordItemImpressions(ctx, uid, []ImpressionRef{{Kind: "movie", ID: 1}}); err != nil {
		t.Fatal(err)
	}
	imps, err := d.GetMovieImpressions(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if imps[1] != 2 {
		t.Fatalf("movie 1 served count = %d, want 2", imps[1])
	}
	if imps[2] != 1 {
		t.Fatalf("movie 2 served count = %d, want 1", imps[2])
	}
	if _, ok := imps[9]; ok {
		t.Fatal("GetMovieImpressions should not include shows")
	}
}

func TestHomeCollectionsServeAndCredit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	uid, _ := d.CreateProfile(ctx, "Viewer", "")

	served := []HomeCollectionServe{
		{Key: "for-you", Title: "For You", Strategy: "for-you", ItemIDs: []int64{1, 2, 3}},
		{Key: "theme-heist", Title: "Movies About Heist", Strategy: "theme", ItemIDs: []int64{2, 4}},
	}
	if err := d.RecordHomeCollectionsServed(ctx, uid, served); err != nil {
		t.Fatal(err)
	}
	// Serve again to confirm served_count increments and membership is refreshed.
	if err := d.RecordHomeCollectionsServed(ctx, uid, served); err != nil {
		t.Fatal(err)
	}
	reg, err := d.GetHomeCollections(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if reg["for-you"].ServedCount != 2 {
		t.Fatalf("for-you served = %d, want 2", reg["for-you"].ServedCount)
	}
	if len(reg["for-you"].ItemIDs) != 3 {
		t.Fatalf("for-you items = %v, want 3", reg["for-you"].ItemIDs)
	}

	// Watching movie 2 credits both rows that contained it; movie 5 credits none.
	n, err := d.CreditHomeCollectionWatch(ctx, uid, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("movie 2 credited %d rows, want 2", n)
	}
	n, _ = d.CreditHomeCollectionWatch(ctx, uid, 5)
	if n != 0 {
		t.Fatalf("movie 5 credited %d rows, want 0", n)
	}
	reg, _ = d.GetHomeCollections(ctx, uid)
	if reg["for-you"].ClickCount != 1 || reg["theme-heist"].ClickCount != 1 {
		t.Fatalf("click counts = for-you:%d theme:%d, want 1/1",
			reg["for-you"].ClickCount, reg["theme-heist"].ClickCount)
	}
}

func TestHomeCollectionStateTransitions(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	uid, _ := d.CreateProfile(ctx, "Viewer", "")

	_ = d.RecordHomeCollectionsServed(ctx, uid, []HomeCollectionServe{{Key: "r", Title: "R"}})
	_, _ = d.CreditHomeCollectionWatch(ctx, uid, 0) // no-op

	// Go dormant with a cooldown.
	until := time.Now().Add(24 * time.Hour)
	if err := d.SetHomeCollectionState(ctx, uid, "r", "dormant", until); err != nil {
		t.Fatal(err)
	}
	reg, _ := d.GetHomeCollections(ctx, uid)
	if reg["r"].State != "dormant" || reg["r"].DormantUntil.IsZero() {
		t.Fatalf("row not dormant: %+v", reg["r"])
	}

	// Reviving resets counters.
	_ = d.RecordHomeCollectionsServed(ctx, uid, []HomeCollectionServe{{Key: "r", Title: "R"}})
	if err := d.SetHomeCollectionState(ctx, uid, "r", "active", time.Time{}); err != nil {
		t.Fatal(err)
	}
	reg, _ = d.GetHomeCollections(ctx, uid)
	if reg["r"].State != "active" || reg["r"].ServedCount != 0 || reg["r"].ClickCount != 0 {
		t.Fatalf("revived row should reset counters: %+v", reg["r"])
	}
}
