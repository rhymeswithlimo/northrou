package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// CreateProfile inserts a new viewer profile and returns its id. avatar may be
// empty. Callers should pass a trimmed, non-empty name.
func (d *DB) CreateProfile(ctx context.Context, name, avatar string) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO profiles (name, avatar) VALUES (?, ?)`,
		name, nullIfEmpty(avatar))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetProfile looks up a profile by id.
func (d *DB) GetProfile(ctx context.Context, id int64) (*model.Profile, error) {
	row := d.QueryRowContext(ctx,
		`SELECT id, name, avatar, created_at FROM profiles WHERE id = ?`, id)
	return scanProfile(row)
}

// ListProfiles returns all profiles, oldest first (creation order).
func (d *DB) ListProfiles(ctx context.Context) ([]model.Profile, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id, name, avatar, created_at FROM profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Profile
	for rows.Next() {
		var p model.Profile
		var avatar sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &avatar, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Avatar = avatar.String
		out = append(out, p)
	}
	return out, rows.Err()
}

// RenameProfile updates a profile's display name and avatar.
func (d *DB) RenameProfile(ctx context.Context, id int64, name, avatar string) error {
	res, err := d.ExecContext(ctx,
		`UPDATE profiles SET name = ?, avatar = ? WHERE id = ?`,
		name, nullIfEmpty(avatar), id)
	if err != nil {
		return err
	}
	return requireRow(res)
}

// DeleteProfile removes a profile and (via ON DELETE CASCADE) all of its
// per-viewer data. It refuses to delete the final profile so the account is
// never left with none.
func (d *DB) DeleteProfile(ctx context.Context, id int64) error {
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM profiles`).Scan(&n); err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastProfile
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id)
		if err != nil {
			return err
		}
		return requireRow(res)
	})
}

// CountProfiles returns how many profiles exist.
func (d *DB) CountProfiles(ctx context.Context) (int, error) {
	var n int
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM profiles`).Scan(&n)
	return n, err
}

// ErrLastProfile is returned when a delete would remove the only profile.
var ErrLastProfile = errors.New("cannot delete the last profile")

func scanProfile(row *sql.Row) (*model.Profile, error) {
	var p model.Profile
	var avatar sql.NullString
	err := row.Scan(&p.ID, &p.Name, &avatar, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Avatar = avatar.String
	return &p, nil
}

func requireRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
