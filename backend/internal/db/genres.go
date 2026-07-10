package db

import (
	"context"
	"fmt"
)

// setGenres replaces the genre links for a movie/show. Genres are stored by
// name (upserted) so the recommendation engine can compute genre affinity.
// linkTable is "movie_genres" or "show_genres"; idCol is "movie_id"/"show_id".
func (d *DB) setGenres(ctx context.Context, linkTable, idCol string, mediaID int64, names []string) error {
	if _, err := d.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, linkTable, idCol), mediaID); err != nil {
		return err
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, err := d.ExecContext(ctx,
			`INSERT INTO genres (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, name); err != nil {
			return err
		}
		var gid int64
		if err := d.QueryRowContext(ctx, `SELECT id FROM genres WHERE name = ?`, name).Scan(&gid); err != nil {
			return err
		}
		if _, err := d.ExecContext(ctx,
			fmt.Sprintf(`INSERT OR IGNORE INTO %s (%s, genre_id) VALUES (?, ?)`, linkTable, idCol),
			mediaID, gid); err != nil {
			return err
		}
	}
	return nil
}

// getGenres returns the genre names linked to a movie/show.
func (d *DB) getGenres(ctx context.Context, linkTable, idCol string, mediaID int64) ([]string, error) {
	rows, err := d.QueryContext(ctx, fmt.Sprintf(`
		SELECT g.name FROM genres g
		JOIN %s l ON l.genre_id = g.id
		WHERE l.%s = ? ORDER BY g.name`, linkTable, idCol), mediaID)
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
