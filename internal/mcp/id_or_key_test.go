package mcp

import (
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// TestSingleItemToolsAcceptKeyAlias is ACP-182: every MCP tool that looks up
// or changes a single ticket, project, epic, label or document by id must
// also accept the exact same value under key, so an agent that reaches for
// the field name its own JSON responses already use for the human-readable
// identifier (a ticket's key, e.g. ACP-117) does not get a schema-validation
// error for guessing the "wrong" argument name. Each case below sends the
// tool key instead of id (never both) and checks the call succeeds and
// reaches the intended record.
func TestSingleItemToolsAcceptKeyAlias(t *testing.T) {
	s := newTestServer(t)

	project, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	epic, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: project.ID, Name: "Launch"})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	label, err := s.store.CreateLabel(models.CreateLabelRequest{Name: "bug", Color: "#FF0000"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	ticket, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: project.ID, Title: "Invoice"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ticketKey := ticket.DisplayKey()
	if ticketKey != "BILL-1" {
		t.Fatalf("ticket display key = %q, want BILL-1", ticketKey)
	}
	doc, err := s.store.CreateDocument(models.CreateDocumentRequest{TicketID: ticket.ID, Name: "Notes", Content: "hello"})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	t.Run("get_ticket", func(t *testing.T) {
		result, err := s.callTool("get_ticket", mustJSON(t, map[string]any{"key": ticketKey}))
		if err != nil {
			t.Fatalf("get_ticket by key: %v", err)
		}
		if got := result.(*models.Ticket).ID; got != ticket.ID {
			t.Fatalf("get_ticket by key returned id %q, want %q", got, ticket.ID)
		}
	})

	t.Run("update_ticket", func(t *testing.T) {
		result, err := s.callTool("update_ticket", mustJSON(t, map[string]any{"key": ticketKey, "priority": "high", "full": true}))
		if err != nil {
			t.Fatalf("update_ticket by key: %v", err)
		}
		if got := result.(*models.Ticket).Priority; got != "high" {
			t.Fatalf("priority = %q, want high", got)
		}
	})

	t.Run("move_ticket", func(t *testing.T) {
		result, err := s.callTool("move_ticket", mustJSON(t, map[string]any{"key": ticketKey, "status": "in_progress", "full": true}))
		if err != nil {
			t.Fatalf("move_ticket by key: %v", err)
		}
		if got := result.(*models.Ticket).Status; got != "in_progress" {
			t.Fatalf("status = %q, want in_progress", got)
		}
	})

	t.Run("get_project", func(t *testing.T) {
		result, err := s.callTool("get_project", mustJSON(t, map[string]any{"key": project.Prefix}))
		if err != nil {
			t.Fatalf("get_project by key: %v", err)
		}
		if got := result.(*models.Project).ID; got != project.ID {
			t.Fatalf("get_project by key returned id %q, want %q", got, project.ID)
		}
	})

	t.Run("update_project", func(t *testing.T) {
		result, err := s.callTool("update_project", mustJSON(t, map[string]any{"key": project.Prefix, "name": "Billing Co", "full": true}))
		if err != nil {
			t.Fatalf("update_project by key: %v", err)
		}
		if got := result.(*models.Project).Name; got != "Billing Co" {
			t.Fatalf("name = %q, want Billing Co", got)
		}
	})

	t.Run("update_epic", func(t *testing.T) {
		result, err := s.callTool("update_epic", mustJSON(t, map[string]any{
			"key": epic.Name, "project": project.Prefix, "description": "Longer.", "full": true,
		}))
		if err != nil {
			t.Fatalf("update_epic by key: %v", err)
		}
		if got := result.(*models.Epic).Description; got != "Longer." {
			t.Fatalf("description = %q, want Longer.", got)
		}
	})

	t.Run("update_label", func(t *testing.T) {
		result, err := s.callTool("update_label", mustJSON(t, map[string]any{"key": label.Name, "color": "#00FF00", "full": true}))
		if err != nil {
			t.Fatalf("update_label by key: %v", err)
		}
		if got := result.(*models.Label).Color; got != "#00FF00" {
			t.Fatalf("color = %q, want #00FF00", got)
		}
	})

	t.Run("get_document", func(t *testing.T) {
		result, err := s.callTool("get_document", mustJSON(t, map[string]any{"key": doc.Name, "ticket": ticketKey}))
		if err != nil {
			t.Fatalf("get_document by key: %v", err)
		}
		if got := result.(*models.Document).ID; got != doc.ID {
			t.Fatalf("get_document by key returned id %q, want %q", got, doc.ID)
		}
	})

	t.Run("update_document", func(t *testing.T) {
		result, err := s.callTool("update_document", mustJSON(t, map[string]any{
			"key": doc.Name, "ticket": ticketKey, "content": "new content", "full": true,
		}))
		if err != nil {
			t.Fatalf("update_document by key: %v", err)
		}
		if got := result.(*models.Document).Content; got != "new content" {
			t.Fatalf("content = %q, want %q", got, "new content")
		}
	})

	// Deletions consume the record they act on, so each gets its own
	// throwaway fixture rather than sharing the ones above.
	t.Run("delete_document", func(t *testing.T) {
		d, err := s.store.CreateDocument(models.CreateDocumentRequest{TicketID: ticket.ID, Name: "Scratch", Content: "x"})
		if err != nil {
			t.Fatalf("CreateDocument: %v", err)
		}
		if _, err := s.callTool("delete_document", mustJSON(t, map[string]any{"key": d.Name, "ticket": ticketKey})); err != nil {
			t.Fatalf("delete_document by key: %v", err)
		}
		if got, err := s.store.GetDocument(d.ID); err != nil || got != nil {
			t.Fatalf("document survived delete by key: %+v, err=%v", got, err)
		}
	})

	t.Run("delete_label", func(t *testing.T) {
		l, err := s.store.CreateLabel(models.CreateLabelRequest{Name: "chore", Color: "#123456"})
		if err != nil {
			t.Fatalf("CreateLabel: %v", err)
		}
		if _, err := s.callTool("delete_label", mustJSON(t, map[string]any{"key": l.Name})); err != nil {
			t.Fatalf("delete_label by key: %v", err)
		}
		labels, err := s.store.ListLabels()
		if err != nil {
			t.Fatal(err)
		}
		for _, got := range labels {
			if got.ID == l.ID {
				t.Fatalf("label survived delete by key: %+v", got)
			}
		}
	})

	t.Run("delete_epic", func(t *testing.T) {
		e, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: project.ID, Name: "Scratch epic"})
		if err != nil {
			t.Fatalf("CreateEpic: %v", err)
		}
		if _, err := s.callTool("delete_epic", mustJSON(t, map[string]any{"key": e.Name, "project": project.Prefix})); err != nil {
			t.Fatalf("delete_epic by key: %v", err)
		}
		if got, err := s.store.GetEpic(e.ID); err != nil || got != nil {
			t.Fatalf("epic survived delete by key: %+v, err=%v", got, err)
		}
	})

	t.Run("delete_ticket", func(t *testing.T) {
		tk, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: project.ID, Title: "Scratch"})
		if err != nil {
			t.Fatalf("CreateTicket: %v", err)
		}
		if _, err := s.callTool("delete_ticket", mustJSON(t, map[string]any{"key": tk.DisplayKey()})); err != nil {
			t.Fatalf("delete_ticket by key: %v", err)
		}
		if got, err := s.store.GetTicket(tk.ID); err != nil || got != nil {
			t.Fatalf("ticket survived delete by key: %+v, err=%v", got, err)
		}
	})

	t.Run("delete_project", func(t *testing.T) {
		p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Scratch", Prefix: "SCR"})
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if _, err := s.callTool("delete_project", mustJSON(t, map[string]any{"key": p.Prefix})); err != nil {
			t.Fatalf("delete_project by key: %v", err)
		}
		if got, err := s.store.GetProject(p.ID); err != nil || got != nil {
			t.Fatalf("project survived delete by key: %+v, err=%v", got, err)
		}
	})
}

