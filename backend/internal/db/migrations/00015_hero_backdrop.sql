-- A dedicated high-resolution backdrop for the home-screen hero. The regular
-- backdrop_path stays at a modest size (w1280) because it feeds small surfaces -
-- Continue Watching thumbnails and the detail sheet - where a 4K image would be
-- wasted bandwidth (and, over the peer-to-peer tunnel, real latency). The hero
-- fills the whole screen, so it gets its own column cached at full resolution
-- (aiming for >=2560x1440). Existing rows stay NULL until a re-scan refetches
-- them; the DTO falls back to backdrop_path when this is empty.

-- +goose Up
ALTER TABLE movies ADD COLUMN hero_backdrop_path TEXT NOT NULL DEFAULT '';
ALTER TABLE shows ADD COLUMN hero_backdrop_path TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE shows DROP COLUMN hero_backdrop_path;
ALTER TABLE movies DROP COLUMN hero_backdrop_path;
