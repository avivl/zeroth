package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

const migrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at_unix_nano INTEGER NOT NULL
)`

type migration struct {
	version int
	name    string
	up      string
	down    string
}

func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("store sqlite migrations: %w", err)
	}
	byVer := map[int]*migration{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ver, slug, dir, ok := parseMigrationName(name)
		if !ok {
			return nil, fmt.Errorf("store sqlite migrations: bad filename %q", name)
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("store sqlite migrations read %s: %w", name, err)
		}
		m := byVer[ver]
		if m == nil {
			m = &migration{version: ver, name: slug}
			byVer[ver] = m
		}
		if m.name != slug {
			return nil, fmt.Errorf("store sqlite migrations: version %d slug mismatch %q vs %q", ver, m.name, slug)
		}
		switch dir {
		case "up":
			m.up = string(body)
		case "down":
			m.down = string(body)
		}
	}
	out := make([]migration, 0, len(byVer))
	for _, m := range byVer {
		if strings.TrimSpace(m.up) == "" {
			return nil, fmt.Errorf("store sqlite migrations: %04d_%s missing up", m.version, m.name)
		}
		if strings.TrimSpace(m.down) == "" {
			return nil, fmt.Errorf("store sqlite migrations: %04d_%s missing down (a tool that cannot roll back is not done)", m.version, m.name)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func parseMigrationName(name string) (version int, slug, direction string, ok bool) {
	// 0001_init.up.sql
	if !strings.HasSuffix(name, ".sql") {
		return 0, "", "", false
	}
	base := strings.TrimSuffix(name, ".sql")
	dot := strings.LastIndex(base, ".")
	if dot < 0 {
		return 0, "", "", false
	}
	direction = base[dot+1:]
	if direction != "up" && direction != "down" {
		return 0, "", "", false
	}
	rest := base[:dot]
	under := strings.Index(rest, "_")
	if under < 0 {
		return 0, "", "", false
	}
	version, err := strconv.Atoi(rest[:under])
	if err != nil || version < 1 {
		return 0, "", "", false
	}
	slug = rest[under+1:]
	if slug == "" {
		return 0, "", "", false
	}
	return version, slug, direction, true
}

func (s *Store) migrate(ctx context.Context, fsys fs.FS) error {
	migs, err := loadMigrations(fsys)
	if err != nil {
		return err
	}
	return s.applyUpTo(ctx, migs, currentMaxVersion(migs))
}

func currentMaxVersion(migs []migration) int {
	if len(migs) == 0 {
		return 0
	}
	return migs[len(migs)-1].version
}

func (s *Store) appliedVersion(ctx context.Context) (int, error) {
	if _, err := s.db.ExecContext(ctx, migrationsTable); err != nil {
		return 0, fmt.Errorf("store sqlite schema_migrations: %w", err)
	}
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("store sqlite schema version: %w", err)
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func (s *Store) applyUpTo(ctx context.Context, migs []migration, target int) error {
	cur, err := s.appliedVersion(ctx)
	if err != nil {
		return err
	}
	if target < 0 {
		return fmt.Errorf("store sqlite migrate: target %d: invalid", target)
	}
	if target < cur {
		return s.applyDownTo(ctx, migs, target)
	}
	for _, m := range migs {
		if m.version <= cur || m.version > target {
			continue
		}
		if err := s.execMigration(ctx, m.version, m.name, m.up, true); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyDownTo(ctx context.Context, migs []migration, target int) error {
	cur, err := s.appliedVersion(ctx)
	if err != nil {
		return err
	}
	for i := len(migs) - 1; i >= 0; i-- {
		m := migs[i]
		if m.version > cur || m.version <= target {
			continue
		}
		if err := s.execMigration(ctx, m.version, m.name, m.down, false); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) execMigration(ctx context.Context, version int, name, sqlText string, up bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store sqlite migrate %04d_%s begin: %w", version, name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("store sqlite migrate %04d_%s: %w", version, name, err)
	}
	if up {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, applied_at_unix_nano) VALUES (?, ?, ?)`,
			version, name, time.Now().UTC().UnixNano(),
		)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, version)
	}
	if err != nil {
		return fmt.Errorf("store sqlite migrate %04d_%s catalog: %w", version, name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store sqlite migrate %04d_%s commit: %w", version, name, err)
	}
	return nil
}

// Version returns the applied schema version. Tests use this to assert Up/Down.
func (s *Store) Version(ctx context.Context) (int, error) {
	if err := s.guard(); err != nil {
		return 0, err
	}
	return s.appliedVersion(ctx)
}

// MigrateDown rolls back one migration. Product data from that migration is
// removed as the Down script specifies; earlier rows stay when Down is
// additive (column/index drop).
func (s *Store) MigrateDown(ctx context.Context) error {
	if err := s.guard(); err != nil {
		return err
	}
	migs, err := loadMigrations(s.fsys)
	if err != nil {
		return err
	}
	cur, err := s.appliedVersion(ctx)
	if err != nil {
		return err
	}
	if cur == 0 {
		return fmt.Errorf("store sqlite migrate down: already at version 0")
	}
	return s.applyDownTo(ctx, migs, cur-1)
}

// MigrateUp applies pending embedded migrations.
func (s *Store) MigrateUp(ctx context.Context) error {
	if err := s.guard(); err != nil {
		return err
	}
	return s.migrate(ctx, s.fsys)
}
