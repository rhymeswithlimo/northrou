package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// ErrNotFound is returned by query helpers when no row matches.
var ErrNotFound = errors.New("not found")

// CreateUser inserts a new account keyed by email and returns its id. Callers
// are expected to pass an already-normalized (lower-cased, trimmed) address.
func (d *DB) CreateUser(ctx context.Context, email string, isAdmin bool) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO users (email, is_admin) VALUES (?, ?)`,
		email, boolToInt(isAdmin))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetUserByEmail looks up a user by email address.
func (d *DB) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	row := d.QueryRowContext(ctx,
		`SELECT id, email, is_admin, created_at FROM users WHERE email = ?`,
		email)
	return scanUser(row)
}

// GetUser looks up a user by id.
func (d *DB) GetUser(ctx context.Context, id int64) (*model.User, error) {
	row := d.QueryRowContext(ctx,
		`SELECT id, email, is_admin, created_at FROM users WHERE id = ?`,
		id)
	return scanUser(row)
}

// CountUsers returns the number of accounts (used for first-run detection).
func (d *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func scanUser(row *sql.Row) (*model.User, error) {
	var u model.User
	var admin int
	err := row.Scan(&u.ID, &u.Email, &admin, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.IsAdmin = admin == 1
	return &u, nil
}

// LoginPin is a stored, hashed one-time sign-in code.
type LoginPin struct {
	ID        int64
	UserID    int64
	Hash      string
	ExpiresAt time.Time
	Attempts  int
	CreatedAt time.Time
}

// ReplaceLoginPin deletes any existing pins for the user and inserts a fresh
// one, so at most one pin is ever active per account.
func (d *DB) ReplaceLoginPin(ctx context.Context, userID int64, pinHash string, expiresAt time.Time) error {
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM login_pins WHERE user_id = ?`, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO login_pins (user_id, pin_hash, expires_at) VALUES (?, ?, ?)`,
			userID, pinHash, expiresAt)
		return err
	})
}

// GetLoginPin returns the active pin for a user, or ErrNotFound if none exists.
func (d *DB) GetLoginPin(ctx context.Context, userID int64) (*LoginPin, error) {
	row := d.QueryRowContext(ctx,
		`SELECT id, user_id, pin_hash, expires_at, attempts, created_at
		   FROM login_pins WHERE user_id = ?`, userID)
	var p LoginPin
	err := row.Scan(&p.ID, &p.UserID, &p.Hash, &p.ExpiresAt, &p.Attempts, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// IncrementPinAttempts bumps the failed-attempt counter for a pin.
func (d *DB) IncrementPinAttempts(ctx context.Context, id int64) error {
	_, err := d.ExecContext(ctx, `UPDATE login_pins SET attempts = attempts + 1 WHERE id = ?`, id)
	return err
}

// DeleteLoginPin removes a pin (called after a successful, or exhausted, use).
func (d *DB) DeleteLoginPin(ctx context.Context, id int64) error {
	_, err := d.ExecContext(ctx, `DELETE FROM login_pins WHERE id = ?`, id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
