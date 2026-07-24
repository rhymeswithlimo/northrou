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

// coldStartRows builds a diverse, balanced set of category rows from library
// composition. Every generator contributes candidate rows tagged (by key) with
// a family; balanceColdRows then round-robins across families so no single one
// (franchises, blockbusters) can crowd out the rest - which is what made the old
// cold start read as basic. Movies and shows are interleaved, not segregated.
func coldStartRows(rc *rowContext) []Row {
	var all []Row
	all = append(all, rc.catAcclaimedFilms()...)
	all = append(all, rc.catDirectors()...)
	all = append(all, rc.catGenreFilms()...)
	all = append(all, rc.catThemes()...)
	all = append(all, rc.catCollections()...)
	all = append(all, rc.catForeignFilms()...)
	all = append(all, rc.catRuntime()...)
	all = append(all, rc.catBlockbusters()...)

	all = append(all, rc.catStudios()...)

	all = append(all, rc.catCreators()...)
	all = append(all, rc.catTopRatedShows()...)
	all = append(all, rc.catGenreShows()...)
	all = append(all, rc.catCountryShows()...)

	all = append(all, rc.catFallback()...)
	return balanceColdRows(all)
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
	sort.Slice(films, func(i, j int) bool {
		if films[i].Rating != films[j].Rating {
			return films[i].Rating > films[j].Rating
		}
		return films[i].ID < films[j].ID // stable across rebuilds
	})
	return []Row{{Key: "cold-acclaimed-films", Title: "Critically Acclaimed Films", Confidence: 1.0, Items: rc.itemsOf(films)}}
}

// catBlockbusters: a single "Blockbusters" row of the biggest films by box
// office. This deliberately replaces the old per-decade rows ("2010s
// Blockbusters", "2000s Blockbusters", …): those genre-blind rows dominated the
// screen and lumped romance/horror/action together by nothing but release date.
// One demoted row is plenty; genre/director/theme rows carry the real browsing.
func (rc *rowContext) catBlockbusters() []Row {
	films := rc.playableMovies()
	if len(films) < 4 {
		return nil
	}
	sort.Slice(films, func(i, j int) bool {
		if blockbusterScore(films[i]) != blockbusterScore(films[j]) {
			return blockbusterScore(films[i]) > blockbusterScore(films[j])
		}
		return films[i].ID < films[j].ID
	})
	return []Row{{Key: "cold-blockbusters", Title: "Blockbusters", Confidence: 0.5, Items: rc.itemsOf(films)}}
}

// minDirectorFilms: how many owned films by one director warrant a row.
const minDirectorFilms = 2

// Director scoring weights. Ranking by film count alone surfaces franchise
// assemblers (e.g. the Russo brothers, all-MCU) over auteurs; the franchise
// penalty demotes directors whose owned films are mostly collection entries so
// the likes of Tarantino/Chazelle aren't buried. Heuristic - see the score calc.
const (
	dirCountWeight     = 0.6 // per film, diminishing (capped at dirCountCap)
	dirCountCap        = 4
	dirRatingPivot     = 6.5 // avg rating above this adds, below subtracts
	dirFranchisePenalty = 2.0 // times the fraction of films that are franchise entries
)

// catDirectors: "Directed by X" rows for directors the user owns several films
// by. Co-directors with an identical film set (e.g. the Russo brothers) are
// merged into one row, which also prevents duplicate near-identical rows.
func (rc *rowContext) catDirectors() []Row {
	byDir := map[int64][]db.MovieFeature{}
	for _, m := range rc.playableMovies() {
		for _, d := range m.Directors {
			byDir[d.ID] = append(byDir[d.ID], m)
		}
	}

	// Merge directors sharing an identical film set (co-directors).
	type entry struct {
		ids   []int64
		names []string
		films []db.MovieFeature
	}
	bySig := map[string]*entry{}
	dirIDs := make([]int64, 0, len(byDir))
	for id := range byDir {
		dirIDs = append(dirIDs, id)
	}
	sort.Slice(dirIDs, func(i, j int) bool { return dirIDs[i] < dirIDs[j] }) // deterministic
	for _, id := range dirIDs {
		films := byDir[id]
		if len(films) < minDirectorFilms {
			continue
		}
		name := rc.names[id]
		if name == "" {
			continue
		}
		sig := filmSignature(films)
		e := bySig[sig]
		if e == nil {
			e = &entry{films: films}
			bySig[sig] = e
		}
		e.ids = append(e.ids, id)
		e.names = append(e.names, name)
	}

	type scoredRow struct {
		row   Row
		score float64
	}
	var scored []scoredRow
	for _, e := range bySig {
		sort.Slice(e.films, func(i, j int) bool { return e.films[i].Year < e.films[j].Year })
		scored = append(scored, scoredRow{
			row: Row{
				Key:   "cold-director-" + itoa(e.ids[0]),
				Title: "Directed by " + joinNames(e.names),
				Items: rc.itemsOf(e.films),
			},
			score: directorScore(e.films),
		})
	}
	// Rank by auteur score; confidence descends so within-family order is stable.
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].row.Key < scored[j].row.Key
	})
	rows := make([]Row, 0, len(scored))
	for i, s := range scored {
		s.row.Confidence = 1.0 - 0.01*float64(i) // preserve rank; family weight applied in balance
		rows = append(rows, s.row)
	}
	return rows
}

