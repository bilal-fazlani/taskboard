package db

import (
	"path/filepath"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// agentInstructions reads a project's instructions, failing the test when a
// read of one project left them unset.
func agentInstructions(t *testing.T, p *models.Project) string {
	t.Helper()
	if p.AgentInstructions == nil {
		t.Fatalf("project %s has no agent instructions field", p.Prefix)
	}
	return *p.AgentInstructions
}

func TestProjectAgentInstructionsRoundTrip(t *testing.T) {
	s := newTestStore(t)

	created, err := s.CreateProject(models.CreateProjectRequest{
		Name: "Billing", Prefix: "BILL", Description: "What billing is.",
		AgentInstructions: "Run the tests before landing.",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if got := agentInstructions(t, created); got != "Run the tests before landing." || !created.HasAgentInstructions {
		t.Fatalf("created project instructions = %q (has %v)", got, created.HasAgentInstructions)
	}

	got, err := s.GetProject(created.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if agentInstructions(t, got) != "Run the tests before landing." || !got.HasAgentInstructions {
		t.Fatalf("read back %q (has %v)", *got.AgentInstructions, got.HasAgentInstructions)
	}
	if got.Description != "What billing is." {
		t.Fatalf("description = %q, want it kept apart from the instructions", got.Description)
	}

	// An update that leaves the instructions out leaves them alone.
	updated, err := s.UpdateProject(created.ID, models.UpdateProjectRequest{Name: strPtr("Billing 2")})
	if err != nil {
		t.Fatalf("UpdateProject name: %v", err)
	}
	if agentInstructions(t, updated) != "Run the tests before landing." {
		t.Fatalf("update without instructions changed them to %q", *updated.AgentInstructions)
	}

	updated, err = s.UpdateProject(created.ID, models.UpdateProjectRequest{AgentInstructions: strPtr("Review every ticket.")})
	if err != nil {
		t.Fatalf("UpdateProject instructions: %v", err)
	}
	got, _ = s.GetProject(created.ID)
	if agentInstructions(t, updated) != "Review every ticket." || agentInstructions(t, got) != "Review every ticket." {
		t.Fatalf("update returned %q, read back %q", *updated.AgentInstructions, *got.AgentInstructions)
	}

	// An empty string clears them.
	if _, err := s.UpdateProject(created.ID, models.UpdateProjectRequest{AgentInstructions: strPtr("")}); err != nil {
		t.Fatalf("UpdateProject clear: %v", err)
	}
	got, _ = s.GetProject(created.ID)
	if agentInstructions(t, got) != "" || got.HasAgentInstructions {
		t.Fatalf("cleared instructions read back %q (has %v)", *got.AgentInstructions, got.HasAgentInstructions)
	}
}

func TestProjectWithoutAgentInstructionsReadsBackEmpty(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	if agentInstructions(t, p) != "" || p.HasAgentInstructions {
		t.Fatalf("new project instructions = %q (has %v)", *p.AgentInstructions, p.HasAgentInstructions)
	}
	got, _ := s.GetProject(p.ID)
	if agentInstructions(t, got) != "" || got.HasAgentInstructions {
		t.Fatalf("read back %q (has %v)", *got.AgentInstructions, got.HasAgentInstructions)
	}
}

// The list says whether each project has instructions but never reads them.
func TestListProjectsFlagsAgentInstructionsWithoutText(t *testing.T) {
	s := newTestStore(t)
	with, err := s.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL", AgentInstructions: "Be careful."})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	without := seedProject(t, s, "Support", "SUP")

	projects, err := s.ListProjects("")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	has := map[string]bool{}
	for _, p := range projects {
		if p.AgentInstructions != nil {
			t.Fatalf("list carried %s's instructions: %q", p.Prefix, *p.AgentInstructions)
		}
		has[p.ID] = p.HasAgentInstructions
	}
	if len(has) != 2 || !has[with.ID] || has[without.ID] {
		t.Fatalf("has instructions = %v, want only %s", has, with.ID)
	}
}

// Projects made before the column existed come through the migration with
// empty instructions.
func TestMigrationGivesExistingProjectsEmptyAgentInstructions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openLegacyDB(t, path, "012_project_agent_instructions.sql")
	if _, err := legacy.Exec(
		`INSERT INTO projects (id, name, prefix, description) VALUES ('p1', 'Billing', 'BILL', 'What billing is.')`,
	); err != nil {
		t.Fatalf("seeding legacy project: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("closing legacy database: %v", err)
	}

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating legacy database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewStore(database)

	p, err := s.GetProject("p1")
	if err != nil || p == nil {
		t.Fatalf("GetProject p1: %v, %v", p, err)
	}
	if agentInstructions(t, p) != "" || p.HasAgentInstructions {
		t.Fatalf("migrated project instructions = %q (has %v)", *p.AgentInstructions, p.HasAgentInstructions)
	}
	if p.Description != "What billing is." {
		t.Fatalf("migrated description = %q", p.Description)
	}
}

// Agent instructions are stored trimmed, so whitespace-only ones are empty:
// the project reads back without any and the list doesn't flag it.
func TestAgentInstructionsAreStoredTrimmed(t *testing.T) {
	s := newTestStore(t)

	blank, err := s.CreateProject(models.CreateProjectRequest{Name: "Blank", Prefix: "BLANK", AgentInstructions: " \n\t "})
	if err != nil {
		t.Fatalf("CreateProject blank: %v", err)
	}
	padded, err := s.CreateProject(models.CreateProjectRequest{Name: "Padded", Prefix: "PAD", AgentInstructions: "\n  Run the tests.\n\n"})
	if err != nil {
		t.Fatalf("CreateProject padded: %v", err)
	}
	if agentInstructions(t, blank) != "" || blank.HasAgentInstructions {
		t.Fatalf("created blank = %q (has %v)", *blank.AgentInstructions, blank.HasAgentInstructions)
	}
	if agentInstructions(t, padded) != "Run the tests." {
		t.Fatalf("created padded = %q", *padded.AgentInstructions)
	}

	got, _ := s.GetProject(blank.ID)
	if agentInstructions(t, got) != "" || got.HasAgentInstructions {
		t.Fatalf("blank read back %q (has %v)", *got.AgentInstructions, got.HasAgentInstructions)
	}
	got, _ = s.GetProject(padded.ID)
	if agentInstructions(t, got) != "Run the tests." {
		t.Fatalf("padded read back %q", *got.AgentInstructions)
	}

	// Updates trim too; whitespace alone empties them.
	updated, err := s.UpdateProject(padded.ID, models.UpdateProjectRequest{AgentInstructions: strPtr("  Review first.  ")})
	if err != nil {
		t.Fatalf("UpdateProject padded: %v", err)
	}
	got, _ = s.GetProject(padded.ID)
	if agentInstructions(t, updated) != "Review first." || agentInstructions(t, got) != "Review first." {
		t.Fatalf("padded update returned %q, read back %q", *updated.AgentInstructions, *got.AgentInstructions)
	}
	if _, err := s.UpdateProject(padded.ID, models.UpdateProjectRequest{AgentInstructions: strPtr("\n   \n")}); err != nil {
		t.Fatalf("UpdateProject whitespace: %v", err)
	}
	got, _ = s.GetProject(padded.ID)
	if agentInstructions(t, got) != "" || got.HasAgentInstructions {
		t.Fatalf("whitespace update read back %q (has %v)", *got.AgentInstructions, got.HasAgentInstructions)
	}

	projects, err := s.ListProjects("")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	for _, p := range projects {
		if p.HasAgentInstructions {
			t.Fatalf("list flags %s, whose instructions were only whitespace", p.Prefix)
		}
	}
	var stored int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE agent_instructions <> ''`).Scan(&stored); err != nil {
		t.Fatalf("counting stored instructions: %v", err)
	}
	if stored != 0 {
		t.Fatalf("%d projects stored non-empty instructions, want 0", stored)
	}
}
