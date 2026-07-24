package db

import (
	"context"
	"fmt"
)

// setKeywords replaces the keyword links for a movie/show. Keywords are stored
// by name (upserted) so the recommendation engine can build keyword
// co-occurrence vectors. Mirrors setGenres. linkTable is "movie_keywords" or
// "show_keywords"; idCol is "movie_id"/"show_id".
func setKeywords(ctx context.Context, q execer, linkTable, idCol string, mediaID int64, names []string) error {
	if _, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, linkTable, idCol), mediaID); err != nil {
		return err
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, err := q.ExecContext(ctx,
			`INSERT INTO keywords (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, name); err != nil {
			return err
		}
		var kid int64
		if err := q.QueryRowContext(ctx, `SELECT id FROM keywords WHERE name = ?`, name).Scan(&kid); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx,
			fmt.Sprintf(`INSERT OR IGNORE INTO %s (%s, keyword_id) VALUES (?, ?)`, linkTable, idCol),
			mediaID, kid); err != nil {
			return err
		}
	}
	return nil
}

// TitleRef identifies a matched title by its local id and TMDB id. Used by the
// keyword backfill to know which TMDB record to re-fetch.
type TitleRef struct {
	ID     int64
	TMDBID int64
}

// MoviesMissingKeywords returns movies that have a TMDB id but no keyword links,
// so the backfill can re-fetch only what it needs. Idempotent: a title stays in
// the list until it gets at least one keyword row.
func (d *DB) MoviesMissingKeywords(ctx context.Context) ([]TitleRef, error) {
	return d.titlesMissingKeywords(ctx, "movies", "movie_keywords", "movie_id")
}

// ShowsMissingKeywords returns shows with a TMDB id but no keyword links.
func (d *DB) ShowsMissingKeywords(ctx context.Context) ([]TitleRef, error) {
	return d.titlesMissingKeywords(ctx, "shows", "show_keywords", "show_id")
}

func (d *DB) titlesMissingKeywords(ctx context.Context, table, linkTable, idCol string) ([]TitleRef, error) {
	rows, err := d.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.id, t.tmdb_id FROM %s t
		WHERE t.tmdb_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM %s l WHERE l.%s = t.id)
		ORDER BY t.id`, table, linkTable, idCol))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TitleRef
	for rows.Next() {
		var r TitleRef
		if err := rows.Scan(&r.ID, &r.TMDBID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetMovieKeywords replaces a movie's keyword links (used by the backfill).
func (d *DB) SetMovieKeywords(ctx context.Context, movieID int64, names []string) error {
	return setKeywords(ctx, d, "movie_keywords", "movie_id", movieID, names)
}

// SetShowKeywords replaces a show's keyword links (used by the backfill).
func (d *DB) SetShowKeywords(ctx context.Context, showID int64, names []string) error {
	return setKeywords(ctx, d, "show_keywords", "show_id", showID, names)
}

// getKeywords returns the keyword names linked to a movie/show.
func (d *DB) getKeywords(ctx context.Context, linkTable, idCol string, mediaID int64) ([]string, error) {
	rows, err := d.QueryContext(ctx, fmt.Sprintf(`
		SELECT k.name FROM keywords k
		JOIN %s l ON l.keyword_id = k.id
		WHERE l.%s = ? ORDER BY k.name`, linkTable, idCol), mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
