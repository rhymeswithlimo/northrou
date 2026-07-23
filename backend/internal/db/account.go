package db

import (
	"context"
	"errors"
)

// ErrNotFound is returned by query helpers when no row matches.
var ErrNotFound = errors.New("not found")

// AccountExists reports whether first-run setup has completed (the singleton
// account row exists). The account no longer carries an email or any credential;
// it is simply a marker that setup has run.
func (d *DB) AccountExists(ctx context.Context) (bool, error) {
	var n int
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM account WHERE id = 1`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateAccount marks first-run setup as complete by inserting the singleton
// account row. Idempotent.
func (d *DB) CreateAccount(ctx context.Context) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO account (id) VALUES (1) ON CONFLICT(id) DO NOTHING`)
	return err
}
