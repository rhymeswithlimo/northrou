-- Title logos: the TMDB "logo" image (the stylized wordmark/title treatment,
-- usually a transparent PNG). Shown in the detail header in place of the plain
-- text title when present. Cached locally like the poster/backdrop; the column
-- holds the same cache-relative path. Existing rows stay NULL until a re-scan
-- refetches them.

-- +goose Up
ALTER TABLE movies ADD COLUMN logo_path TEXT NOT NULL DEFAULT '';
ALTER TABLE shows ADD COLUMN logo_path TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE shows DROP COLUMN logo_path;
ALTER TABLE movies DROP COLUMN logo_path;
