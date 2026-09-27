package mcp

import (
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// TestProjectScopedToolsAcceptProjectAlias is ACP-184: every MCP tool that
// takes a project-reference argument accepted it under projectId on most
// tools but under project on update_epic, delete_epic and the document
// tools, so an agent had to guess which name a given tool wanted. Each case
// below sends the tool project instead of projectId (never both) and checks
// the call still reaches the intended project.
func TestProjectScopedToolsAcceptProjectAlias(t *testing.T) {
	s := newTestServer(t)

	project, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	epic, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: project.ID, Name: "Launch"})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}

	t.Run("list_epics", func(t *testing.T) {
		result, err := s.callTool("list_epics", mustJSON(t, map[string]any{"project": project.Prefix}))
		if err != nil {
			t.Fatalf("list_epics by project: %v", err)
		}
		epics := result.(map[string]any)["epics"].([]models.Epic)
		if len(epics) != 1 || epics[0].ID != epic.ID {
			t.Fatalf("list_epics by project returned %+v, want just %+v", epics, epic)
		}
	})

	t.Run("create_epic", func(t *testing.T) {
		result, err := s.callTool("create_epic", mustJSON(t, map[string]any{"project": project.Prefix, "name": "Renewal", "full": true}))
		if err != nil {
			t.Fatalf("create_epic by project: %v", err)
		}
		if got := result.(*models.Epic).ProjectID; got != project.ID {
			t.Fatalf("create_epic by project created it under %q, want %q", got, project.ID)
		}
	})

	t.Run("create_ticket", func(t *testing.T) {
		result, err := s.callTool("create_ticket", mustJSON(t, map[string]any{"project": project.Prefix, "title": "Invoice", "full": true}))
		if err != nil {
			t.Fatalf("create_ticket by project: %v", err)
		}
		if got := result.(*models.Ticket).ProjectID; got != project.ID {
			t.Fatalf("create_ticket by project created it under %q, want %q", got, project.ID)
		}
	})

	t.Run("list_tickets", func(t *testing.T) {
		if _, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: project.ID, Title: "Filtered"}); err != nil {
			t.Fatalf("CreateTicket: %v", err)
		}
		result, err := s.callTool("list_tickets", mustJSON(t, map[string]any{"project": project.Prefix}))
		if err != nil {
			t.Fatalf("list_tickets by project: %v", err)
		}
		tickets := result.([]models.Ticket)
		if len(tickets) == 0 {
			t.Fatalf("list_tickets by project returned none, want at least one ticket in %s", project.Prefix)
		}
		for _, tk := range tickets {
			if tk.ProjectID != project.ID {
				t.Fatalf("list_tickets by project returned a ticket from another project: %+v", tk)
			}
		}
	})

	t.Run("get_board", func(t *testing.T) {
		result, err := s.callTool("get_board", mustJSON(t, map[string]any{"project": project.Prefix}))
		if err != nil {
			t.Fatalf("get_board by project: %v", err)
		}
		if got := result.(*models.Board).ProjectID; got != project.ID {
			t.Fatalf("get_board by project returned board for %q, want %q", got, project.ID)
		}
	})

	t.Run("append_project_journal", func(t *testing.T) {
		entry, err := s.callTool("append_project_journal", mustJSON(t, map[string]any{"project": project.Prefix, "author": "agent", "text": "Shipped."}))
		if err != nil {
			t.Fatalf("append_project_journal by project: %v", err)
		}
		if got := entry.(*models.JournalEntry).ProjectID; got != project.ID {
			t.Fatalf("append_project_journal by project wrote entry for %q, want %q", got, project.ID)
		}
	})

	t.Run("list_project_journal", func(t *testing.T) {
		page, err := s.callTool("list_project_journal", mustJSON(t, map[string]any{"project": project.Prefix}))
		if err != nil {
			t.Fatalf("list_project_journal by project: %v", err)
		}
		if got := page.(models.JournalPage).Total; got == 0 {
			t.Fatalf("list_project_journal by project returned an empty journal, want the entry appended above")
		}
	})
}

