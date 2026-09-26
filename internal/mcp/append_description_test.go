package mcp

import (
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// update_ticket's appendDescription adds a paragraph without resending the
// description, and answers like any other update: short by default, with
// description among the changed fields, or the whole ticket on request.
func TestUpdateTicketToolAppendsToDescription(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "BILL", "title": "Invoice", "description": "The plan.\nStep two."})

	short := callJSON(t, s, "update_ticket", map[string]any{"id": "BILL-1", "appendDescription": "Worktree: /w/bill-1"})
	wantKeys(t, "update_ticket", short, "id", "key", "title", "status", "changed", "updatedAt", "url")
	wantChanged(t, "update_ticket", short, "description")

	full := callJSON(t, s, "update_ticket", map[string]any{
		"id": "bill-1", "appendDescription": "Review: APPROVE", "priority": "low", "full": true,
	})
	want := "The plan.\nStep two.\n\nWorktree: /w/bill-1\n\nReview: APPROVE"
	if full["description"] != want || full["priority"] != "low" {
		t.Fatalf("full answer description = %q, priority = %v, want %q and low", full["description"], full["priority"], want)
	}
	if _, ok := full["changed"]; ok {
		t.Fatalf("full answer carries the short form's changed: %v", full)
	}
}

func TestUpdateTicketToolRejectsBadAppends(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "BILL", "title": "Invoice", "description": "Draft"})

	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"id": "BILL-1", "description": "New", "appendDescription": "More"}, "pass description or appendDescription, not both"},
		{map[string]any{"id": "BILL-1", "appendDescription": "  "}, "appendDescription is empty"},
	} {
		text, isError := callToolText(t, s, "update_ticket", tc.args)
		if !isError || !strings.Contains(text, tc.want) {
			t.Fatalf("update_ticket %v = %q (error %v), want an error containing %q", tc.args, text, isError, tc.want)
		}
	}
	tk, err := s.store.GetTicket(mustTicketID(t, s, "BILL-1"))
	if err != nil || tk.Description != "Draft" {
		t.Fatalf("description = %q, %v; want it unchanged", tk.Description, err)
	}
}

func TestUpdateTicketToolDescribesAppend(t *testing.T) {
	s := newTestServer(t)
	for _, def := range s.toolDefinitions() {
		if def.Name != "update_ticket" {
			continue
		}
		prop, ok := def.InputSchema.Properties["appendDescription"]
		if !ok || prop.Type != "string" || !strings.Contains(prop.Description, "new paragraph") {
			t.Fatalf("appendDescription = %+v, want a string that states the joining rule", prop)
		}
		if !strings.Contains(def.Description, "appendDescription") {
			t.Fatalf("update_ticket description does not point at appendDescription: %q", def.Description)
		}
		return
	}
	t.Fatal("no update_ticket tool")
}

func mustTicketID(t *testing.T, s *MCPServer, key string) string {
	t.Helper()
	id, err := s.store.ResolveTicketID(key)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
