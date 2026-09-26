package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// The web editor saves field by field: it sends only the fields the user
// edited, so an edit made elsewhere to a field they never touched survives
// the save instead of being written back over. That rests entirely on
// PUT /api/tickets/{id} reading an omitted field as "leave it unchanged" —
// for the collections (labels, repos, dependsOn) as much as for the scalars
// and the due date, since a JSON body that simply has no "labels" key must
// not be taken for one that clears them.

func doJSON[T any](t *testing.T, method, url string, body any) T {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		text, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: status %d: %s", method, url, resp.StatusCode, text)
	}
	var decoded T
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("%s %s: decoding response: %v", method, url, err)
	}
	return decoded
}

// rawUpdate sends a PUT with the body exactly as written, so a test can leave
// a field out rather than relying on Go's own omitempty.
func rawUpdate(t *testing.T, url, body string) models.Ticket {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		text, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT %s with %s: status %d: %s", url, body, resp.StatusCode, text)
	}
	var updated models.Ticket
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return updated
}

// A label embedded in a ticket must never carry a ticketCount key: computing
// the real count there is an N+1 cost nobody needs, and a 0 would be
// indistinguishable from a label genuinely on no other ticket (ACP-48).
func TestTicketLabelsOmitTicketCount(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Billing",
		"prefix": "BILL",
	})
	ticket := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Invoice export",
		"labels":    []string{"api"},
	})

	resp, err := http.Get(r.url + "/api/tickets/" + ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Labels []map[string]any `json:"labels"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding ticket: %v (body: %s)", err, raw)
	}
	if len(decoded.Labels) != 1 {
		t.Fatalf("labels = %+v, want 1", decoded.Labels)
	}
	if _, has := decoded.Labels[0]["ticketCount"]; has {
		t.Fatalf("embedded label = %+v, want no ticketCount key", decoded.Labels[0])
	}
}

func labelNames(labels []models.EmbeddedLabel) []string {
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		names = append(names, l.Name)
	}
	return names
}

func dependencyKeys(refs []models.TicketRef) []string {
	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.Key)
	}
	return keys
}

// sameSet reports whether both hold the same values, whatever order they
// arrive in. Only the list endpoint sorts a ticket's labels (attachListDetails
// orders by name); the single-ticket query behind GET and PUT has no ORDER BY,
// so the order it answers with is SQLite's to choose and nothing here may
// depend on it. What these tests are about is which values survive an update,
// not the order they come back in.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := append([]string(nil), a...)
	sortedB := append([]string(nil), b...)
	sort.Strings(sortedA)
	sort.Strings(sortedB)
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}

func TestUpdateTicketLeavesOmittedFieldsUnchanged(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Billing",
		"prefix": "BILL",
	})
	blocker := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Pick a provider",
	})
	due := "2026-10-01"
	ticket := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId":   project.ID,
		"title":       "Invoice export",
		"description": "The first draft",
		"priority":    "high",
		"dueDate":     due,
		"repos":       []string{"acme/billing-api", "acme/billing-web"},
		"labels":      []string{"web", "hardening"},
		"dependsOn":   []map[string]string{{"ticket": blocker.ID}},
	})
	url := r.url + "/api/tickets/" + ticket.ID

	// What the editor sends for a save that touched only the title.
	updated := rawUpdate(t, url, `{"title":"Invoice export v2"}`)

	if updated.Title != "Invoice export v2" {
		t.Fatalf("Title = %q, want the edited one", updated.Title)
	}
	if updated.Description != "The first draft" {
		t.Errorf("Description = %q, want it unchanged", updated.Description)
	}
	if updated.Status != ticket.Status || updated.Priority != "high" {
		t.Errorf("status/priority = %q/%q, want them unchanged", updated.Status, updated.Priority)
	}
	if updated.DueDate == nil {
		t.Errorf("DueDate = nil, want %s: an omitted due date must not clear it", due)
	} else if got := updated.DueDate.Format("2006-01-02"); got != due {
		t.Errorf("DueDate = %q, want %q", got, due)
	}
	if want := []string{"acme/billing-api", "acme/billing-web"}; !sameSet(updated.Repos, want) {
		t.Errorf("Repos = %v, want %v", updated.Repos, want)
	}
	if want := []string{"web", "hardening"}; !sameSet(labelNames(updated.Labels), want) {
		t.Errorf("Labels = %v, want %v", labelNames(updated.Labels), want)
	}
	if want := []string{blocker.DisplayKey()}; !sameSet(dependencyKeys(updated.DependsOn), want) {
		t.Errorf("DependsOn = %v, want %v", dependencyKeys(updated.DependsOn), want)
	}

	// The same body read back, so this is the stored ticket and not just what
	// the update handler happened to return.
	stored := doJSON[models.Ticket](t, http.MethodGet, url, nil)
	if stored.Title != "Invoice export v2" || len(stored.Labels) != 2 || len(stored.DependsOn) != 1 {
		t.Fatalf("stored ticket = %+v, want the edit applied and nothing else lost", stored)
	}

	// A JSON null is the same as leaving the field out, since that is what a
	// client sending a field it holds as "nothing" looks like.
	withNulls := rawUpdate(t, url, `{"status":"in_progress","dueDate":null,"labels":null,"repos":null,"dependsOn":null}`)
	if withNulls.Status != "in_progress" {
		t.Fatalf("Status = %q, want in_progress", withNulls.Status)
	}
	if withNulls.DueDate == nil || len(withNulls.Labels) != 2 || len(withNulls.Repos) != 2 || len(withNulls.DependsOn) != 1 {
		t.Fatalf("ticket after nulls = %+v, want the due date, labels, repos and dependencies kept", withNulls)
	}
}

// The other half of the contract: a field that is present, and empty, does
// clear what it names. Without this an editor could never empty one.
func TestUpdateTicketClearsFieldsSentEmpty(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Billing",
		"prefix": "BILL",
	})
	blocker := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Pick a provider",
	})
	due := "2026-10-01"
	ticket := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Invoice export",
		"dueDate":   due,
		"repos":     []string{"acme/billing-api"},
		"labels":    []string{"web"},
		"dependsOn": []map[string]string{{"ticket": blocker.ID}},
	})

	cleared := rawUpdate(t, r.url+"/api/tickets/"+ticket.ID,
		`{"dueDate":"","labels":[],"repos":[],"dependsOn":[]}`)

	if cleared.DueDate != nil {
		t.Errorf("DueDate = %v, want nil: \"\" is the explicit clear", cleared.DueDate)
	}
	if len(cleared.Labels) != 0 || len(cleared.Repos) != 0 || len(cleared.DependsOn) != 0 {
		t.Errorf("ticket = %+v, want its labels, repos and dependencies cleared", cleared)
	}
}

// The store's status validation (internal/db) must actually reach an HTTP
// caller as a 400, not a 500 or a silently-accepted ticket: no view has a
// column for a status no one recognizes.
func TestCreateTicketWithUnknownStatusIs400(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Billing",
		"prefix": "BILL",
	})

	body, status := errorBody(t, http.MethodPost, r.url+"/api/tickets",
		`{"projectId":"`+project.ID+`","title":"Invoice export","status":"bogus"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if body.Error == "" {
		t.Fatal("want an error message naming the valid statuses")
	}
}

// DELETE /api/tickets/{id} takes a raw ULID with no key resolution, so unlike
// the CLI and MCP (which resolve a display key or reject an unknown one
// first), this route reaches store.DeleteTicket directly with whatever id was
// given. Before this ticket, a well-formed but unknown ULID here answered 204
// as if a ticket had actually been deleted.
func TestDeleteTicketNotFound(t *testing.T) {
	r := serve(t)

	body, status := errorBody(t, http.MethodDelete, r.url+"/api/tickets/01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if body.Error == "" {
		t.Fatal("want an error message")
	}
}
