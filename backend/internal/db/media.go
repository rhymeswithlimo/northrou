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
