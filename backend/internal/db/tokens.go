package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// StoredToken is a persisted refresh token record (hash only, never the raw
// token).
type StoredToken struct {
	ID        int64
	UserID    int64
	ExpiresAt time.Time
	Revoked   bool
}

// InsertRefreshToken stores the hash of a refresh token for a user.
func (d *DB) InsertRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		userID, tokenHash, expiresAt)
	return err
}

// GetRefreshToken looks up a refresh token by its hash.
func (d *DB) GetRefreshToken(ctx context.Context, tokenHash string) (*StoredToken, error) {
	row := d.QueryRowContext(ctx,
		`SELECT id, user_id, expires_at, revoked FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash)
	var t StoredToken
	var revoked int
	err := row.Scan(&t.ID, &t.UserID, &t.ExpiresAt, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
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