// TestIDOrKeyRequiredError checks that omitting both id and key on an
// affected tool fails with a message naming both accepted argument names,
// rather than the pre-ACP-182 "id is required" that never mentioned key.
func TestIDOrKeyRequiredError(t *testing.T) {
	s := newTestServer(t)
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"get_ticket", nil},
		{"update_ticket", nil},
		{"move_ticket", map[string]any{"status": "in_progress"}},
		{"delete_ticket", nil},
		{"get_project", nil},
		{"update_project", nil},
		{"delete_project", nil},
		// update_label checks "nothing to update" before resolving id/key, so
		// it needs a field to update to reach the check under test.
		{"update_label", map[string]any{"color": "#00FF00"}},
		{"delete_label", nil},
		// update_epic and delete_epic need a project too, but must still fail
		// on the missing id/key first; update_epic likewise needs a field to
		// update to get past its own "nothing to update" check first.
		{"update_epic", map[string]any{"project": "BILL", "description": "x"}},
		{"delete_epic", map[string]any{"project": "BILL"}},
		{"get_document", nil},
		// update_document checks "nothing to update" before resolving id/key
		// too, so it needs a field to update to reach the check under test.
		{"update_document", map[string]any{"content": "x"}},
		{"delete_document", nil},
	} {
		args := tc.args
		if args == nil {
			args = map[string]any{}
		}
		if _, err := s.callTool(tc.tool, mustJSON(t, args)); err == nil || err.Error() != "id or key is required" {
			t.Fatalf("%s with neither id nor key: err = %v, want %q", tc.tool, err, "id or key is required")
		}
	}
}