// directorScore favors auteurs: film count (with diminishing returns) plus a
// rating bonus, minus a penalty for directors whose owned films are mostly
// franchise/collection entries.
func directorScore(films []db.MovieFeature) float64 {
	n := float64(len(films))
	var ratingSum float64
	var franchise int
	for _, f := range films {
		ratingSum += f.Rating
		if f.CollectionID != 0 {
			franchise++
		}
	}
	avgRating := ratingSum / n
	franchiseFrac := float64(franchise) / n
	countTerm := dirCountWeight * float64(min(len(films), dirCountCap))
	return countTerm + (avgRating - dirRatingPivot) - dirFranchisePenalty*franchiseFrac
}

// filmSignature is a stable key for a set of films, so co-directors with the
// same filmography merge.
func filmSignature(films []db.MovieFeature) string {
	ids := make([]int64, len(films))
	for i, f := range films {
		ids[i] = f.ID
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(itoa(id))
	}
	return b.String()
}

// joinNames renders one or more director names ("A", "A & B", "A, B & C").
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " & " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " & " + names[len(names)-1]
	}
}

// catCollections: film series the user owns more than one entry of.
func (rc *rowContext) catCollections() []Row {
	byColl := map[int64][]db.MovieFeature{}
	for _, m := range rc.playableMovies() {
		if m.CollectionID != 0 {
			byColl[m.CollectionID] = append(byColl[m.CollectionID], m)
		}
	}
	// Deterministic order: biggest series first, then by id (map iteration order
	// is random, which would blink franchise rows in and out across rebuilds).
	ids := make([]int64, 0, len(byColl))
	for id := range byColl {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(byColl[ids[i]]) != len(byColl[ids[j]]) {
			return len(byColl[ids[i]]) > len(byColl[ids[j]])
		}
		return ids[i] < ids[j]
	})
	var rows []Row
	for _, id := range ids {
		films := byColl[id]
		if len(films) < 2 {
			continue
		}
		name := rc.collections[id]
		if name == "" {
			continue
		}
		sort.Slice(films, func(i, j int) bool {
			if films[i].Year != films[j].Year {
				return films[i].Year < films[j].Year
			}
			return films[i].ID < films[j].ID
		})
		rows = append(rows, Row{
			Key:        fmtKey("cold-collection", id),
			Title:      name,
			Confidence: 0.85,
			Items:      rc.itemsOf(films),
		})
	}
	return rows
}

// broadGenres are the catch-all genres that sit on a huge fraction of any
// library (a Marvel film is Action+Adventure, most dramas are Drama). Ranking
// genre rows by raw count surfaces only these and buries the distinctive genres
// (Animation, Horror, Romance) a user actually browses by, so broad genres are
// demoted in genreRowScore.
var broadGenres = map[string]bool{
	"Action": true, "Adventure": true, "Drama": true,
	"Thriller": true, "Comedy": true, "Family": true,
}

// genreRowScore ranks a genre for cold-start rows: size, but broad genres
// heavily discounted so specific ones surface.
func genreRowScore(name string, count int) float64 {
	s := float64(count)
	if broadGenres[name] {
		s *= 0.4
	}
	return s
}