// TestProjectScopedToolsAcceptProjectIDOnFormerlyProjectOnlyTools is the
// other half of ACP-184: update_epic, delete_epic and the document tools
// used to accept only project, never projectId. Each case below sends
// projectId instead and checks the call still reaches the intended project.
func TestProjectScopedToolsAcceptProjectIDOnFormerlyProjectOnlyTools(t *testing.T) {
	s := newTestServer(t)

	project, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	t.Run("update_epic", func(t *testing.T) {
		epic, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: project.ID, Name: "Launch"})
		if err != nil {
			t.Fatalf("CreateEpic: %v", err)
		}
		result, err := s.callTool("update_epic", mustJSON(t, map[string]any{
			"key": epic.Name, "projectId": project.Prefix, "description": "Longer.", "full": true,
		}))
		if err != nil {
			t.Fatalf("update_epic by projectId: %v", err)
		}
		if got := result.(*models.Epic).Description; got != "Longer." {
			t.Fatalf("description = %q, want Longer.", got)
		}
	})

	t.Run("delete_epic", func(t *testing.T) {
		epic, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: project.ID, Name: "Scratch epic"})
		if err != nil {
			t.Fatalf("CreateEpic: %v", err)
		}
		if _, err := s.callTool("delete_epic", mustJSON(t, map[string]any{"key": epic.Name, "projectId": project.Prefix})); err != nil {
			t.Fatalf("delete_epic by projectId: %v", err)
		}
		if got, err := s.store.GetEpic(epic.ID); err != nil || got != nil {
			t.Fatalf("epic survived delete by projectId: %+v, err=%v", got, err)
		}
	})

	t.Run("create_document epic owner", func(t *testing.T) {
		epic, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: project.ID, Name: "Doc epic"})
		if err != nil {
			t.Fatalf("CreateEpic: %v", err)
		}
		result, err := s.callTool("create_document", mustJSON(t, map[string]any{
			"epic": epic.Name, "projectId": project.Prefix, "name": "Notes", "content": "hello", "full": true,
		}))
		if err != nil {
			t.Fatalf("create_document by projectId: %v", err)
		}
		if got := result.(*models.Document).EpicID; got != epic.ID {
			t.Fatalf("create_document by projectId attached it to epic %q, want %q", got, epic.ID)
		}
	})
}

// TestProjectRefRequiredError checks that omitting both projectId and
// project on an affected tool fails with a message naming both accepted
// argument names, rather than the pre-ACP-184 "projectId is required" that
// never mentioned project.
func TestProjectRefRequiredError(t *testing.T) {
	s := newTestServer(t)
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"list_epics", nil},
		{"append_project_journal", map[string]any{"author": "a", "text": "t"}},
		{"list_project_journal", nil},
	} {
		args := tc.args
		if args == nil {
			args = map[string]any{}
		}
		want := "projectId or project is required"
		if _, err := s.callTool(tc.tool, mustJSON(t, args)); err == nil || err.Error() != want {
			t.Fatalf("%s with neither projectId nor project: err = %v, want %q", tc.tool, err, want)
		}
	}
}

// TestProjectIDWinsOverProjectAlias checks projectRefArg.projectRef()'s
// documented precedence: when a call gives both projectId and project and
// they name different projects, projectId is the one that is used, on both a
// read-only lookup (list_epics) and a mutation (create_ticket).
func TestProjectIDWinsOverProjectAlias(t *testing.T) {
	s := newTestServer(t)

	winner, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Winner", Prefix: "WIN"})
	if err != nil {
		t.Fatalf("CreateProject winner: %v", err)
	}
	loser, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Loser", Prefix: "LSE"})
	if err != nil {
		t.Fatalf("CreateProject loser: %v", err)
	}
	winnerEpic, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: winner.ID, Name: "Only in winner"})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}

	t.Run("list_epics", func(t *testing.T) {
		result, err := s.callTool("list_epics", mustJSON(t, map[string]any{
			"projectId": winner.Prefix, "project": loser.Prefix,
		}))
		if err != nil {
			t.Fatalf("list_epics with projectId and project: %v", err)
		}
		epics := result.(map[string]any)["epics"].([]models.Epic)
		if len(epics) != 1 || epics[0].ID != winnerEpic.ID {
			t.Fatalf("list_epics with projectId=%s project=%s returned %+v, want just winner's epic %+v",
				winner.Prefix, loser.Prefix, epics, winnerEpic)
		}
	})

	t.Run("create_ticket", func(t *testing.T) {
		result, err := s.callTool("create_ticket", mustJSON(t, map[string]any{
			"projectId": winner.Prefix, "project": loser.Prefix, "title": "Goes to winner", "full": true,
		}))
		if err != nil {
			t.Fatalf("create_ticket with projectId and project: %v", err)
		}
		got := result.(*models.Ticket)
		if got.ProjectID != winner.ID {
			t.Fatalf("create_ticket with projectId=%s project=%s created it under %q, want winner %q",
				winner.Prefix, loser.Prefix, got.ProjectID, winner.ID)
		}
	})
}
