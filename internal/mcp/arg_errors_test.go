package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// argErrorFixture holds ids for one project, epic, label, ticket, subtask
// and document that the wrongly typed argument table test below sends
// alongside a mistyped field, so each call is well-formed except for the one
// field under test.
type argErrorFixture struct {
	s          *MCPServer
	projectID  string
	epicID     string
	labelID    string
	ticketID   string
	subtaskID  string
	documentID string
}

func newArgErrorFixture(t *testing.T) *argErrorFixture {
	t.Helper()
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
	subtask, err := s.store.AddSubtask(ticket.ID, models.CreateSubtaskRequest{Title: "Step one"})
	if err != nil {
		t.Fatalf("AddSubtask: %v", err)
	}
	doc, err := s.store.CreateDocument(models.CreateDocumentRequest{TicketID: ticket.ID, Name: "Notes", Content: "hello"})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	return &argErrorFixture{
		s:          s,
		projectID:  project.ID,
		epicID:     epic.ID,
		labelID:    label.ID,
		ticketID:   ticket.ID,
		subtaskID:  subtask.ID,
		documentID: doc.ID,
	}
}

// snapshot dumps every piece of state a tool in the table below could
// possibly touch: every project (including the fixture project's own read,
// which is the only one that carries its agentInstructions text), every
// ticket (with its subtasks, labels and epic), the fixture ticket's status
// history, every label, the fixture project's epics, and the fixture
// ticket's documents, each with its content. Comparing two snapshots
// byte-for-byte after a rejected call is how the test checks "no change to
// the data" — not just that an error came back.
func (f *argErrorFixture) snapshot(t *testing.T) string {
	t.Helper()
	s := f.s

	projects, err := s.store.ListProjects("")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	project, err := s.store.GetProject(f.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	tickets, err := s.store.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	labels, err := s.store.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	epics, err := s.store.ListEpics(f.projectID)
	if err != nil {
		t.Fatalf("ListEpics: %v", err)
	}
	ticket, err := s.store.GetTicket(f.ticketID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	history, err := s.store.ListStatusChanges(f.ticketID)
	if err != nil {
		t.Fatalf("ListStatusChanges: %v", err)
	}
	docMetas, err := s.store.ListDocuments(db.DocumentOwner{TicketID: f.ticketID})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	docs := make([]*models.Document, len(docMetas))
	for i, meta := range docMetas {
		d, err := s.store.GetDocument(meta.ID)
		if err != nil {
			t.Fatalf("GetDocument: %v", err)
		}
		docs[i] = d
	}

	blob := struct {
		Projects  []models.Project
		Project   *models.Project
		Tickets   []models.Ticket
		Labels    []models.Label
		Epics     []models.Epic
		Ticket    *models.Ticket
		History   []models.StatusChange
		Documents []*models.Document
	}{projects, project, tickets, labels, epics, ticket, history, docs}

	data, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshaling snapshot: %v", err)
	}
	return string(data)
}

// TestToolHandlersRejectWronglyTypedArguments sends every MCP tool handler
// in internal/mcp/mcp.go and internal/mcp/documents.go a call that is
// well-formed except for one argument sent as the wrong JSON type (a number
// where a string belongs, or a string where a list belongs). Before ACP-119
// each handler dropped json.Unmarshal's error, so the mistyped field silently
// decoded to its zero value and the call went on to report success. Every
// handler must now return an error, and the store must be left exactly as it
// was — checked with a full snapshot, not just the presence of an error.
//
// list_labels takes no arguments, so it has no wrongly typed argument to
// send and is not in this table; toggle_subtask is included even though
// ACP-83 already covers it (TestToggleSubtaskToolRejectsNonBooleanCompleted),
// so this table alone demonstrates every handler is covered.
func TestToolHandlersRejectWronglyTypedArguments(t *testing.T) {
	f := newArgErrorFixture(t)

	tests := []struct {
		tool string
		args map[string]any
	}{
		{"list_projects", map[string]any{"status": 1}},
		{"get_project", map[string]any{"id": 1}},
		{"create_project", map[string]any{"name": 1, "prefix": "X"}},
		{"update_project", map[string]any{"id": f.projectID, "agentInstructions": 1}},
		{"delete_project", map[string]any{"id": 1}},
		{"list_epics", map[string]any{"projectId": 1}},
		{"create_epic", map[string]any{"projectId": f.projectID, "name": 1}},
		{"update_epic", map[string]any{"id": f.epicID, "name": 1}},
		{"delete_epic", map[string]any{"id": f.epicID, "project": 1}},
		{"create_label", map[string]any{"name": 1}},
		{"update_label", map[string]any{"id": f.labelID, "name": 1}},
		{"delete_label", map[string]any{"id": 1}},
		{"list_tickets", map[string]any{"projectId": 1}},
		{"get_ticket", map[string]any{"id": 1}},
		{"create_ticket", map[string]any{"projectId": f.projectID, "title": 1}},
		{"update_ticket", map[string]any{"id": f.ticketID, "priority": 1}},
		{"move_ticket", map[string]any{"id": f.ticketID, "status": 1}},
		{"delete_ticket", map[string]any{"id": 1}},
		{"get_board", map[string]any{"projectId": 1}},
		{"create_subtask", map[string]any{"ticketId": f.ticketID, "title": 1}},
		{"batch_create_subtasks", map[string]any{"ticketId": f.ticketID, "subtasks": "not-a-list"}},
		{"delete_subtask", map[string]any{"id": 1}},
		{"toggle_subtask", map[string]any{"id": f.subtaskID, "completed": "true"}},
		{"list_documents", map[string]any{"ticket": 1}},
		{"get_document", map[string]any{"id": 1}},
		{"create_document", map[string]any{"ticket": f.ticketID, "name": 1}},
		{"update_document", map[string]any{"id": f.documentID, "name": 1}},
		{"delete_document", map[string]any{"id": 1}},
	}

	seen := make(map[string]bool, len(tests))
	for _, tc := range tests {
		seen[tc.tool] = true
	}
	for _, def := range f.s.toolDefinitions() {
		if !seen[def.Name] && def.Name != "list_labels" {
			t.Errorf("tool %q has no entry in this table", def.Name)
		}
	}

	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			before := f.snapshot(t)

			_, err := f.s.callTool(tc.tool, mustJSON(t, tc.args))
			if err == nil {
				t.Fatalf("%s: expected an error for a wrongly typed argument, got none", tc.tool)
			}
			// The error must come from the decode step itself, not from some
			// unrelated validation the mistyped field's zero value happens to
			// trip (e.g. "id is required" or "name is required"): on the
			// pre-fix code, 14 of these 28 cases still error that way even
			// though the field never actually decoded.
			if !strings.Contains(err.Error(), "invalid arguments") {
				t.Fatalf("%s: error = %q, want it to contain %q", tc.tool, err.Error(), "invalid arguments")
			}

			after := f.snapshot(t)
			if before != after {
				t.Fatalf("%s: store state changed after a rejected call\nbefore: %s\nafter:  %s", tc.tool, before, after)
			}
		})
	}
}

