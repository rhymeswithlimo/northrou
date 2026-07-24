package recommend

import (
	"context"
	"strings"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// seedRatedMovie inserts a playable movie with rating/revenue/country set.
func seedRatedMovie(t *testing.T, d *db.DB, title string, year int, genres []string, rating float64, votes int, revenue int64, country string) int64 {
	t.Helper()
	ctx := context.Background()
	nextTMDB++
	fileID, err := d.UpsertMediaFile(ctx, &model.MediaFile{Path: "/m/" + title + ".mkv", SizeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	m := &model.Movie{
		TMDBID: nextTMDB, Title: title, Year: year, Runtime: 120, OriginalLang: "en",
		Genres: genres, Rating: rating, Votes: votes, Popularity: rating * 10,
		Revenue: revenue, Country: country,
		File: &model.MediaFile{ID: fileID},
	}
	id, err := d.UpsertMovie(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// seedShow inserts a show with one playable episode.
func seedShow(t *testing.T, d *db.DB, title string, year int, genres []string, rating float64, country string) int64 {
	t.Helper()
	ctx := context.Background()
	nextTMDB++
	show := &model.Show{
		TMDBID: nextTMDB, Title: title, Year: year, OriginalLang: "en",
		Genres: genres, Rating: rating, Popularity: rating * 10, Country: country,
	}
	showID, err := d.UpsertShow(ctx, show)
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := d.UpsertMediaFile(ctx, &model.MediaFile{Path: "/tv/" + title + "-s01e01.mkv", SizeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, &model.Episode{
		ShowID: showID, SeasonID: seasonID, Season: 1, Number: 1,
		File: &model.MediaFile{ID: fileID},
	}); err != nil {
		t.Fatal(err)
	}
	return showID
}

// TestColdStartCategories checks that a diverse library yields a diverse slate:
// the acclaimed row and a top-rated-shows row both survive because they aren't
// near-duplicates of each other. (Degenerate all-identical libraries collapse to
// one row by design; see TestColdStartDiverseLibrary for the richer case.)
// seedFull inserts a playable movie with directors, genres, keywords, rating,
// and optional collection - enough to exercise every cold-start family.
func seedFull(t *testing.T, e *Engine, title string, year int, genres, keywords []string, directors []model.Credit, collectionID int64, rating float64) int64 {
	t.Helper()
	ctx := context.Background()
	nextTMDB++
	fileID, err := e.db.UpsertMediaFile(ctx, &model.MediaFile{Path: "/m/" + title + ".mkv", SizeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if collectionID != 0 {
		_ = e.db.UpsertCollection(ctx, collectionID, "Collection "+title, "", "")
	}
	m := &model.Movie{
		TMDBID: nextTMDB, Title: title, Year: year, Runtime: 120, OriginalLang: "en",
		Genres: genres, Keywords: keywords, CollectionID: collectionID,
		Crew: directors, Rating: rating, Votes: 1000, Popularity: rating * 10,
		File: &model.MediaFile{ID: fileID},
	}
	id, err := e.db.UpsertMovie(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestColdStartDiverseLibrary mirrors the user's real library and asserts the
// slate is diverse and hits the specific things they called out: a Tarantino
// director row, an Animation genre row, a theme row, co-directors merged, no
// blockbuster domination, and no title plastered across too many rows.
func TestColdStartDiverseLibrary(t *testing.T) {
	e, _, uid := newTestEngine(t)
	ctx := context.Background()

	nolan := model.Credit{PersonID: 100, Name: "Christopher Nolan", Role: "Director"}
	tarantino := model.Credit{PersonID: 101, Name: "Quentin Tarantino", Role: "Director"}
	russoA := model.Credit{PersonID: 102, Name: "Anthony Russo", Role: "Director"}
	russoB := model.Credit{PersonID: 103, Name: "Joe Russo", Role: "Director"}
	villeneuve := model.Credit{PersonID: 104, Name: "Denis Villeneuve", Role: "Director"}

	// Nolan x4 (one franchise entry).
	seedFull(t, e, "Interstellar", 2014, []string{"Science Fiction"}, []string{"space", "time-travel"}, []model.Credit{nolan}, 0, 8.6)
	seedFull(t, e, "Oppenheimer", 2023, []string{"Drama"}, []string{"world-war-two", "biography"}, []model.Credit{nolan}, 0, 8.3)
	seedFull(t, e, "Tenet", 2020, []string{"Science Fiction"}, []string{"time-travel", "espionage"}, []model.Credit{nolan}, 0, 7.8)
	seedFull(t, e, "The Dark Knight", 2008, []string{"Action"}, []string{"superhero"}, []model.Credit{nolan}, 700, 9.0)
	// Tarantino x2 (no franchise) - the row the user demanded.
	seedFull(t, e, "Inglourious Basterds", 2009, []string{"War"}, []string{"world-war-two", "revenge"}, []model.Credit{tarantino}, 0, 8.3)
	seedFull(t, e, "Once Upon a Time in Hollywood", 2019, []string{"Comedy"}, []string{"hollywood", "revenge"}, []model.Credit{tarantino}, 0, 7.6)
	// Russo brothers x4 (co-directed, all franchise) - should merge into one row.
	// Distinct ratings (not all identical) so the "acclaimed" row isn't just the
	// superhero cluster.
	for _, m := range []struct {
		title  string
		year   int
		kw     []string
		rating float64
	}{
		{"Avengers: Infinity War", 2018, []string{"superhero"}, 8.4},
		{"Avengers: Endgame", 2019, []string{"superhero", "time-travel"}, 8.3},
		{"Captain America: The Winter Soldier", 2014, []string{"superhero", "espionage"}, 7.7},
		{"Captain America: Civil War", 2016, []string{"superhero"}, 7.8},
	} {
		seedFull(t, e, m.title, m.year, []string{"Action"}, m.kw, []model.Credit{russoA, russoB}, 800, m.rating)
	}
	// Villeneuve x2 (Dune is franchise) - gives a "space" theme its extra titles.
	seedFull(t, e, "Arrival", 2016, []string{"Science Fiction"}, []string{"space", "aliens"}, []model.Credit{villeneuve}, 0, 7.9)
	seedFull(t, e, "Dune", 2021, []string{"Science Fiction"}, []string{"space", "desert"}, []model.Credit{villeneuve}, 900, 8.0)
	seedFull(t, e, "Avatar", 2009, []string{"Science Fiction"}, []string{"space", "aliens"}, nil, 0, 7.9)

	// More auteurs, so Tarantino competes against a realistic field (~8
	// directors), not a rigged 4. This is the real test of whether the director
	// cap + ranking still surface him on the first render.
	spielberg := model.Credit{PersonID: 105, Name: "Steven Spielberg", Role: "Director"}
	chazelle := model.Credit{PersonID: 106, Name: "Damien Chazelle", Role: "Director"}
	guadagnino := model.Credit{PersonID: 107, Name: "Luca Guadagnino", Role: "Director"}
	muschietti := model.Credit{PersonID: 108, Name: "Andy Muschietti", Role: "Director"}
	seedFull(t, e, "Jaws", 1975, []string{"Thriller"}, []string{"shark"}, []model.Credit{spielberg}, 0, 8.1)
	seedFull(t, e, "Raiders of the Lost Ark", 1981, []string{"Adventure"}, []string{"treasure"}, []model.Credit{spielberg}, 1000, 8.4)
	seedFull(t, e, "Catch Me If You Can", 2002, []string{"Drama"}, []string{"con-artist"}, []model.Credit{spielberg}, 0, 8.1)
	seedFull(t, e, "La La Land", 2016, []string{"Romance"}, []string{"jazz", "musical"}, []model.Credit{chazelle}, 0, 8.0)
	seedFull(t, e, "Whiplash", 2014, []string{"Drama"}, []string{"jazz", "music"}, []model.Credit{chazelle}, 0, 8.5)
	seedFull(t, e, "Call Me By Your Name", 2017, []string{"Romance"}, []string{"lgbt", "summer"}, []model.Credit{guadagnino}, 0, 7.8)
	seedFull(t, e, "Challengers", 2024, []string{"Romance"}, []string{"tennis", "love-triangle"}, []model.Credit{guadagnino}, 0, 7.0)
	seedFull(t, e, "It", 2017, []string{"Horror"}, []string{"clown", "coming-of-age"}, []model.Credit{muschietti}, 1100, 7.3)
	seedFull(t, e, "It Chapter Two", 2019, []string{"Horror"}, []string{"clown"}, []model.Credit{muschietti}, 1100, 6.5)
	// Animation x4 (distinct genre the user said was missing).
	seedFull(t, e, "Shrek", 2001, []string{"Animation"}, []string{"ogre", "fairy-tale"}, nil, 0, 7.9)
	seedFull(t, e, "Moana", 2016, []string{"Animation"}, []string{"ocean", "island"}, nil, 0, 7.6)
	seedFull(t, e, "Spider-Verse", 2018, []string{"Animation"}, []string{"multiverse"}, nil, 0, 8.4)
	seedFull(t, e, "Puss in Boots", 2022, []string{"Animation"}, []string{"cat", "fairy-tale"}, nil, 0, 7.9)

	e.InvalidateAll()
	rows, err := e.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}

	byKey := map[string]Row{}
	families := map[string]int{}
	appear := map[string]int{}
	for _, r := range rows {
		byKey[r.Key] = r
		families[coldFamily(r.Key)]++
		if len(r.Items) > maxItemsPerRow {
			t.Errorf("row %q has %d items, want <= %d", r.Key, len(r.Items), maxItemsPerRow)
		}
		for _, it := range r.Items {
			appear[it.Kind+":"+itoa(it.ID)]++
		}
	}

	// Hard bar: a Tarantino director row exists.
	if r, ok := byKey["cold-director-101"]; !ok {
		t.Errorf("no Tarantino director row; keys: %v", rowKeys(rows))
	} else if r.Title != "Directed by Quentin Tarantino" {
		t.Errorf("Tarantino row title = %q", r.Title)
	}
	// Animation genre row exists.
	if _, ok := byKey["cold-genre-film-Animation"]; !ok {
		t.Errorf("no Animation genre row; keys: %v", rowKeys(rows))
	}
	// At least one keyword-theme row exists.
	var hasTheme bool
	for k := range byKey {
		if coldFamily(k) == "theme" {
			hasTheme = true
		}
	}
	if !hasTheme {
		t.Errorf("no theme row; keys: %v", rowKeys(rows))
	}
	// Co-directors merged: exactly one row for the Russo film-set, named for both.
	russoRows := 0
	for _, r := range rows {
		if coldFamily(r.Key) == "director" && strings.Contains(r.Title, "Russo") {
			russoRows++
			if !strings.Contains(r.Title, "Anthony Russo") || !strings.Contains(r.Title, "Joe Russo") {
				t.Errorf("Russo row not merged: %q", r.Title)
			}
		}
	}
	if russoRows > 1 {
		t.Errorf("expected co-directors merged into one row, got %d", russoRows)
	}
	// No blockbuster domination and a diverse slate.
	if families["blockbuster"] > 1 {
		t.Errorf("blockbuster rows = %d, want <= 1", families["blockbuster"])
	}
	if len(families) < 4 {
		t.Errorf("slate not diverse: only %d families (%v)", len(families), rowKeys(rows))
	}
	// No title over-exposed across rows.
	for key, n := range appear {
		if n > maxTitleAppearances {
			t.Errorf("title %s appears in %d rows, want <= %d", key, n, maxTitleAppearances)
		}
	}
}

func TestCatStudios(t *testing.T) {
	rc := &rowContext{features: []db.MovieFeature{
		{ID: 1, Title: "Iron Man", Playable: true, Popularity: 10, Companies: []string{"Marvel Studios", "Paramount Pictures"}},
		{ID: 2, Title: "Thor", Playable: true, Popularity: 9, Companies: []string{"Marvel Studios", "Walt Disney Pictures"}},
		{ID: 3, Title: "Avengers", Playable: true, Popularity: 11, Companies: []string{"Marvel Studios", "Walt Disney Pictures"}},
		{ID: 4, Title: "Shrek", Playable: true, Popularity: 8, Companies: []string{"DreamWorks Animation"}},
		{ID: 5, Title: "Puss in Boots", Playable: true, Popularity: 7, Companies: []string{"DreamWorks Animation"}},
		{ID: 6, Title: "Some Disney Film", Playable: true, Popularity: 5, Companies: []string{"Walt Disney Pictures"}}, // umbrella only
	}}
	rows := rc.catStudios()

	got := map[string]int{}
	for _, r := range rows {
		got[r.Title] = len(r.Items)
	}
	if got["Marvel Studios"] != 3 {
		t.Errorf("Marvel Studios row = %d items, want 3", got["Marvel Studios"])
	}
	if got["DreamWorks Animation"] != 2 {
		t.Errorf("DreamWorks row = %d items, want 2", got["DreamWorks Animation"])
	}
	// Umbrella distributors must never become a row.
	for title := range got {
		if strings.Contains(title, "Disney Pictures") || strings.Contains(title, "Paramount") {
			t.Errorf("umbrella/financier studio leaked as a row: %q", title)
		}
	}
}

func TestCatCreators(t *testing.T) {
	rc := &rowContext{shows: []db.ShowFeature{
		{ID: 1, Title: "Breaking Bad", Playable: true, Rating: 9.5, Creators: []string{"Vince Gilligan"}},
		{ID: 2, Title: "Better Call Saul", Playable: true, Rating: 9.0, Creators: []string{"Vince Gilligan", "Peter Gould"}},
		{ID: 3, Title: "Stranger Things", Playable: true, Rating: 8.7, Creators: []string{"Matt Duffer", "Ross Duffer"}},
		{ID: 4, Title: "Stranger Things 2", Playable: true, Rating: 8.5, Creators: []string{"Matt Duffer", "Ross Duffer"}},
	}}
	rows := rc.catCreators()

	titles := map[string]bool{}
	for _, r := range rows {
		titles[r.Title] = true
	}
	// Vince Gilligan has 2 shows -> a row. Peter Gould has 1 -> no row.
	if !titles["Created by Vince Gilligan"] {
		t.Errorf("no Vince Gilligan creator row; got %v", titles)
	}
	// The Duffers co-created an identical set -> merged into one row.
	if !titles["Created by Matt Duffer & Ross Duffer"] {
		t.Errorf("Duffers not merged into one row; got %v", titles)
	}
	if titles["Created by Peter Gould"] {
		t.Errorf("Peter Gould (1 show) should not get a row")
	}
}

func TestColdStartCategories(t *testing.T) {
	e, d, uid := newTestEngine(t)
	ctx := context.Background()

	// A spread of acclaimed films across distinct genres (so acclaimed and the
	// genre rows aren't identical lists).
	seedRatedMovie(t, d, "Space Epic", 2005, []string{"Science Fiction"}, 8.4, 2000, 500_000_000, "US")
	seedRatedMovie(t, d, "Haunted House", 2006, []string{"Horror"}, 7.8, 1500, 100_000_000, "US")
	seedRatedMovie(t, d, "Love Story", 2007, []string{"Romance"}, 7.9, 1200, 80_000_000, "US")
	seedRatedMovie(t, d, "Big Laugh", 2008, []string{"Comedy"}, 7.6, 900, 200_000_000, "US")
	seedRatedMovie(t, d, "Toon Tale", 2009, []string{"Animation"}, 8.1, 1800, 300_000_000, "US")

	// Three top-rated American drama series.
	for _, title := range []string{"Prestige Drama", "Acclaimed Series", "Great Show"} {
		seedShow(t, d, title, 2010, []string{"Drama"}, 8.6, "US")
	}

	rows, err := e.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Row{}
	for _, r := range rows {
		got[r.Key] = r
	}

	if _, ok := got["cold-acclaimed-films"]; !ok {
		t.Errorf("expected acclaimed row; got %v", rowKeys(rows))
	}
	// A show row should appear (interleaved, not dumped) and carry show items.
	var showRow *Row
	for k := range got {
		if r := got[k]; len(r.Items) > 0 && r.Items[0].Kind == "show" {
			showRow = &r
		}
	}
	if showRow == nil {
		t.Errorf("expected a show row in the cold-start slate; got %v", rowKeys(rows))
	}
	// Category rows are capped at 8 items.
	for _, r := range rows {
		if len(r.Items) > maxItemsPerRow {
			t.Errorf("row %q has %d items, want <= %d", r.Key, len(r.Items), maxItemsPerRow)
		}
	}
}
