package recommend

import (
	"sort"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// Cold start: before any watch history exists, Northrou can't personalize, so
// it organizes the library the user already owns into meaningful, browsable
// category rows by decade + box office, critical acclaim, genre, country,
// language, runtime, and collections, spanning both movies and TV shows.

const minCategoryItems = 3

// coldStartRows builds category rows purely from library composition.
func coldStartRows(rc *rowContext) []Row {
	var rows []Row
	rows = append(rows, rc.catAcclaimedFilms()...)
	rows = append(rows, rc.catDecadeBlockbusters()...)
	rows = append(rows, rc.catCollections()...)
	rows = append(rows, rc.catGenreFilms()...)
	rows = append(rows, rc.catForeignFilms()...)
	rows = append(rows, rc.catRuntime()...)

	rows = append(rows, rc.catTopRatedShows()...)
	rows = append(rows, rc.catCountryShows()...)
	rows = append(rows, rc.catGenreShows()...)

	rows = append(rows, rc.catFallback()...)
	return rows
}

// playableMovies returns all movies with a media file.
func (rc *rowContext) playableMovies() []db.MovieFeature {
	var out []db.MovieFeature
	for _, m := range rc.features {
		if m.Playable {
			out = append(out, m)
		}
	}
	return out
}

func (rc *rowContext) playableShows() []db.ShowFeature {
	var out []db.ShowFeature
	for _, s := range rc.shows {
		if s.Playable {
			out = append(out, s)
		}
	}
	return out
}

// --- movie categories ---

// catAcclaimedFilms: highly-rated films with enough votes to be meaningful.
func (rc *rowContext) catAcclaimedFilms() []Row {
	var films []db.MovieFeature
	for _, m := range rc.playableMovies() {
		if m.Rating >= 7.5 && m.Votes >= 500 {
			films = append(films, m)
		}
	}
	if len(films) < minCategoryItems {
		return nil
	}
	sort.Slice(films, func(i, j int) bool { return films[i].Rating > films[j].Rating })
	return []Row{{Key: "cold-acclaimed-films", Title: "Critically Acclaimed Films", Confidence: 1.0, Items: rc.itemsOf(films)}}
}

// catDecadeBlockbusters: the biggest films you own from each well-represented
// decade, ranked by box office (falling back to popularity).
func (rc *rowContext) catDecadeBlockbusters() []Row {
	byDecade := map[string][]db.MovieFeature{}
	for _, m := range rc.playableMovies() {
		if dk := decadeKey(m.Year); dk != "" {
			byDecade[dk] = append(byDecade[dk], m)
		}
	}
	var rows []Row
	for decade, films := range byDecade {
		if len(films) < 4 {
			continue
		}
		sort.Slice(films, func(i, j int) bool { return blockbusterScore(films[i]) > blockbusterScore(films[j]) })
		rows = append(rows, Row{
			Key:        "cold-blockbusters-" + decade,
			Title:      decade + "s Blockbusters",
			Confidence: 0.9,
			Items:      rc.itemsOf(films),
		})
	}
	return rows
}

// catCollections: film series the user owns more than one entry of.
func (rc *rowContext) catCollections() []Row {
	byColl := map[int64][]db.MovieFeature{}
	for _, m := range rc.playableMovies() {
		if m.CollectionID != 0 {
			byColl[m.CollectionID] = append(byColl[m.CollectionID], m)
		}
	}
	var rows []Row
	for id, films := range byColl {
		if len(films) < 2 {
			continue
		}
		name := rc.collections[id]
		if name == "" {
			continue
		}
		sort.Slice(films, func(i, j int) bool { return films[i].Year < films[j].Year })
		rows = append(rows, Row{
			Key:        fmtKey("cold-collection", id),
			Title:      name,
			Confidence: 0.85,
			Items:      rc.itemsOf(films),
		})
	}
	return rows
}

// catGenreFilms: a row per well-represented film genre.
func (rc *rowContext) catGenreFilms() []Row {
	byGenre := map[string][]db.MovieFeature{}
	for _, m := range rc.playableMovies() {
		for _, g := range m.Genres {
			byGenre[g] = append(byGenre[g], m)
		}
	}
	var rows []Row
	for _, gc := range topGroups(byGenre, 6) {
		films := byGenre[gc]
		if len(films) < 4 {
			continue
		}
		sort.Slice(films, func(i, j int) bool { return films[i].Popularity > films[j].Popularity })
		rows = append(rows, Row{
			Key:        "cold-genre-film-" + gc,
			Title:      gc + " Films",
			Confidence: 0.7,
			Items:      rc.itemsOf(films),
		})
	}
	return rows
}

// catForeignFilms: non-English-language cinema.
func (rc *rowContext) catForeignFilms() []Row {
	var films []db.MovieFeature
	for _, m := range rc.playableMovies() {
		if m.Language != "" && m.Language != "en" {
			films = append(films, m)
		}
	}
	if len(films) < minCategoryItems {
		return nil
	}
	sort.Slice(films, func(i, j int) bool { return films[i].Popularity > films[j].Popularity })
	return []Row{{Key: "cold-foreign-films", Title: "World Cinema", Confidence: 0.6, Items: rc.itemsOf(films)}}
}

// catRuntime: quick watches and epics.
func (rc *rowContext) catRuntime() []Row {
	var quick, epic []db.MovieFeature
	for _, m := range rc.playableMovies() {
		switch {
		case m.Runtime > 0 && m.Runtime < 90:
			quick = append(quick, m)
		case m.Runtime >= 150:
			epic = append(epic, m)
		}
	}
	var rows []Row
	if len(quick) >= minCategoryItems {
		rows = append(rows, Row{Key: "cold-quick", Title: "Quick Watches (Under 90 min)", Confidence: 0.45, Items: rc.itemsOf(quick)})
	}
	if len(epic) >= minCategoryItems {
		rows = append(rows, Row{Key: "cold-epic", Title: "Epic Films", Confidence: 0.45, Items: rc.itemsOf(epic)})
	}
	return rows
}

// --- show categories ---

// catTopRatedShows: the highest-rated series in the library.
func (rc *rowContext) catTopRatedShows() []Row {
	var shows []db.ShowFeature
	for _, s := range rc.playableShows() {
		if s.Rating >= 8.0 {
			shows = append(shows, s)
		}
	}
	if len(shows) < minCategoryItems {
		return nil
	}
	sort.Slice(shows, func(i, j int) bool { return shows[i].Rating > shows[j].Rating })
	return []Row{{Key: "cold-toprated-tv", Title: "Top-Rated TV Shows", Confidence: 0.9, Items: rc.showItems(shows)}}
}

// catCountryShows: a row per well-represented origin country, e.g. "American
// TV Shows", "British TV Shows".
func (rc *rowContext) catCountryShows() []Row {
	byCountry := map[string][]db.ShowFeature{}
	for _, s := range rc.playableShows() {
		if s.Country != "" {
			byCountry[s.Country] = append(byCountry[s.Country], s)
		}
	}
	var rows []Row
	for _, code := range topShowGroups(byCountry, 3) {
		shows := byCountry[code]
		if len(shows) < minCategoryItems {
			continue
		}
		sort.Slice(shows, func(i, j int) bool { return shows[i].Popularity > shows[j].Popularity })
		rows = append(rows, Row{
			Key:        "cold-country-tv-" + code,
			Title:      countryAdjective(code) + " TV Shows",
			Confidence: 0.7,
			Items:      rc.showItems(shows),
		})
	}
	return rows
}

// catGenreShows: a row per well-represented TV genre.
func (rc *rowContext) catGenreShows() []Row {
	byGenre := map[string][]db.ShowFeature{}
	for _, s := range rc.playableShows() {
		for _, g := range s.Genres {
			byGenre[g] = append(byGenre[g], s)
		}
	}
	var rows []Row
	for _, gc := range topShowGroups(byGenre, 4) {
		shows := byGenre[gc]
		if len(shows) < minCategoryItems {
			continue
		}
		sort.Slice(shows, func(i, j int) bool { return shows[i].Popularity > shows[j].Popularity })
		rows = append(rows, Row{
			Key:        "cold-genre-tv-" + gc,
			Title:      gc + " Series",
			Confidence: 0.65,
			Items:      rc.showItems(shows),
		})
	}
	return rows
}

// catFallback: guarantees the home screen is never empty.
func (rc *rowContext) catFallback() []Row {
	var rows []Row
	if m := rc.playableMovies(); len(m) > 0 {
		sort.Slice(m, func(i, j int) bool { return m[i].Popularity > m[j].Popularity })
		rows = append(rows, Row{Key: "cold-all-movies", Title: "Your Movies", Confidence: 0.3, Items: rc.itemsOf(m)})
	}
	if s := rc.playableShows(); len(s) > 0 {
		sort.Slice(s, func(i, j int) bool { return s[i].Popularity > s[j].Popularity })
		rows = append(rows, Row{Key: "cold-all-shows", Title: "Your TV Shows", Confidence: 0.3, Items: rc.showItems(s)})
	}
	return rows
}

// --- helpers ---

func (rc *rowContext) showItems(ss []db.ShowFeature) []Item {
	if len(ss) > maxItemsPerRow {
		ss = ss[:maxItemsPerRow]
	}
	out := make([]Item, 0, len(ss))
	for _, s := range ss {
		out = append(out, rc.toShowItem(s))
	}
	return out
}

// blockbusterScore ranks films by box office, falling back to popularity when
// revenue is unknown (older or foreign titles).
func blockbusterScore(m db.MovieFeature) float64 {
	if m.Revenue > 0 {
		return float64(m.Revenue)
	}
	return m.Popularity // always below any real revenue figure
}

// topGroups returns the keys of the n largest movie groups, most-populous first.
func topGroups(groups map[string][]db.MovieFeature, n int) []string {
	type kc struct {
		k string
		c int
	}
	var ks []kc
	for k, v := range groups {
		ks = append(ks, kc{k, len(v)})
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i].c > ks[j].c })
	var out []string
	for i, k := range ks {
		if i >= n {
			break
		}
		out = append(out, k.k)
	}
	return out
}

