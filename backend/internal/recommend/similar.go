package recommend

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// SimResult is one "similar title" hit, carrying a human-readable reason for the
// detail card ("Directed by ...", "Shares the theme ...").
type SimResult struct {
	Kind       string
	ID         int64
	Title      string
	Year       int
	PosterPath string
	Reason     string
}

// Priors blended on top of cosine similarity. Cosine is in [0,1]; a shared
// collection is the strongest explicit signal (sequels/franchise), a shared
// director next. Sized so a same-collection title reliably outranks a merely
// thematically-similar one.
const (
	simCollectionBonus = 0.5
	simDirectorBonus   = 0.25
)

// maxSharedThemesInReason caps how many shared keywords a reason string names.
const maxSharedThemesInReason = 2

// SimilarMovies returns titles related to a movie, ranked by thematic (keyword)
// cosine blended with same-collection and shared-director priors. Falls back to
// the genre-overlap query when content vectors aren't available yet (e.g. before
// a keyword backfill).
func (e *Engine) SimilarMovies(ctx context.Context, id int64, limit int) ([]SimResult, error) {
	cat, err := e.loadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	index := make(map[int64]*db.MovieFeature, len(cat.movies))
	for i := range cat.movies {
		index[cat.movies[i].ID] = &cat.movies[i]
	}
	target := index[id]
	targetVec, hasVec := cat.space.vecOf(movieKey(id))
	if target == nil || !hasVec {
		return e.similarMoviesFallback(ctx, id, limit)
	}

	targetDirs := directorIDs(target)

	// Score every other movie: cosine + explicit priors. Same-collection and
	// shared-director titles are included even if their cosine is ~0.
	type scored struct {
		f      *db.MovieFeature
		score  float64
		reason string
	}
	var out []scored
	for oid, f := range index {
		if oid == id {
			continue
		}
		score := cosine(targetVec, mustVec(cat.space, movieKey(oid)))
		reason := ""

		sameCollection := target.CollectionID != 0 && f.CollectionID == target.CollectionID
		if sameCollection {
			score += simCollectionBonus
		}
		sharedDir := firstSharedDirector(targetDirs, f)
		if sharedDir != "" {
			score += simDirectorBonus
		}
		if score <= 0 {
			continue
		}

		// Reason, strongest signal first.
		switch {
		case sameCollection:
			reason = "From the same collection"
		case sharedDir != "":
			reason = "Directed by " + sharedDir
		default:
			reason = themeReason(cat.space.sharedKeywords(movieKey(id), movieKey(oid)))
		}
		out = append(out, scored{f: f, score: score, reason: reason})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].f.Rating != out[j].f.Rating {
			return out[i].f.Rating > out[j].f.Rating
		}
		return out[i].f.ID < out[j].f.ID
	})

	if limit <= 0 {
		limit = 12
	}
	results := make([]SimResult, 0, limit)
	for _, s := range out {
		if len(results) >= limit {
			break
		}
		results = append(results, SimResult{
			Kind: "movie", ID: s.f.ID, Title: s.f.Title, Year: s.f.Year,
			PosterPath: s.f.PosterPath, Reason: s.reason,
		})
	}
	return results, nil
}

// SimilarShows returns shows related to a show, ranked by content-vector cosine.
// Shows carry no collection/director features, so the blend is cosine plus the
// genre backbone already inside the vectors; it falls back to the genre-overlap
// query when vectors aren't available.
func (e *Engine) SimilarShows(ctx context.Context, id int64, limit int) ([]SimResult, error) {
	cat, err := e.loadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	index := make(map[int64]*db.ShowFeature, len(cat.shows))
	for i := range cat.shows {
		index[cat.shows[i].ID] = &cat.shows[i]
	}
	target := index[id]
	targetVec, hasVec := cat.space.vecOf(showKey(id))
	if target == nil || !hasVec {
		return e.similarShowsFallback(ctx, id, limit)
	}

	nbrs := cat.space.neighbors(targetVec, "show", func(k titleKey) bool { return k.id != id })
	if limit <= 0 {
		limit = 12
	}
	results := make([]SimResult, 0, limit)
	for _, nb := range nbrs {
		if len(results) >= limit {
			break
		}
		f := index[nb.key.id]
		if f == nil {
			continue
		}
		results = append(results, SimResult{
			Kind: "show", ID: f.ID, Title: f.Title, Year: f.Year,
			PosterPath: f.PosterPath,
			Reason:     themeReason(cat.space.sharedKeywords(showKey(id), nb.key)),
		})
	}
	return results, nil
}

// --- helpers ---

// mustVec returns a title's vector (possibly empty). Used where the key is known
// to be in the space; an empty vector yields cosine 0, which is correct.
func mustVec(space *vectorSpace, k titleKey) sparseVec {
	v := space.vecs[k]
	return v
}

func directorIDs(m *db.MovieFeature) map[int64]struct{} {
	if len(m.Directors) == 0 {
		return nil
	}
	out := make(map[int64]struct{}, len(m.Directors))
	for _, d := range m.Directors {
		out[d.ID] = struct{}{}
	}
	return out
}

// firstSharedDirector returns the name of a director shared with the target, or
// "" if none.
func firstSharedDirector(targetDirs map[int64]struct{}, f *db.MovieFeature) string {
	if len(targetDirs) == 0 {
		return ""
	}
	for _, d := range f.Directors {
		if _, ok := targetDirs[d.ID]; ok {
			return d.Name
		}
	}
	return ""
}

// themeReason turns shared keywords into detail-card copy. Empty when there are
// none, so the caller can omit the line.
func themeReason(shared []string) string {
	switch len(shared) {
	case 0:
		return ""
	case 1:
		return "Shares the theme: " + humanKeyword(shared[0])
	default:
		named := shared
		if len(named) > maxSharedThemesInReason {
			named = named[:maxSharedThemesInReason]
		}
		out := "Shared themes: "
		for i, k := range named {
			if i > 0 {
				out += ", "
			}
			out += humanKeyword(k)
		}
		if len(shared) > maxSharedThemesInReason {
			out += fmt.Sprintf(" +%d more", len(shared)-maxSharedThemesInReason)
		}
		return out
	}
}

func (e *Engine) similarMoviesFallback(ctx context.Context, id int64, limit int) ([]SimResult, error) {
	rs, err := e.db.SimilarMovies(ctx, id, limit)
	if err != nil {
		return nil, err
	}
	return searchResultsToSim(rs), nil
}

func (e *Engine) similarShowsFallback(ctx context.Context, id int64, limit int) ([]SimResult, error) {
	rs, err := e.db.SimilarShows(ctx, id, limit)
	if err != nil {
		return nil, err
	}
	return searchResultsToSim(rs), nil
}

// humanKeyword turns a normalized keyword ("slow-burn") into display text
// ("Slow Burn") for reason copy.
func humanKeyword(k string) string {
	words := strings.Split(k, "-")
	for i, w := range words {
		if w == "" {
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

func searchResultsToSim(rs []db.SearchResult) []SimResult {
	out := make([]SimResult, 0, len(rs))
	for _, r := range rs {
		out = append(out, SimResult{
			Kind: string(r.Kind), ID: r.ID, Title: r.Title, Year: r.Year,
			PosterPath: r.PosterPath,
		})
	}
	return out
}
