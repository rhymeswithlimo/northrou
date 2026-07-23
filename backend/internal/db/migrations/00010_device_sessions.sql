-- Device metadata on refresh tokens, so the operator can see and revoke the
-- devices paired with this server. A device's token rotates on every refresh
-- (a new row each time), so rows carry a stable device_id minted at pair time
-- and inherited across rotations; grouping by it yields one entry per device.
-- Rows from before this migration have an empty device_id and stand alone.

-- +goose Up
ALTER TABLE refresh_tokens ADD COLUMN device_id TEXT NOT NULL DEFAULT '';
ALTER TABLE refresh_tokens ADD COLUMN device_name TEXT NOT NULL DEFAULT '';
ALTER TABLE refresh_tokens ADD COLUMN last_used_at TIMESTAMP;
CREATE INDEX idx_refresh_tokens_device ON refresh_tokens(device_id);

-- +goose Down
DROP INDEX idx_refresh_tokens_device;
ALTER TABLE refresh_tokens DROP COLUMN last_used_at;
ALTER TABLE refresh_tokens DROP COLUMN device_name;
ALTER TABLE refresh_tokens DROP COLUMN device_id;
