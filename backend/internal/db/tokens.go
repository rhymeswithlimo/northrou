package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// StoredToken is a persisted refresh token record (hash only, never the raw
// token). ProfileID records which profile the device is currently using, so a
// refresh mints an access token for the same profile without another pin.
// DeviceID/DeviceName identify the paired device across token rotations.
type StoredToken struct {
	ID         int64
	ProfileID  int64
	ExpiresAt  time.Time
	Revoked    bool
	DeviceID   string
	DeviceName string
}

// InsertRefreshToken stores the hash of a refresh token bound to a profile and
// the device it belongs to. deviceID stays stable across rotations; a fresh
// pair mints a new one.
func (d *DB) InsertRefreshToken(ctx context.Context, profileID int64, tokenHash string, expiresAt time.Time, deviceID, deviceName string) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO refresh_tokens (profile_id, token_hash, expires_at, device_id, device_name, last_used_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		profileID, tokenHash, expiresAt, deviceID, deviceName, time.Now())
	return err
}

// GetRefreshToken looks up a refresh token by its hash.
func (d *DB) GetRefreshToken(ctx context.Context, tokenHash string) (*StoredToken, error) {
	row := d.QueryRowContext(ctx,
		`SELECT id, profile_id, expires_at, revoked, device_id, device_name
		 FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash)
	var t StoredToken
	var revoked int
	var profileID sql.NullInt64
	err := row.Scan(&t.ID, &profileID, &t.ExpiresAt, &revoked, &t.DeviceID, &t.DeviceName)
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

// ErrTokenExpired is returned when a refresh token is present but past expiry.
var ErrTokenExpired = errors.New("refresh token expired")

// ErrTokenReused is returned when an already-revoked refresh token is presented
// again — the fingerprint of a stolen, previously-rotated token being replayed.
// RotateRefreshToken responds by revoking the whole device family; the caller
// should treat it as a compromise (the device must pair again).
var ErrTokenReused = errors.New("refresh token reused")

// RotateRefreshToken atomically consumes oldHash and stores newHash for the same
// device, in ONE transaction, so two concurrent refreshes presenting the same
// token cannot both succeed (the old code did SELECT-check-REVOKE-INSERT as four
// separate statements, allowing a double-spend). It also detects reuse: if
// oldHash is already revoked, every token for its device is revoked and
// ErrTokenReused is returned.
//
// profileID, when > 0, scopes the new token to that profile (a profile switch);
// otherwise the device's current profile is kept. On success it returns the
// consumed token's record (with the effective ProfileID) so the caller can mint
// a matching access token.
func (d *DB) RotateRefreshToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time, profileID int64) (*StoredToken, error) {
	// Managed directly (not via WithTx) because the reuse path must COMMIT the
	// device-family revocation while still returning a sentinel error.
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var t StoredToken
	var revoked int
	var storedPID sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT id, profile_id, expires_at, revoked, device_id, device_name
		 FROM refresh_tokens WHERE token_hash = ?`, oldHash).
		Scan(&t.ID, &storedPID, &t.ExpiresAt, &revoked, &t.DeviceID, &t.DeviceName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Reuse: an already-revoked token presented again means a stolen,
	// previously-rotated token is being replayed. Nuke the device family and
	// COMMIT that revocation before signalling the compromise.
	if revoked == 1 {
		if t.DeviceID != "" {
			if _, err := tx.ExecContext(ctx,
				`UPDATE refresh_tokens SET revoked = 1 WHERE device_id = ? AND device_id != ''`, t.DeviceID); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		committed = true
		return nil, ErrTokenReused
	}
	if time.Now().After(t.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	pid := storedPID.Int64
	if profileID > 0 {
		pid = profileID
	}
	if pid == 0 {
		return nil, ErrNotFound // token bound to no profile
	}

	// Consume the old token, guarded on revoked=0 so a concurrent rotation (same
	// token, racing) affects zero rows and loses.
	res, err := tx.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked = 1 WHERE token_hash = ? AND revoked = 0`, oldHash)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrTokenExpired // lost the race: already consumed
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO refresh_tokens (profile_id, token_hash, expires_at, device_id, device_name, last_used_at)
		 VALUES (?, ?, ?, ?, ?, ?)`, pid, newHash, newExpiresAt, t.DeviceID, t.DeviceName, time.Now()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	t.ProfileID = pid
	t.Revoked = false
	return &t, nil
}

// DeleteExpiredTokens purges expired/revoked tokens; call periodically.
func (d *DB) DeleteExpiredTokens(ctx context.Context) error {
	_, err := d.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at < ? OR revoked = 1`, time.Now())
	return err
}

// DeviceSession is one paired device, as shown to the operator: the newest
// active token per device plus when the device first paired and was last seen.
type DeviceSession struct {
	// Key identifies the device for revocation: its device_id, or "row-<id>"
	// for tokens from before device tracking existed.
	Key         string
	DeviceName  string
	ProfileName string
	CreatedAt   time.Time
	LastUsedAt  time.Time
}

// ListDeviceSessions returns the currently-paired devices (active, unexpired
// tokens grouped per device), most recently used first.
func (d *DB) ListDeviceSessions(ctx context.Context) ([]DeviceSession, error) {
	// last_used_at is selected raw and coalesced in Go: written by this code
	// it is a driver time value, but created_at comes from SQLite's own
	// CURRENT_TIMESTAMP, and a SQL COALESCE across the two degrades to a plain
	// string the driver refuses to scan into time.Time.
	rows, err := d.QueryContext(ctx,
		`SELECT rt.id, rt.device_id, rt.device_name, rt.created_at,
		        rt.last_used_at, COALESCE(p.name, '')
		 FROM refresh_tokens rt
		 LEFT JOIN profiles p ON p.id = rt.profile_id
		 WHERE rt.revoked = 0 AND rt.expires_at > ?
		 ORDER BY rt.id ASC`, time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Group in Go: a household has a handful of devices, and later rows (the
	// ORDER BY follows insertion order, i.e. rotation order) carry the newest
	// name/profile/last-seen, so each device ends up described by its latest
	// token while keeping the earliest created_at as "paired since".
	byKey := map[string]*DeviceSession{}
	var order []string
	for rows.Next() {
		var id int64
		var deviceID, deviceName, profileName string
		var created time.Time
		var lastUsedN sql.NullTime
		if err := rows.Scan(&id, &deviceID, &deviceName, &created, &lastUsedN, &profileName); err != nil {
			return nil, err
		}
		lastUsed := created
		if lastUsedN.Valid {
			lastUsed = lastUsedN.Time
		}
		key := deviceID
		if key == "" {
			key = fmt.Sprintf("row-%d", id)
		}
		s, ok := byKey[key]
		if !ok {
			s = &DeviceSession{Key: key, CreatedAt: created}
			byKey[key] = s
			order = append(order, key)
		}
		s.DeviceName = deviceName
		s.ProfileName = profileName
		s.LastUsedAt = lastUsed
		if created.Before(s.CreatedAt) {
			s.CreatedAt = created
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DeviceSession, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	// Most recently used first.
	slices.SortStableFunc(out, func(a, b DeviceSession) int {
		return b.LastUsedAt.Compare(a.LastUsedAt)
	})
	return out, nil
}

// RevokeDeviceSession revokes every token belonging to the device identified
// by a DeviceSession.Key. It reports ErrNotFound when nothing matched.
func (d *DB) RevokeDeviceSession(ctx context.Context, key string) error {
	var res sql.Result
	var err error
	if id, ok := strings.CutPrefix(key, "row-"); ok {
		res, err = d.ExecContext(ctx,
			`UPDATE refresh_tokens SET revoked = 1 WHERE id = ? AND revoked = 0`, id)
	} else {
		res, err = d.ExecContext(ctx,
			`UPDATE refresh_tokens SET revoked = 1 WHERE device_id = ? AND device_id != '' AND revoked = 0`, key)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAllTokens revokes every refresh token: every paired device must pair
// again. Used when the connection code is rotated.
func (d *DB) RevokeAllTokens(ctx context.Context) error {
	_, err := d.ExecContext(ctx, `UPDATE refresh_tokens SET revoked = 1`)
	return err
}
