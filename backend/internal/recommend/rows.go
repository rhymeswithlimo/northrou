package recommend

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// Item is a single title in a home-screen row (a movie or a TV show).
type Item struct {
	Kind       string `json:"kind"` // "movie" | "show"
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	PosterPath string `json:"poster_path,omitempty"`
}

// Row is a named, ranked list of recommendations for the home screen.
type Row struct {
	Key        string  `json:"key"`
	Title      string  `json:"title"`
	Subtitle   string  `json:"subtitle,omitempty"` // one-line "why", optional
	Confidence float64 `json:"confidence"`
	Items      []Item  `json:"items"`
}

// maxItemsPerRow caps how many titles a row carries.
const maxItemsPerRow = 24

// Home builds the ranked, rotated set of home-screen rows for a user. Results
// are cached briefly (see homeCacheTTL) because building them loads the whole
// library into memory.
func (e *Engine) Home(ctx context.Context, userID int64) ([]Row, error) {
	if rows, ok := e.cachedRows(userID); ok {
		return rows, nil
	}
	profile, err := e.LoadProfile(ctx, userID)
	if err != nil {
		return nil, err
	}
	cat, err := e.loadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	features, shows := cat.movies, cat.shows
	history, err := e.db.GetMovieWatchHistory(ctx, userID)
	if err != nil {
		return nil, err
	}
	collections, err := e.db.LoadCollections(ctx)
	if err != nil {
		return nil, err
	}
	impressions, err := e.db.GetMovieImpressions(ctx, userID)
	if err != nil {
		return nil, err
	}

	rc := &rowContext{
		profile:     profile,
		features:    features,
		shows:       shows,
		history:     history,
		collections: collections,
		now:         time.Now(),
		names:       buildNameIndex(features),
		movieByID:   indexMovies(features),
		space:       cat.space,
		taste:       tasteVector(cat.space, history, time.Now()),
		impressions: impressions,
	}

	var rows []Row
	if !profile.HasData() {
		rows = coldStartRows(rc) // library-composition rows
	} else {
		rows = append(rows, rc.genRecommended()...)
		rows = append(rows, rc.genBecauseYouWatched()...)
		rows = append(rows, rc.genKeywordThemes()...)
		rows = append(rows, rc.genDirectorRows()...)
		rows = append(rows, rc.genDecadeGenreRows()...)
		rows = append(rows, rc.genCollectionRows()...)
		rows = append(rows, rc.genLanguageRows()...)
		rows = append(rows, rc.genRewatchRows()...)
		rows = append(rows, rc.genTimeContextRows()...)
		rows = append(rows, rc.genContrastRows()...)
	}

	// Rotation: penalize recently-shown rows so the home screen evolves.
	state, _ := e.db.GetRowState(ctx, userID)
	for i := range rows {
		rows[i].Confidence *= rotationPenalty(state[rows[i].Key], rc.now)
	}

	// Lifecycle: drop resting rows, revive rested ones, and re-weight by
	// historical engagement before ranking.
	registry, _ := e.db.GetHomeCollections(ctx, userID)
	var revive []string
	rows, revive = applyLifecycle(rows, registry, rc.now)
	for _, key := range revive {
		_ = e.db.SetHomeCollectionState(ctx, userID, key, "active", time.Time{})
		delete(registry, key) // revived: treat as fresh for the retirement check
	}

	rows = dedupeAndRank(rows)

	// Persist what we're about to show: impressions, served counts, retirement.
	_ = e.recordServe(ctx, userID, rows, registry, rc.now)

	keys := make([]string, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, r.Key)
	}
	_ = e.db.MarkRowsShown(ctx, userID, keys)
	e.storeRows(userID, rows)
	return rows, nil
}

// rowContext carries shared state to the generators.
type rowContext struct {
	profile     *Profile
	features    []db.MovieFeature
	shows       []db.ShowFeature
	history     map[int64]db.WatchRow
	collections map[int64]string
	now         time.Time
	names       map[int64]string             // person id -> name
	movieByID   map[int64]*db.MovieFeature   // movie id -> feature
	space       *vectorSpace                 // library content vectors
	taste       sparseVec                    // user's unit taste vector (empty if no history/vectors)
	impressions map[int64]int                // movie id -> prior served count (fatigue)
}