// TestKeyWinsOverID checks idOrKeyArg.ref()'s documented precedence: when a
// call gives both id and key and they name different records, key is the one
// that is used, on both a read-only lookup (get_ticket) and a mutation
// (update_ticket). Each case points id at one ticket and key at another, and
// asserts the call acted on key's ticket, never id's.
func TestKeyWinsOverID(t *testing.T) {
	s := newTestServer(t)

	project, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	idTicket, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: project.ID, Title: "By id"})
	if err != nil {
		t.Fatalf("CreateTicket idTicket: %v", err)
	}
	keyTicket, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: project.ID, Title: "By key"})
	if err != nil {
		t.Fatalf("CreateTicket keyTicket: %v", err)
	}
	if idTicket.ID == keyTicket.ID || idTicket.DisplayKey() == keyTicket.DisplayKey() {
		t.Fatalf("fixture tickets must be distinct: %+v, %+v", idTicket, keyTicket)
	}

	t.Run("get_ticket", func(t *testing.T) {
		result, err := s.callTool("get_ticket", mustJSON(t, map[string]any{
			"id": idTicket.ID, "key": keyTicket.DisplayKey(),
		}))
		if err != nil {
			t.Fatalf("get_ticket with id and key: %v", err)
		}
		got := result.(*models.Ticket)
		if got.ID != keyTicket.ID {
			t.Fatalf("get_ticket with id=%s key=%s returned id %q, want key's ticket %q",
				idTicket.ID, keyTicket.DisplayKey(), got.ID, keyTicket.ID)
		}
	})

	t.Run("update_ticket", func(t *testing.T) {
		result, err := s.callTool("update_ticket", mustJSON(t, map[string]any{
			"id": idTicket.ID, "key": keyTicket.DisplayKey(), "priority": "urgent", "full": true,
		}))
		if err != nil {
			t.Fatalf("update_ticket with id and key: %v", err)
		}
		got := result.(*models.Ticket)
		if got.ID != keyTicket.ID {
			t.Fatalf("update_ticket with id=%s key=%s changed ticket %q, want key's ticket %q",
				idTicket.ID, keyTicket.DisplayKey(), got.ID, keyTicket.ID)
		}
		if got.Priority != "urgent" {
			t.Fatalf("key's ticket priority = %q, want urgent", got.Priority)
		}

		unchanged, err := s.store.GetTicket(idTicket.ID)
		if err != nil {
			t.Fatalf("GetTicket idTicket: %v", err)
		}
		if unchanged.Priority == "urgent" {
			t.Fatalf("id's ticket was changed too: %+v", unchanged)
		}
	})
}

// TestSubtaskToolsRejectWhitespaceOnlyTicketIDWithTheirOwnError checks that a
// whitespace-only ticketId on create_subtask and batch_create_subtasks fails
// with the same "ticketId ... required" message an empty ticketId gets,
// rather than falling through to resolveTicketRefOrError's "id or key is
// required" — a message naming arguments these two tools don't even have,
// since they take ticketId, not id or key.
func TestSubtaskToolsRejectWhitespaceOnlyTicketIDWithTheirOwnError(t *testing.T) {
	s := newTestServer(t)

	result, err := s.callTool("create_subtask", mustJSON(t, map[string]any{"ticketId": "   ", "title": "Step one"}))
	if err == nil {
		t.Fatalf("create_subtask with whitespace-only ticketId: expected an error, got %+v", result)
	}
	if want := "ticketId and title are required"; err.Error() != want {
		t.Fatalf("create_subtask with whitespace-only ticketId: err = %q, want %q", err.Error(), want)
	}

	result, err = s.callTool("batch_create_subtasks", mustJSON(t, map[string]any{
		"ticketId": "\t", "subtasks": []map[string]any{{"title": "Step one"}},
	}))
	if err == nil {
		t.Fatalf("batch_create_subtasks with whitespace-only ticketId: expected an error, got %+v", result)
	}
	if want := "ticketId and at least one subtask are required"; err.Error() != want {
		t.Fatalf("batch_create_subtasks with whitespace-only ticketId: err = %q, want %q", err.Error(), want)
	}
}
