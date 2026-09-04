package database

import (
	"io/fs"
	"testing"
)

func TestEmbeddedMigrations_FilesExist(t *testing.T) {
	expectedFiles := []string{
		"migrations/000001_setup.up.sql",
		"migrations/000001_setup.down.sql",
		"migrations/000002_core.up.sql",
		"migrations/000002_core.down.sql",
		"migrations/000003_documents.up.sql",
		"migrations/000003_documents.down.sql",
		"migrations/000004_incidents.up.sql",
		"migrations/000004_incidents.down.sql",
	}

	for _, expected := range expectedFiles {
		content, err := fs.ReadFile(migrationsFS, expected)
		if err != nil {
			t.Errorf("failed to read embedded migration file %s: %v", expected, err)
			continue
		}
		if len(content) == 0 {
			t.Errorf("embedded migration file %s is unexpectedly empty", expected)
		}
	}
}

func TestEmbeddedMigrations_Count(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("failed to read migrations directory from embed.FS: %v", err)
	}

	// 4 up migrations + 4 down migrations = 8 files
	if len(entries) != 8 {
		t.Errorf("expected 8 embedded migration files, found %d", len(entries))
	}
}
