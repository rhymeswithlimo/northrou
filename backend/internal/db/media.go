package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// UpsertMediaFile inserts or updates a media file by path and returns its id.
// Technical stream data is stored as JSON blobs.
func (d *DB) UpsertMediaFile(ctx context.Context, mf *model.MediaFile) (int64, error) {
	videoJSON, _ := json.Marshal(mf.Video)
	audioJSON, _ := json.Marshal(mf.Audio)
	subsJSON, _ := json.Marshal(mf.Subtitles)

	_, err := d.ExecContext(ctx, `
		INSERT INTO media_files (path, size_bytes, mod_time, container, duration,
			video_json, audio_json, subs_json, hdr_type, width, height, probed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			size_bytes=excluded.size_bytes, mod_time=excluded.mod_time,
			container=excluded.container, duration=excluded.duration,
			video_json=excluded.video_json, audio_json=excluded.audio_json,
			subs_json=excluded.subs_json, hdr_type=excluded.hdr_type,
			width=excluded.width, height=excluded.height, probed_at=excluded.probed_at`,
		mf.Path, mf.SizeBytes, mf.ModTime, mf.Container, mf.Duration,
		string(videoJSON), string(audioJSON), string(subsJSON),
		string(mf.Video.HDR), mf.Video.Width, mf.Video.Height, time.Now())
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.QueryRowContext(ctx, `SELECT id FROM media_files WHERE path = ?`, mf.Path).Scan(&id)
	return id, err
}

// MediaFileRef is a lightweight (id, path) pair for reconciliation walks.
type MediaFileRef struct {
	ID   int64
	Path string
}

// AllMediaFiles returns every media file id+path, for on-disk reconciliation
// (detecting files deleted since the last scan).
func (d *DB) AllMediaFiles(ctx context.Context) ([]MediaFileRef, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, path FROM media_files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaFileRef
	for rows.Next() {
		var r MediaFileRef
		if err := rows.Scan(&r.ID, &r.Path); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LinkedFile is a media file currently linked to a movie or episode (a duplicate
// "winner"), with the fields needed to re-derive its quality score.
type LinkedFile struct {
	Path      string
	Kind      model.MediaKind
	Height    int
	BitRate   int64
	SizeBytes int64
	Duration  float64
}

// LinkedFiles returns the media files referenced by a movie or episode. The
// scanner seeds duplicate resolution with these so a still-present best copy is
// never displaced by a lesser duplicate on a later scan.
func (d *DB) LinkedFiles(ctx context.Context) ([]LinkedFile, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT mf.path, 'movie', mf.height, mf.size_bytes, mf.duration, mf.video_json
			FROM media_files mf JOIN movies m ON m.file_id = mf.id
		UNION ALL
		SELECT mf.path, 'episode', mf.height, mf.size_bytes, mf.duration, mf.video_json
			FROM media_files mf JOIN episodes e ON e.file_id = mf.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LinkedFile
	for rows.Next() {
		var lf LinkedFile
		var videoJSON string
		if err := rows.Scan(&lf.Path, &lf.Kind, &lf.Height, &lf.SizeBytes, &lf.Duration, &videoJSON); err != nil {
			return nil, err
		}
		var v model.VideoStream
		_ = json.Unmarshal([]byte(videoJSON), &v)
		lf.BitRate = v.BitRate
		if lf.Height == 0 {
			lf.Height = v.Height
		}
		out = append(out, lf)
	}
	return out, rows.Err()
}

// DeleteMediaFile removes a media_files row (subtitle rows cascade; movie/episode
// file_id is set null). Used when a source file no longer exists on disk.
func (d *DB) DeleteMediaFile(ctx context.Context, id int64) error {
	_, err := d.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, id)
	return err
}

// DeleteTitlesWithoutFile removes movies and episodes whose file was deleted
// (file_id went null). Returns how many rows were removed in total.
func (d *DB) DeleteTitlesWithoutFile(ctx context.Context) (int64, error) {
	var total int64
	for _, table := range []string{"movies", "episodes"} {
		res, err := d.ExecContext(ctx, `DELETE FROM `+table+` WHERE file_id IS NULL`)
		if err != nil {
			return total, err
		}
		if n, err := res.RowsAffected(); err == nil {
			total += n
		}
	}
	return total, nil
}

// MediaFileIDByPath returns the id of the media_files row for a path, or
// ErrNotFound. Used to reconcile sidecar subtitles for files whose video did not
// change (so NeedsScan skipped a full reprocess).
func (d *DB) MediaFileIDByPath(ctx context.Context, path string) (int64, error) {
	var id int64
	err := d.QueryRowContext(ctx, `SELECT id FROM media_files WHERE path = ?`, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// PruneOrphanMediaFiles deletes media_files rows not referenced by any movie or
// episode (duplicate losers, or titles whose file was replaced). Subtitle rows
// cascade away via their foreign key. Returns the number of rows removed.
func (d *DB) PruneOrphanMediaFiles(ctx context.Context) (int64, error) {
	res, err := d.ExecContext(ctx, `
		DELETE FROM media_files
		WHERE id NOT IN (SELECT file_id FROM movies WHERE file_id IS NOT NULL)
		  AND id NOT IN (SELECT file_id FROM episodes WHERE file_id IS NOT NULL)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetMediaFile loads a media file by id.
func (d *DB) GetMediaFile(ctx context.Context, id int64) (*model.MediaFile, error) {
	row := d.QueryRowContext(ctx, `
		SELECT id, path, size_bytes, mod_time, container, duration,
			video_json, audio_json, subs_json
		FROM media_files WHERE id = ?`, id)
	return scanMediaFile(row)
}

func scanMediaFile(row *sql.Row) (*model.MediaFile, error) {
	var mf model.MediaFile
	var videoJSON, audioJSON, subsJSON string
	err := row.Scan(&mf.ID, &mf.Path, &mf.SizeBytes, &mf.ModTime, &mf.Container,
		&mf.Duration, &videoJSON, &audioJSON, &subsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(videoJSON), &mf.Video)
	_ = json.Unmarshal([]byte(audioJSON), &mf.Audio)
	_ = json.Unmarshal([]byte(subsJSON), &mf.Subtitles)
	return &mf, nil
}
