package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PinPurpose selects which account-level one-time code is meant: sign-in versus
// admin elevation. At most one pin of each purpose is active at a time.
type PinPurpose string

const (
	PinLogin PinPurpose = "login"
	PinAdmin PinPurpose = "admin"
)

// AuthPin is a stored, hashed one-time code for the account.
type AuthPin struct {
	Purpose   PinPurpose
	Hash      string
	ExpiresAt time.Time
	Attempts  int
	CreatedAt time.Time
}

// ReplacePin upserts the single active pin for a purpose, so issuing a new code
// invalidates any prior one of the same purpose.
func (d *DB) ReplacePin(ctx context.Context, purpose PinPurpose, pinHash string, expiresAt time.Time) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO auth_pins (purpose, pin_hash, expires_at, attempts) VALUES (?, ?, ?, 0)
		   ON CONFLICT(purpose) DO UPDATE SET
		     pin_hash = excluded.pin_hash,
		     expires_at = excluded.expires_at,
		     attempts = 0,
		     created_at = CURRENT_TIMESTAMP`,
		string(purpose), pinHash, expiresAt)
	return err
}

// GetPin returns the active pin for a purpose, or ErrNotFound if none exists.
func (d *DB) GetPin(ctx context.Context, purpose PinPurpose) (*AuthPin, error) {
	var p AuthPin
	err := d.QueryRowContext(ctx,
		`SELECT purpose, pin_hash, expires_at, attempts, created_at
		   FROM auth_pins WHERE purpose = ?`, string(purpose)).
		Scan(&p.Purpose, &p.Hash, &p.ExpiresAt, &p.Attempts, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// IncrementPinAttempts bumps the failed-attempt counter for a purpose's pin.
func (d *DB) IncrementPinAttempts(ctx context.Context, purpose PinPurpose) error {
	_, err := d.ExecContext(ctx,
		`UPDATE auth_pins SET attempts = attempts + 1 WHERE purpose = ?`, string(purpose))
	return err
}

// DeletePin removes a purpose's pin (after a successful or exhausted use).
func (d *DB) DeletePin(ctx context.Context, purpose PinPurpose) error {
	_, err := d.ExecContext(ctx, `DELETE FROM auth_pins WHERE purpose = ?`, string(purpose))
	return err
}
