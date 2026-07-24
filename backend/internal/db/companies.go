package db

import (
	"context"
	"fmt"
)

// setCompanies replaces the production-company links for a movie/show. Companies
// are stored by name (upserted), mirroring setKeywords. linkTable is
// "movie_companies" or "show_companies"; idCol is "movie_id"/"show_id".
func setCompanies(ctx context.Context, q execer, linkTable, idCol string, mediaID int64, names []string) error {
	if _, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, linkTable, idCol), mediaID); err != nil {
		return err
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, err := q.ExecContext(ctx,
			`INSERT INTO production_companies (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, name); err != nil {
			return err
		}
		var cid int64
		if err := q.QueryRowContext(ctx, `SELECT id FROM production_companies WHERE name = ?`, name).Scan(&cid); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx,
			fmt.Sprintf(`INSERT OR IGNORE INTO %s (%s, company_id) VALUES (?, ?)`, linkTable, idCol),
			mediaID, cid); err != nil {
			return err
		}
	}
	return nil
}

// getCompanies returns the company names linked to a movie/show.
func (d *DB) getCompanies(ctx context.Context, linkTable, idCol string, mediaID int64) ([]string, error) {
	rows, err := d.QueryContext(ctx, fmt.Sprintf(`
		SELECT c.name FROM production_companies c
		JOIN %s l ON l.company_id = c.id
		WHERE l.%s = ? ORDER BY c.name`, linkTable, idCol), mediaID)
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

// setCreators replaces a show's creator names (stored directly, no vocab table).
func setCreators(ctx context.Context, q execer, showID int64, names []string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM show_creators WHERE show_id = ?`, showID); err != nil {
		return err
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, err := q.ExecContext(ctx,
			`INSERT OR IGNORE INTO show_creators (show_id, name) VALUES (?, ?)`, showID, name); err != nil {
			return err
		}
	}
	return nil
}

// getShowCreators returns a show's creator names.
func (d *DB) getShowCreators(ctx context.Context, showID int64) ([]string, error) {
	rows, err := d.QueryContext(ctx, `SELECT name FROM show_creators WHERE show_id = ? ORDER BY name`, showID)
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

// --- backfill helpers ---

// MoviesMissingCompanies returns movies with a TMDB id but no company links.
func (d *DB) MoviesMissingCompanies(ctx context.Context) ([]TitleRef, error) {
	return d.titlesMissingLink(ctx, "movies", "movie_companies", "movie_id")
}

// ShowsMissingCompanies returns shows with a TMDB id but no company links.
func (d *DB) ShowsMissingCompanies(ctx context.Context) ([]TitleRef, error) {
	return d.titlesMissingLink(ctx, "shows", "show_companies", "show_id")
}

// titlesMissingLink returns titles that have a TMDB id but no rows in linkTable.
func (d *DB) titlesMissingLink(ctx context.Context, table, linkTable, idCol string) ([]TitleRef, error) {
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

// SetMovieCompanies replaces a movie's company links (used by the backfill).
func (d *DB) SetMovieCompanies(ctx context.Context, movieID int64, names []string) error {
	return setCompanies(ctx, d, "movie_companies", "movie_id", movieID, names)
}

// SetShowCompanies replaces a show's company links (used by the backfill).
func (d *DB) SetShowCompanies(ctx context.Context, showID int64, names []string) error {
	return setCompanies(ctx, d, "show_companies", "show_id", showID, names)
}

// SetShowCreators replaces a show's creators (used by the backfill).
func (d *DB) SetShowCreators(ctx context.Context, showID int64, names []string) error {
	return setCreators(ctx, d, showID, names)
}
