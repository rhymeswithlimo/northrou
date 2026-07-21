package db

import (
	"context"
	"testing"
)

func TestSetProfileLanguages(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	id := seedProfile(t, d)

	// Defaults are empty (fall back to server default).
	p, err := d.GetProfile(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if p.PreferredAudioLang != "" || p.PreferredSubtitleLang != "" {
		t.Fatalf("new profile should have no language prefs, got %+v", p)
	}

	if err := d.SetProfileLanguages(ctx, id, "es", "fr"); err != nil {
		t.Fatal(err)
	}
	p, _ = d.GetProfile(ctx, id)
	if p.PreferredAudioLang != "es" || p.PreferredSubtitleLang != "fr" {
		t.Fatalf("got audio=%q subtitle=%q, want es/fr", p.PreferredAudioLang, p.PreferredSubtitleLang)
	}

	// Clearing (empty) falls back to the default again.
	if err := d.SetProfileLanguages(ctx, id, "", ""); err != nil {
		t.Fatal(err)
	}
	p, _ = d.GetProfile(ctx, id)
	if p.PreferredAudioLang != "" || p.PreferredSubtitleLang != "" {
		t.Errorf("clearing should reset prefs, got %+v", p)
	}
}
