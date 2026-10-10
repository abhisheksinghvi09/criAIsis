package database

import (
	"io/fs"
	"regexp"
	"strconv"
	"testing"
)

// migrationName matches the golang-migrate naming the iofs source driver requires.
var migrationName = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.(up|down)\.sql$`)

// Every migration must embed with a non-empty body: an empty file applies
// silently and leaves the schema short of what the code expects.
func TestEmbeddedMigrations_AreNonEmptyAndWellNamed(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations were embedded")
	}

	for _, entry := range entries {
		if !migrationName.MatchString(entry.Name()) {
			t.Errorf("%s does not match the golang-migrate naming scheme", entry.Name())
			continue
		}
		content, err := fs.ReadFile(migrationsFS, "migrations/"+entry.Name())
		if err != nil {
			t.Errorf("reading %s: %v", entry.Name(), err)
			continue
		}
		if len(content) == 0 {
			t.Errorf("%s is empty", entry.Name())
		}
	}
}

// A version with no down migration cannot be rolled back, which is only discovered
// during an incident. Assert pairing and contiguous numbering instead of a count,
// so adding a migration does not require editing this test.
func TestEmbeddedMigrations_EveryVersionHasBothDirections(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}

	directions := map[int]map[string]bool{}
	for _, entry := range entries {
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("unparseable version in %s: %v", entry.Name(), err)
		}
		if directions[version] == nil {
			directions[version] = map[string]bool{}
		}
		directions[version][match[3]] = true
	}

	if len(directions) == 0 {
		t.Fatal("no versioned migrations found")
	}

	for version, dirs := range directions {
		if !dirs["up"] {
			t.Errorf("migration %06d has no up file", version)
		}
		if !dirs["down"] {
			t.Errorf("migration %06d has no down file, so it cannot be rolled back", version)
		}
	}

	// golang-migrate applies versions in order; a gap means a migration was deleted
	// rather than reverted, and databases at different versions will diverge.
	for version := 1; version <= len(directions); version++ {
		if _, ok := directions[version]; !ok {
			t.Errorf("migration version %06d is missing: the sequence has a gap", version)
		}
	}
}
