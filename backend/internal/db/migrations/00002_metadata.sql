-- +goose Up
-- Additional TMDB metadata used to build richer cold-start category rows
-- (blockbusters by revenue, critically-acclaimed by rating, US/foreign by
-- country/language).
ALTER TABLE movies ADD COLUMN vote_average REAL    NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN vote_count   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN popularity   REAL    NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN revenue      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN country      TEXT    NOT NULL DEFAULT '';

ALTER TABLE shows ADD COLUMN vote_average REAL NOT NULL DEFAULT 0;
ALTER TABLE shows ADD COLUMN popularity   REAL NOT NULL DEFAULT 0;
ALTER TABLE shows ADD COLUMN country      TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE shows  DROP COLUMN country;
ALTER TABLE shows  DROP COLUMN popularity;
ALTER TABLE shows  DROP COLUMN vote_average;
ALTER TABLE movies DROP COLUMN country;
ALTER TABLE movies DROP COLUMN revenue;
ALTER TABLE movies DROP COLUMN popularity;
ALTER TABLE movies DROP COLUMN vote_count;
ALTER TABLE movies DROP COLUMN vote_average;
