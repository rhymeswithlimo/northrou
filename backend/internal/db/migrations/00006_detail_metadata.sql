-- +goose Up
-- Metadata the title detail screen shows but the schema had nowhere to put:
-- taglines, content ratings, episode stills/air dates, and cast headshots.
--
-- tagline and vote_average come back on the ordinary TMDB detail call.
-- certification needs append_to_response=release_dates (movies) or
-- content_ratings (tv), and is stored already-resolved to a single country's
-- rating rather than as the full per-country payload; the client shows one badge.
ALTER TABLE movies ADD COLUMN tagline       TEXT NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN certification TEXT NOT NULL DEFAULT '';

ALTER TABLE shows ADD COLUMN tagline       TEXT NOT NULL DEFAULT '';
ALTER TABLE shows ADD COLUMN certification TEXT NOT NULL DEFAULT '';

-- still_path is a cached local path, like poster_path/backdrop_path, not a
-- TMDB path: the client must never talk to TMDB directly.
ALTER TABLE episodes ADD COLUMN still_path TEXT NOT NULL DEFAULT '';
ALTER TABLE episodes ADD COLUMN air_date   TEXT NOT NULL DEFAULT '';

-- Cast headshots, cached the same way.
ALTER TABLE people ADD COLUMN profile_path TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE people   DROP COLUMN profile_path;
ALTER TABLE episodes DROP COLUMN air_date;
ALTER TABLE episodes DROP COLUMN still_path;
ALTER TABLE shows    DROP COLUMN certification;
ALTER TABLE shows    DROP COLUMN tagline;
ALTER TABLE movies   DROP COLUMN certification;
ALTER TABLE movies   DROP COLUMN tagline;