// catGenreFilms: a row per film genre, ranked so distinctive genres surface
// ahead of catch-all ones. balanceColdRows caps how many are shown, and
// rotation cycles the rest across sessions.
func (rc *rowContext) catGenreFilms() []Row {
	byGenre := map[string][]db.MovieFeature{}
	for _, m := range rc.playableMovies() {
		for _, g := range m.Genres {
			byGenre[g] = append(byGenre[g], m)
		}
	}
	type gc struct {
		name  string
		films []db.MovieFeature
		score float64
	}
	var gs []gc
	for name, films := range byGenre {
		if len(films) < minCategoryItems {
			continue
		}
		gs = append(gs, gc{name, films, genreRowScore(name, len(films))})
	}
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].score != gs[j].score {
			return gs[i].score > gs[j].score
		}
		return gs[i].name < gs[j].name // deterministic
	})
	rows := make([]Row, 0, len(gs))
	for i, g := range gs {
		films := g.films
		sort.Slice(films, func(a, b int) bool {
			if films[a].Popularity != films[b].Popularity {
				return films[a].Popularity > films[b].Popularity
			}
			return films[a].ID < films[b].ID
		})
		rows = append(rows, Row{
			Key:        "cold-genre-film-" + g.name,
			Title:      g.name + " Films",
			Confidence: 1.0 - 0.01*float64(i),
			Items:      rc.itemsOf(films),
		})
	}
	return rows
}

// Theme-row cutoffs. A theme needs enough titles to be a row, but a keyword on
// too large a fraction of the library is uninformative (not a distinctive theme).
const (
	minThemeItems = 4
	maxThemeFrac  = 0.5
)

// catThemes: keyword-driven rows pooling movies AND shows, so a theme like
// "space" surfaces Dune, Interstellar, and a sci-fi series in one row. This is
// the first use of the content-vector keyword data at cold start. Titles are
// kind-neutral because the rows mix films and series.
func (rc *rowContext) catThemes() []Row {
	if rc.space == nil {
		return nil
	}
	type cand struct {
		key  titleKey
		item Item
		pop  float64
	}
	var cands []cand
	for _, m := range rc.playableMovies() {
		cands = append(cands, cand{movieKey(m.ID), rc.toItem(m), m.Popularity})
	}
	for _, s := range rc.playableShows() {
		cands = append(cands, cand{showKey(s.ID), rc.toShowItem(s), s.Popularity})
	}

	byKeyword := map[string][]cand{}
	for _, c := range cands {
		for norm := range rc.space.kw[c.key] {
			if isThematicKeyword(norm) {
				byKeyword[norm] = append(byKeyword[norm], c)
			}
		}
	}

	maxItems := int(float64(len(cands)) * maxThemeFrac)
	type group struct {
		kw    string
		cands []cand
	}
	var groups []group
	for kw, cs := range byKeyword {
		if len(cs) < minThemeItems {
			continue
		}
		if maxItems >= minThemeItems && len(cs) > maxItems {
			continue // too common to be a distinctive theme
		}
		groups = append(groups, group{kw, cs})
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].cands) != len(groups[j].cands) {
			return len(groups[i].cands) > len(groups[j].cands)
		}
		return groups[i].kw < groups[j].kw // deterministic
	})

	rows := make([]Row, 0, len(groups))
	for i, g := range groups {
		cs := g.cands
		sort.Slice(cs, func(a, b int) bool {
			if cs[a].pop != cs[b].pop {
				return cs[a].pop > cs[b].pop
			}
			return cs[a].key.id < cs[b].key.id
		})
		items := make([]Item, 0, maxItemsPerRow)
		for _, c := range cs {
			if len(items) >= maxItemsPerRow {
				break
			}
			items = append(items, c.item)
		}
		rows = append(rows, Row{
			Key:        "cold-theme-" + g.kw,
			Title:      humanKeyword(g.kw),
			Confidence: 1.0 - 0.01*float64(i),
			Items:      items,
		})
	}
	return rows
}

// brandStudios maps a lower-cased substring of a TMDB production-company name to
// a clean display label. Deliberately an allowlist of recognizable *brands*, not
// a frequency heuristic: umbrella financiers ("Walt Disney Pictures") lump
// unrelated films, and vanity shingles ("Syncopy") mean nothing to a viewer.
// Biased toward animation houses and distinctive labels the genre/theme rows
// won't already produce.
var brandStudios = []struct{ match, name string }{
	{"marvel studios", "Marvel Studios"},
	{"pixar", "Pixar"},
	{"walt disney animation", "Disney Animation"},
	{"dreamworks animation", "DreamWorks Animation"},
	{"illumination", "Illumination"},
	{"laika", "Laika"},
	{"studio ghibli", "Studio Ghibli"},
	{"aardman", "Aardman"},
	{"a24", "A24"},
	{"blumhouse", "Blumhouse"},
	{"legendary", "Legendary"},
	{"lucasfilm", "Lucasfilm"},
}

