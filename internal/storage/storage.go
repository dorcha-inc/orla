// Package storage owns Orla's embedded SQLite database. It centralizes
// connection lifecycle, WAL/pragma settings, schema migrations, and helpers
// for transactional writes and async batched writes.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/harvard-cns/orla/internal/core"
	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// driverName is the modernc.org/sqlite driver identifier registered with database/sql.
const driverName = "sqlite"

// Store is the daemon's handle to the embedded database.
type Store struct {
	db *sql.DB
}

// Open opens or creates the database at path, applies WAL pragmas, and runs
// any pending migrations. Use ":memory:" for ephemeral in-process databases
// (intended for tests).
func Open(ctx context.Context, path string) (*Store, error) {
	dsn, err := buildDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %q: %w", path, err)
	}
	// Single open connection for :memory: so the schema persists across calls.
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if err := applyPragmas(ctx, db); err != nil {
		core.LogDeferredError(db.Close)
		return nil, err
	}
	if err := runMigrations(ctx, db); err != nil {
		core.LogDeferredError(db.Close)
		return nil, err
	}
	return &Store{db: db}, nil
}

// DB returns the underlying *sql.DB. Callers should prefer Tx for writes.
func (s *Store) DB() *sql.DB { return s.db }

// Close shuts the database down.
func (s *Store) Close() error { return s.db.Close() }

// Tx runs fn inside a transaction. On a non-nil error it rolls back; otherwise it commits.
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		core.LogDeferredError(tx.Rollback)
		return err
	}
	return tx.Commit()
}

// buildDSN constructs the modernc.org/sqlite DSN with our standard pragmas.
// Pragmas via DSN are applied once per connection, so we don't have to issue
// them on every checkout from the pool.
func buildDSN(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("storage path is required")
	}
	if path == ":memory:" {
		return "file::memory:?cache=shared&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve storage path %q: %w", path, err)
	}
	return "file:" + abs + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", nil
}

// applyPragmas runs a Ping so the driver opens at least one connection and
// applies the DSN-level pragmas. It also verifies WAL mode is in effect on
// file-backed databases (in-memory always reports "memory").
func applyPragmas(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite: %w", err)
	}
	return nil
}
