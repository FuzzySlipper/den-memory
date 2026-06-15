// Package store contains SQLite storage helpers for Den Memories.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// Store owns the SQLite database handle.
type Store struct {
	db *sql.DB
}

// Open opens a SQLite database and configures required pragmas.
func Open(path string) (*Store, error) {
	if path == "" {
		path = "./runtime/den-memories.sqlite"
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	return s, nil
}

// Close closes the database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database handle for transaction orchestration.
func (s *Store) DB() *sql.DB {
	return s.db
}

// ApplyMigrations applies embedded SQL migrations and returns applied versions.
func (s *Store) ApplyMigrations() ([]string, error) {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (datetime('now')))`); err != nil {
		return nil, fmt.Errorf("ensure schema_migrations: %w", err)
	}
	rows, err := s.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	known := map[string]struct{}{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema version: %w", err)
		}
		known[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema versions: %w", err)
	}

	names := migrationNames()
	sort.Strings(names)
	applied := []string{}
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if _, ok := known[version]; ok {
			continue
		}
		sqlText, err := migrationSQL(name)
		if err != nil {
			return nil, err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return nil, fmt.Errorf("begin migration %s: %w", version, err)
		}
		if _, err := tx.Exec(sqlText); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit migration %s: %w", version, err)
		}
		applied = append(applied, version)
	}
	return applied, nil
}

// CheckCapabilities verifies SQLite features required by Den Memories.
func (s *Store) CheckCapabilities() error {
	if _, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS temp.__den_memories_fts_check USING fts5(body)`); err != nil {
		return fmt.Errorf("sqlite fts5 unavailable: %w", err)
	}
	defer s.db.Exec(`DROP TABLE IF EXISTS temp.__den_memories_fts_check`)

	var got int
	if err := s.db.QueryRow(`SELECT json_extract('{"ok":1}', '$.ok')`).Scan(&got); err != nil {
		return fmt.Errorf("sqlite json unavailable: %w", err)
	}
	if got != 1 {
		return fmt.Errorf("sqlite json check returned %d", got)
	}
	return nil
}

// TableNames returns all table and view names in the database.
func (s *Store) TableNames() (map[string]struct{}, error) {
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type IN ('table', 'view')`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	names := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		names[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table names: %w", err)
	}
	return names, nil
}

// MigrationVersions returns applied migration versions in order.
func (s *Store) MigrationVersions() ([]string, error) {
	rows, err := s.db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	defer rows.Close()
	versions := []string{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan migration version: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration versions: %w", err)
	}
	return versions, nil
}

func sqliteError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "unique constraint") {
		return fmt.Errorf("%w: %v", ErrDuplicate, err)
	}
	if strings.Contains(text, "constraint failed") || strings.Contains(text, "check constraint") {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return err
}
