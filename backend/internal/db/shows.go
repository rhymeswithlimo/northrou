package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// UpsertShow inserts or updates a show (keyed by TMDB id) with genres/credits
// and returns the local show id.
func (d *DB) UpsertShow(ctx context.Context, s *model.Show) (int64, error) {
	// One transaction for the show row plus its genre and credit writes (see
	// UpsertMovie for why).
	var id int64
	err := d.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO shows (tmdb_id, title, year, overview, original_lang, poster_path, backdrop_path,
				vote_average, popularity, country)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(tmdb_id) DO UPDATE SET
				title=excluded.title, year=excluded.year, overview=excluded.overview,
				original_lang=excluded.original_lang, poster_path=excluded.poster_path,
				backdrop_path=excluded.backdrop_path, vote_average=excluded.vote_average,
				popularity=excluded.popularity, country=excluded.country`,
			s.TMDBID, s.Title, s.Year, s.Overview, s.OriginalLang, s.PosterPath, s.BackdropPath,
			s.Rating, s.Popularity, s.Country); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM shows WHERE tmdb_id = ?`, s.TMDBID).Scan(&id); err != nil {
			return err
		}
		if err := setGenres(ctx, tx, "show_genres", "show_id", id, s.Genres); err != nil {
			return err
		}
		return setCredits(ctx, tx, model.KindShow, id, s.Cast, s.Crew)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// UpsertSeason ensures a season row exists and returns its id.
func (d *DB) UpsertSeason(ctx context.Context, showID int64, number int) (int64, error) {
	if _, err := d.ExecContext(ctx,
		`INSERT INTO seasons (show_id, number) VALUES (?, ?) ON CONFLICT(show_id, number) DO NOTHING`,
		showID, number); err != nil {
		return 0, err
	}
	var id int64
	err := d.QueryRowContext(ctx, `SELECT id FROM seasons WHERE show_id = ? AND number = ?`, showID, number).Scan(&id)
	return id, err
}

// UpsertEpisode inserts or updates an episode and returns its id.
func (d *DB) UpsertEpisode(ctx context.Context, e *model.Episode) (int64, error) {
	_, err := d.ExecContext(ctx, `
		INSERT INTO episodes (show_id, season_id, season, number, title, overview, runtime, file_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(show_id, season, number) DO UPDATE SET
			season_id=excluded.season_id, title=excluded.title,
			overview=excluded.overview, runtime=excluded.runtime, file_id=excluded.file_id`,
		e.ShowID, e.SeasonID, e.Season, e.Number, e.Title, e.Overview, e.Runtime, fileIDOrNil(e.File))
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.QueryRowContext(ctx,
		`SELECT id FROM episodes WHERE show_id = ? AND season = ? AND number = ?`,
		e.ShowID, e.Season, e.Number).Scan(&id)
	return id, err
}

// FindShowByTMDB returns the local show id for a TMDB id, or ErrNotFound.
func (d *DB) FindShowByTMDB(ctx context.Context, tmdbID int64) (int64, error) {
	var id int64
	err := d.QueryRowContext(ctx, `SELECT id FROM shows WHERE tmdb_id = ?`, tmdbID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// ListShows returns shows (summary), most recently added first. A limit <= 0
// returns all shows (backward-compatible default); a positive limit pages the
// result with the given offset.
func (d *DB) ListShows(ctx context.Context, limit, offset int) ([]model.Show, error) {
	query := `
		SELECT id, tmdb_id, title, year, overview, original_lang, poster_path, backdrop_path, added_at
		FROM shows ORDER BY added_at DESC`
	var args []any
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Show
	for rows.Next() {
		var s model.Show
		if err := rows.Scan(&s.ID, &s.TMDBID, &s.Title, &s.Year, &s.Overview,
			&s.OriginalLang, &s.PosterPath, &s.BackdropPath, &s.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetShow loads a show with its seasons and episodes.
func (d *DB) GetShow(ctx context.Context, id int64) (*model.Show, error) {
	row := d.QueryRowContext(ctx, `
		SELECT id, tmdb_id, title, year, overview, original_lang, poster_path, backdrop_path, added_at
		FROM shows WHERE id = ?`, id)
	var s model.Show
	err := row.Scan(&s.ID, &s.TMDBID, &s.Title, &s.Year, &s.Overview,
		&s.OriginalLang, &s.PosterPath, &s.BackdropPath, &s.AddedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.Genres, _ = d.getGenres(ctx, "show_genres", "show_id", id)

	rows, err := d.QueryContext(ctx, `
		SELECT id, season_id, season, number, title, overview, runtime, COALESCE(file_id,0)
		FROM episodes WHERE show_id = ? ORDER BY season, number`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bySeason := map[int]*model.Season{}
	for rows.Next() {
		var e model.Episode
		var fileID int64
		if err := rows.Scan(&e.ID, &e.SeasonID, &e.Season, &e.Number, &e.Title,
			&e.Overview, &e.Runtime, &fileID); err != nil {
			return nil, err
		}
		e.ShowID = id
		if fileID != 0 {
			e.File = &model.MediaFile{ID: fileID}
		}
		sea, ok := bySeason[e.Season]
		if !ok {
			sea = &model.Season{ShowID: id, Number: e.Season, ID: e.SeasonID}
			bySeason[e.Season] = sea
		}
		sea.Episodes = append(sea.Episodes, e)
	}
	for _, sea := range bySeason {
		s.Seasons = append(s.Seasons, *sea)
	}
	return &s, rows.Err()
}
