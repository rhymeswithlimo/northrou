package recommend

import (
	"context"
	"sync"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
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

	mu    sync.RWMutex
	home  map[int64]cachedHome // per-user computed home rows
}

type cachedHome struct {
	rows    []Row
	expires time.Time
}

// New builds a recommendation Engine.
func New(database *db.DB) *Engine {
	return &Engine{db: database, home: map[int64]cachedHome{}}
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
	e.mu.Unlock()
}

// Profile is a user's taste profile in memory: normalized affinities (mean
// signal) and per-key confidence (accumulated weight), plus rewatch tendency.
type Profile struct {
	aff    map[string]map[string]float64
	weight map[string]map[string]float64
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

// RecordWatch updates watch history and incrementally adjusts the taste profile
// for a movie watch. pos/dur are the playback position and total duration.
func (e *Engine) RecordWatch(ctx context.Context, userID, movieID int64, pos, dur float64) error {
	// A watch changes this user's taste profile, so their cached home is stale.
	defer e.invalidate(userID)
	completed := dur > 0 && pos/dur >= 0.9
	wr, err := e.db.UpsertWatchEvent(ctx, userID, "movie", movieID, pos, dur, completed)
	if err != nil {
		return err
	}

	mf, ok, err := e.movieFeature(ctx, movieID)
	if err != nil {
		return err
	}
	if !ok {
		return nil // unknown movie (or an episode), nothing to profile
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
