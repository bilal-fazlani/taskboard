package mcp

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// newTestServer returns an MCPServer backed by a throwaway database in the
// test's temp dir. It must never call db.Open(), which resolves to the
// user's real database.
func newTestServer(t *testing.T) *MCPServer {
	t.Helper()
	database, err := db.OpenAt(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewServer(db.NewStore(database))
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling args: %v", err)
	}
	return data
}

func TestCreateLabelTool(t *testing.T) {
	s := newTestServer(t)

	result, err := s.callTool("create_label", mustJSON(t, map[string]any{
		"name":  "bug",
		"color": "#FF0000",
	}))
	if err != nil {
		t.Fatalf("create_label: %v", err)
	}
	l, ok := result.(*models.Label)
	if !ok {
		t.Fatalf("result type = %T, want *models.Label", result)
	}
	if l.Name != "bug" || l.Color != "#FF0000" {
		t.Fatalf("created label = %+v, want name=bug color=#FF0000", l)
	}

	// Omitted color falls back to the standard default rather than being
	// written as an empty string.
	result, err = s.callTool("create_label", mustJSON(t, map[string]any{"name": "chore"}))
	if err != nil {
		t.Fatalf("create_label without color: %v", err)
	}
	l = result.(*models.Label)
	if l.Color != db.DefaultLabelColor {
		t.Fatalf("color = %q, want default %q", l.Color, db.DefaultLabelColor)
	}

	// A blank name is rejected before it reaches the store.
	if _, err := s.callTool("create_label", mustJSON(t, map[string]any{"name": "  "})); err == nil {
		t.Fatal("expected an error for a blank name")
	}
}

func TestUpdateLabelToolResolvesIDOrExactName(t *testing.T) {
	s := newTestServer(t)

	created, err := s.callTool("create_label", mustJSON(t, map[string]any{"name": "bug", "color": "#FF0000"}))
	if err != nil {
		t.Fatalf("create_label: %v", err)
	}
	label := created.(*models.Label)

	// Update by id.
	newColor := "#00FF00"
	result, err := s.callTool("update_label", mustJSON(t, map[string]any{
		"id":    label.ID,
		"color": newColor,
	}))
	if err != nil {
		t.Fatalf("update_label by id: %v", err)
	}
	updated := result.(*models.Label)
	if updated.Color != newColor || updated.Name != "bug" {
		t.Fatalf("updated = %+v, want name=bug color=%s", updated, newColor)
	}

	// Update by exact name, case-insensitive, renaming it. The rename must
	// keep the label attached to every ticket that carries it, so this is
	// checked against the ticket, not just the label response.
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	tk, err := s.store.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Tagged", Labels: []string{"bug"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	newName := "defect"
	result, err = s.callTool("update_label", mustJSON(t, map[string]any{
		"id":   "BUG", // exact name, different case
		"name": newName,
	}))
	if err != nil {
		t.Fatalf("update_label by name: %v", err)
	}
	updated = result.(*models.Label)
	if updated.ID != label.ID || updated.Name != newName {
		t.Fatalf("updated = %+v, want id=%s name=%s", updated, label.ID, newName)
	}

	got, err := s.store.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != newName {
		t.Fatalf("ticket labels = %+v, want a single %q label after the rename", got.Labels, newName)
	}

	// A reference matching nothing is a clear error, not a silent no-op.
	if _, err := s.callTool("update_label", mustJSON(t, map[string]any{"id": "does-not-exist", "name": "x"})); err == nil {
		t.Fatal("expected an error for an unresolvable label reference")
	}

	// A missing id is also an error.
	if _, err := s.callTool("update_label", mustJSON(t, map[string]any{"name": "x"})); err == nil {
		t.Fatal("expected an error when id is omitted")
	}
}

func TestUpdateLabelToolRejectsNothingToUpdate(t *testing.T) {
	s := newTestServer(t)

	created, err := s.callTool("create_label", mustJSON(t, map[string]any{"name": "bug", "color": "#FF0000"}))
	if err != nil {
		t.Fatalf("create_label: %v", err)
	}
	label := created.(*models.Label)

	// Neither name nor color given: reject before even resolving the id, so
	// this never silently succeeds as a no-op.
	if _, err := s.callTool("update_label", mustJSON(t, map[string]any{"id": label.ID})); err == nil {
		t.Fatal("expected an error when neither name nor color is given")
	}
}

