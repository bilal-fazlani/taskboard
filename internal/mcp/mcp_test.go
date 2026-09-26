package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
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

	moved, err := s.callTool("move_ticket", mustJSON(t, map[string]any{
		"id":     ticket.ID,
		"status": "in_progress",
	}))
	if err != nil {
		t.Fatalf("move_ticket: %v", err)
	}
	if url := moved.(*models.Ticket).URL; url != want {
		t.Fatalf("move_ticket URL = %q, want %q", url, want)
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

// move_ticket on an unknown key must report the same clear error
// update_ticket does for the same key, not a silently empty result.
func TestMoveTicketToolNotFoundMatchesUpdateTicket(t *testing.T) {
	s := newTestServer(t)

	_, updateErr := s.callTool("update_ticket", mustJSON(t, map[string]any{"id": "nope", "priority": "high"}))
	if updateErr == nil {
		t.Fatal("update_ticket: expected an error for a ticket that does not exist")
	}

	_, moveErr := s.callTool("move_ticket", mustJSON(t, map[string]any{"id": "nope", "status": "in_progress"}))
	if moveErr == nil {
		t.Fatal("move_ticket: expected an error for a ticket that does not exist")
	}

	if moveErr.Error() != updateErr.Error() {
		t.Fatalf("move_ticket error = %q, want the same as update_ticket %q", moveErr, updateErr)
	}
}

// toggle_subtask omitting `completed` must keep flipping the current state,
// exactly as it always has, so existing callers keep working.
func TestToggleSubtaskToolOmittingCompletedStillFlips(t *testing.T) {
	s := newTestServer(t)
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	ticket, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Invoice"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	st, err := s.store.AddSubtask(ticket.ID, models.CreateSubtaskRequest{Title: "Step one"})
	if err != nil {
		t.Fatalf("AddSubtask: %v", err)
	}
	if st.Completed {
		t.Fatalf("new subtask started completed")
	}

	result, err := s.callTool("toggle_subtask", mustJSON(t, map[string]any{"id": st.ID}))
	if err != nil {
		t.Fatalf("toggle_subtask (no completed): %v", err)
	}
	if !result.(*models.Subtask).Completed {
		t.Fatalf("first flip left completed = false, want true")
	}

	result, err = s.callTool("toggle_subtask", mustJSON(t, map[string]any{"id": st.ID}))
	if err != nil {
		t.Fatalf("toggle_subtask (no completed), second call: %v", err)
	}
	if result.(*models.Subtask).Completed {
		t.Fatalf("second flip left completed = true, want false")
	}
}

// Passing `completed` sets the subtask to that exact state, whichever way it
// currently sits, and a repeated call with the same value is a harmless no-op
// that still succeeds.
func TestToggleSubtaskToolWithCompletedSetsState(t *testing.T) {
	s := newTestServer(t)
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	ticket, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Invoice"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	st, err := s.store.AddSubtask(ticket.ID, models.CreateSubtaskRequest{Title: "Step one"})
	if err != nil {
		t.Fatalf("AddSubtask: %v", err)
	}

	// Setting it true from an already-false state.
	result, err := s.callTool("toggle_subtask", mustJSON(t, map[string]any{"id": st.ID, "completed": true}))
	if err != nil {
		t.Fatalf("toggle_subtask completed=true: %v", err)
	}
	if !result.(*models.Subtask).Completed {
		t.Fatalf("completed = false after setting true")
	}

	// Setting it true again is a no-op that still succeeds.
	result, err = s.callTool("toggle_subtask", mustJSON(t, map[string]any{"id": st.ID, "completed": true}))
	if err != nil {
		t.Fatalf("toggle_subtask completed=true (repeat): %v", err)
	}
	if !result.(*models.Subtask).Completed {
		t.Fatalf("completed = false after repeating true")
	}

	// Setting it false.
	result, err = s.callTool("toggle_subtask", mustJSON(t, map[string]any{"id": st.ID, "completed": false}))
	if err != nil {
		t.Fatalf("toggle_subtask completed=false: %v", err)
	}
	if result.(*models.Subtask).Completed {
		t.Fatalf("completed = true after setting false")
	}

	// Setting it false again is a no-op that still succeeds.
	if _, err := s.callTool("toggle_subtask", mustJSON(t, map[string]any{"id": st.ID, "completed": false})); err != nil {
		t.Fatalf("toggle_subtask completed=false (repeat): %v", err)
	}
}

// An unknown subtask id must fail clearly, not with a bare sql.ErrNoRows,
// when completed is given.
func TestToggleSubtaskToolWithCompletedReportsUnknownID(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.callTool("toggle_subtask", mustJSON(t, map[string]any{"id": "nope", "completed": true})); err == nil {
		t.Fatal("expected an error for a subtask that does not exist")
	} else if strings.Contains(err.Error(), "no rows") {
		t.Fatalf("error leaked the raw sql.ErrNoRows: %v", err)
	}
}

// delete_subtask on an unknown id must report a clear "subtask not found"
// error, matching the wording of the other delete tools since ACP-63,
// instead of reporting deleted: true for a subtask that was never there
// (ACP-121).
func TestDeleteSubtaskToolReportsUnknownID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.callTool("delete_subtask", mustJSON(t, map[string]any{"id": "nope"}))
	if err == nil {
		t.Fatal("expected an error for a subtask that does not exist")
	}
	if !strings.Contains(err.Error(), `subtask not found: "nope"`) {
		t.Fatalf("error = %q, want it to say the subtask was not found", err.Error())
	}
}

