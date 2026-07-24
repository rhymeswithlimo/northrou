package recommend

import (
	"sort"
	"strconv"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// Content-vector-driven generators. These are the visible face of the keyword
// upgrade: rows a genre-only engine could never produce.

const (
	// becauseWindow bounds how recent a completed movie must be to seed a
	// "Because you watched X" row.
	becauseWindow = 30 * 24 * time.Hour
	// maxBecauseSeeds caps how many recent titles seed such rows, so the home
	// screen isn't a wall of "Because you watched".
	maxBecauseSeeds = 3
	// minThemeRowItems is the floor for a theme/because row to be worth showing.
	minThemeRowItems = 3
	// maxKeywordThemeRows caps how many "Movies about X" rows we emit.
	maxKeywordThemeRows = 3
)

// indexMovies maps movie id -> feature for quick lookup by generators.
func indexMovies(features []db.MovieFeature) map[int64]*db.MovieFeature {
	out := make(map[int64]*db.MovieFeature, len(features))
	for i := range features {
		out[features[i].ID] = &features[i]
	}
	return out
}

// genBecauseYouWatched builds a row of nearest unwatched neighbors for each of
// the most recently finished movies. This is the marquee new capability: it
// reads thematic proximity, not shared genre. Gated on a real completion inside
// becauseWindow so it only fires when there's a fresh, strong seed.
func (rc *rowContext) genBecauseYouWatched() []Row {
	if rc.space == nil {
		return nil
	}
	// Recently completed movies that still exist in the library, newest first.
	type seed struct {
		id      int64
		watched time.Time
	}
	var seeds []seed
	for id, wr := range rc.history {
		if !wr.Completed {
			continue
		}
		if rc.now.Sub(wr.UpdatedAt) > becauseWindow {
			continue
		}
		if _, ok := rc.movieByID[id]; !ok {
			continue
		}
		if _, hasVec := rc.space.vecOf(movieKey(id)); !hasVec {
			continue // no vector -> no meaningful neighbors
		}
		seeds = append(seeds, seed{id: id, watched: wr.UpdatedAt})
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].watched.After(seeds[j].watched) })
	if len(seeds) > maxBecauseSeeds {
		seeds = seeds[:maxBecauseSeeds]
	}

	var rows []Row
	for _, s := range seeds {
		vec, _ := rc.space.vecOf(movieKey(s.id))
		nbrs := rc.space.neighbors(vec, "movie", func(k titleKey) bool {
			m := rc.movieByID[k.id]
			return m != nil && rc.candidate(*m) // unwatched, playable
		})
		items := make([]Item, 0, maxItemsPerRow)
		for _, nb := range nbrs {
			if len(items) >= maxItemsPerRow {
				break
			}
			items = append(items, rc.toItem(*rc.movieByID[nb.key.id]))
		}
		if len(items) < minThemeRowItems {
			continue
		}
		seedTitle := rc.movieByID[s.id].Title
		rows = append(rows, Row{
			Key:      "becausewatched-" + strconv.FormatInt(s.id, 10),
			Title:    "Because You Watched " + seedTitle,
			Subtitle: "Thematically closest to " + seedTitle,
			// Recency-weighted: a fresher watch makes a more compelling row.
			Confidence: 0.9 + recencyBoost(rc.now.Sub(s.watched)),
			Items:      items,
		})
	}
	return rows
}

// genKeywordThemes builds "Movies about X" rows from the keywords that dominate
// the user's completed history, recovering the thematic-row payoff without SVD
// or hand-named dimensions.
func (rc *rowContext) genKeywordThemes() []Row {
	if rc.space == nil {
		return nil
	}
	// Tally normalized keywords across completed movies, weighted by signal.
	weight := map[string]float64{}
	for id, wr := range rc.history {
		if !wr.Completed {
			continue
		}
		kw := rc.space.kw[movieKey(id)]
		s := amplifyRewatch(1.0, wr.RewatchCount) * decayFactor(rc.now.Sub(wr.UpdatedAt))
		for k := range kw {
			weight[k] += s
		}
	}
	if len(weight) == 0 {
		return nil
	}
	type kw struct {
		name string
		w    float64
	}
	ranked := make([]kw, 0, len(weight))
	for k, w := range weight {
		ranked = append(ranked, kw{k, w})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].w != ranked[j].w {
			return ranked[i].w > ranked[j].w
		}
		return ranked[i].name < ranked[j].name
	})

	var rows []Row
	for _, r := range ranked {
		if len(rows) >= maxKeywordThemeRows {
			break
		}
		// Unwatched, playable movies carrying this keyword, ranked by thematic fit.
		var cands []db.MovieFeature
		for _, m := range rc.features {
			if !rc.candidate(m) {
				continue
			}
			if _, ok := rc.space.kw[movieKey(m.ID)][r.name]; ok {
				cands = append(cands, m)
			}
		}
		if len(cands) < minThemeRowItems {
			continue
		}
		sort.Slice(cands, func(i, j int) bool {
			return rc.scoreMovie(cands[i]) > rc.scoreMovie(cands[j])
		})
		// The strongest theme leads; later ones sit slightly lower.
		conf := 0.65 - 0.05*float64(len(rows))
		rows = append(rows, Row{
			Key:        "theme-" + r.name,
			Title:      "Movies About " + humanKeyword(r.name),
			Confidence: conf,
			Items:      rc.itemsOf(cands),
		})
	}
	return rows
}

// recencyBoost adds a small confidence bump for a very recent watch, decaying to
// ~0 across the becauseWindow.
func recencyBoost(age time.Duration) float64 {
	if age <= 0 {
		return 0.3
	}
	frac := 1 - age.Seconds()/becauseWindow.Seconds()
	if frac < 0 {
		frac = 0
	}
	return 0.3 * frac
}
