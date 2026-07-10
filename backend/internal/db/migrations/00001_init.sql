-- +goose Up
-- Core schema for Northrou: accounts, libraries, media, metadata, watch
-- history, subtitles, taste profile, and scan bookkeeping.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE refresh_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMP NOT NULL,
    revoked    INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);

CREATE TABLE libraries (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('movie','show')),
    path TEXT NOT NULL UNIQUE
);

-- media_files: one row per physical file. Technical metadata (codecs, HDR,
-- track lists) is authoritative from ffprobe and stored as JSON blobs.
CREATE TABLE media_files (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT NOT NULL UNIQUE,
    size_bytes INTEGER NOT NULL,
    mod_time   TIMESTAMP NOT NULL,
    container  TEXT,
    duration   REAL,
    video_json TEXT,   -- VideoStream
    audio_json TEXT,   -- []AudioStream
    subs_json  TEXT,   -- []SubtitleStream
    hdr_type   TEXT,
    width      INTEGER,
    height     INTEGER,
    probed_at  TIMESTAMP
);

CREATE TABLE collections (
    id      INTEGER PRIMARY KEY,          -- TMDB collection id
    name    TEXT NOT NULL,
    poster  TEXT,
    backdrop TEXT
);

CREATE TABLE movies (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id       INTEGER UNIQUE,
    title         TEXT NOT NULL,
    year          INTEGER,
    overview      TEXT,
    runtime       INTEGER,
    original_lang TEXT,
    collection_id INTEGER REFERENCES collections(id),
    poster_path   TEXT,
    backdrop_path TEXT,
    file_id       INTEGER REFERENCES media_files(id) ON DELETE SET NULL,
    added_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_movies_collection ON movies(collection_id);

CREATE TABLE shows (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id       INTEGER UNIQUE,
    title         TEXT NOT NULL,
    year          INTEGER,
    overview      TEXT,
    original_lang TEXT,
    poster_path   TEXT,
    backdrop_path TEXT,
    added_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE seasons (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    show_id INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    number  INTEGER NOT NULL,
    UNIQUE (show_id, number)
);

CREATE TABLE episodes (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    show_id   INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    season_id INTEGER NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
    season    INTEGER NOT NULL,
    number    INTEGER NOT NULL,
    title     TEXT,
    overview  TEXT,
    runtime   INTEGER,
    file_id   INTEGER REFERENCES media_files(id) ON DELETE SET NULL,
    UNIQUE (show_id, season, number)
);
CREATE INDEX idx_episodes_show ON episodes(show_id);

CREATE TABLE genres (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);
CREATE TABLE movie_genres (
    movie_id INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    genre_id INTEGER NOT NULL REFERENCES genres(id) ON DELETE CASCADE,
    PRIMARY KEY (movie_id, genre_id)
);
CREATE TABLE show_genres (
    show_id  INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    genre_id INTEGER NOT NULL REFERENCES genres(id) ON DELETE CASCADE,
    PRIMARY KEY (show_id, genre_id)
);

CREATE TABLE people (
    id   INTEGER PRIMARY KEY,   -- TMDB person id
    name TEXT NOT NULL
);
-- credits link people to movies/shows; media_kind selects which table.
CREATE TABLE credits (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id  INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    media_kind TEXT NOT NULL CHECK (media_kind IN ('movie','show')),
    media_id   INTEGER NOT NULL,
    department TEXT NOT NULL,   -- 'cast' or crew department
    role       TEXT,            -- character or job
    ord        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_credits_media ON credits(media_kind, media_id);
CREATE INDEX idx_credits_person ON credits(person_id);

-- watch_history: one row per (user, item). media_kind is 'movie' or 'episode'.
CREATE TABLE watch_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_kind    TEXT NOT NULL CHECK (media_kind IN ('movie','episode')),
    media_id      INTEGER NOT NULL,
    position_sec  REAL NOT NULL DEFAULT 0,
    duration_sec  REAL NOT NULL DEFAULT 0,
    completed     INTEGER NOT NULL DEFAULT 0,
    rewatch_count INTEGER NOT NULL DEFAULT 0,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, media_kind, media_id)
);
CREATE INDEX idx_watch_user ON watch_history(user_id);

-- subtitle_tracks: extracted/converted subtitle assets served as WebVTT.
CREATE TABLE subtitle_tracks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id    INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    track_index INTEGER NOT NULL,
    language   TEXT,
    title      TEXT,
    format     TEXT NOT NULL,   -- source format: 'subrip','ass','pgs',...
    source     TEXT NOT NULL,   -- 'embedded' or 'external'
    forced     INTEGER NOT NULL DEFAULT 0,
    vtt_path   TEXT,            -- generated WebVTT path (null until ready)
    ocr_status TEXT NOT NULL DEFAULT 'none', -- none|queued|processing|done|failed|skipped
    UNIQUE (file_id, track_index)
);
CREATE INDEX idx_subs_file ON subtitle_tracks(file_id);

-- unmatched_files: scanned files needing manual correction.
CREATE TABLE unmatched_files (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    path         TEXT NOT NULL UNIQUE,
    kind         TEXT NOT NULL,
    reason       TEXT,
    parsed_title TEXT,
    parsed_year  INTEGER,
    found_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- taste_profile: a single per-user profile row plus keyed affinity rows.
CREATE TABLE taste_profile (
    user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    rewatch_tendency REAL NOT NULL DEFAULT 0,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- affinities: generic (dimension,key)->score store with time-decay bookkeeping.
-- dimension in ('genre','decade','director','actor','language','runtime','hour').
CREATE TABLE affinities (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    dimension  TEXT NOT NULL,
    key        TEXT NOT NULL,
    score      REAL NOT NULL DEFAULT 0,
    weight     REAL NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, dimension, key)
);

-- home_row_state: tracks recently-shown rows so the home screen can rotate.
CREATE TABLE home_row_state (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    row_key   TEXT NOT NULL,
    last_shown TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, row_key)
);

-- scan_state: per-file bookkeeping for incremental scans.
CREATE TABLE scan_state (
    path       TEXT PRIMARY KEY,
    size_bytes INTEGER NOT NULL,
    mod_time   TIMESTAMP NOT NULL,
    scanned_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE scan_state;
DROP TABLE home_row_state;
DROP TABLE affinities;
DROP TABLE taste_profile;
DROP TABLE unmatched_files;
DROP TABLE subtitle_tracks;
DROP TABLE watch_history;
DROP TABLE credits;
DROP TABLE people;
DROP TABLE show_genres;
DROP TABLE movie_genres;
DROP TABLE genres;
DROP TABLE episodes;
DROP TABLE seasons;
DROP TABLE shows;
DROP TABLE movies;
DROP TABLE collections;
DROP TABLE media_files;
DROP TABLE libraries;
DROP TABLE refresh_tokens;
DROP TABLE users;