// minStudioItems is how many owned titles a studio needs to warrant a row.
const minStudioItems = 2

// brandFor returns the display label if a company name matches a known brand.
func brandFor(company string) (string, bool) {
	c := strings.ToLower(company)
	for _, b := range brandStudios {
		if strings.Contains(c, b.match) {
			return b.name, true
		}
	}
	return "", false
}

// catStudios: rows for recognizable studios (Marvel Studios, DreamWorks…),
// pooling movies and shows. Uses the brandStudios allowlist so umbrella
// distributors and vanity production shingles never become rows.
func (rc *rowContext) catStudios() []Row {
	type cand struct {
		item Item
		pop  float64
		id   int64
		kind string
	}
	byBrand := map[string][]cand{}
	add := func(companies []string, it Item, pop float64) {
		seen := map[string]bool{}
		for _, co := range companies {
			brand, ok := brandFor(co)
			if !ok || seen[brand] {
				continue
			}
			seen[brand] = true
			byBrand[brand] = append(byBrand[brand], cand{it, pop, it.ID, it.Kind})
		}
	}
	for _, m := range rc.playableMovies() {
		add(m.Companies, rc.toItem(m), m.Popularity)
	}
	for _, s := range rc.playableShows() {
		add(s.Companies, rc.toShowItem(s), s.Popularity)
	}

	brands := make([]string, 0, len(byBrand))
	for b := range byBrand {
		brands = append(brands, b)
	}
	sort.Slice(brands, func(i, j int) bool {
		if len(byBrand[brands[i]]) != len(byBrand[brands[j]]) {
			return len(byBrand[brands[i]]) > len(byBrand[brands[j]])
		}
		return brands[i] < brands[j]
	})

	var rows []Row
	for i, brand := range brands {
		cs := byBrand[brand]
		if len(cs) < minStudioItems {
			continue
		}
		sort.Slice(cs, func(a, b int) bool {
			if cs[a].pop != cs[b].pop {
				return cs[a].pop > cs[b].pop
			}
			return cs[a].id < cs[b].id
		})
		items := make([]Item, 0, maxItemsPerRow)
		for _, c := range cs {
			if len(items) >= maxItemsPerRow {
				break
			}
			items = append(items, c.item)
		}
		rows = append(rows, Row{
			Key:        "cold-studio-" + brand,
			Title:      brand,
			Confidence: 1.0 - 0.01*float64(i),
			Items:      items,
		})
	}
	return rows
}

// catCreators: "Created by X" rows for shows sharing a creator - the TV analog
// of catDirectors. Co-creators with an identical show set (the Duffer Brothers,
// etc.) merge into one row.
func (rc *rowContext) catCreators() []Row {
	byCreator := map[string][]db.ShowFeature{}
	for _, s := range rc.playableShows() {
		for _, name := range s.Creators {
			byCreator[name] = append(byCreator[name], s)
		}
	}

	type entry struct {
		names []string
		shows []db.ShowFeature
	}
	bySig := map[string]*entry{}
	names := make([]string, 0, len(byCreator))
	for name := range byCreator {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic
	for _, name := range names {
		shows := byCreator[name]
		if len(shows) < minDirectorFilms {
			continue
		}
		sig := showSignature(shows)
		e := bySig[sig]
		if e == nil {
			e = &entry{shows: shows}
			bySig[sig] = e
		}
		e.names = append(e.names, name)
	}

	type scoredRow struct {
		row   Row
		score float64
	}
	var scored []scoredRow
	for _, e := range bySig {
		sort.Slice(e.shows, func(i, j int) bool {
			if e.shows[i].Rating != e.shows[j].Rating {
				return e.shows[i].Rating > e.shows[j].Rating
			}
			return e.shows[i].ID < e.shows[j].ID
		})
		var ratingSum float64
		for _, s := range e.shows {
			ratingSum += s.Rating
		}
		score := 0.6*float64(min(len(e.shows), dirCountCap)) + (ratingSum/float64(len(e.shows)) - dirRatingPivot)
		scored = append(scored, scoredRow{
			row: Row{
				Key:   "cold-creator-" + e.names[0],
				Title: "Created by " + joinNames(e.names),
				Items: rc.showItems(e.shows),
			},
			score: score,
		})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].row.Key < scored[j].row.Key
	})
	rows := make([]Row, 0, len(scored))
	for i, s := range scored {
		s.row.Confidence = 1.0 - 0.01*float64(i)
		rows = append(rows, s.row)
	}
	return rows
}

