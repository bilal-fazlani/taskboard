package mcp

import (
	"encoding/json"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// wantEmptyJSONArray fails unless v marshals to exactly "[]": an agent has
// no way to tell "null" from "nothing here yet" without extra code, so
// every MCP list tool must answer an empty result as [] (ACP-148).
func wantEmptyJSONArray(t *testing.T, label string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s: marshaling %#v: %v", label, v, err)
	}
	if string(data) != "[]" {
		t.Errorf("%s = %s, want [] (not null)", label, data)
	}
}

// list_tickets without summary, limit or offset answers every match in
// full, a bare JSON array; a filter matching nothing must still be [].
func TestListTicketsToolFullFormEmptyIsArray(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}

	out, err := s.callTool("list_tickets", mustJSON(t, map[string]any{"status": "in_progress"}))
	if err != nil {
		t.Fatal(err)
	}
	wantEmptyJSONArray(t, "list_tickets(status=in_progress)", out)
}

// list_labels takes no arguments and answers every label; with none created
// it must be [] rather than null.
func TestListLabelsToolEmptyIsArray(t *testing.T) {
	s := newTestServer(t)

	out, err := s.callTool("list_labels", mustJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	wantEmptyJSONArray(t, "list_labels()", out)
}

// list_projects with no projects created, or a status filter matching
// none, must be [] rather than null.
func TestListProjectsToolEmptyIsArray(t *testing.T) {
	s := newTestServer(t)

	out, err := s.callTool("list_projects", mustJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	wantEmptyJSONArray(t, "list_projects()", out)

	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}
	out, err = s.callTool("list_projects", mustJSON(t, map[string]any{"status": "archived"}))
	if err != nil {
		t.Fatal(err)
	}
	wantEmptyJSONArray(t, `list_projects(status=archived)`, out)
}

// list_epics answers {"epics": [...], "noEpic": {...}}; a project with no
// epics yet must carry "epics": [] rather than null.
func TestListEpicsToolEmptyIsArray(t *testing.T) {
	s := newTestServer(t)
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatal(err)
	}

	out, err := s.callTool("list_epics", mustJSON(t, map[string]any{"projectId": p.ID}))
	if err != nil {
		t.Fatal(err)
	}
	m := toJSONMap(t, out)
	epics, ok := m["epics"].([]any)
	if !ok {
		t.Fatalf(`list_epics(no epics)["epics"] = %#v, want an array`, m["epics"])
	}
	if len(epics) != 0 {
		t.Fatalf(`list_epics(no epics)["epics"] = %#v, want []`, epics)
	}
}

// list_documents answers a ticket's or epic's documents; an owner with none
// must be [] rather than null.
func TestListDocumentsToolEmptyIsArray(t *testing.T) {
	s := newTestServer(t)
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "No documents yet"})
	if err != nil {
		t.Fatal(err)
	}

	out, err := s.callTool("list_documents", mustJSON(t, map[string]any{"ticket": tk.ID}))
	if err != nil {
		t.Fatal(err)
	}
	wantEmptyJSONArray(t, "list_documents(ticket with none)", out)
}
