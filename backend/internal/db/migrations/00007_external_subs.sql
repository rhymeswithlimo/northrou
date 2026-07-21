-- +goose Up
-- External (sidecar) subtitles: a subtitle_tracks row can now come from a file
-- on disk next to the video, not only an embedded container stream. track_index
-- is meaningless for those, so uniqueness widens to include the external path,
-- and we record the SDH/hearing-impaired flag alongside the existing forced one.
-- SQLite cannot alter a UNIQUE constraint in place, so rebuild the table.

CREATE TABLE subtitle_tracks_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id     INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    track_index INTEGER NOT NULL,          -- embedded stream index; 0 for external
    ext_path    TEXT NOT NULL DEFAULT '',  -- source file path for external subs, '' for embedded
    language    TEXT,
    title       TEXT,
    format      TEXT NOT NULL,             -- 'subrip','ass','pgs','vobsub',...
    source      TEXT NOT NULL,             -- 'embedded' or 'external'
    forced      INTEGER NOT NULL DEFAULT 0,
    sdh         INTEGER NOT NULL DEFAULT 0, -- hearing-impaired / SDH track
    vtt_path    TEXT,
    ocr_status  TEXT NOT NULL DEFAULT 'none',
    UNIQUE (file_id, track_index, ext_path)
);

INSERT INTO subtitle_tracks_new
    (id, file_id, track_index, ext_path, language, title, format, source, forced, sdh, vtt_path, ocr_status)
    SELECT id, file_id, track_index, '', language, title, format, source, forced, 0, vtt_path, ocr_status
    FROM subtitle_tracks;

DROP TABLE subtitle_tracks;
ALTER TABLE subtitle_tracks_new RENAME TO subtitle_tracks;
CREATE INDEX idx_subs_file ON subtitle_tracks(file_id);

-- +goose Down
CREATE TABLE subtitle_tracks_old (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id     INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    track_index INTEGER NOT NULL,
    language    TEXT,
    title       TEXT,
    format      TEXT NOT NULL,
    source      TEXT NOT NULL,
    forced      INTEGER NOT NULL DEFAULT 0,
    vtt_path    TEXT,
    ocr_status  TEXT NOT NULL DEFAULT 'none',
    UNIQUE (file_id, track_index)
);

-- Keep only embedded tracks; external ones have no place in the old schema.
INSERT INTO subtitle_tracks_old
    (id, file_id, track_index, language, title, format, source, forced, vtt_path, ocr_status)
    SELECT id, file_id, track_index, language, title, format, source, forced, vtt_path, ocr_status
    FROM subtitle_tracks WHERE source = 'embedded';

DROP TABLE subtitle_tracks;
ALTER TABLE subtitle_tracks_old RENAME TO subtitle_tracks;
CREATE INDEX idx_subs_file ON subtitle_tracks(file_id);