// TestToolCallsWithNoArgumentsStillSucceed drives list_projects, list_tickets
// and get_board through handleToolCall (not callTool directly) on a
// tools/call request that leaves out `arguments` entirely, the way a real
// client is allowed to for a tool that takes none: MCP makes `arguments`
// optional. That leaves params.Arguments nil, so decodeArgs must treat a nil
// (or empty) args the same as "{}" and decode to the handler's zero-valued
// filter, exactly as these three calls always behaved, rather than failing
// every argument-less call with "unexpected end of JSON input".
func TestToolCallsWithNoArgumentsStillSucceed(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	for _, tool := range []string{"list_projects", "list_tickets", "get_board"} {
		t.Run(tool, func(t *testing.T) {
			params := mustJSON(t, map[string]any{"name": tool})
			resp := s.handleToolCall(jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
			if resp.Error != nil {
				t.Fatalf("%s: unexpected JSON-RPC error: %+v", tool, resp.Error)
			}
			result, ok := resp.Result.(map[string]any)
			if !ok {
				t.Fatalf("%s: result type = %T, want map[string]any", tool, resp.Result)
			}
			if isErr, _ := result["isError"].(bool); isErr {
				t.Fatalf("%s: call with no arguments failed: %+v", tool, result)
			}
		})
	}
}