// topShowGroups is topGroups for show groups.
func topShowGroups(groups map[string][]db.ShowFeature, n int) []string {
	type kc struct {
		k string
		c int
	}
	var ks []kc
	for k, v := range groups {
		ks = append(ks, kc{k, len(v)})
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i].c > ks[j].c })
	var out []string
	for i, k := range ks {
		if i >= n {
			break
		}
		out = append(out, k.k)
	}
	return out
}

func fmtKey(prefix string, id int64) string {
	return prefix + "-" + itoa(id)
}

func itoa(v int64) string {
	return strings.TrimSpace(personKey(v))
}

// countryAdjective maps an ISO 3166-1 code to an adjective for "X TV Shows".
func countryAdjective(code string) string {
	switch code {
	case "US":
		return "American"
	case "GB":
		return "British"
	case "CA":
		return "Canadian"
	case "AU":
		return "Australian"
	case "JP":
		return "Japanese"
	case "KR":
		return "Korean"
	case "FR":
		return "French"
	case "DE":
		return "German"
	case "IT":
		return "Italian"
	case "ES":
		return "Spanish"
	case "IN":
		return "Indian"
	case "CN":
		return "Chinese"
	case "SE":
		return "Swedish"
	case "DK":
		return "Danish"
	case "BR":
		return "Brazilian"
	case "MX":
		return "Mexican"
	default:
		return code
	}
}

// languageName maps an ISO code to a display name, defaulting to upper-case.
func languageName(code string) string {
	switch code {
	case "en":
		return "English"
	case "es":
		return "Spanish"
	case "fr":
		return "French"
	case "de":
		return "German"
	case "it":
		return "Italian"
	case "ja":
		return "Japanese"
	case "ko":
		return "Korean"
	case "zh":
		return "Chinese"
	case "hi":
		return "Hindi"
	case "ru":
		return "Russian"
	case "sv":
		return "Swedish"
	case "da":
		return "Danish"
	default:
		return strings.ToUpper(code)
	}
}