// tasteVector builds a user's content taste vector from movie watch history:
// the recency- and completion-weighted average of the vectors of movies they
// watched. Reuses the same signal weighting as the affinity engine
// (signalFromCompletion x amplifyRewatch x time decay) so the two views of
// taste agree. Returns an empty vector when there is no usable history.
func tasteVector(space *vectorSpace, history map[int64]db.WatchRow, now time.Time) sparseVec {
	if space == nil {
		return sparseVec{}
	}
	weights := map[titleKey]float64{}
	for id, wr := range history {
		c := 0.0
		if wr.DurationSec > 0 {
			c = wr.PositionSec / wr.DurationSec
		}
		if wr.Completed && c < 0.95 {
			c = 0.95
		}
		signal := amplifyRewatch(signalFromCompletion(c), wr.RewatchCount)
		if signal <= 0 {
			continue // an abandoned title shouldn't pull the taste toward itself
		}
		w := signal * decayFactor(now.Sub(wr.UpdatedAt))
		if w > 0 {
			weights[movieKey(id)] = w
		}
	}
	return space.taste(weights)
}

// completed reports whether the user finished a movie.
func (rc *rowContext) completed(id int64) bool {
	w, ok := rc.history[id]
	return ok && w.Completed
}

// candidate reports whether a movie is a recommendable unwatched, playable one.
func (rc *rowContext) candidate(m db.MovieFeature) bool {
	return m.Playable && !rc.completed(m.ID)
}

// scoreMovie combines the profile's affinities for a movie's attributes into a
// single relevance score.
func (rc *rowContext) scoreMovie(m db.MovieFeature) float64 {
	p := rc.profile
	var score float64

	// Genres (average).
	if len(m.Genres) > 0 {
		var g float64
		for _, name := range m.Genres {
			g += p.Affinity(DimGenre, name)
		}
		score += 0.30 * (g / float64(len(m.Genres)))
	}
	// Director (best).
	var bestDir float64
	for _, d := range m.Directors {
		if v := p.Affinity(DimDirector, personKey(d.ID)); v > bestDir {
			bestDir = v
		}
	}
	score += 0.25 * bestDir
	// Decade.
	score += 0.15 * p.Affinity(DimDecade, decadeKey(m.Year))
	// Actors (average of top-billed).
	if len(m.Actors) > 0 {
		var a float64
		n := 0
		for i, act := range m.Actors {
			if i >= maxActorsScored {
				break
			}
			a += p.Affinity(DimActor, personKey(act.ID))
			n++
		}
		if n > 0 {
			score += 0.15 * (a / float64(n))
		}
	}
	// Language and runtime.
	score += 0.10 * p.Affinity(DimLanguage, m.Language)
	score += 0.05 * p.Affinity(DimRuntime, runtimeBucket(m.Runtime))

	// Thematic fit: cosine between the user's taste vector and this movie's
	// content vector. This is the term the old engine lacked entirely - it sees
	// keyword-level theme/tone, not just genre. Weighted to matter alongside
	// genre without swamping the explicit director signal. Both vectors are unit
	// length, so the cosine is in [0,1]; it's 0 when either side has no vector
	// (no history, or a title with no surviving keywords), leaving the classic
	// affinity score untouched.
	score += embedWeight * rc.themeFit(m.ID)

	// Fatigue: a title shown repeatedly but never played drifts down so fresher
	// candidates surface. Purely subtractive, so it can never empty a row.
	score -= rc.fatiguePenalty(m.ID)
	return score
}

// embedWeight scales the thematic (content-vector) term in scoreMovie. Sized so
// a strong thematic match (cosine ~0.4) contributes ~0.28, comparable to a
// strong single-genre affinity (0.30 x ~1.0) - keywords are meant to be a
// first-class signal, not a tiebreaker. Heuristic; the cosine/affinity scales
// aren't commensurable, so this is worth revisiting against a real library.
const embedWeight = 0.7

