package recommend

import (
	"context"
	"sync"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// homeCacheTTL bounds how long a computed home screen is reused. Building it
// loads the whole library into memory (genres + credits stitched), so on a
// low-RAM box with a big library, recomputing it per request is an OOM risk;
// the cache collapses a burst of requests into one load. Watches and scans
// invalidate it explicitly, so the TTL only guards against unchanged reloads.
const homeCacheTTL = 60 * time.Second

// Engine computes recommendations and maintains taste profiles.
type Engine struct {
	db *db.DB

	mu   sync.RWMutex
	home map[int64]cachedHome // per-user computed home rows
	cat  *catalog             // memoized library features + content vectors
}

type cachedHome struct {
	rows    []Row
	expires time.Time
}

// catalog is the library-wide snapshot shared across users: every movie/show's
// scoring features plus their content vectors. Unlike home rows (per-user,
// short-TTL) it changes only when the library does, so it is memoized until a
// scan/catalog change clears it via InvalidateAll. Building it loads the whole
// library, so sharing one copy across users and requests matters on a low-RAM
// box with a big library.
type catalog struct {
	movies []db.MovieFeature
	shows  []db.ShowFeature
	space  *vectorSpace
}

// New builds a recommendation Engine.
func New(database *db.DB) *Engine {
	return &Engine{db: database, home: map[int64]cachedHome{}}
}

// loadCatalog returns the memoized catalog, building it (and the content-vector
// space) on first use after a scan.
func (e *Engine) loadCatalog(ctx context.Context) (*catalog, error) {
	e.mu.RLock()
	c := e.cat
	e.mu.RUnlock()
	if c != nil {
		return c, nil
	}
	movies, err := e.db.LoadMovieFeatures(ctx)
	if err != nil {
		return nil, err
	}
	shows, err := e.db.LoadShowFeatures(ctx)
	if err != nil {
		return nil, err
	}
	c = &catalog{movies: movies, shows: shows, space: buildVectorSpace(movies, shows)}
	e.mu.Lock()
	e.cat = c
	e.mu.Unlock()
	return c, nil
}

// cachedRows returns a user's cached home rows if still fresh.
func (e *Engine) cachedRows(userID int64) ([]Row, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	c, ok := e.home[userID]
	if !ok || time.Now().After(c.expires) {
		return nil, false
	}
	return c.rows, true
}

// storeRows caches a user's computed home rows.
func (e *Engine) storeRows(userID int64, rows []Row) {
	e.mu.Lock()
	e.home[userID] = cachedHome{rows: rows, expires: time.Now().Add(homeCacheTTL)}
	e.mu.Unlock()
}

// invalidate drops one user's cached home rows (e.g. after they watch something).
func (e *Engine) invalidate(userID int64) {
	e.mu.Lock()
	delete(e.home, userID)
	e.mu.Unlock()
}

// InvalidateAll drops every cached home screen. Call it after a library scan,
// which changes the catalog for all users.
func (e *Engine) InvalidateAll() {
	e.mu.Lock()
	clear(e.home)
	e.cat = nil // the library changed; rebuild features + content vectors
	e.mu.Unlock()
}

// Profile is a user's taste profile in memory: normalized affinities (mean
// signal) and per-key confidence (accumulated weight), plus rewatch tendency.
type Profile struct {
	aff     map[string]map[string]float64
	weight  map[string]map[string]float64
	Rewatch float64
}

func newProfile() *Profile {
	return &Profile{
		aff:    map[string]map[string]float64{},
		weight: map[string]map[string]float64{},
	}
}

// Affinity returns the normalized affinity for a dimension/key (0 if unknown).
func (p *Profile) Affinity(dim, key string) float64 {
	if m := p.aff[dim]; m != nil {
		return m[key]
	}
	return 0
}

// Confidence returns the accumulated weight for a dimension/key.
func (p *Profile) Confidence(dim, key string) float64 {
	if m := p.weight[dim]; m != nil {
		return m[key]
	}
	return 0
}

// HasData reports whether the profile has any signal at all.
func (p *Profile) HasData() bool {
	for _, m := range p.aff {
		if len(m) > 0 {
			return true
		}
	}
	return false
}

// LoadProfile builds a Profile from persisted affinities.
func (e *Engine) LoadProfile(ctx context.Context, userID int64) (*Profile, error) {
	affs, err := e.db.GetAffinities(ctx, userID)
	if err != nil {
		return nil, err
	}
	p := newProfile()
	for dim, keys := range affs {
		p.aff[dim] = map[string]float64{}
		p.weight[dim] = map[string]float64{}
		for key, row := range keys {
			p.aff[dim][key] = normalized(row)
			p.weight[dim][key] = row.Weight
		}
	}
	p.Rewatch, _ = e.db.GetRewatchTendency(ctx, userID)
	return p, nil
}

// RecordWatch updates watch history and incrementally adjusts the taste profile.
// pos/dur are the playback position and total duration.
//
// kind is KindMovie or KindEpisode. Episodes are recorded so they can be
// resumed, but they don't feed the taste profile: it is built from movie
// features (genre, director, decade), and there is no episode equivalent. This
// used to hardcode "movie", which meant episode progress could never be stored
// at all and Continue Watching could not show a partway-through show.
func (e *Engine) RecordWatch(ctx context.Context, userID int64, kind model.MediaKind, mediaID int64, pos, dur float64) error {
	// A watch changes this user's taste profile, so their cached home is stale.
	defer e.invalidate(userID)
	completed := dur > 0 && pos/dur >= 0.9
	wr, err := e.db.UpsertWatchEvent(ctx, userID, string(kind), mediaID, pos, dur, completed)
	if err != nil {
		return err
	}

	if kind != model.KindMovie {
		return nil
	}

	// Playing a movie is the engagement signal for the home rows that surfaced
	// it: credit them so the lifecycle can tell working rows from ignored ones.
	// Non-fatal - a failure here must not fail recording the watch.
	_, _ = e.db.CreditHomeCollectionWatch(ctx, userID, mediaID)

	mf, ok, err := e.movieFeature(ctx, mediaID)
	if err != nil {
		return err
	}
	if !ok {
		return nil // unknown movie, nothing to profile
	}

	c := 0.0
	if dur > 0 {
		c = pos / dur
	}
	if wr.Completed {
		if c < 0.95 {
			c = 0.95
		}
	}
	signal := amplifyRewatch(signalFromCompletion(c), wr.RewatchCount)

	now := time.Now()
	affs, err := e.db.GetAffinities(ctx, userID)
	if err != nil {
		return err
	}
	for _, dk := range movieDimensionKeys(mf, now) {
		existing := affs[dk.dim][dk.key]
		existing.Dimension = dk.dim
		existing.Key = dk.key
		updated := updateAccumulator(existing, signal, now)
		if err := e.db.UpsertAffinity(ctx, userID, updated); err != nil {
			return err
		}
	}

	// Rewatch tendency: EMA toward whether this was a rewatch.
	target := 0.0
	if wr.RewatchCount > 0 {
		target = 1.0
	}
	rt, _ := e.db.GetRewatchTendency(ctx, userID)
	rt = rt*0.9 + target*0.1
	return e.db.SetRewatchTendency(ctx, userID, rt)
}

type dimKey struct{ dim, key string }

// movieDimensionKeys returns every (dimension, key) a watch of mf should update.
func movieDimensionKeys(mf db.MovieFeature, now time.Time) []dimKey {
	var out []dimKey
	for _, g := range mf.Genres {
		out = append(out, dimKey{DimGenre, g})
	}
	if dk := decadeKey(mf.Year); dk != "" {
		out = append(out, dimKey{DimDecade, dk})
	}
	for _, d := range mf.Directors {
		out = append(out, dimKey{DimDirector, personKey(d.ID)})
	}
	for i, a := range mf.Actors {
		if i >= maxActorsScored {
			break
		}
		out = append(out, dimKey{DimActor, personKey(a.ID)})
	}
	if mf.Language != "" {
		out = append(out, dimKey{DimLanguage, mf.Language})
	}
	if rb := runtimeBucket(mf.Runtime); rb != "" {
		out = append(out, dimKey{DimRuntime, rb})
	}
	out = append(out, dimKey{DimHour, hourBucket(now)})
	return out
}

// movieFeature loads a single movie's features. It queries just that movie
// rather than loading the whole library and scanning for one id.
func (e *Engine) movieFeature(ctx context.Context, movieID int64) (db.MovieFeature, bool, error) {
	return e.db.LoadMovieFeature(ctx, movieID)
}
