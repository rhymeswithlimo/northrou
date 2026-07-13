package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// ErrNotFound is returned by query helpers when no row matches.
var ErrNotFound = errors.New("not found")

// GetAccount returns the singleton account (the household's auth-root email),
// or ErrNotFound if setup has not run yet.
func (d *DB) GetAccount(ctx context.Context) (*model.Account, error) {
	var a model.Account
	err := d.QueryRowContext(ctx,
		`SELECT email, created_at FROM account WHERE id = 1`).Scan(&a.Email, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AccountExists reports whether first-run setup has established the account.
func (d *DB) AccountExists(ctx context.Context) (bool, error) {
	var n int
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM account WHERE id = 1`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetAccountEmail creates (or replaces) the singleton account with the given
// already-normalized email. Used by first-run setup.
func (d *DB) SetAccountEmail(ctx context.Context, email string) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO account (id, email) VALUES (1, ?)
		   ON CONFLICT(id) DO UPDATE SET email = excluded.email`,
		email)
	return err
}
