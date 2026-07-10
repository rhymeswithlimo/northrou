// Package db opens and migrates Northrou's SQLite database and provides a thin,
// hand-written query layer grouped by aggregate. It uses the pure-Go
// modernc.org/sqlite driver so the binary cross-compiles without a C toolchain.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps *sql.DB with Northrou-specific helpers.
type DB struct {
	*sql.DB
	path string
}

// Open opens (creating if needed) the SQLite database at path, applies
// connection pragmas (WAL, foreign keys, busy timeout), and runs all pending
// migrations. Parent directories are created automatically.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// Pragmas via DSN so every pooled connection gets them.
	//   cache_size(-8000): 8 MiB page cache (default is 2 MiB), so hot pages stay
	//     in memory instead of hitting a slow disk repeatedly. Modest so it does
	//     not pressure a low-RAM box.
	//   mmap_size: memory-map up to 128 MiB of the DB so reads skip a syscall/copy;
	//     mapped pages are demand-loaded and reclaimable, so it is virtual, not
	//     resident, RAM.
	dsn := path + "?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=cache_size(-8000)" +
		"&_pragma=mmap_size(134217728)"

	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite writers serialize; keep the pool small and predictable.
	sqldb.SetMaxOpenConns(1)

	if err := sqldb.Ping(); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	d := &DB{DB: sqldb, path: path}
	if err := d.migrate(); err != nil {
		sqldb.Close()
		return nil, err
	}
	return d, nil
}

// migrate runs embedded goose migrations to the latest version.
func (d *DB) migrate() error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(d.DB, "migrations"); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// Version returns the current migration version.
func (d *DB) Version() (int64, error) {
	return goose.GetDBVersion(d.DB)
}

// Path returns the on-disk database path.
func (d *DB) Path() string { return d.path }

// execer is the subset of *sql.DB / *sql.Tx used by the write helpers, so they
// can run either directly (auto-commit) or inside a transaction. Both *DB (via
// the embedded *sql.DB) and *sql.Tx satisfy it.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WithTx runs fn inside a transaction, committing on success and rolling back
// on error or panic.
func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
