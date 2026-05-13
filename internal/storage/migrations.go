package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/harvard-cns/orla/internal/core"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// runMigrations applies any pending migration files under migrations/ in
// version order. Each file is named NNNN_description.sql where NNNN is the
// version. Already-applied versions are skipped.
func runMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := listMigrationFiles()
	if err != nil {
		return err
	}

	applied, err := loadAppliedVersions(ctx, db)
	if err != nil {
		return err
	}

	for _, m := range files {
		if _, ok := applied[m.version]; ok {
			continue
		}
		body, err := fs.ReadFile(migrationsFS, m.relPath)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.relPath, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for migration %d: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			core.LogDeferredError(tx.Rollback)
			return fmt.Errorf("apply migration %d (%s): %w", m.version, m.relPath, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?, strftime('%s','now'))`, m.version); err != nil {
			core.LogDeferredError(tx.Rollback)
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
	}
	return nil
}

type migrationFile struct {
	version int
	relPath string
}

func listMigrationFiles() ([]migrationFile, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	out := make([]migrationFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := parseMigrationVersion(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migrationFile{version: v, relPath: path.Join("migrations", e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

func parseMigrationVersion(name string) (int, error) {
	// e.g. "0001_init.sql" → 1
	base := strings.TrimSuffix(name, ".sql")
	prefix := base
	if idx := strings.Index(base, "_"); idx > 0 {
		prefix = base[:idx]
	}
	v, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("migration filename %q: leading version must be numeric", name)
	}
	return v, nil
}

func loadAppliedVersions(ctx context.Context, db *sql.DB) (map[int]struct{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("load applied migrations: %w", err)
	}
	defer core.LogDeferredError(rows.Close)
	out := map[int]struct{}{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		out[v] = struct{}{}
	}
	return out, rows.Err()
}