func TestUpdateLabelToolValidatesNameAndColor(t *testing.T) {
	s := newTestServer(t)

	created, err := s.callTool("create_label", mustJSON(t, map[string]any{"name": "bug", "color": "#FF0000"}))
	if err != nil {
		t.Fatalf("create_label: %v", err)
	}
	label := created.(*models.Label)

	// {"id": "x", "name": "", "color": ""} must not blank either field: a
	// blank name is rejected, and the label is left untouched.
	if _, err := s.callTool("update_label", mustJSON(t, map[string]any{
		"id":    label.ID,
		"name":  "",
		"color": "",
	})); err == nil {
		t.Fatal("expected an error for a blank name")
	}

	labels, err := s.store.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "bug" || labels[0].Color != "#FF0000" {
		t.Fatalf("labels = %+v, want unchanged [bug #FF0000]", labels)
	}

	// A blank color alone (valid name given) is treated as "not given": the
	// existing color survives instead of being blanked.
	result, err := s.callTool("update_label", mustJSON(t, map[string]any{
		"id":    label.ID,
		"name":  "defect",
		"color": "  ",
	}))
	if err != nil {
		t.Fatalf("update_label with blank color: %v", err)
	}
	updated := result.(*models.Label)
	if updated.Name != "defect" || updated.Color != "#FF0000" {
		t.Fatalf("updated = %+v, want name=defect color=#FF0000 (unchanged)", updated)
	}
}

func TestDeleteLabelToolReportsDetachedCount(t *testing.T) {
	s := newTestServer(t)

	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := s.store.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "One", Labels: []string{"bug"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.store.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Two", Labels: []string{"bug"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	// Delete by exact name (case-insensitive), not id.
	result, err := s.callTool("delete_label", mustJSON(t, map[string]any{"id": "Bug"}))
	if err != nil {
		t.Fatalf("delete_label: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if m["deleted"] != true {
		t.Fatalf("deleted = %v, want true", m["deleted"])
	}
	if m["detachedFromTickets"] != 2 {
		t.Fatalf("detachedFromTickets = %v, want 2", m["detachedFromTickets"])
	}

	labels, err := s.store.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("labels = %d, want 0 after delete", len(labels))
	}

	// An unresolvable reference is an error, not a silent no-op.
	if _, err := s.callTool("delete_label", mustJSON(t, map[string]any{"id": "does-not-exist"})); err == nil {
		t.Fatal("expected an error for an unresolvable label reference")
	}
}

// Every ticket an agent gets back carries the URL that opens it in the web UI,
// so the agent can print a link instead of a bare key.
func TestTicketToolsIncludeTheTicketURL(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://localhost:3013")
	s := newTestServer(t)

	project, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Agent control plane", Prefix: "ACP"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	created, err := s.callTool("create_ticket", mustJSON(t, map[string]any{
		"projectId": project.ID,
		"title":     "URL-addressable tickets",
	}))
	if err != nil {
		t.Fatalf("create_ticket: %v", err)
	}
	ticket := created.(*models.Ticket)
	want := "http://localhost:3013/?ticket=" + ticket.DisplayKey()
	if ticket.URL != want {
		t.Fatalf("create_ticket URL = %q, want %q", ticket.URL, want)
	}

	got, err := s.callTool("get_ticket", mustJSON(t, map[string]any{"id": ticket.ID}))
	if err != nil {
		t.Fatalf("get_ticket: %v", err)
	}
	if url := got.(*models.Ticket).URL; url != want {
		t.Fatalf("get_ticket URL = %q, want %q", url, want)
	}

	updated, err := s.callTool("update_ticket", mustJSON(t, map[string]any{
		"id":       ticket.ID,
		"priority": "high",
	}))
	if err != nil {
		t.Fatalf("update_ticket: %v", err)
	}
	if url := updated.(*models.Ticket).URL; url != want {
		t.Fatalf("update_ticket URL = %q, want %q", url, want)
	}

	listed, err := s.callTool("list_tickets", mustJSON(t, map[string]any{}))
	if err != nil {
		t.Fatalf("list_tickets: %v", err)
	}
	tickets := listed.([]models.Ticket)
	if len(tickets) != 1 || tickets[0].URL != want {
		t.Fatalf("list_tickets URLs = %+v, want one %q", tickets, want)
	}

	// The URL travels as a `url` field beside the other ticket fields.
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshaling get_ticket result: %v", err)
	}
	var shape map[string]any
	if err := json.Unmarshal(data, &shape); err != nil {
		t.Fatalf("unmarshaling get_ticket result: %v", err)
	}
	if shape["url"] != want {
		t.Fatalf("serialized url = %v, want %q", shape["url"], want)
	}
}