// showSignature is filmSignature for shows (co-creator merge).
func showSignature(shows []db.ShowFeature) string {
	ids := make([]int64, len(shows))
	for i, s := range shows {
		ids[i] = s.ID
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(itoa(id))
	}
	return b.String()
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
	sort.Slice(films, func(i, j int) bool {
		if films[i].Popularity != films[j].Popularity {
			return films[i].Popularity > films[j].Popularity
		}
		return films[i].ID < films[j].ID
	})
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
	sort.Slice(shows, func(i, j int) bool {
		if shows[i].Rating != shows[j].Rating {
			return shows[i].Rating > shows[j].Rating
		}
		return shows[i].ID < shows[j].ID
	})
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
		sort.Slice(shows, func(i, j int) bool {
			if shows[i].Popularity != shows[j].Popularity {
				return shows[i].Popularity > shows[j].Popularity
			}
			return shows[i].ID < shows[j].ID
		})
		rows = append(rows, Row{
			Key:        "cold-country-tv-" + code,
			Title:      countryAdjective(code) + " TV Shows",
			Confidence: 0.7,
			Items:      rc.showItems(shows),
		})
	}
	return rows
}

// catGenreShows: a row per TV genre, distinctive genres ranked ahead of
// catch-all ones (same as catGenreFilms).
func (rc *rowContext) catGenreShows() []Row {
	byGenre := map[string][]db.ShowFeature{}
	for _, s := range rc.playableShows() {
		for _, g := range s.Genres {
			byGenre[g] = append(byGenre[g], s)
		}
	}
	type gc struct {
		name  string
		shows []db.ShowFeature
		score float64
	}
	var gs []gc
	for name, shows := range byGenre {
		if len(shows) < minCategoryItems {
			continue
		}
		gs = append(gs, gc{name, shows, genreRowScore(name, len(shows))})
	}
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].score != gs[j].score {
			return gs[i].score > gs[j].score
		}
		return gs[i].name < gs[j].name
	})
	rows := make([]Row, 0, len(gs))
	for i, g := range gs {
		shows := g.shows
		sort.Slice(shows, func(a, b int) bool {
			if shows[a].Popularity != shows[b].Popularity {
				return shows[a].Popularity > shows[b].Popularity
			}
			return shows[a].ID < shows[b].ID
		})
		rows = append(rows, Row{
			Key:        "cold-genre-tv-" + g.name,
			Title:      g.name + " Series",
			Confidence: 1.0 - 0.01*float64(i),
			Items:      rc.showItems(shows),
		})
	}
	return rows
}

