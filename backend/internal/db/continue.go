package db

import (
	"context"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// InProgressItem is one resumable item for the Continue Watching row.
//
// For an episode the display identity is the show (its title and backdrop),
// while the playable identity is the episode: the card reads "SILO / S03:E02"
// and opens the show, but resumes that episode's file.
type InProgressItem struct {
	Kind        model.MediaKind // KindMovie or KindEpisode
	ID          int64           // movie id, or episode id
	ShowID      int64           // 0 for movies
	Title       string          // movie title, or show title
	Season      int
	Number      int
	PositionSec float64
	DurationSec float64
	UpdatedAt   string
	BackdropPath string
	FileID      int64
}

// ListInProgress returns what this profile has started but not finished, most
// recently watched first.
//
// "In progress" deliberately excludes anything completed and anything barely
// started: a title you opened and closed after ten seconds is not something you
// are partway through, and it would otherwise pin itself to the top of the row.
func (d *DB) ListInProgress(ctx context.Context, profileID int64, limit int) ([]InProgressItem, error) {
	if limit <= 0 {
		limit = 20
	}
	const minPositionSec = 30

	rows, err := d.QueryContext(ctx, `
		SELECT 'movie' AS kind, m.id, 0 AS show_id, m.title, 0 AS season, 0 AS number,
			w.position_sec, w.duration_sec, w.updated_at, m.backdrop_path, COALESCE(m.file_id, 0)
		FROM watch_history w
		JOIN movies m ON m.id = w.media_id
		WHERE w.user_id = ? AND w.media_kind = 'movie'
			AND w.completed = 0 AND w.position_sec > ?

		UNION ALL

		SELECT 'episode' AS kind, e.id, e.show_id, s.title, e.season, e.number,
			w.position_sec, w.duration_sec, w.updated_at, s.backdrop_path, COALESCE(e.file_id, 0)
		FROM watch_history w
		JOIN episodes e ON e.id = w.media_id
		JOIN shows s ON s.id = e.show_id
		WHERE w.user_id = ? AND w.media_kind = 'episode'
			AND w.completed = 0 AND w.position_sec > ?

		ORDER BY updated_at DESC
		LIMIT ?`,
		profileID, minPositionSec, profileID, minPositionSec, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InProgressItem
	for rows.Next() {
		var it InProgressItem
		var kind string
		if err := rows.Scan(&kind, &it.ID, &it.ShowID, &it.Title, &it.Season, &it.Number,
			&it.PositionSec, &it.DurationSec, &it.UpdatedAt, &it.BackdropPath, &it.FileID); err != nil {
			return nil, err
		}
		it.Kind = model.MediaKind(kind)
		out = append(out, it)
	}
	return out, rows.Err()
}
