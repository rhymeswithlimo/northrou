package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// ErrNotFound is returned by query helpers when no row matches.
var ErrNotFound = errors.New("not found")

// CreateUser inserts a new account and returns its id.
func (d *DB) CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)`,
		username, passwordHash, boolToInt(isAdmin))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetUserByUsername looks up a user by username.
func (d *DB) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	row := d.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE username = ?`,
		username)
	return scanUser(row)
}

// GetUser looks up a user by id.
func (d *DB) GetUser(ctx context.Context, id int64) (*model.User, error) {
	row := d.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE id = ?`,
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
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &admin, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.IsAdmin = admin == 1
	return &u, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
