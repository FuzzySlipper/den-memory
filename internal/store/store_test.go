package store

import (
	"path/filepath"
	"testing"
)

func TestApplyMigrationsAndCapabilities(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	applied, err := s.ApplyMigrations()
	if err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	if len(applied) != 3 {
		t.Fatalf("applied migrations = %v, want 3 migrations", applied)
	}
	if err := s.CheckCapabilities(); err != nil {
		t.Fatalf("CheckCapabilities: %v", err)
	}
	names, err := s.TableNames()
	if err != nil {
		t.Fatalf("TableNames: %v", err)
	}
	for _, name := range []string{"memory_entries", "memory_candidates", "topic_nodes", "memory_entries_fts", "memory_candidates_fts"} {
		if _, ok := names[name]; !ok {
			t.Fatalf("missing table %s", name)
		}
	}
}

func TestSQLiteJSONAndFTS(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if _, err := s.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	var got int
	if err := s.DB().QueryRow(`SELECT value FROM json_each('{"value":7}')`).Scan(&got); err != nil {
		t.Fatalf("json_each: %v", err)
	}
	if got != 7 {
		t.Fatalf("json_each value = %d, want 7", got)
	}
	if _, err := s.DB().Exec(`INSERT INTO memory_candidates_fts(rowid,title,summary,body_md,proposed_kind) VALUES (1,'Hermes invariant','summary','body','fact')`); err != nil {
		t.Fatalf("insert fts: %v", err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM memory_candidates_fts WHERE memory_candidates_fts MATCH 'Hermes'`).Scan(&count); err != nil {
		t.Fatalf("fts match: %v", err)
	}
	if count != 1 {
		t.Fatalf("fts count = %d, want 1", count)
	}
}