// themeFit returns the cosine between the user's taste vector and a movie's
// content vector, or 0 if either is absent.
func (rc *rowContext) themeFit(movieID int64) float64 {
	if rc.space == nil || len(rc.taste) == 0 {
		return 0
	}
	v, ok := rc.space.vecOf(movieKey(movieID))
	if !ok {
		return 0
	}
	return cosine(rc.taste, v)
}

func (rc *rowContext) toItem(m db.MovieFeature) Item {
	return Item{Kind: "movie", ID: m.ID, Title: m.Title, Year: m.Year, PosterPath: m.PosterPath}
}

func (rc *rowContext) toShowItem(s db.ShowFeature) Item {
	return Item{Kind: "show", ID: s.ID, Title: s.Title, Year: s.Year, PosterPath: s.PosterPath}
}

// --- Generators ---

// genRecommended: the highest-scoring unwatched movies overall.
func (rc *rowContext) genRecommended() []Row {
	type scored struct {
		m db.MovieFeature
		s float64
	}
	var cands []scored
	for _, m := range rc.features {
		if rc.candidate(m) {
			cands = append(cands, scored{m, rc.scoreMovie(m)})
		}
	}
	if len(cands) == 0 {
		return nil
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].s > cands[j].s })

	var items []Item
	var top float64
	for i, c := range cands {
		if i == 0 {
			top = c.s
		}
		if len(items) >= maxItemsPerRow {
			break
		}
		items = append(items, rc.toItem(c.m))
	}
	return []Row{{Key: "for-you", Title: "Recommended for You", Confidence: 1.0 + top, Items: items}}
}

// genDirectorRows: "More from <Director>" for well-liked directors.
func (rc *rowContext) genDirectorRows() []Row {
	byDir := map[int64][]db.MovieFeature{}
	for _, m := range rc.features {
		if !rc.candidate(m) {
			continue
		}
		for _, d := range m.Directors {
			byDir[d.ID] = append(byDir[d.ID], m)
		}
	}
	var rows []Row
	for id, movies := range byDir {
		aff := rc.profile.Affinity(DimDirector, personKey(id))
		conf := rc.profile.Confidence(DimDirector, personKey(id))
		if aff <= 0.3 || conf < 1 || len(movies) < 2 {
			continue
		}
		rows = append(rows, Row{
			Key:        "director-" + personKey(id),
			Title:      "More from " + rc.names[id],
			Confidence: 0.8 + aff,
			Items:      rc.itemsOf(movies),
		})
	}
	return rows
}

// genDecadeGenreRows: strong decade×genre combinations.
func (rc *rowContext) genDecadeGenreRows() []Row {
	type combo struct{ decade, genre string }
	buckets := map[combo][]db.MovieFeature{}
	for _, m := range rc.features {
		if !rc.candidate(m) {
			continue
		}
		dk := decadeKey(m.Year)
		if dk == "" {
			continue
		}
		for _, g := range m.Genres {
			buckets[combo{dk, g}] = append(buckets[combo{dk, g}], m)
		}
	}
	var rows []Row
	for c, movies := range buckets {
		if len(movies) < 3 {
			continue
		}
		aff := 0.6*rc.profile.Affinity(DimGenre, c.genre) + 0.4*rc.profile.Affinity(DimDecade, c.decade)
		if aff <= 0.35 {
			continue
		}
		rows = append(rows, Row{
			Key:        "decgenre-" + c.decade + "-" + c.genre,
			Title:      fmt.Sprintf("%ss %s", c.decade, c.genre),
			Confidence: 0.6 + aff,
			Items:      rc.itemsOf(movies),
		})
	}
	return rows
}

// genCollectionRows: "Finish the <Collection>" where some are watched.
func (rc *rowContext) genCollectionRows() []Row {
	byColl := map[int64][]db.MovieFeature{}
	watchedInColl := map[int64]int{}
	for _, m := range rc.features {
		if m.CollectionID == 0 {
			continue
		}
		if rc.completed(m.ID) {
			watchedInColl[m.CollectionID]++
		} else if m.Playable {
			byColl[m.CollectionID] = append(byColl[m.CollectionID], m)
		}
	}
	var rows []Row
	for id, remaining := range byColl {
		if watchedInColl[id] == 0 || len(remaining) == 0 {
			continue
		}
		name := rc.collections[id]
		if name == "" {
			name = "Collection"
		}
		rows = append(rows, Row{
			Key:        fmt.Sprintf("collection-%d", id),
			Title:      "Finish " + strings.TrimSuffix(name, " Collection"),
			Confidence: 1.3, // completing a started collection is a strong signal
			Items:      rc.itemsOf(remaining),
		})
	}
	return rows
}

