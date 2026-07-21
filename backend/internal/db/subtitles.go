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
	ExtPath    string `json:"-"`     // source file path for external subs; "" for embedded
	Language   string `json:"language"`
	Title      string `json:"title"`
	Format     string `json:"format"` // subrip|ass|pgs|vobsub|...
	Source     string `json:"source"` // embedded|external
	Forced     bool   `json:"forced"`
	SDH        bool   `json:"sdh"`
	VTTPath    string `json:"-"`          // local path, served via the API
	OCRStatus  string `json:"ocr_status"` // none|queued|processing|done|failed|skipped
}

const subtitleCols = `id, file_id, track_index, COALESCE(ext_path,''), language, title, format, source, forced, sdh, COALESCE(vtt_path,''), ocr_status`

// UpsertSubtitleTrack inserts or updates a subtitle track. Embedded tracks are
// keyed by (file, index); external tracks by (file, ext_path).
func (d *DB) UpsertSubtitleTrack(ctx context.Context, t *SubtitleTrack) (int64, error) {
	_, err := d.ExecContext(ctx, `
		INSERT INTO subtitle_tracks (file_id, track_index, ext_path, language, title, format, source, forced, sdh, vtt_path, ocr_status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_id, track_index, ext_path) DO UPDATE SET
			language=excluded.language, title=excluded.title, format=excluded.format,
			source=excluded.source, forced=excluded.forced, sdh=excluded.sdh,
			vtt_path=COALESCE(NULLIF(excluded.vtt_path,''), subtitle_tracks.vtt_path),
			ocr_status=excluded.ocr_status`,
		t.FileID, t.TrackIndex, t.ExtPath, t.Language, t.Title, t.Format, t.Source,
		boolToInt(t.Forced), boolToInt(t.SDH), t.VTTPath, t.OCRStatus)
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.QueryRowContext(ctx,
		`SELECT id FROM subtitle_tracks WHERE file_id = ? AND track_index = ? AND ext_path = ?`,
		t.FileID, t.TrackIndex, t.ExtPath).Scan(&id)
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
	row := d.QueryRowContext(ctx,
		`SELECT `+subtitleCols+` FROM subtitle_tracks WHERE id = ?`, id)
	return scanSubtitleRow(row)
}

// GetExternalSubtitle returns the existing track for an external sidecar path, or
// ErrNotFound. Lets the scanner skip re-converting a sidecar already processed.
func (d *DB) GetExternalSubtitle(ctx context.Context, fileID int64, extPath string) (*SubtitleTrack, error) {
	row := d.QueryRowContext(ctx,
		`SELECT `+subtitleCols+` FROM subtitle_tracks WHERE file_id = ? AND ext_path = ?`,
		fileID, extPath)
	return scanSubtitleRow(row)
}

// ListSubtitleTracks returns all tracks for a media file.
func (d *DB) ListSubtitleTracks(ctx context.Context, fileID int64) ([]SubtitleTrack, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT `+subtitleCols+` FROM subtitle_tracks WHERE file_id = ? ORDER BY source, track_index, ext_path`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubtitleRows(rows)
}

// PendingOCRTracks returns tracks queued for OCR (used to resume after restart).
func (d *DB) PendingOCRTracks(ctx context.Context) ([]SubtitleTrack, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT `+subtitleCols+` FROM subtitle_tracks WHERE ocr_status = 'queued' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubtitleRows(rows)
}

func scanSubtitleInto(sc interface{ Scan(...any) error }, t *SubtitleTrack) error {
	var forced, sdh int
	if err := sc.Scan(&t.ID, &t.FileID, &t.TrackIndex, &t.ExtPath, &t.Language, &t.Title,
		&t.Format, &t.Source, &forced, &sdh, &t.VTTPath, &t.OCRStatus); err != nil {
		return err
	}
	t.Forced = forced == 1
	t.SDH = sdh == 1
	return nil
}

func scanSubtitleRow(row *sql.Row) (*SubtitleTrack, error) {
	var t SubtitleTrack
	err := scanSubtitleInto(row, &t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanSubtitleRows(rows *sql.Rows) ([]SubtitleTrack, error) {
	var out []SubtitleTrack
	for rows.Next() {
		var t SubtitleTrack
		if err := scanSubtitleInto(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
