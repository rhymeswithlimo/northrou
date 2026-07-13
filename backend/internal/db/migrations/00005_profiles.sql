-- +goose NO TRANSACTION
-- Split the old per-user account model into three ideas:
--   account  - the single email that is the authentication root (singleton)
--   profiles - Netflix-style viewers (name + avatar), no email, no admin flag
--   admin    - no longer an identity attribute; it is an OTP-proven capability
-- Per-profile data (watch_history, taste_profile, affinities, home_row_state)
-- keeps its row ids and foreign keys; those end up referencing profiles(id).
--
-- This runs without a transaction because it toggles foreign_keys (a no-op
-- inside a transaction) to rebuild the users table: the old email column
-- carries a UNIQUE index that DROP COLUMN cannot remove, so the table is
-- recreated rather than altered in place.

-- +goose Up
PRAGMA foreign_keys = OFF;

-- The account: exactly one row (id pinned to 1). Seed its email from the former
-- admin user so an upgraded database keeps working; a fresh database has no
-- users yet and first-run setup populates this instead.
CREATE TABLE account (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    email      TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO account (id, email)
    SELECT 1, email FROM users WHERE is_admin = 1 LIMIT 1;

-- Repoint every per-profile foreign key from "users" to "profiles" by renaming
-- the table (SQLite rewrites child references), then rebuild it with the new
-- shape (dropping the UNIQUE email column requires a rebuild, not ALTER).
ALTER TABLE users RENAME TO profiles;
CREATE TABLE profiles_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL DEFAULT 'Me',
    avatar     TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO profiles_new (id, name, avatar, created_at)
    SELECT id,
           CASE WHEN instr(email, '@') > 1
                THEN substr(email, 1, instr(email, '@') - 1)
                ELSE 'Me' END,
           NULL,
           created_at
    FROM profiles;
DROP TABLE profiles;
ALTER TABLE profiles_new RENAME TO profiles;

-- A refresh token belongs to the profile a device is signed in as; rename the
-- column to say so (its NOT NULL and its index follow the rename).
ALTER TABLE refresh_tokens RENAME COLUMN user_id TO profile_id;

-- Account-level one-time codes, at most one active per purpose. 'login' proves
-- control of the account email (device sign-in); 'admin' elevates a session to
-- perform admin mutations. Replaces the per-user login_pins table.
DROP TABLE login_pins;
CREATE TABLE auth_pins (
    purpose    TEXT PRIMARY KEY CHECK (purpose IN ('login','admin')),
    pin_hash   TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;

DROP TABLE auth_pins;
CREATE TABLE login_pins (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pin_hash   TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE refresh_tokens RENAME COLUMN profile_id TO user_id;

-- Rebuild users from profiles with the old identity columns restored.
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    email         TEXT NOT NULL UNIQUE,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO users (id, email, is_admin, created_at)
    SELECT id,
           COALESCE((SELECT email FROM account WHERE id = 1), '') ||
               CASE WHEN id = (SELECT MIN(id) FROM profiles) THEN '' ELSE '+' || id END,
           CASE WHEN id = (SELECT MIN(id) FROM profiles) THEN 1 ELSE 0 END,
           created_at
    FROM profiles;
DROP TABLE profiles;
CREATE INDEX idx_login_pins_user ON login_pins(user_id);

DROP TABLE account;

PRAGMA foreign_keys = ON;
