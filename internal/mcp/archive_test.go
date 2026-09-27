package mcp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

func findToolDef(t *testing.T, s *MCPServer, name string) toolDef {
	t.Helper()
	for _, def := range s.toolDefinitions() {
		if def.Name == name {
			return def
		}
	}
	t.Fatalf("no tool %q", name)
	return toolDef{}
}

// delete_project says what deleting does now; list_projects has no status
// to filter on; update_project offers only active.
func TestProjectToolsDescribeDeleteAsArchive(t *testing.T) {
	s := newTestServer(t)
	if got := findToolDef(t, s, "delete_project").Description; got != db.DeleteProjectHelp {
		t.Fatalf("delete_project description = %q, want %q", got, db.DeleteProjectHelp)
	}
	if props := findToolDef(t, s, "list_projects").InputSchema.Properties; len(props) != 0 {
		t.Fatalf("list_projects properties = %v, want none", props)
	}
	if enum := findToolDef(t, s, "update_project").InputSchema.Properties["status"].Enum; !reflect.DeepEqual(enum, []string{"active"}) {
		t.Fatalf("update_project status enum = %v, want [active]", enum)
	}
}

// After delete_project, a project and everything in it are gone from every
// tool, update_project refuses to archive, and the prefix stays taken.
func TestDeletedProjectIsGoneFromTheTools(t *testing.T) {
	s := newTestServer(t)
	for _, prefix := range []string{"GONE", "KEEP"} {
		callJSON(t, s, "create_project", map[string]any{"name": prefix, "prefix": prefix})
	}
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "GONE", "title": "Gone", "status": "in_progress", "labels": []string{"shared"}})
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "KEEP", "title": "Kept", "labels": []string{"shared"},
		"dependsOn": []map[string]any{{"ticket": "GONE-1"}}})
	sub, err := s.store.AddSubtask(mustResolve(t, s, "GONE-1"), models.CreateSubtaskRequest{Title: "Step"})
	if err != nil {
		t.Fatal(err)
	}

	text, isError := callToolText(t, s, "update_project", map[string]any{"id": "KEEP", "status": "archived"})
	if !isError || !strings.Contains(text, "to archive a project, delete it") {
		t.Fatalf("update_project status archived = %q (error %v), want a refusal pointing at delete", text, isError)
	}

	callJSON(t, s, "delete_project", map[string]any{"id": "GONE"})

	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"delete_project", map[string]any{"id": "GONE"}},
		{"get_project", map[string]any{"id": "GONE"}},
		{"update_project", map[string]any{"id": "GONE", "name": "Back"}},
		{"get_ticket", map[string]any{"id": "GONE-1"}},
		{"update_ticket", map[string]any{"id": "GONE-1", "title": "Changed"}},
		{"move_ticket", map[string]any{"id": "GONE-1", "status": "done"}},
		{"create_ticket", map[string]any{"projectId": "GONE", "title": "New"}},
		{"create_subtask", map[string]any{"ticketId": "GONE-1", "title": "More"}},
		{"toggle_subtask", map[string]any{"id": sub.ID, "completed": true}},
		{"list_epics", map[string]any{"projectId": "GONE"}},
		{"list_project_journal", map[string]any{"projectId": "GONE"}},
		{"append_project_journal", map[string]any{"projectId": "GONE", "author": "Bilal", "text": "more"}},
		{"create_project", map[string]any{"name": "Again", "prefix": "GONE"}},
	} {
		text, isError := callToolText(t, s, c.tool, c.args)
		notFound := strings.Contains(text, "not found") || strings.Contains(text, "no ticket matches") ||
			strings.Contains(text, "deleted project")
		if !isError || !notFound {
			t.Fatalf("%s %v after delete = %q (error %v), want it refused as not found", c.tool, c.args, text, isError)
		}
	}
	text, _ = callToolText(t, s, "create_project", map[string]any{"name": "Again", "prefix": "GONE"})
	if !strings.Contains(text, "already used by a deleted project") {
		t.Fatalf("create_project with a deleted project's prefix = %q", text)
	}

	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"list_projects", map[string]any{}},
		{"list_tickets", map[string]any{}},
		{"get_board", map[string]any{}},
		{"get_now", map[string]any{}},
		{"get_ticket", map[string]any{"id": "KEEP-1"}},
	} {
		text, isError := callToolText(t, s, c.tool, c.args)
		if isError || strings.Contains(text, "GONE") {
			t.Fatalf("%s after delete = %q, want no trace of GONE", c.tool, text)
		}
	}
	text, _ = callToolText(t, s, "list_labels", map[string]any{})
	if !strings.Contains(text, `"ticketCount":1`) {
		t.Fatalf("list_labels after delete = %s, want shared on 1 ticket", text)
	}
}

func mustResolve(t *testing.T, s *MCPServer, key string) string {
	t.Helper()
	id, err := s.store.ResolveTicketID(key)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
