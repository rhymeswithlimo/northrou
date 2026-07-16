package db

import (
	"context"
	"database/sql"
	"errors"
)

// SubtitleTrack is a persisted subtitle asset row.
type SubtitleTrack struct {
	ID         int64  `json:"id"`
	FileID     int64  `json:"file_id"`
	TrackIndex int    `json:"track_index"`
	Language   string `json:"language"`
	Title      string `json:"title"`
	Format     string `json:"format"` // subrip|ass|pgs|...
	Source     string `json:"source"` // embedded|external
	Forced     bool   `json:"forced"`
	VTTPath    string `json:"-"`          // local path, served via the API
	OCRStatus  string `json:"ocr_status"` // none|queued|processing|done|failed|skipped
}

// UpsertSubtitleTrack inserts or updates a subtitle track by (file, index).
func (d *DB) UpsertSubtitleTrack(ctx context.Context, t *SubtitleTrack) (int64, error) {
	_, err := d.ExecContext(ctx, `
		INSERT INTO subtitle_tracks (file_id, track_index, language, title, format, source, forced, vtt_path, ocr_status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_id, track_index) DO UPDATE SET
			language=excluded.language, title=excluded.title, format=excluded.format,
			source=excluded.source, forced=excluded.forced,
			vtt_path=COALESCE(NULLIF(excluded.vtt_path,''), subtitle_tracks.vtt_path),
			ocr_status=excluded.ocr_status`,
		t.FileID, t.TrackIndex, t.Language, t.Title, t.Format, t.Source,
		boolToInt(t.Forced), t.VTTPath, t.OCRStatus)
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.QueryRowContext(ctx,
		`SELECT id FROM subtitle_tracks WHERE file_id = ? AND track_index = ?`,
		t.FileID, t.TrackIndex).Scan(&id)
	return id, err
}

// SetSubtitleVTT records the generated WebVTT path and status for a track.
func (d *DB) SetSubtitleVTT(ctx context.Context, id int64, vttPath, status string) error {
	_, err := d.ExecContext(ctx,
		`UPDATE subtitle_tracks SET vtt_path = ?, ocr_status = ? WHERE id = ?`,
		vttPath, status, id)
	return err
}

// GetSubtitleTrack loads a single track.
func (d *DB) GetSubtitleTrack(ctx context.Context, id int64) (*SubtitleTrack, error) {
	row := d.QueryRowContext(ctx, `
		SELECT id, file_id, track_index, language, title, format, source, forced, COALESCE(vtt_path,''), ocr_status
		FROM subtitle_tracks WHERE id = ?`, id)
	return scanSubtitle(row)
}

// ListSubtitleTracks returns all tracks for a media file.
func (d *DB) ListSubtitleTracks(ctx context.Context, fileID int64) ([]SubtitleTrack, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT id, file_id, track_index, language, title, format, source, forced, COALESCE(vtt_path,''), ocr_status
		FROM subtitle_tracks WHERE file_id = ? ORDER BY track_index`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubtitleTrack
	for rows.Next() {
		var t SubtitleTrack
		var forced int
		if err := rows.Scan(&t.ID, &t.FileID, &t.TrackIndex, &t.Language, &t.Title,
			&t.Format, &t.Source, &forced, &t.VTTPath, &t.OCRStatus); err != nil {
			return nil, err
		}
		t.Forced = forced == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

// PendingOCRTracks returns tracks queued for OCR (used to resume after restart).
func (d *DB) PendingOCRTracks(ctx context.Context) ([]SubtitleTrack, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT id, file_id, track_index, language, title, format, source, forced, COALESCE(vtt_path,''), ocr_status
		FROM subtitle_tracks WHERE ocr_status = 'queued' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubtitleTrack
	for rows.Next() {
		var t SubtitleTrack
		var forced int
		if err := rows.Scan(&t.ID, &t.FileID, &t.TrackIndex, &t.Language, &t.Title,
			&t.Format, &t.Source, &forced, &t.VTTPath, &t.OCRStatus); err != nil {
			return nil, err
		}
		t.Forced = forced == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanSubtitle(row *sql.Row) (*SubtitleTrack, error) {
	var t SubtitleTrack
	var forced int
	err := row.Scan(&t.ID, &t.FileID, &t.TrackIndex, &t.Language, &t.Title,
		&t.Format, &t.Source, &forced, &t.VTTPath, &t.OCRStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Forced = forced == 1
	return &t, nil
}