// genLanguageRows: cinema in a preferred non-English language.
func (rc *rowContext) genLanguageRows() []Row {
	langs := rc.profile.aff[DimLanguage]
	var best string
	var bestAff float64
	for lang, aff := range langs {
		if lang == "en" || lang == "" {
			continue
		}
		if aff > bestAff {
			bestAff, best = aff, lang
		}
	}
	if best == "" || bestAff <= 0.3 {
		return nil
	}
	var movies []db.MovieFeature
	for _, m := range rc.features {
		if rc.candidate(m) && m.Language == best {
			movies = append(movies, m)
		}
	}
	if len(movies) < 2 {
		return nil
	}
	return []Row{{
		Key:        "language-" + best,
		Title:      languageName(best) + " Cinema",
		Confidence: 0.5 + bestAff,
		Items:      rc.itemsOf(movies),
	}}
}

// genRewatchRows: favorites to watch again, for users who rewatch.
func (rc *rowContext) genRewatchRows() []Row {
	if rc.profile.Rewatch < 0.3 {
		return nil
	}
	type scored struct {
		m db.MovieFeature
		s float64
	}
	var favs []scored
	for _, m := range rc.features {
		if rc.completed(m.ID) && m.Playable {
			favs = append(favs, scored{m, rc.scoreMovie(m)})
		}
	}
	if len(favs) < 2 {
		return nil
	}
	sort.Slice(favs, func(i, j int) bool { return favs[i].s > favs[j].s })
	var items []Item
	for i, f := range favs {
		if i >= maxItemsPerRow {
			break
		}
		items = append(items, rc.toItem(f.m))
	}
	return []Row{{Key: "rewatch", Title: "Watch Again", Confidence: 0.4 + rc.profile.Rewatch, Items: items}}
}

// genTimeContextRows: picks matching the current time of day.
func (rc *rowContext) genTimeContextRows() []Row {
	bucket := hourBucket(rc.now)
	// Preferred runtime for this time of day: nights favor shorter films.
	preferRuntime := map[string]string{"night": "short", "morning": "medium", "afternoon": "long", "evening": "epic"}[bucket]
	title := map[string]string{"night": "Late Night Picks", "morning": "Morning Watch", "afternoon": "Afternoon Matinee", "evening": "For Tonight"}[bucket]

	var movies []db.MovieFeature
	for _, m := range rc.features {
		if rc.candidate(m) && runtimeBucket(m.Runtime) == preferRuntime {
			movies = append(movies, m)
		}
	}
	if len(movies) < 3 {
		return nil
	}
	// Rank by overall relevance within the time bucket.
	sort.Slice(movies, func(i, j int) bool { return rc.scoreMovie(movies[i]) > rc.scoreMovie(movies[j]) })
	conf := 0.5 + rc.profile.Affinity(DimHour, bucket)
	return []Row{{Key: "timectx-" + bucket, Title: title, Confidence: conf, Items: rc.itemsOf(movies)}}
}

// genContrastRows: novelty picks. With a taste vector it becomes "one step
// outside" - well-regarded titles thematically adjacent to but outside the
// user's cluster, rather than simply the lowest-scoring films (which tends to
// dredge up junk they'll never play). Falls back to the old lowest-score
// behavior when there's no usable vector.
func (rc *rowContext) genContrastRows() []Row {
	if items := rc.explorationItems(); len(items) >= 3 {
		return []Row{{
			Key: "contrast", Title: "One Step Outside",
			Subtitle:   "Adjacent to your taste, but new territory",
			Confidence: 0.4, Items: items,
		}}
	}

	// Fallback: least-like-the-profile unseen titles.
	type scored struct {
		m db.MovieFeature
		s float64
	}
	var cands []scored
	for _, m := range rc.features {
		if rc.candidate(m) {
			cands = append(cands, scored{m, rc.scoreMovie(m)})
		}
	}
	if len(cands) < 3 {
		return nil
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].s < cands[j].s })
	var items []Item
	for i, c := range cands {
		if i >= 12 {
			break
		}
		items = append(items, rc.toItem(c.m))
	}
	return []Row{{Key: "contrast", Title: "Something Different", Confidence: 0.35, Items: items}}
}

