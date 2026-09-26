package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

const prefixMigration = "013_project_prefix_case_unique.sql"

// seedCaseCollision recreates a database from before migration 013: it drops
// the case-insensitive index and inserts a project directly, so a prefix that
// differs from an existing one only by case can exist. The store refuses to
// create one, so this is the only way to test the resolvers' ambiguity guard,
// which stays as a second line of defence.
func seedCaseCollision(t *testing.T, s *Store, name, prefix string) *models.Project {
	t.Helper()
	if _, err := s.db.Exec("DROP INDEX IF EXISTS idx_projects_prefix_lower"); err != nil {
		t.Fatalf("dropping the prefix index: %v", err)
	}
	p := &models.Project{ID: newID(), Name: name, Prefix: prefix}
	now := stamp(time.Now())
	if _, err := s.db.Exec(
		"INSERT INTO projects (id, name, prefix, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		p.ID, p.Name, p.Prefix, now, now,
	); err != nil {
		t.Fatalf("inserting project %s: %v", prefix, err)
	}
	return p
}

func assertPrefixRefused(t *testing.T, err error, context string, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: accepted, want a rejection", context)
	}
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("%s: error %v (%T) is not an ErrInvalidInput", context, err, err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("%s: error %q should mention %q", context, err, w)
		}
	}
}

func projectPrefixes(t *testing.T, s *Store) []string {
	t.Helper()
	projects, err := s.ListProjects("")
	if err != nil {
		t.Fatal(err)
	}
	var prefixes []string
	for _, p := range projects {
		prefixes = append(prefixes, p.Prefix)
	}
	return prefixes
}

func TestCreateProjectRejectsPrefixTakenIgnoringCase(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "Glow", "GLOW")

	_, err := s.CreateProject(models.CreateProjectRequest{Name: "Glowworm", Prefix: "glow"})
	assertPrefixRefused(t, err, "create glow beside GLOW", `"glow"`, `"Glow"`, `"GLOW"`, "letter case")

	_, err = s.CreateProject(models.CreateProjectRequest{Name: "Glow again", Prefix: "GLOW"})
	assertPrefixRefused(t, err, "create GLOW twice", `"GLOW"`, `"Glow"`)

	if got := projectPrefixes(t, s); len(got) != 1 || got[0] != "GLOW" {
		t.Fatalf("projects after the rejections = %v, want only GLOW", got)
	}

	if _, err := s.CreateProject(models.CreateProjectRequest{Name: "Glowworm", Prefix: "GLOWW"}); err != nil {
		t.Fatalf("a prefix that merely starts the same was refused: %v", err)
	}
}

func TestUpdateProjectRejectsPrefixTakenIgnoringCase(t *testing.T) {
	s := newTestStore(t)
	glow := seedProject(t, s, "Glow", "GLOW")
	bill := seedProject(t, s, "Billing", "BILL")

	lower := "glow"
	newName := "Renamed"
	_, err := s.UpdateProject(bill.ID, models.UpdateProjectRequest{Name: &newName, Prefix: &lower})
	assertPrefixRefused(t, err, "rename BILL to glow", `"glow"`, `"Glow"`, `"GLOW"`, "letter case")

	got, err := s.GetProject(bill.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prefix != "BILL" || got.Name != "Billing" {
		t.Fatalf("after a rejected rename the project is %q [%s], want Billing [BILL] unchanged", got.Name, got.Prefix)
	}

	// A project may change the case of its own prefix.
	mixed := "Glow"
	if p, err := s.UpdateProject(glow.ID, models.UpdateProjectRequest{Prefix: &mixed}); err != nil {
		t.Fatalf("recasing a project's own prefix: %v", err)
	} else if p.Prefix != "Glow" {
		t.Fatalf("prefix after recasing = %q, want Glow", p.Prefix)
	}

	// Leaving the prefix out never checks it.
	if _, err := s.UpdateProject(bill.ID, models.UpdateProjectRequest{Name: &newName}); err != nil {
		t.Fatalf("an update without a prefix: %v", err)
	}
}

// The index is what holds the rule for any write that skips the store.
func TestPrefixIndexRejectsCaseOnlyDuplicate(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "Glow", "GLOW")
	now := stamp(time.Now())
	_, err := s.db.Exec(
		"INSERT INTO projects (id, name, prefix, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		newID(), "Glowworm", "glow", now, now,
	)
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("raw insert of glow beside GLOW: err = %v, want a UNIQUE failure", err)
	}
}

