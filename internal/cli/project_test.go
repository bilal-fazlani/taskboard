package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
)

// project delete must accept a project prefix (case-insensitive), the same
// as ticket create's --project flag, and reject an unknown one with a clear
// error rather than silently doing nothing.
func TestProjectDeleteAcceptsPrefixAndRejectsUnknownOne(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}

	if _, err := runCLI(t, "--db", path, "project", "delete", "NOPE"); err == nil {
		t.Fatal("expected an error for an unknown project prefix")
	}

	listed, err := runCLI(t, "--db", path, "project", "list")
	if err != nil {
		t.Fatalf("project list: %v", err)
	}
	if !strings.Contains(listed, "BILL") {
		t.Fatalf("project list after a failed delete = %q, want BILL still present", listed)
	}

	if _, err := runCLI(t, "--db", path, "project", "delete", "bill"); err != nil {
		t.Fatalf("project delete bill: %v", err)
	}

	listedAfter, err := runCLI(t, "--db", path, "project", "list")
	if err != nil {
		t.Fatalf("project list: %v", err)
	}
	if !strings.Contains(listedAfter, "No projects found") {
		t.Fatalf("project list after delete by prefix = %q, want no projects", listedAfter)
	}
}

// Two projects whose prefixes differ only by case made "project delete glow"
// delete the other one, with all its tickets. "project create" now refuses
// the second prefix with a clear error and a non-zero exit, so a
// case-insensitive prefix always names exactly one project.
func TestProjectCreateRejectsPrefixTakenIgnoringCase(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Glow Upper", "--prefix", "GLOW"); err != nil {
		t.Fatalf("project create GLOW: %v", err)
	}
	_, err := runCLI(t, "--db", path, "project", "create", "Glow Lower", "--prefix", "glow")
	if err == nil {
		t.Fatal("project create glow beside GLOW succeeded")
	}
	if !strings.Contains(err.Error(), `already used by project "Glow Upper"`) || !strings.Contains(err.Error(), "letter case") {
		t.Fatalf("error %q should name the project and the case rule", err)
	}

	listed, err := runCLI(t, "--db", path, "project", "list")
	if err != nil {
		t.Fatalf("project list: %v", err)
	}
	if !strings.Contains(listed, "[GLOW]") || strings.Contains(listed, "[glow]") {
		t.Fatalf("project list after the refusal = %q, want GLOW only", listed)
	}
}

// project create stores --agent-instructions, and leaves them empty without
// the flag.
func TestProjectCreateTakesAgentInstructions(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL",
		"--agent-instructions", "Run the tests before landing."); err != nil {
		t.Fatalf("project create with instructions: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "project", "create", "Support", "--prefix", "SUP"); err != nil {
		t.Fatalf("project create without instructions: %v", err)
	}

	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	defer database.Close()
	store := db.NewStore(database)
	for prefix, want := range map[string]string{"BILL": "Run the tests before landing.", "SUP": ""} {
		id, err := store.ResolveProjectRef(prefix)
		if err != nil {
			t.Fatalf("resolving %s: %v", prefix, err)
		}
		p, err := store.GetProject(id)
		if err != nil || p == nil || p.AgentInstructions == nil {
			t.Fatalf("GetProject %s: %+v, %v", prefix, p, err)
		}
		if *p.AgentInstructions != want {
			t.Fatalf("%s agent instructions = %q, want %q", prefix, *p.AgentInstructions, want)
		}
	}
}

// project create stores --description, and leaves it empty without the flag.
func TestProjectCreateTakesDescription(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL",
		"--description", "What billing is."); err != nil {
		t.Fatalf("project create with description: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "project", "create", "Support", "--prefix", "SUP"); err != nil {
		t.Fatalf("project create without description: %v", err)
	}

	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	defer database.Close()
	store := db.NewStore(database)
	for prefix, want := range map[string]string{"BILL": "What billing is.", "SUP": ""} {
		id, err := store.ResolveProjectRef(prefix)
		if err != nil {
			t.Fatalf("resolving %s: %v", prefix, err)
		}
		p, err := store.GetProject(id)
		if err != nil || p == nil {
			t.Fatalf("GetProject %s: %+v, %v", prefix, p, err)
		}
		if p.Description != want {
			t.Fatalf("%s description = %q, want %q", prefix, p.Description, want)
		}
	}
}
