package db

import (
	"context"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// SearchResult is one library hit, across movies and shows.
type SearchResult struct {
	Kind       model.MediaKind
	ID         int64
	Title      string
	Year       int
	PosterPath string
}

// Search finds movies and shows whose title matches q.
//
// Deliberately a LIKE scan rather than FTS5: a household library is thousands
// of rows, not millions, the titles are already in memory-resident pages, and
// an FTS virtual table would be another thing to keep in sync on every upsert.
// Ordering puts prefix matches first so typing "the" surfaces "The Thing"
// before "Breathe".
func (d *DB) Search(ctx context.Context, q string, limit int) ([]SearchResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 40
	}

	// Escape LIKE wildcards so a title containing % or _ is matched literally.
	esc := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q)
	prefix := esc + "%"
	anywhere := "%" + esc + "%"

	rows, err := d.QueryContext(ctx, `
		SELECT kind, id, title, year, poster_path FROM (
			SELECT 'movie' AS kind, id, title, year, poster_path FROM movies
			WHERE title LIKE ? ESCAPE '\'
			UNION ALL
			SELECT 'show' AS kind, id, title, year, poster_path FROM shows
			WHERE title LIKE ? ESCAPE '\'
		)
		ORDER BY (CASE WHEN title LIKE ? ESCAPE '\' THEN 0 ELSE 1 END), title
		LIMIT ?`,
		anywhere, anywhere, prefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var kind string
		if err := rows.Scan(&kind, &r.ID, &r.Title, &r.Year, &r.PosterPath); err != nil {
			return nil, err
		}
		r.Kind = model.MediaKind(kind)
		out = append(out, r)
	}
	return out, rows.Err()
}

// SimilarMovies returns titles related to a movie: everything in the same TMDB
// collection first (a sequel is the strongest "more like this" signal a local
// library has), then the closest matches by shared genre.
//
// There is no TMDB /similar call here on purpose. It would mean a network round
// trip per detail view, and would return titles the household does not own.
func (d *DB) SimilarMovies(ctx context.Context, id int64, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := d.QueryContext(ctx, `
		WITH target AS (
			SELECT id, COALESCE(collection_id, 0) AS collection_id FROM movies WHERE id = ?
		),
		target_genres AS (
			SELECT genre_id FROM movie_genres WHERE movie_id = ?
		)
		SELECT id, title, year, poster_path, same_collection, shared FROM (
			SELECT m.id, m.title, m.year, m.poster_path, m.vote_average,
				CASE WHEN t.collection_id != 0 AND m.collection_id = t.collection_id THEN 1 ELSE 0 END AS same_collection,
				(SELECT COUNT(*) FROM movie_genres mg
					WHERE mg.movie_id = m.id AND mg.genre_id IN (SELECT genre_id FROM target_genres)) AS shared
			FROM movies m, target t
			WHERE m.id != t.id
		)
		WHERE same_collection = 1 OR shared > 0
		ORDER BY same_collection DESC, shared DESC, vote_average DESC, title
		LIMIT ?`, id, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var sameCollection, shared int
		if err := rows.Scan(&r.ID, &r.Title, &r.Year, &r.PosterPath, &sameCollection, &shared); err != nil {
			return nil, err
		}
		r.Kind = model.KindMovie
		out = append(out, r)
	}
	return out, rows.Err()
}

// SimilarShows mirrors SimilarMovies by shared genre. Shows have no collection
// equivalent in TMDB.
func (d *DB) SimilarShows(ctx context.Context, id int64, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := d.QueryContext(ctx, `
		WITH target_genres AS (
			SELECT genre_id FROM show_genres WHERE show_id = ?
		)
		SELECT id, title, year, poster_path, shared FROM (
			SELECT s.id, s.title, s.year, s.poster_path, s.vote_average,
				(SELECT COUNT(*) FROM show_genres sg
					WHERE sg.show_id = s.id AND sg.genre_id IN (SELECT genre_id FROM target_genres)) AS shared
			FROM shows s
			WHERE s.id != ?
		)
		WHERE shared > 0
		ORDER BY shared DESC, vote_average DESC, title
		LIMIT ?`, id, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var shared int
		if err := rows.Scan(&r.ID, &r.Title, &r.Year, &r.PosterPath, &shared); err != nil {
			return nil, err
		}
		r.Kind = model.KindShow
		out = append(out, r)
	}
	return out, rows.Err()
}
