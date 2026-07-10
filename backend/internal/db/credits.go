package db

import (
	"context"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// setCredits replaces cast/crew for a movie or show. People are upserted by
// their TMDB id. Cast rows use department 'cast'; crew rows use the crew job as
// role and department for their actual department, enabling director/actor
// affinity in the recommendation engine.
func (d *DB) setCredits(ctx context.Context, kind model.MediaKind, mediaID int64, cast, crew []model.Credit) error {
	if _, err := d.ExecContext(ctx,
		`DELETE FROM credits WHERE media_kind = ? AND media_id = ?`, string(kind), mediaID); err != nil {
		return err
	}
	upsertAll := append(append([]model.Credit{}, cast...), crew...)
	for _, c := range upsertAll {
		if _, err := d.ExecContext(ctx,
			`INSERT INTO people (id, name) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET name=excluded.name`,
			c.PersonID, c.Name); err != nil {
			return err
		}
	}
	for _, c := range cast {
		if _, err := d.ExecContext(ctx,
			`INSERT INTO credits (person_id, media_kind, media_id, department, role, ord)
			 VALUES (?, ?, ?, 'cast', ?, ?)`,
			c.PersonID, string(kind), mediaID, c.Role, c.Order); err != nil {
			return err
		}
	}
	for _, c := range crew {
		// department 'crew', role = job (e.g. "Director") so the recommender
		// can query directors via role = 'Director'.
		if _, err := d.ExecContext(ctx,
			`INSERT INTO credits (person_id, media_kind, media_id, department, role, ord)
			 VALUES (?, ?, ?, 'crew', ?, 0)`,
			c.PersonID, string(kind), mediaID, c.Role); err != nil {
			return err
		}
	}
	return nil
}
