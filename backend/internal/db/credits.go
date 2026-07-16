package db

import (
	"context"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// setCredits replaces cast/crew for a movie or show. People are upserted by
// their TMDB id. Cast rows use department 'cast'; crew rows use the crew job as
// role and department for their actual department, enabling director/actor
// affinity in the recommendation engine.
func setCredits(ctx context.Context, q execer, kind model.MediaKind, mediaID int64, cast, crew []model.Credit) error {
	if _, err := q.ExecContext(ctx,
		`DELETE FROM credits WHERE media_kind = ? AND media_id = ?`, string(kind), mediaID); err != nil {
		return err
	}
	upsertAll := append(append([]model.Credit{}, cast...), crew...)
	for _, c := range upsertAll {
		// A person can be credited on many titles and only some payloads carry a
		// headshot, so never overwrite a stored path with an empty one.
		if _, err := q.ExecContext(ctx,
			`INSERT INTO people (id, name, profile_path) VALUES (?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				profile_path = CASE WHEN excluded.profile_path != '' THEN excluded.profile_path ELSE people.profile_path END`,
			c.PersonID, c.Name, c.ProfilePath); err != nil {
			return err
		}
	}
	for _, c := range cast {
		if _, err := q.ExecContext(ctx,
			`INSERT INTO credits (person_id, media_kind, media_id, department, role, ord)
			 VALUES (?, ?, ?, 'cast', ?, ?)`,
			c.PersonID, string(kind), mediaID, c.Role, c.Order); err != nil {
			return err
		}
	}
	for _, c := range crew {
		// department 'crew', role = job (e.g. "Director") so the recommender
		// can query directors via role = 'Director'.
		if _, err := q.ExecContext(ctx,
			`INSERT INTO credits (person_id, media_kind, media_id, department, role, ord)
			 VALUES (?, ?, ?, 'crew', ?, 0)`,
			c.PersonID, string(kind), mediaID, c.Role); err != nil {
			return err
		}
	}
	return nil
}

// getCredits returns the cast and crew linked to a movie or show, mirroring
// getGenres. Cast comes back in TMDB billing order (ord); crew follows in
// insertion order, which puts the key jobs first as the scanner picked them.
func (d *DB) getCredits(ctx context.Context, kind model.MediaKind, mediaID int64) (cast, crew []model.Credit, err error) {
	rows, err := d.QueryContext(ctx, `
		SELECT c.department, p.id, p.name, COALESCE(c.role,''), c.ord, p.profile_path
		FROM credits c
		JOIN people p ON p.id = c.person_id
		WHERE c.media_kind = ? AND c.media_id = ?
		ORDER BY CASE c.department WHEN 'cast' THEN 0 ELSE 1 END, c.ord, c.id`,
		string(kind), mediaID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var dept string
		var c model.Credit
		if err := rows.Scan(&dept, &c.PersonID, &c.Name, &c.Role, &c.Order, &c.ProfilePath); err != nil {
			return nil, nil, err
		}
		if dept == "cast" {
			cast = append(cast, c)
		} else {
			crew = append(crew, c)
		}
	}
	return cast, crew, rows.Err()
}
