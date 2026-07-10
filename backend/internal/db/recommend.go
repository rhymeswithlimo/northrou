package db

import (
	"context"
	"time"
)

// MovieFeature is a movie with the attributes the recommendation engine scores
// against.
type MovieFeature struct {
	ID           int64
	Title        string
	Year         int
	Runtime      int
	Language     string
	PosterPath   string
	CollectionID int64
	Rating       float64
	Votes        int
	Popularity   float64
	Revenue      int64
	Country      string
	Genres       []string
	Directors    []Person
	Actors       []Person
	Playable     bool // has a linked media file
}

// ShowFeature is a TV show with the attributes cold-start categories score
// against.
type ShowFeature struct {
	ID         int64
	Title      string
	Year       int
	Language   string
	PosterPath string
	Rating     float64
	Popularity float64
	Country    string
	Genres     []string
	Playable   bool // has at least one episode with a media file
}

// Person is a lightweight (id, name) pair.
type Person struct {
	ID   int64
	Name string
}

// LoadMovieFeatures loads every movie with its genres, directors, and actors
// stitched in memory. Suitable for a single household's library.
func (d *DB) LoadMovieFeatures(ctx context.Context) ([]MovieFeature, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT id, title, year, COALESCE(runtime,0), COALESCE(original_lang,''),
			COALESCE(poster_path,''), COALESCE(collection_id,0),
			vote_average, vote_count, popularity, revenue, COALESCE(country,''),
			file_id IS NOT NULL
		FROM movies`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[int64]*MovieFeature{}
	var order []int64
	for rows.Next() {
		var m MovieFeature
		if err := rows.Scan(&m.ID, &m.Title, &m.Year, &m.Runtime, &m.Language,
			&m.PosterPath, &m.CollectionID, &m.Rating, &m.Votes, &m.Popularity,
			&m.Revenue, &m.Country, &m.Playable); err != nil {
			return nil, err
		}
		byID[m.ID] = &m
		order = append(order, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Genres.
	gr, err := d.QueryContext(ctx, `
		SELECT mg.movie_id, g.name FROM movie_genres mg JOIN genres g ON g.id = mg.genre_id`)
	if err != nil {
		return nil, err
	}
	for gr.Next() {
		var mid int64
		var name string
		if err := gr.Scan(&mid, &name); err != nil {
			gr.Close()
			return nil, err
		}
		if m := byID[mid]; m != nil {
			m.Genres = append(m.Genres, name)
		}
	}
	gr.Close()

	// Credits (directors + actors).
	cr, err := d.QueryContext(ctx, `
		SELECT c.media_id, c.department, c.role, c.person_id, p.name, c.ord
		FROM credits c JOIN people p ON p.id = c.person_id
		WHERE c.media_kind = 'movie' ORDER BY c.ord`)
	if err != nil {
		return nil, err
	}
	for cr.Next() {
		var mid, pid int64
		var dept, role, name string
		var ord int
		if err := cr.Scan(&mid, &dept, &role, &pid, &name, &ord); err != nil {
			cr.Close()
			return nil, err
		}
		m := byID[mid]
		if m == nil {
			continue
		}
		switch {
		case dept == "cast":
			m.Actors = append(m.Actors, Person{ID: pid, Name: name})
		case role == "Director":
			m.Directors = append(m.Directors, Person{ID: pid, Name: name})
		}
	}
	cr.Close()

	out := make([]MovieFeature, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// LoadShowFeatures loads every TV show with its genres. A show is "playable"
// when at least one episode has a linked media file.
func (d *DB) LoadShowFeatures(ctx context.Context) ([]ShowFeature, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT id, title, year, COALESCE(original_lang,''), COALESCE(poster_path,''),
			vote_average, popularity, COALESCE(country,''),
			EXISTS(SELECT 1 FROM episodes e WHERE e.show_id = shows.id AND e.file_id IS NOT NULL)
		FROM shows`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[int64]*ShowFeature{}
	var order []int64
	for rows.Next() {
		var s ShowFeature
		if err := rows.Scan(&s.ID, &s.Title, &s.Year, &s.Language, &s.PosterPath,
			&s.Rating, &s.Popularity, &s.Country, &s.Playable); err != nil {
			return nil, err
		}
		byID[s.ID] = &s
		order = append(order, s.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	gr, err := d.QueryContext(ctx, `
		SELECT sg.show_id, g.name FROM show_genres sg JOIN genres g ON g.id = sg.genre_id`)
	if err != nil {
		return nil, err
	}
	for gr.Next() {
		var sid int64
		var name string
		if err := gr.Scan(&sid, &name); err != nil {
			gr.Close()
			return nil, err
		}
		if s := byID[sid]; s != nil {
			s.Genres = append(s.Genres, name)
		}
	}
	gr.Close()

	out := make([]ShowFeature, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// LoadCollections returns TMDB collection id -> name for collection-completion
// row generation.
func (d *DB) LoadCollections(ctx context.Context) (map[int64]string, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, name FROM collections`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// AffinityRow is one persisted affinity signal accumulator.
type AffinityRow struct {
	Dimension string
	Key       string
	Score     float64
	Weight    float64
	UpdatedAt time.Time
}

// GetAffinities loads all affinity rows for a user, keyed by dimension then key.
func (d *DB) GetAffinities(ctx context.Context, userID int64) (map[string]map[string]AffinityRow, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT dimension, key, score, weight, updated_at FROM affinities WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]AffinityRow{}
	for rows.Next() {
		var a AffinityRow
		if err := rows.Scan(&a.Dimension, &a.Key, &a.Score, &a.Weight, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if out[a.Dimension] == nil {
			out[a.Dimension] = map[string]AffinityRow{}
		}
		out[a.Dimension][a.Key] = a
	}
	return out, rows.Err()
}

// UpsertAffinity persists an affinity accumulator.
func (d *DB) UpsertAffinity(ctx context.Context, userID int64, a AffinityRow) error {
	_, err := d.ExecContext(ctx, `
		INSERT INTO affinities (user_id, dimension, key, score, weight, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, dimension, key) DO UPDATE SET
			score=excluded.score, weight=excluded.weight, updated_at=excluded.updated_at`,
		userID, a.Dimension, a.Key, a.Score, a.Weight, a.UpdatedAt)
	return err
}

// GetRewatchTendency returns the user's rewatch tendency (0 if no profile yet).
func (d *DB) GetRewatchTendency(ctx context.Context, userID int64) (float64, error) {
	var v float64
	err := d.QueryRowContext(ctx,
		`SELECT rewatch_tendency FROM taste_profile WHERE user_id = ?`, userID).Scan(&v)
	if err != nil {
		return 0, nil //nolint:nilerr // absent profile => 0
	}
	return v, nil
}

// SetRewatchTendency upserts the user's rewatch tendency.
func (d *DB) SetRewatchTendency(ctx context.Context, userID int64, v float64) error {
	_, err := d.ExecContext(ctx, `
		INSERT INTO taste_profile (user_id, rewatch_tendency, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET rewatch_tendency=excluded.rewatch_tendency, updated_at=excluded.updated_at`,
		userID, v, time.Now())
	return err
}

// GetMovieWatchHistory returns the user's movie watch events keyed by movie id.
func (d *DB) GetMovieWatchHistory(ctx context.Context, userID int64) (map[int64]WatchRow, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT media_id, position_sec, duration_sec, completed, rewatch_count, updated_at
		FROM watch_history WHERE user_id = ? AND media_kind = 'movie'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]WatchRow{}
	for rows.Next() {
		var wr WatchRow
		var completed int
		if err := rows.Scan(&wr.MediaID, &wr.PositionSec, &wr.DurationSec, &completed, &wr.RewatchCount, &wr.UpdatedAt); err != nil {
			return nil, err
		}
		wr.Completed = completed == 1
		out[wr.MediaID] = wr
	}
	return out, rows.Err()
}

// WatchRow is a watch history record used by the recommender.
type WatchRow struct {
	MediaID      int64
	PositionSec  float64
	DurationSec  float64
	Completed    bool
	RewatchCount int
	UpdatedAt    time.Time
}

// UpsertWatchEvent records progress for a (user, movie/episode). It increments
// rewatch_count when an already-completed item is watched from near the start
// again. Returns the resulting row.
func (d *DB) UpsertWatchEvent(ctx context.Context, userID int64, kind string, mediaID int64, pos, dur float64, completed bool) (WatchRow, error) {
	_, err := d.ExecContext(ctx, `
		INSERT INTO watch_history (user_id, media_kind, media_id, position_sec, duration_sec, completed, rewatch_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?)
		ON CONFLICT(user_id, media_kind, media_id) DO UPDATE SET
			position_sec=excluded.position_sec,
			duration_sec=excluded.duration_sec,
			completed=MAX(watch_history.completed, excluded.completed),
			rewatch_count=watch_history.rewatch_count +
				(CASE WHEN watch_history.completed = 1 AND excluded.completed = 1 THEN 1 ELSE 0 END),
			updated_at=excluded.updated_at`,
		userID, kind, mediaID, pos, dur, boolToInt(completed), time.Now())
	if err != nil {
		return WatchRow{}, err
	}
	var wr WatchRow
	var c int
	err = d.QueryRowContext(ctx, `
		SELECT media_id, position_sec, duration_sec, completed, rewatch_count, updated_at
		FROM watch_history WHERE user_id = ? AND media_kind = ? AND media_id = ?`,
		userID, kind, mediaID).Scan(&wr.MediaID, &wr.PositionSec, &wr.DurationSec, &c, &wr.RewatchCount, &wr.UpdatedAt)
	wr.Completed = c == 1
	return wr, err
}

// GetRowState returns last-shown times for home rows, keyed by row key.
func (d *DB) GetRowState(ctx context.Context, userID int64) (map[string]time.Time, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT row_key, last_shown FROM home_row_state WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var k string
		var t time.Time
		if err := rows.Scan(&k, &t); err != nil {
			return nil, err
		}
		out[k] = t
	}
	return out, rows.Err()
}

// MarkRowsShown records that the given row keys were displayed now.
func (d *DB) MarkRowsShown(ctx context.Context, userID int64, keys []string) error {
	now := time.Now()
	for _, k := range keys {
		if _, err := d.ExecContext(ctx, `
			INSERT INTO home_row_state (user_id, row_key, last_shown) VALUES (?, ?, ?)
			ON CONFLICT(user_id, row_key) DO UPDATE SET last_shown=excluded.last_shown`,
			userID, k, now); err != nil {
			return err
		}
	}
	return nil
}