// explorationBand bounds how far outside the taste cluster an "adjacent" title
// sits: near enough to be relatable, far enough to be new. Cosine to taste.
const (
	explorationMinCosine = 0.05
	explorationMaxCosine = 0.30
)

// explorationItems returns high-quality unwatched titles at a moderate thematic
// distance from the taste vector, ranked by quality. Empty when there's no
// taste vector (cold-ish user) so the caller falls back.
func (rc *rowContext) explorationItems() []Item {
	if rc.space == nil || len(rc.taste) == 0 {
		return nil
	}
	type scored struct {
		m db.MovieFeature
		q float64
	}
	var cands []scored
	for _, m := range rc.features {
		if !rc.candidate(m) {
			continue
		}
		v, ok := rc.space.vecOf(movieKey(m.ID))
		if !ok {
			continue
		}
		c := cosine(rc.taste, v)
		if c < explorationMinCosine || c > explorationMaxCosine {
			continue
		}
		// Quality prior: rating tempered by vote count, so obscure 9.0s with two
		// votes don't dominate.
		q := m.Rating
		if m.Votes < 50 {
			q -= 1.0
		}
		cands = append(cands, scored{m, q})
	}
	if len(cands) < 3 {
		return nil
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].q != cands[j].q {
			return cands[i].q > cands[j].q
		}
		return cands[i].m.ID < cands[j].m.ID
	})
	items := make([]Item, 0, maxItemsPerRow)
	for _, c := range cands {
		if len(items) >= maxItemsPerRow {
			break
		}
		items = append(items, rc.toItem(c.m))
	}
	return items
}

// itemsOf caps and converts a movie slice to items.
func (rc *rowContext) itemsOf(movies []db.MovieFeature) []Item {
	if len(movies) > maxItemsPerRow {
		movies = movies[:maxItemsPerRow]
	}
	out := make([]Item, 0, len(movies))
	for _, m := range movies {
		out = append(out, rc.toItem(m))
	}
	return out
}

// --- ranking, rotation, dedupe ---

// rotationPenalty reduces confidence for rows shown recently.
func rotationPenalty(lastShown, now time.Time) float64 {
	if lastShown.IsZero() {
		return 1.0
	}
	age := now.Sub(lastShown)
	switch {
	case age < 6*time.Hour:
		return 0.5
	case age < 24*time.Hour:
		return 0.8
	default:
		return 1.0
	}
}

// dedupeAndRank sorts rows by confidence, drops empties/tiny rows, dedupes by
// key, caps how many rows of one strategy appear (so near-duplicates don't
// stack), and returns the top rows.
func dedupeAndRank(rows []Row) []Row {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Confidence > rows[j].Confidence })
	seen := map[string]bool{}
	byStrategy := map[string]int{}
	var out []Row
	for _, r := range rows {
		if seen[r.Key] || len(r.Items) == 0 {
			continue
		}
		if len(r.Items) < 2 && r.Key != "for-you" {
			continue
		}
		if s := strategyOf(r.Key); byStrategy[s] >= maxSameStrategyRows {
			continue // don't stack three "Because You Watched" rows
		}
		seen[r.Key] = true
		byStrategy[strategyOf(r.Key)]++
		out = append(out, r)
		if len(out) >= 10 {
			break
		}
	}
	return out
}

// buildNameIndex maps person ids to display names from the library.
func buildNameIndex(features []db.MovieFeature) map[int64]string {
	names := map[int64]string{}
	for _, m := range features {
		for _, d := range m.Directors {
			names[d.ID] = d.Name
		}
		for _, a := range m.Actors {
			names[a.ID] = a.Name
		}
	}
	return names
}