// A `completed` that is not a JSON boolean (e.g. the string "true") must be
// rejected as a tool error, not silently ignored: decoding it into *bool
// fails, and a dropped decode error would leave Completed nil, which falls
// through to a flip and reports success for a call that was actually
// malformed.
func TestToggleSubtaskToolRejectsNonBooleanCompleted(t *testing.T) {
	s := newTestServer(t)
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	ticket, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Invoice"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	st, err := s.store.AddSubtask(ticket.ID, models.CreateSubtaskRequest{Title: "Step one"})
	if err != nil {
		t.Fatalf("AddSubtask: %v", err)
	}

	if _, err := s.callTool("toggle_subtask", mustJSON(t, map[string]any{"id": st.ID, "completed": "true"})); err == nil {
		t.Fatal("expected an error for a non-boolean completed value")
	}

	final, err := s.store.GetTicket(ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if len(final.Subtasks) != 1 || final.Subtasks[0].Completed {
		t.Fatalf("subtask changed after a rejected call: %+v", final.Subtasks)
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

// list_epics returns each epic with its progress, and a noEpic summary for
// the project's tickets that carry no epic.
func TestListEpicsToolReturnsProgressAndNoEpicSummary(t *testing.T) {
	s := newTestServer(t)

	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	epic, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Invoicing"})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	epicName := epic.Name
	if _, err := s.store.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "In the epic", Epic: &epicName,
	}); err != nil {
		t.Fatalf("CreateTicket in epic: %v", err)
	}
	if _, err := s.store.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "No epic",
	}); err != nil {
		t.Fatalf("CreateTicket without epic: %v", err)
	}

	result, err := s.callTool("list_epics", mustJSON(t, map[string]any{"projectId": "bill"}))
	if err != nil {
		t.Fatalf("list_epics: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	epics, ok := m["epics"].([]models.Epic)
	if !ok || len(epics) != 1 {
		t.Fatalf("epics = %+v (%T), want one epic", m["epics"], m["epics"])
	}
	if epics[0].Name != "Invoicing" || epics[0].Total != 1 || epics[0].Counts["todo"] != 1 {
		t.Fatalf("epic progress = %+v, want total=1 counts[todo]=1", epics[0])
	}
	noEpic, ok := m["noEpic"].(*models.EpicProgress)
	if !ok {
		t.Fatalf("noEpic type = %T, want *models.EpicProgress", m["noEpic"])
	}
	if noEpic.Total != 1 || noEpic.Counts["todo"] != 1 {
		t.Fatalf("noEpic progress = %+v, want total=1 counts[todo]=1", noEpic)
	}

	// An unknown project is a clear error.
	if _, err := s.callTool("list_epics", mustJSON(t, map[string]any{"projectId": "NOPE"})); err == nil {
		t.Fatal("list_epics: expected an error for an unknown project")
	}

	// A missing projectId names the field, not the generic "id".
	_, err = s.callTool("list_epics", mustJSON(t, map[string]any{}))
	if err == nil || err.Error() != "projectId is required" {
		t.Fatalf("list_epics with no projectId: err = %v, want \"projectId is required\"", err)
	}
}

// create_epic, update_epic (by id, and by name+project) and delete_epic, plus
// the rules' error messages surfacing unchanged from the store.
func TestEpicTools(t *testing.T) {
	s := newTestServer(t)

	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Support", Prefix: "SUP"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	created, err := s.callTool("create_epic", mustJSON(t, map[string]any{
		"projectId":   "bill",
		"name":        "Invoicing",
		"description": "Bill customers",
	}))
	if err != nil {
		t.Fatalf("create_epic: %v", err)
	}
	epic, ok := created.(*models.Epic)
	if !ok {
		t.Fatalf("result type = %T, want *models.Epic", created)
	}
	if epic.Name != "Invoicing" || epic.Description != "Bill customers" || epic.ProjectID != p.ID {
		t.Fatalf("created epic = %+v", epic)
	}

	// A duplicate name (case-insensitive) is the store's own message,
	// unchanged.
	_, err = s.callTool("create_epic", mustJSON(t, map[string]any{
		"projectId": "bill",
		"name":      "invoicing",
	}))
	if err == nil || err.Error() != `This project already has an epic called "Invoicing".` {
		t.Fatalf("create_epic duplicate name: err = %v, want the store's duplicate-name message", err)
	}

	// A blank name is rejected with the store's message.
	_, err = s.callTool("create_epic", mustJSON(t, map[string]any{
		"projectId": "bill",
		"name":      "   ",
	}))
	if err == nil || err.Error() != "Enter a name" {
		t.Fatalf("create_epic blank name: err = %v, want \"Enter a name\"", err)
	}

	// "none" is reserved, with the store's message.
	_, err = s.callTool("create_epic", mustJSON(t, map[string]any{
		"projectId": "bill",
		"name":      "NONE",
	}))
	if err == nil || err.Error() != `"none" is reserved for tickets without an epic.` {
		t.Fatalf("create_epic reserved name: err = %v, want the store's reserved-name message", err)
	}

	// An id that names an epic in a different project gets its own message,
	// since the caller clearly meant that epic, just addressed from the
	// wrong project.
	_, err = s.callTool("update_epic", mustJSON(t, map[string]any{
		"id":      epic.ID,
		"project": "sup",
		"name":    "Nope",
	}))
	if err == nil || err.Error() != `Epic "Invoicing" belongs to another project.` {
		t.Fatalf("update_epic cross-project id: err = %v, want the store's \"belongs to another project\" message", err)
	}

	// update_epic by id.
	updated, err := s.callTool("update_epic", mustJSON(t, map[string]any{
		"id":          epic.ID,
		"description": "Bill customers on time",
	}))
	if err != nil {
		t.Fatalf("update_epic by id: %v", err)
	}
	if u := updated.(*models.Epic); u.Name != "Invoicing" || u.Description != "Bill customers on time" {
		t.Fatalf("updated epic = %+v", u)
	}

	// Neither name nor description given: reject before even resolving the
	// id, like update_label, so this never silently just bumps updatedAt.
	if _, err := s.callTool("update_epic", mustJSON(t, map[string]any{"id": epic.ID})); err == nil {
		t.Fatal("update_epic: expected an error when neither name nor description is given")
	}

	// update_epic by name together with project, renaming it.
	renamed, err := s.callTool("update_epic", mustJSON(t, map[string]any{
		"id":      "invoicing",
		"project": "bill",
		"name":    "Invoices",
	}))
	if err != nil {
		t.Fatalf("update_epic by name+project: %v", err)
	}
	if r := renamed.(*models.Epic); r.ID != epic.ID || r.Name != "Invoices" {
		t.Fatalf("renamed epic = %+v", r)
	}

	// A name without its project cannot be resolved: names are only unique
	// within a project.
	if _, err := s.callTool("update_epic", mustJSON(t, map[string]any{
		"id":   "Invoices",
		"name": "Nope",
	})); err == nil {
		t.Fatal("update_epic: expected an error for a name given without its project")
	}

	// An unresolvable id, with no project given, is a clear error, not a
	// silent no-op — this is what the GetEpic check in resolveEpicRefOrError
	// exists for.
	if _, err := s.callTool("update_epic", mustJSON(t, map[string]any{
		"id":   "does-not-exist",
		"name": "Nope",
	})); err == nil {
		t.Fatal("update_epic: expected an error for an unresolvable id")
	}

	// A ticket carries the epic before it's deleted.
	tk, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Send invoice"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	epicRef := "Invoices"
	if _, err := s.store.UpdateTicket(tk.ID, models.UpdateTicketRequest{Epic: &epicRef}); err != nil {
		t.Fatalf("UpdateTicket epic: %v", err)
	}

	// delete_epic by id alone (no project) succeeds: an id on its own is
	// enough to address an epic.
	scratch, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Scratch"})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	deletedByID, err := s.callTool("delete_epic", mustJSON(t, map[string]any{"id": scratch.ID}))
	if err != nil {
		t.Fatalf("delete_epic by id alone: %v", err)
	}
	if m := deletedByID.(map[string]any); m["deleted"] != true || m["clearedFromTickets"] != 0 {
		t.Fatalf("delete_epic by id alone = %+v, want deleted=true clearedFromTickets=0", m)
	}
	if e, err := s.store.GetEpic(scratch.ID); err != nil || e != nil {
		t.Fatalf("epic still present after delete_epic by id alone: %+v, err=%v", e, err)
	}

	// An unknown id with no project is a clear error, not a silent
	// {deleted:true, clearedFromTickets:0} — DeleteEpic itself reports
	// (0, nil) for an unknown id, so the tool must check first.
	if _, err := s.callTool("delete_epic", mustJSON(t, map[string]any{"id": "does-not-exist"})); err == nil {
		t.Fatal("delete_epic: expected an error for an unresolvable id with no project")
	}

	// delete_epic by name+project clears it from the ticket and reports the
	// count.
	deleted, err := s.callTool("delete_epic", mustJSON(t, map[string]any{
		"id":      "invoices",
		"project": "bill",
	}))
	if err != nil {
		t.Fatalf("delete_epic: %v", err)
	}
	dm, ok := deleted.(map[string]any)
	if !ok {
		t.Fatalf("delete_epic result type = %T, want map[string]any", deleted)
	}
	if dm["deleted"] != true || dm["clearedFromTickets"] != 1 {
		t.Fatalf("delete_epic result = %+v, want deleted=true clearedFromTickets=1", dm)
	}
	got, err := s.store.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.Epic != nil {
		t.Fatalf("ticket epic = %+v, want nil after its epic was deleted", got.Epic)
	}

	// The epic itself is gone, so a further delete resolves to nothing —
	// this time addressed by name+project, which reports the store's own
	// "no such epic" message.
	_, err = s.callTool("delete_epic", mustJSON(t, map[string]any{
		"id":      "invoices",
		"project": "bill",
	}))
	if err == nil || err.Error() != `This project has no epic called "invoices".` {
		t.Fatalf("delete_epic already-deleted epic: err = %v, want the store's \"no such epic\" message", err)
	}
}

