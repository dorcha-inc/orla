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

func TestOpen_MemorySucceedsAndMigrates(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })

	var count int
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count))
	assert.Greater(t, count, 0, "at least one migration should have run")
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

func TestOpen_AppliesEachMigrationOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orla.db")

	store1, err := Open(ctx, path)
	require.NoError(t, err)
	var first int
	require.NoError(t, store1.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&first))
	require.NoError(t, store1.Close())

	store2, err := Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store2.Close) })
	var second int
	require.NoError(t, store2.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&second))
	assert.Equal(t, first, second, "re-opening should not re-apply migrations")
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

func TestParseMigrationVersion(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		want   int
		err    bool
	}{
		{name: "standard", input: "0001_init.sql", want: 1},
		{name: "no underscore", input: "0042.sql", want: 42},
		{name: "non-numeric prefix", input: "init.sql", err: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseMigrationVersion(c.input)
			if c.err {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}
