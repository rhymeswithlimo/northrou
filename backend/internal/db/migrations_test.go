package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestProfilesMigrationRoundTrip verifies 00005 applies cleanly (account,
// profiles, and auth_pins replace users and login_pins), that the per-profile
// foreign keys are rewritten to reference profiles(id), and that Down/Up
// round-trips without error.
func TestProfilesMigrationRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m5.db")
	d, err := Open(path) // Open runs migrations up to the latest version.
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		want int
	}{
		{"account", 1}, {"profiles", 1}, {"auth_pins", 1},
		{"users", 0}, {"login_pins", 0},
	} {
		var n int
		if err := d.QueryRowContext(ctx,
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tc.name).Scan(&n); err != nil {
			t.Fatalf("count table %s: %v", tc.name, err)
		}
		if n != tc.want {
			t.Errorf("table %s present=%d, want %d", tc.name, n, tc.want)
		}
	}

	// The rename must rewrite child foreign keys to point at profiles.
	var ddl string
	if err := d.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='watch_history'`).Scan(&ddl); err != nil {
		t.Fatalf("watch_history ddl: %v", err)
	}
	if !strings.Contains(ddl, "profiles") {
		t.Fatalf("watch_history FK not rewritten to profiles:\n%s", ddl)
	}

	// The rewritten FK must actually hold: a child row needs a real profile.
	res, err := d.ExecContext(ctx, `INSERT INTO profiles (name) VALUES ('Test')`)
	if err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	pid, _ := res.LastInsertId()
	if _, err := d.ExecContext(ctx,
		`INSERT INTO watch_history (user_id, media_kind, media_id) VALUES (?, 'movie', 1)`, pid); err != nil {
		t.Fatalf("insert watch_history: %v", err)
	}

	// Down to the previous version and back up must both succeed.
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.DownTo(d.DB, "migrations", 4); err != nil {
		t.Fatalf("down to 4: %v", err)
	}
	if err := goose.Up(d.DB, "migrations"); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
