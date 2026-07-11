-- +goose Up
-- Switch authentication from username+password to email + one-time pins.
-- The users table is altered in place (ids and inbound foreign keys are
-- preserved); the UNIQUE index on the account identifier follows the rename.
ALTER TABLE users RENAME COLUMN username TO email;
ALTER TABLE users DROP COLUMN password_hash;

-- login_pins: short-lived, single-use sign-in codes stored hashed. Issuing a
-- new pin deletes any prior pins for that user, so at most one is ever active.
CREATE TABLE login_pins (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pin_hash   TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_login_pins_user ON login_pins(user_id);

-- +goose Down
DROP TABLE login_pins;
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users RENAME COLUMN email TO username;
