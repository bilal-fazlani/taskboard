package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// rawGET reads the raw response body of a GET request, so a test can assert
// on the JSON on the wire ("[]" vs "null") rather than on a Go value a
// nil-tolerant Unmarshal would make look the same either way.
func rawGET(t *testing.T, url string) (raw string, status int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body), resp.StatusCode
}

// A list endpoint with no matches answers [] on the wire, never null: a
// client that calls .map() on the response must never see null (ACP-148).
func wantEmptyJSONArrayBody(t *testing.T, label, url string) {
	t.Helper()
	raw, status := rawGET(t, url)
	if status != http.StatusOK {
		t.Fatalf("%s: status = %d, want 200 (body %s)", label, status, raw)
	}
	if trimmed := strings.TrimSpace(raw); trimmed != "[]" {
		t.Errorf("%s = %s, want [] (not null)", label, raw)
	}
}

func TestListTicketsHTTPEmptyIsArray(t *testing.T) {
	r := serve(t)
	if _, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}
	wantEmptyJSONArrayBody(t, "GET /api/tickets?status=in_progress", r.url+"/api/tickets?status=in_progress")
}

func TestListLabelsHTTPEmptyIsArray(t *testing.T) {
	r := serve(t)
	wantEmptyJSONArrayBody(t, "GET /api/labels", r.url+"/api/labels")
}

func TestListProjectsHTTPEmptyIsArray(t *testing.T) {
	r := serve(t)
	wantEmptyJSONArrayBody(t, "GET /api/projects", r.url+"/api/projects")

	if _, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}
	wantEmptyJSONArrayBody(t, "GET /api/projects?status=archived", r.url+"/api/projects?status=archived")
}

func TestListTicketDocumentsHTTPEmptyIsArray(t *testing.T) {
	r := serve(t)
	p, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := r.srv.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "No documents yet"})
	if err != nil {
		t.Fatal(err)
	}
	wantEmptyJSONArrayBody(t, "GET /api/tickets/{id}/documents", r.url+"/api/tickets/"+tk.ID+"/documents")
}

func TestListEpicDocumentsHTTPEmptyIsArray(t *testing.T) {
	r := serve(t)
	p, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.srv.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	wantEmptyJSONArrayBody(t, "GET /api/epics/{id}/documents", r.url+"/api/epics/"+e.ID+"/documents")
}
