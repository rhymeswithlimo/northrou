package db

import (
	"context"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// NeedsScan reports whether the file at path needs (re)scanning based on its
// size and modification time versus the recorded scan state. Unknown files
// always need scanning.
func (d *DB) NeedsScan(ctx context.Context, path string, size int64, modTime time.Time) (bool, error) {
	var recSize int64
	var recMod time.Time
	err := d.QueryRowContext(ctx,
		`SELECT size_bytes, mod_time FROM scan_state WHERE path = ?`, path).Scan(&recSize, &recMod)
	if err != nil {
		return true, nil //nolint:nilerr // absent or error => rescan
	}
	if recSize != size || !recMod.Equal(modTime) {
		return true, nil
	}
	return false, nil
}

// MarkScanned records that a file has been scanned at its current size/mtime.
func (d *DB) MarkScanned(ctx context.Context, path string, size int64, modTime time.Time) error {
	_, err := d.ExecContext(ctx, `
		INSERT INTO scan_state (path, size_bytes, mod_time, scanned_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET size_bytes=excluded.size_bytes,
			mod_time=excluded.mod_time, scanned_at=excluded.scanned_at`,
		path, size, modTime, time.Now())
	return err
}

// ClearScanStateForPrefix forgets the scan state of every file whose path begins
// with prefix, so they are re-evaluated on the next scan. Used when a file was
// deleted, so a duplicate copy beside it can be promoted to the linked one. The
// caller passes a separator-terminated directory prefix. A substring match (not
// LIKE) avoids `_`/`%` in real paths acting as wildcards.
func (d *DB) ClearScanStateForPrefix(ctx context.Context, prefix string) error {
	_, err := d.ExecContext(ctx,
		`DELETE FROM scan_state WHERE substr(path, 1, ?) = ?`, len(prefix), prefix)
	return err
}

// InsertUnmatched records a file the scanner could not confidently match.
func (d *DB) InsertUnmatched(ctx context.Context, u *model.UnmatchedFile) error {
	_, err := d.ExecContext(ctx, `
		INSERT INTO unmatched_files (path, kind, reason, parsed_title, parsed_year)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET reason=excluded.reason,
			parsed_title=excluded.parsed_title, parsed_year=excluded.parsed_year`,
		u.Path, string(u.Kind), u.Reason, u.ParsedTitle, u.ParsedYear)
	return err
}

// DeleteUnmatched removes a path from the unmatched list (e.g. after a manual
// or successful match).
func (d *DB) DeleteUnmatched(ctx context.Context, path string) error {
	_, err := d.ExecContext(ctx, `DELETE FROM unmatched_files WHERE path = ?`, path)
	return err
}

// ListUnmatched returns all files awaiting manual correction.
func (d *DB) ListUnmatched(ctx context.Context) ([]model.UnmatchedFile, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT id, path, kind, reason, parsed_title, parsed_year, found_at
		FROM unmatched_files ORDER BY found_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UnmatchedFile
	for rows.Next() {
		var u model.UnmatchedFile
		var kind string
		if err := rows.Scan(&u.ID, &u.Path, &kind, &u.Reason, &u.ParsedTitle, &u.ParsedYear, &u.FoundAt); err != nil {
			return nil, err
		}
		u.Kind = model.MediaKind(kind)
		out = append(out, u)
	}
	return out, rows.Err()
}