// create_ticket and update_ticket accept an epic (by name, within the
// ticket's project), and get_ticket/list_tickets report it back.
func TestTicketToolsTakeAndReportEpic(t *testing.T) {
	s := newTestServer(t)

	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := s.callTool("create_epic", mustJSON(t, map[string]any{
		"projectId": "bill",
		"name":      "Invoicing",
	})); err != nil {
		t.Fatalf("create_epic: %v", err)
	}

	created, err := s.callTool("create_ticket", mustJSON(t, map[string]any{
		"projectId": "bill",
		"title":     "Send invoice",
		"epic":      "invoicing",
	}))
	if err != nil {
		t.Fatalf("create_ticket with epic: %v", err)
	}
	ticket := created.(*models.Ticket)
	if ticket.Epic == nil || ticket.Epic.Name != "Invoicing" {
		t.Fatalf("created ticket epic = %+v, want Invoicing", ticket.Epic)
	}

	got, err := s.callTool("get_ticket", mustJSON(t, map[string]any{"id": ticket.ID}))
	if err != nil {
		t.Fatalf("get_ticket: %v", err)
	}
	if e := got.(*models.Ticket).Epic; e == nil || e.Name != "Invoicing" {
		t.Fatalf("get_ticket epic = %+v, want Invoicing", e)
	}

	// Omitting the epic field on an update leaves the ticket's epic
	// unchanged, the same as it does for dueDate.
	omitted, err := s.callTool("update_ticket", mustJSON(t, map[string]any{
		"id":       ticket.ID,
		"priority": "high",
	}))
	if err != nil {
		t.Fatalf("update_ticket with epic omitted: %v", err)
	}
	if e := omitted.(*models.Ticket).Epic; e == nil || e.Name != "Invoicing" {
		t.Fatalf("ticket epic after an update omitting epic = %+v, want unchanged Invoicing", e)
	}

	// An explicit JSON null decodes to the same nil pointer as an omitted
	// field, so it also leaves the epic unchanged.
	withNull, err := s.callTool("update_ticket", mustJSON(t, map[string]any{
		"id":   ticket.ID,
		"epic": nil,
	}))
	if err != nil {
		t.Fatalf("update_ticket with epic=null: %v", err)
	}
	if e := withNull.(*models.Ticket).Epic; e == nil || e.Name != "Invoicing" {
		t.Fatalf("ticket epic after epic=null update = %+v, want unchanged Invoicing", e)
	}

	// An empty string clears it.
	cleared, err := s.callTool("update_ticket", mustJSON(t, map[string]any{
		"id":   ticket.ID,
		"epic": "",
	}))
	if err != nil {
		t.Fatalf("update_ticket with epic=\"\": %v", err)
	}
	if e := cleared.(*models.Ticket).Epic; e != nil {
		t.Fatalf("ticket epic after epic=\"\" update = %+v, want nil", e)
	}

	// Put the epic back, then clear it again with "none" (any case) — the
	// other spelling of "no epic".
	withEpic := "invoicing"
	if _, err := s.store.UpdateTicket(ticket.ID, models.UpdateTicketRequest{Epic: &withEpic}); err != nil {
		t.Fatalf("UpdateTicket restoring epic: %v", err)
	}
	updated, err := s.callTool("update_ticket", mustJSON(t, map[string]any{
		"id":   ticket.ID,
		"epic": "NONE",
	}))
	if err != nil {
		t.Fatalf("update_ticket clearing epic: %v", err)
	}
	if e := updated.(*models.Ticket).Epic; e != nil {
		t.Fatalf("updated ticket epic = %+v, want nil after clearing with \"NONE\"", e)
	}

	// An epic name from another project is rejected with the store's "no
	// such epic" message: from the other project's point of view, no epic
	// by that name exists.
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Support", Prefix: "SUP"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, err = s.callTool("create_ticket", mustJSON(t, map[string]any{
		"projectId": "sup",
		"title":     "Ticket in another project",
		"epic":      "invoicing",
	}))
	if err == nil || err.Error() != `This project has no epic called "invoicing".` {
		t.Fatalf("create_ticket epic from another project: err = %v, want the store's \"no such epic\" message", err)
	}
}

