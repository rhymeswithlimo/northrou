-- Production companies (for "studio" browse rows like Marvel Studios /
-- DreamWorks) and TV creators (for "Created by …" show rows, the TV analog of
-- director rows). Companies are stored by name and upserted, mirroring genres
-- and keywords. Creators are stored by name per show (no shared vocab table -
-- a creator name only ever needs to group that show).

-- +goose Up
CREATE TABLE production_companies (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);
CREATE TABLE movie_companies (
    movie_id   INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    company_id INTEGER NOT NULL REFERENCES production_companies(id) ON DELETE CASCADE,
    PRIMARY KEY (movie_id, company_id)
);
CREATE TABLE show_companies (
    show_id    INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    company_id INTEGER NOT NULL REFERENCES production_companies(id) ON DELETE CASCADE,
    PRIMARY KEY (show_id, company_id)
);
CREATE TABLE show_creators (
    show_id INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    name    TEXT NOT NULL,
    PRIMARY KEY (show_id, name)
);
CREATE INDEX idx_movie_companies_co ON movie_companies(company_id);
CREATE INDEX idx_show_companies_co ON show_companies(company_id);

-- +goose Down
DROP INDEX idx_show_companies_co;
DROP INDEX idx_movie_companies_co;
DROP TABLE show_creators;
DROP TABLE show_companies;
DROP TABLE movie_companies;
DROP TABLE production_companies;
