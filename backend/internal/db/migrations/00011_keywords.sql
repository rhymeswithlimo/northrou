-- TMDB keywords: the richest free thematic/tonal signal (user-contributed tags
-- like 'small-town', 'grief', 'slow-burn'). Stored by name and upserted, exactly
-- like genres, so the recommendation engine can build keyword co-occurrence
-- vectors. Raw names are kept here; normalization (lowercase/alias) happens at
-- embedding-build time so the alias map can evolve without a re-scan.

-- +goose Up
CREATE TABLE keywords (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);
CREATE TABLE movie_keywords (
    movie_id   INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    keyword_id INTEGER NOT NULL REFERENCES keywords(id) ON DELETE CASCADE,
    PRIMARY KEY (movie_id, keyword_id)
);
CREATE TABLE show_keywords (
    show_id    INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    keyword_id INTEGER NOT NULL REFERENCES keywords(id) ON DELETE CASCADE,
    PRIMARY KEY (show_id, keyword_id)
);
-- Reverse-lookup indexes (keyword -> titles) for co-occurrence/vector builds.
CREATE INDEX idx_movie_keywords_kw ON movie_keywords(keyword_id);
CREATE INDEX idx_show_keywords_kw ON show_keywords(keyword_id);

-- +goose Down
DROP INDEX idx_show_keywords_kw;
DROP INDEX idx_movie_keywords_kw;
DROP TABLE show_keywords;
DROP TABLE movie_keywords;
DROP TABLE keywords;