// list_tickets filters by epic name, case-insensitively, and by "none" for
// tickets without an epic.
func TestListTicketsToolFiltersByEpic(t *testing.T) {
	s := newTestServer(t)

	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Invoicing"}); err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	epicName := "Invoicing"
	if _, err := s.store.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "In the epic", Epic: &epicName,
	}); err != nil {
		t.Fatalf("CreateTicket in epic: %v", err)
	}
	if _, err := s.store.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "No epic",
	}); err != nil {
		t.Fatalf("CreateTicket without epic: %v", err)
	}

	byEpic, err := s.callTool("list_tickets", mustJSON(t, map[string]any{"epic": "INVOICING"}))
	if err != nil {
		t.Fatalf("list_tickets epic=INVOICING: %v", err)
	}
	tickets := byEpic.([]models.Ticket)
	if len(tickets) != 1 || tickets[0].Title != "In the epic" {
		t.Fatalf("list_tickets epic=INVOICING = %+v, want one ticket \"In the epic\"", tickets)
	}

	byNone, err := s.callTool("list_tickets", mustJSON(t, map[string]any{"epic": "none"}))
	if err != nil {
		t.Fatalf("list_tickets epic=none: %v", err)
	}
	tickets = byNone.([]models.Ticket)
	if len(tickets) != 1 || tickets[0].Title != "No epic" {
		t.Fatalf("list_tickets epic=none = %+v, want one ticket \"No epic\"", tickets)
	}
}