func TestPrefixMigrationAppliesToDistinctPrefixes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openLegacyDB(t, path, prefixMigration)
	if _, err := legacy.Exec(`INSERT INTO projects (id, name, prefix) VALUES
		('p1', 'Glow', 'GLOW'), ('p2', 'Glowworm', 'GLOWW'), ('p3', 'Billing', 'bill')`); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	legacy.Close()

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("opening with distinct prefixes: %v", err)
	}
	defer database.Close()

	if !migrationApplied(t, database, prefixMigration) {
		t.Fatal("013 is not recorded as applied")
	}
	if _, err := database.Exec(`INSERT INTO projects (id, name, prefix) VALUES ('p4', 'Bill again', 'BILL')`); err == nil {
		t.Fatal("after 013, BILL was accepted beside bill")
	}
}

func TestPrefixMigrationReportsCollisionsAndChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openLegacyDB(t, path, prefixMigration)
	if _, err := legacy.Exec(`INSERT INTO projects (id, name, prefix) VALUES
		('p1', 'Glow', 'GLOW'), ('p2', 'Glowworm', 'glow'),
		('p3', 'Billing', 'BILL'), ('p4', 'Bills', 'Bill'),
		('p5', 'Other', 'OTH')`); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := legacy.Exec(`INSERT INTO tickets (id, project_id, number, title) VALUES ('t1', 'p2', 1, 'Kept')`); err != nil {
		t.Fatalf("seeding ticket: %v", err)
	}
	legacy.Close()

	_, err := OpenAt(path)
	if err == nil {
		t.Fatal("opening a database with case-only prefix collisions succeeded")
	}
	for _, want := range []string{
		prefixMigration,
		"differ only by letter case",
		`BILL ("Billing", id p3) and Bill ("Bills", id p4)`,
		`GLOW ("Glow", id p1) and glow ("Glowworm", id p2)`,
		"Nothing was changed",
		"keep one project's prefix and give every other project a new one",
		"ticket keys change",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("migration error %q should contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "OTH") {
		t.Fatalf("migration error %q names a project that does not collide", err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if migrationApplied(t, raw, prefixMigration) {
		t.Fatal("013 was recorded as applied after failing")
	}
	var projects, tickets, index int
	raw.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&projects)
	raw.QueryRow(`SELECT COUNT(*) FROM tickets WHERE project_id = 'p2'`).Scan(&tickets)
	raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'idx_projects_prefix_lower'`).Scan(&index)
	if projects != 5 || tickets != 1 || index != 0 {
		t.Fatalf("after the failed migration: %d projects, %d tickets on glow, index present %d; want 5, 1, 0", projects, tickets, index)
	}

	// Once the prefixes are made distinct by hand, the next start applies it.
	if _, err := raw.Exec(`UPDATE projects SET prefix = 'GLOWW' WHERE id = 'p2'; UPDATE projects SET prefix = 'BILLS' WHERE id = 'p4'`); err != nil {
		t.Fatal(err)
	}
	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("opening after fixing the prefixes: %v", err)
	}
	defer database.Close()
	if !migrationApplied(t, database, prefixMigration) {
		t.Fatal("013 not applied after the prefixes were fixed")
	}
}

func migrationApplied(t *testing.T, database *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, name).Scan(&n); err != nil {
		t.Fatalf("reading schema_migrations: %v", err)
	}
	return n > 0
}
