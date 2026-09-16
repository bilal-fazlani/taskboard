package mcp

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
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
