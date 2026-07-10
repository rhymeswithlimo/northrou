package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// UpsertMovie inserts or updates a movie (keyed by TMDB id) along with its
// genres, collection, and credits, and returns the local movie id.
func (d *DB) UpsertMovie(ctx context.Context, m *model.Movie) (int64, error) {
	var collectionID any
	if m.CollectionID != 0 {
		collectionID = m.CollectionID
	}
	// One transaction for the movie row plus its genre and credit writes. This
	// turns the ~40 per-title statements into a single WAL commit, which cuts
	// fsync churn dramatically on a slow disk and shortens the window the scan
	// holds the writer, so browsing stays responsive during a scan.
	var id int64
	err := d.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO movies (tmdb_id, title, year, overview, runtime, original_lang,
				collection_id, poster_path, backdrop_path, file_id,
				vote_average, vote_count, popularity, revenue, country)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(tmdb_id) DO UPDATE SET
				title=excluded.title, year=excluded.year, overview=excluded.overview,
				runtime=excluded.runtime, original_lang=excluded.original_lang,
				collection_id=excluded.collection_id, poster_path=excluded.poster_path,
				backdrop_path=excluded.backdrop_path, file_id=excluded.file_id,
				vote_average=excluded.vote_average, vote_count=excluded.vote_count,
				popularity=excluded.popularity, revenue=excluded.revenue, country=excluded.country`,
			m.TMDBID, m.Title, m.Year, m.Overview, m.Runtime, m.OriginalLang,
			collectionID, m.PosterPath, m.BackdropPath, fileIDOrNil(m.File),
			m.Rating, m.Votes, m.Popularity, m.Revenue, m.Country); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM movies WHERE tmdb_id = ?`, m.TMDBID).Scan(&id); err != nil {
			return err
		}
		if err := setGenres(ctx, tx, "movie_genres", "movie_id", id, m.Genres); err != nil {
			return err
		}
		return setCredits(ctx, tx, model.KindMovie, id, m.Cast, m.Crew)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// UpsertCollection stores a TMDB collection.
func (d *DB) UpsertCollection(ctx context.Context, id int64, name, poster, backdrop string) error {
	_, err := d.ExecContext(ctx, `
		INSERT INTO collections (id, name, poster, backdrop) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, poster=excluded.poster, backdrop=excluded.backdrop`,
		id, name, poster, backdrop)
	return err
}

// ListMovies returns movies (summary fields), most recently added first. A
// limit <= 0 returns the whole library (backward-compatible default); a
// positive limit pages the result with the given offset.
func (d *DB) ListMovies(ctx context.Context, limit, offset int) ([]model.Movie, error) {
	query := `
		SELECT id, tmdb_id, title, year, overview, runtime, original_lang,
			COALESCE(collection_id,0), poster_path, backdrop_path, COALESCE(file_id,0), added_at
		FROM movies ORDER BY added_at DESC`
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
	var out []model.Movie
	for rows.Next() {
		var m model.Movie
		var fileID int64
		if err := rows.Scan(&m.ID, &m.TMDBID, &m.Title, &m.Year, &m.Overview,
			&m.Runtime, &m.OriginalLang, &m.CollectionID, &m.PosterPath,
			&m.BackdropPath, &fileID, &m.AddedAt); err != nil {
			return nil, err
		}
		if fileID != 0 {
			m.File = &model.MediaFile{ID: fileID}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMovie loads a single movie with genres and its media file.
func (d *DB) GetMovie(ctx context.Context, id int64) (*model.Movie, error) {
	row := d.QueryRowContext(ctx, `
		SELECT id, tmdb_id, title, year, overview, runtime, original_lang,
			COALESCE(collection_id,0), poster_path, backdrop_path, COALESCE(file_id,0), added_at
		FROM movies WHERE id = ?`, id)
	var m model.Movie
	var fileID int64
	err := row.Scan(&m.ID, &m.TMDBID, &m.Title, &m.Year, &m.Overview, &m.Runtime,
		&m.OriginalLang, &m.CollectionID, &m.PosterPath, &m.BackdropPath, &fileID, &m.AddedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Genres, _ = d.getGenres(ctx, "movie_genres", "movie_id", id)
	if fileID != 0 {
		if mf, err := d.GetMediaFile(ctx, fileID); err == nil {
			m.File = mf
		}
	}
	return &m, nil
}

func fileIDOrNil(f *model.MediaFile) any {
	if f == nil || f.ID == 0 {
		return nil
	}
	return f.ID
}
