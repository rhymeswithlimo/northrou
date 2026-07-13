package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// StoredToken is a persisted refresh token record (hash only, never the raw
// token). ProfileID records which profile the device is currently using, so a
// refresh mints an access token for the same profile without another pin.
type StoredToken struct {
	ID        int64
	ProfileID int64
	ExpiresAt time.Time
	Revoked   bool
}

// InsertRefreshToken stores the hash of a refresh token bound to a profile.
func (d *DB) InsertRefreshToken(ctx context.Context, profileID int64, tokenHash string, expiresAt time.Time) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO refresh_tokens (profile_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		profileID, tokenHash, expiresAt)
	return err
}

// GetRefreshToken looks up a refresh token by its hash.
func (d *DB) GetRefreshToken(ctx context.Context, tokenHash string) (*StoredToken, error) {
	row := d.QueryRowContext(ctx,
		`SELECT id, profile_id, expires_at, revoked FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash)
	var t StoredToken
	var revoked int
	var profileID sql.NullInt64
	err := row.Scan(&t.ID, &profileID, &t.ExpiresAt, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.ProfileID = profileID.Int64
	t.Revoked = revoked == 1
	return &t, nil
}

// RevokeRefreshToken marks a token hash as revoked (used on rotation/logout).
func (d *DB) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := d.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked = 1 WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteExpiredTokens purges expired/revoked tokens; call periodically.
func (d *DB) DeleteExpiredTokens(ctx context.Context) error {
	_, err := d.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at < ? OR revoked = 1`, time.Now())
	return err
}
