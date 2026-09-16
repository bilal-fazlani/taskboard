package cli

import (
	"path/filepath"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
)

// label update must resolve its argument the same way label delete already
// does: by id or by exact name, not by id alone.
func TestLabelUpdateResolvesByIDOrName(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "label", "create", "bug", "--color", "#FF0000"); err != nil {
		t.Fatalf("label create: %v", err)
	}

	// Update by exact name, not id.
	if _, err := runCLI(t, "--db", path, "label", "update", "bug", "--name", "defect"); err != nil {
		t.Fatalf("label update by name: %v", err)
	}

	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatalf("opening db to verify: %v", err)
	}
	defer database.Close()
	labels, err := db.NewStore(database).ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "defect" {
		t.Fatalf("labels = %+v, want a single label named defect", labels)
	}

	// The rename stuck: a second update by the new name succeeds.
	if _, err := runCLI(t, "--db", path, "label", "update", "defect", "--color", "#00FF00"); err != nil {
		t.Fatalf("label update by new name: %v", err)
	}

	// An unresolvable id-or-name is a clear error, not a silent no-op.
	if _, err := runCLI(t, "--db", path, "label", "update", "does-not-exist", "--name", "x"); err == nil {
		t.Fatal("expected an error for an unresolvable label reference")
	}
}
