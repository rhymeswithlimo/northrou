-- Remove the email/one-time-pin sign-in machinery. Authentication is now the
-- server connection code alone (verified in the API against config), so the
-- account no longer needs an email and there are no pins to store. The account
-- table survives purely as a "setup has completed" marker (a single id=1 row).

-- +goose Up
DROP TABLE auth_pins;
ALTER TABLE account DROP COLUMN email;

-- +goose Down
ALTER TABLE account ADD COLUMN email TEXT NOT NULL DEFAULT '';
CREATE TABLE auth_pins (
    purpose    TEXT PRIMARY KEY CHECK (purpose IN ('login','admin')),
    pin_hash   TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
