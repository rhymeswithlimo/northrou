-- Recommendation persistence + lifecycle. Home rows used to be regenerated per
-- request behind a short cache, so nothing about a row survived across sessions:
-- no memory of what was shown, whether it was ever engaged with, or how often.
-- These tables give the engine that memory, enabling (1) per-item fatigue
-- suppression and (2) collection-level lifecycle (engagement-based ordering and
-- safe retirement of rows the household consistently ignores).

-- +goose Up

-- item_impressions: how often a title has been shown to a profile, and when
-- last. Feeds the fatigue penalty (down-weight a title shown many times that
-- the profile still hasn't played). Global per (profile,title), not per row.
CREATE TABLE item_impressions (
    user_id      INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,   -- 'movie' | 'show'
    item_id      INTEGER NOT NULL,
    served_count INTEGER NOT NULL DEFAULT 0,
    last_shown   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, kind, item_id)
);

-- home_collections: one row per (profile, row key) actually served, with its
-- served/click counters, membership (the item ids it last contained, as a JSON
-- array, used to attribute a later watch back to the rows that surfaced it), and
-- lifecycle state. 'dormant' rows are suppressed until dormant_until passes,
-- then revived for another chance; nothing is deleted here.
CREATE TABLE home_collections (
    user_id       INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    title         TEXT NOT NULL,
    strategy      TEXT NOT NULL DEFAULT '',
    item_ids      TEXT NOT NULL DEFAULT '[]', -- JSON array of movie ids last served
    served_count  INTEGER NOT NULL DEFAULT 0,
    click_count   INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_shown    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    state         TEXT NOT NULL DEFAULT 'active', -- 'active' | 'dormant'
    dormant_until TIMESTAMP,
    PRIMARY KEY (user_id, key)
);
CREATE INDEX idx_home_collections_user ON home_collections(user_id);

-- +goose Down
DROP INDEX idx_home_collections_user;
DROP TABLE home_collections;
DROP TABLE item_impressions;
