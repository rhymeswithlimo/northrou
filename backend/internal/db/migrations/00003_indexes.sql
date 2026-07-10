-- +goose Up
-- Browse and home queries sort the library by recency (ORDER BY added_at DESC).
-- Without these indexes SQLite full-scans and sorts the whole table every time,
-- which hurts most on a weak box with a large library.
CREATE INDEX IF NOT EXISTS idx_movies_added_at ON movies(added_at DESC);
CREATE INDEX IF NOT EXISTS idx_shows_added_at  ON shows(added_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_shows_added_at;
DROP INDEX IF EXISTS idx_movies_added_at;