// The tool descriptions must explain epics, and create_ticket's grouping
// guidance must point to epics instead of telling agents to use a project.
func TestToolDescriptionsExplainEpics(t *testing.T) {
	s := newTestServer(t)

	byName := map[string]toolDef{}
	for _, td := range s.toolDefinitions() {
		byName[td.Name] = td
	}

	for _, name := range []string{"list_epics", "create_epic", "update_epic", "delete_epic"} {
		td, ok := byName[name]
		if !ok {
			t.Fatalf("missing tool definition for %s", name)
		}
		if !strings.Contains(strings.ToLower(td.Description), "epic") {
			t.Fatalf("%s description = %q, want it to mention epics", name, td.Description)
		}
	}

	create := byName["create_ticket"]
	if strings.Contains(create.Description, "'epic' or 'umbrella'") {
		t.Fatalf("create_ticket description still discourages epics: %q", create.Description)
	}
	if !strings.Contains(strings.ToLower(create.Description), "epic") {
		t.Fatalf("create_ticket description = %q, want it to mention epics as the grouping inside a project", create.Description)
	}
	if _, ok := create.InputSchema.Properties["epic"]; !ok {
		t.Fatal("create_ticket is missing the epic property")
	}

	update := byName["update_ticket"]
	if _, ok := update.InputSchema.Properties["epic"]; !ok {
		t.Fatal("update_ticket is missing the epic property")
	}

	list := byName["list_tickets"]
	if _, ok := list.InputSchema.Properties["epic"]; !ok {
		t.Fatal("list_tickets is missing the epic filter property")
	}
}