// catFallback: guarantees the home screen is never empty.
func (rc *rowContext) catFallback() []Row {
	var rows []Row
	if m := rc.playableMovies(); len(m) > 0 {
		sort.Slice(m, func(i, j int) bool {
			if m[i].Popularity != m[j].Popularity {
				return m[i].Popularity > m[j].Popularity
			}
			return m[i].ID < m[j].ID
		})
		rows = append(rows, Row{Key: "cold-all-movies", Title: "Your Movies", Confidence: 0.3, Items: rc.itemsOf(m)})
	}
	if s := rc.playableShows(); len(s) > 0 {
		sort.Slice(s, func(i, j int) bool {
			if s[i].Popularity != s[j].Popularity {
				return s[i].Popularity > s[j].Popularity
			}
			return s[i].ID < s[j].ID
		})
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

// --- balanced cold-start assembly ---

const (
	maxColdSlate        = 12  // rows the balanced cold-start home aims for
	nearDupThreshold    = 0.8 // rows sharing this fraction of items are redundant
	maxTitleAppearances = 3   // a title may repeat across at most this many rows
)

// coldFamily maps a row key to its strategy family, so balanceColdRows can cap
// and interleave families.
func coldFamily(key string) string {
	switch {
	case strings.HasPrefix(key, "cold-director-"):
		return "director"
	case strings.HasPrefix(key, "cold-genre-film-"):
		return "genre"
	case strings.HasPrefix(key, "cold-theme-"):
		return "theme"
	case strings.HasPrefix(key, "cold-collection-"):
		return "franchise"
	case strings.HasPrefix(key, "cold-studio-"):
		return "studio"
	case strings.HasPrefix(key, "cold-creator-"):
		return "creator"
	case strings.HasPrefix(key, "cold-blockbusters"):
		return "blockbuster"
	case strings.HasPrefix(key, "cold-acclaimed"):
		return "acclaimed"
	case strings.HasPrefix(key, "cold-foreign"):
		return "world"
	case key == "cold-quick" || key == "cold-epic":
		return "runtime"
	case strings.HasPrefix(key, "cold-toprated-tv"):
		return "tv-top"
	case strings.HasPrefix(key, "cold-country-tv-"):
		return "tv-country"
	case strings.HasPrefix(key, "cold-genre-tv-"):
		return "tv-genre"
	default:
		return "fallback"
	}
}

// coldFillOrder is the slot-assignment sequence for the cold-start slate. A
// family listed N times gets at most N rows (its de-facto cap); interleaving the
// repeats keeps the screen varied (director, genre, theme lead and recur;
// blockbuster/fallback trail). Movie and show families are interleaved so a
// show-heavy library isn't segregated to the bottom. Families with no rows are
// simply skipped, and their slots flow to whatever's next.
var coldFillOrder = []string{
	"acclaimed",
	"director", "genre", "theme",
	"director", "studio", "franchise",
	"director", "genre", "theme",
	"director", "creator", "tv-genre",
	"genre", "studio", "tv-top",
	"creator", "world", "tv-country",
	"runtime", "blockbuster",
	"fallback",
}

// balanceColdRows selects a diverse slate from all candidate cold-start rows:
// round-robin across families (per coldFillOrder), skipping near-duplicate rows
// and rows that would over-expose an already-repeated title. Order is preserved
// via descending confidence so Home's rotation + dedupeAndRank keep it.
func balanceColdRows(all []Row) []Row {
	byFamily := map[string][]Row{}
	for _, r := range all {
		if len(r.Items) < 2 {
			continue
		}
		f := coldFamily(r.Key)
		byFamily[f] = append(byFamily[f], r)
	}
	next := map[string]int{}      // next unconsumed row index per family
	appear := map[string]int{}    // item key -> how many chosen rows contain it
	out := make([]Row, 0, maxColdSlate)
	for _, fam := range coldFillOrder {
		if len(out) >= maxColdSlate {
			break
		}
		rows := byFamily[fam]
		for next[fam] < len(rows) {
			cand := rows[next[fam]]
			next[fam]++
			if nearDuplicateRow(cand, out) || overExposedRow(cand, appear) {
				continue
			}
			for _, it := range cand.Items {
				appear[itemKey(it)]++
			}
			out = append(out, cand)
			break
		}
	}
	for i := range out {
		out[i].Confidence = float64(len(out) - i) // preserve fill order
	}
	return out
}

func itemKey(it Item) string { return it.Kind + ":" + itoa(it.ID) }

func itemSet(r Row) map[string]bool {
	s := make(map[string]bool, len(r.Items))
	for _, it := range r.Items {
		s[itemKey(it)] = true
	}
	return s
}

// nearDuplicateRow reports whether cand overlaps an already-chosen row by
// nearDupThreshold or more of the smaller row - i.e. it's essentially the same
// list under a different title. Partial overlap (a film in several themed rows)
// is allowed; near-identical rows are not.
func nearDuplicateRow(cand Row, chosen []Row) bool {
	cs := itemSet(cand)
	for _, r := range chosen {
		rs := itemSet(r)
		inter := 0
		for k := range cs {
			if rs[k] {
				inter++
			}
		}
		denom := min(len(cs), len(rs))
		if denom > 0 && float64(inter)/float64(denom) >= nearDupThreshold {
			return true
		}
	}
	return false
}

// overExposedRow reports whether nearly every title in cand has already hit its
// appearance cap - i.e. the row would just be reruns. A row with a few repeated
// titles is fine (repetition in the right amount); one that's all reruns is not.
func overExposedRow(cand Row, appear map[string]int) bool {
	over := 0
	for _, it := range cand.Items {
		if appear[itemKey(it)] >= maxTitleAppearances {
			over++
		}
	}
	return len(cand.Items) > 0 && over >= len(cand.Items)-1
}

// topShowGroups returns the keys of the n largest show groups, most-populous first.
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