// A tool that returns no ticket must not start returning an empty one.
func TestGetTicketToolStillReportsNotFound(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.callTool("get_ticket", mustJSON(t, map[string]any{"id": "nope"})); err == nil {
		t.Fatal("expected an error for a ticket that does not exist")
	}
}

// get_ticket, update_ticket, move_ticket and delete_ticket must accept a
// display key (case-insensitive), the same as their ULID id, and
// create_ticket/list_tickets/get_board must accept a project prefix.
func TestTicketToolsAcceptDisplayKeysAndProjectPrefix(t *testing.T) {
	s := newTestServer(t)

	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// create_ticket accepts a project prefix, case-insensitively.
	created, err := s.callTool("create_ticket", mustJSON(t, map[string]any{
		"projectId": "bill",
		"title":     "Invoice",
	}))
	if err != nil {
		t.Fatalf("create_ticket with project prefix: %v", err)
	}
	ticket := created.(*models.Ticket)
	if ticket.DisplayKey() != "BILL-1" {
		t.Fatalf("display key = %q, want BILL-1", ticket.DisplayKey())
	}

	// get_ticket by display key, case-insensitively.
	got, err := s.callTool("get_ticket", mustJSON(t, map[string]any{"id": "bill-1"}))
	if err != nil {
		t.Fatalf("get_ticket bill-1: %v", err)
	}
	if got.(*models.Ticket).ID != ticket.ID {
		t.Fatalf("get_ticket by key resolved to a different ticket")
	}

	// update_ticket by display key.
	updated, err := s.callTool("update_ticket", mustJSON(t, map[string]any{
		"id":       "BILL-1",
		"priority": "high",
	}))
	if err != nil {
		t.Fatalf("update_ticket BILL-1: %v", err)
	}
	if updated.(*models.Ticket).Priority != "high" {
		t.Fatalf("update_ticket by key did not apply: %+v", updated)
	}

	// list_tickets and get_board filter by project prefix.
	listed, err := s.callTool("list_tickets", mustJSON(t, map[string]any{"projectId": "bill"}))
	if err != nil {
		t.Fatalf("list_tickets projectId=bill: %v", err)
	}
	if tickets := listed.([]models.Ticket); len(tickets) != 1 {
		t.Fatalf("list_tickets projectId=bill = %+v, want one ticket", tickets)
	}

	board, err := s.callTool("get_board", mustJSON(t, map[string]any{"projectId": "bill"}))
	if err != nil {
		t.Fatalf("get_board projectId=bill: %v", err)
	}
	found := false
	for _, col := range board.(*models.Board).Columns {
		found = found || len(col.Tickets) > 0
	}
	if !found {
		t.Fatalf("get_board projectId=bill found no tickets: %+v", board)
	}

	// create_subtask and batch_create_subtasks by display key.
	if _, err := s.callTool("create_subtask", mustJSON(t, map[string]any{
		"ticketId": "bill-1",
		"title":    "Step one",
	})); err != nil {
		t.Fatalf("create_subtask bill-1: %v", err)
	}
	if _, err := s.callTool("batch_create_subtasks", mustJSON(t, map[string]any{
		"ticketId": "bill-1",
		"subtasks": []map[string]any{{"title": "Step two"}},
	})); err != nil {
		t.Fatalf("batch_create_subtasks bill-1: %v", err)
	}
	final, err := s.store.GetTicket(ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if len(final.Subtasks) != 2 {
		t.Fatalf("subtasks = %+v, want 2 added by display key", final.Subtasks)
	}

	// move_ticket by display key.
	if _, err := s.callTool("move_ticket", mustJSON(t, map[string]any{
		"id":     "BILL-1",
		"status": "in_progress",
	})); err != nil {
		t.Fatalf("move_ticket BILL-1: %v", err)
	}

	// delete_ticket by display key.
	if _, err := s.callTool("delete_ticket", mustJSON(t, map[string]any{"id": "BILL-1"})); err != nil {
		t.Fatalf("delete_ticket BILL-1: %v", err)
	}
	if t2, err := s.store.GetTicket(ticket.ID); err != nil || t2 != nil {
		t.Fatalf("ticket still present after delete_ticket by key: %+v, err=%v", t2, err)
	}
}

// An unresolvable ticket key or project must produce a clear error, not a
// raw constraint failure or a silently ignored argument.
func TestTicketToolsRejectUnresolvableKeyOrProject(t *testing.T) {
	s := newTestServer(t)

	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	if _, err := s.callTool("create_ticket", mustJSON(t, map[string]any{
		"projectId": "NOPE",
		"title":     "Invoice",
	})); err == nil {
		t.Fatal("expected an error for an unknown project prefix")
	}

	for _, tool := range []string{"update_ticket", "move_ticket", "delete_ticket"} {
		args := map[string]any{"id": "BILL-99"}
		if tool == "move_ticket" {
			args["status"] = "in_progress"
		}
		if _, err := s.callTool(tool, mustJSON(t, args)); err == nil {
			t.Fatalf("%s: expected an error for an unresolvable display key", tool)
		}
	}

	if _, err := s.callTool("create_subtask", mustJSON(t, map[string]any{
		"ticketId": "BILL-99",
		"title":    "Step",
	})); err == nil {
		t.Fatal("create_subtask: expected an error for an unresolvable display key")
	}
	if _, err := s.callTool("batch_create_subtasks", mustJSON(t, map[string]any{
		"ticketId": "BILL-99",
		"subtasks": []map[string]any{{"title": "Step"}},
	})); err == nil {
		t.Fatal("batch_create_subtasks: expected an error for an unresolvable display key")
	}
}

// get_project, update_project and delete_project must accept a project
// prefix (case-insensitively) the same way ResolveProjectRef does.
func TestProjectToolsAcceptPrefix(t *testing.T) {
	s := newTestServer(t)

	project, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := s.callTool("get_project", mustJSON(t, map[string]any{"id": "bill"}))
	if err != nil {
		t.Fatalf("get_project bill: %v", err)
	}
	if got.(*models.Project).ID != project.ID {
		t.Fatalf("get_project by prefix resolved to a different project")
	}

	updated, err := s.callTool("update_project", mustJSON(t, map[string]any{
		"id":   "bill",
		"name": "Billing Renamed",
	}))
	if err != nil {
		t.Fatalf("update_project bill: %v", err)
	}
	if updated.(*models.Project).Name != "Billing Renamed" {
		t.Fatalf("update_project by prefix did not apply: %+v", updated)
	}

	if _, err := s.callTool("delete_project", mustJSON(t, map[string]any{"id": "BILL"})); err != nil {
		t.Fatalf("delete_project BILL: %v", err)
	}
	if p, err := s.store.GetProject(project.ID); err != nil || p != nil {
		t.Fatalf("project still present after delete_project by prefix: %+v, err=%v", p, err)
	}
}

// An unresolvable project prefix must give a clear error, not a silent
// no-op or a nil-pointer result.
func TestProjectToolsRejectUnknownPrefix(t *testing.T) {
	s := newTestServer(t)

	if _, err := s.callTool("get_project", mustJSON(t, map[string]any{"id": "NOPE"})); err == nil {
		t.Fatal("get_project: expected an error for an unknown project prefix")
	}
	if _, err := s.callTool("update_project", mustJSON(t, map[string]any{
		"id":   "NOPE",
		"name": "Anything",
	})); err == nil {
		t.Fatal("update_project: expected an error for an unknown project prefix")
	}
	if _, err := s.callTool("delete_project", mustJSON(t, map[string]any{"id": "NOPE"})); err == nil {
		t.Fatal("delete_project: expected an error for an unknown project prefix")
	}
}
