package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrationsApplied checks that a domain table from a migration exists,
// which is the strongest available proof that goose ran the up migrations.
func migrationsApplied(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='stages'`).Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "stages", name)
}

func TestOpen_MemorySucceedsAndMigrates(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })

	migrationsApplied(t, ctx, store.DB())
}

func TestOpen_FileBackedWALMode(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orla.db")
	store, err := Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })

	var mode string
	require.NoError(t, store.DB().QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode))
	assert.Equal(t, "wal", mode)
}

func TestOpen_ReopenIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orla.db")

	store1, err := Open(ctx, path)
	require.NoError(t, err)
	migrationsApplied(t, ctx, store1.DB())
	require.NoError(t, store1.Close())

	store2, err := Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store2.Close) })
	migrationsApplied(t, ctx, store2.DB())
}

func TestOpen_EmptyPathErrors(t *testing.T) {
	_, err := Open(context.Background(), "")
	require.Error(t, err)
}

func TestTx_CommitsOnSuccess(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })

	_, err = store.DB().ExecContext(ctx, `CREATE TABLE t(v INTEGER)`)
	require.NoError(t, err)

	require.NoError(t, store.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO t(v) VALUES (1), (2)`)
		return err
	}))

	var count int
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestTx_RollsBackOnError(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })

	_, err = store.DB().ExecContext(ctx, `CREATE TABLE t(v INTEGER)`)
	require.NoError(t, err)

	sentinel := errors.New("oops")
	err = store.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO t(v) VALUES (1)`); err != nil {
			return err
		}
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel)

	var count int
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&count))
	assert.Equal(t, 0, count, "transaction should have rolled back")
}
