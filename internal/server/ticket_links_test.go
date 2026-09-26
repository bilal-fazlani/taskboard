package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// Typed ticket links over HTTP: dependsOn is a list of {ticket, kind, note}
// objects, the whole list on every write, and surfacedFrom is its own field
// with the epic's omitted/""/value contract.

func refKinds(refs []models.TicketRef) map[string]string {
	out := map[string]string{}
	for _, r := range refs {
		out[r.Key] = r.Kind + "|" + r.Note
	}
	return out
}

func TestDependencyKindsAndNotesOverHTTP(t *testing.T) {
	r := serve(t)
	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{"name": "Billing", "prefix": "BILL"})
	for _, title := range []string{"Needed", "Same files"} {
		doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{"projectId": project.ID, "title": title})
	}
	ticket := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID, "title": "Dependent", "dependsOn": []map[string]string{
			{"ticket": "BILL-1"},
			{"ticket": "BILL-2", "kind": "conflict_only", "note": "internal/db/store.go"},
		},
	})
	url := r.url + "/api/tickets/" + ticket.ID

	want := map[string]string{"BILL-1": "needs_work|", "BILL-2": "conflict_only|internal/db/store.go"}
	if got := refKinds(ticket.DependsOn); !reflect.DeepEqual(got, want) {
		t.Fatalf("created dependsOn = %v, want %v", got, want)
	}

	// Every dependsOn entry carries its kind, and its note when it has one.
	stored := doJSON[map[string]any](t, http.MethodGet, url, nil)
	for _, entry := range stored["dependsOn"].([]any) {
		m := entry.(map[string]any)
		if _, has := m["kind"]; !has {
			t.Fatalf("dependsOn entry %v has no kind", entry)
		}
		if m["key"] == "BILL-2" && m["note"] != "internal/db/store.go" {
			t.Fatalf("dependsOn entry %v has no note", entry)
		}
	}
	blocker := doJSON[models.Ticket](t, http.MethodGet, r.url+"/api/tickets/"+ticket.DependsOn[1].ID, nil)
	if got := refKinds(blocker.Blocks); got["BILL-3"] != want["BILL-2"] {
		t.Fatalf("BILL-2 blocks = %v, want BILL-3 conflict only with its note", got)
	}
	list := doJSON[[]models.Ticket](t, http.MethodGet, r.url+"/api/tickets?projectId="+project.ID, nil)
	for _, tk := range list {
		if tk.ID == ticket.ID && !reflect.DeepEqual(refKinds(tk.DependsOn), want) {
			t.Fatalf("listed dependsOn = %v, want %v", refKinds(tk.DependsOn), want)
		}
	}

	// Every write is the whole list: an entry without a kind or note needs
	// work and has no note.
	updated := rawUpdate(t, url, `{"dependsOn":[{"ticket":"BILL-1","kind":"conflict_only","note":"mcp.go"},{"ticket":"BILL-2"}]}`)
	if got := refKinds(updated.DependsOn); !reflect.DeepEqual(got, map[string]string{"BILL-1": "conflict_only|mcp.go", "BILL-2": "needs_work|"}) {
		t.Fatalf("after an update dependsOn = %v", got)
	}

	// A plain string, the old form, is a 400 naming the object shape; so is
	// an unknown kind. Neither applies anything.
	for body, wantText := range map[string]string{
		`{"title":"Changed","dependsOn":["BILL-1"]}`:                                `"ticket"`,
		`{"title":"Changed","dependsOn":[{"ticket":"BILL-1","kind":"someday"}]}`:    "needs_work, conflict_only",
		`{"title":"Changed","dependsOn":[{"ticket":"BILL-1","reason":"store.go"}]}`: `"note"`,
	} {
		errBody, status := errorBody(t, http.MethodPut, url, body)
		if status != http.StatusBadRequest || !strings.Contains(errBody.Error, wantText) {
			t.Fatalf("PUT %s = %d %q, want a 400 containing %s", body, status, errBody.Error, wantText)
		}
	}
	errBody, status := errorBody(t, http.MethodPost, r.url+"/api/tickets",
		`{"projectId":"`+project.ID+`","title":"New","dependsOn":["BILL-1"]}`)
	if status != http.StatusBadRequest || !strings.Contains(errBody.Error, "each dependsOn entry is an object") {
		t.Fatalf("POST with a string dependsOn = %d %q, want a 400 naming the shape", status, errBody.Error)
	}
	if again := doJSON[models.Ticket](t, http.MethodGet, url, nil); again.Title != "Dependent" || len(again.DependsOn) != 2 {
		t.Fatalf("ticket after rejected updates = %q with %d deps, want nothing applied", again.Title, len(again.DependsOn))
	}
}

func TestSurfacedFromOverHTTP(t *testing.T) {
	r := serve(t)
	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{"name": "Agent control plane", "prefix": "ACP"})
	source := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{"projectId": project.ID, "title": "Hardening run"})
	found := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID, "title": "Found in the run", "surfacedFrom": "ACP-1",
	})
	if found.SurfacedFrom == nil || found.SurfacedFrom.ID != source.ID || found.SurfacedFrom.Key != "ACP-1" {
		t.Fatalf("created surfacedFrom = %+v, want ACP-1", found.SurfacedFrom)
	}
	url := r.url + "/api/tickets/" + found.ID

	gotSource := doJSON[models.Ticket](t, http.MethodGet, r.url+"/api/tickets/"+source.ID, nil)
	if len(gotSource.Surfaced) != 1 || gotSource.Surfaced[0].Key != "ACP-2" {
		t.Fatalf("source surfaced = %+v, want ACP-2", gotSource.Surfaced)
	}
	list := doJSON[[]models.Ticket](t, http.MethodGet, r.url+"/api/tickets?projectId="+project.ID, nil)
	for _, tk := range list {
		if tk.ID == found.ID && (tk.SurfacedFrom == nil || tk.SurfacedFrom.Key != "ACP-1") {
			t.Fatalf("listed surfacedFrom = %+v, want ACP-1", tk.SurfacedFrom)
		}
	}

	// Omitted or null leaves it; "" removes it.
	if updated := rawUpdate(t, url, `{"title":"Renamed"}`); updated.SurfacedFrom == nil {
		t.Fatal("an update without surfacedFrom dropped the link")
	}
	if updated := rawUpdate(t, url, `{"surfacedFrom":null}`); updated.SurfacedFrom == nil {
		t.Fatal("surfacedFrom: null dropped the link")
	}
	if updated := rawUpdate(t, url, `{"surfacedFrom":""}`); updated.SurfacedFrom != nil {
		t.Fatalf("surfacedFrom = %+v after \"\", want none", updated.SurfacedFrom)
	}
	raw := doJSON[map[string]any](t, http.MethodGet, url, nil)
	if _, has := raw["surfacedFrom"]; has {
		t.Fatalf("a ticket with no link still has a surfacedFrom key: %v", raw["surfacedFrom"])
	}

	// Its own key is a 400 that applies nothing.
	errBody, status := errorBody(t, http.MethodPut, url, `{"title":"Changed","surfacedFrom":"ACP-2"}`)
	if status != http.StatusBadRequest || !strings.Contains(errBody.Error, "itself") {
		t.Fatalf("PUT surfaced from itself = %d %q, want a 400", status, errBody.Error)
	}
	if again := doJSON[models.Ticket](t, http.MethodGet, url, nil); again.Title != "Renamed" {
		t.Fatalf("title = %q after a rejected update, want nothing applied", again.Title)
	}
}
