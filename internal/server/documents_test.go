package server

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func seedTicketDocument(t *testing.T, r *running) (*models.Ticket, *models.Document) {
	t.Helper()
	p, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := r.srv.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Has docs"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Design spec", Content: "# Spec\n"})
	if err != nil {
		t.Fatal(err)
	}
	return tk, d
}

func TestListTicketDocuments(t *testing.T) {
	r := serve(t)
	tk, d := seedTicketDocument(t, r)

	docs, status := doRequest[[]map[string]any](t, http.MethodGet, r.url+"/api/tickets/"+tk.ID+"/documents", "")
	if status != http.StatusOK || len(docs) != 1 || docs[0]["id"] != d.ID {
		t.Fatalf("status %d, docs %+v", status, docs)
	}
	if _, has := docs[0]["content"]; has {
		t.Fatal("the list must not carry content")
	}
	if docs[0]["size"] != float64(len("# Spec\n")) {
		t.Fatalf("size = %v", docs[0]["size"])
	}

	_, status = errorBody(t, http.MethodGet, r.url+"/api/tickets/nope/documents", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown ticket status = %d, want 404", status)
	}
}

func TestGetDocumentByIDOrTicketAndName(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	byID, status := doRequest[models.Document](t, http.MethodGet, r.url+"/api/documents/"+d.ID, "")
	if status != http.StatusOK || byID.Content != "# Spec\n" {
		t.Fatalf("by id: %d %+v", status, byID)
	}
	for _, name := range []string{"Design spec", "design spec.md"} {
		got, status := doRequest[models.Document](t, http.MethodGet,
			r.url+"/api/documents/"+url.PathEscape(name)+"?ticket=doc-1", "")
		if status != http.StatusOK || got.ID != d.ID {
			t.Fatalf("by name %q: %d %+v", name, status, got)
		}
	}
	body, status := errorBody(t, http.MethodGet, r.url+"/api/documents/"+url.PathEscape("Design spec.html")+"?ticket=DOC-1", "")
	if status != http.StatusNotFound || body.Error != `This ticket has no document called "Design spec.html".` {
		t.Fatalf("wrong extension: %d %q", status, body.Error)
	}
	_, status = errorBody(t, http.MethodGet, r.url+"/api/documents/nope", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown id status = %d", status)
	}
}

func TestRenameDocument(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	got, status := doRequest[models.Document](t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"name":" Plan "}`)
	if status != http.StatusOK || got.Name != "Plan" || got.Revision != 1 {
		t.Fatalf("rename: %d %+v", status, got)
	}
	body, status := errorBody(t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"name":"plan.md"}`)
	if status != http.StatusBadRequest || body.Error != "Use letters, digits, spaces, _ and - only." {
		t.Fatalf("bad name: %d %q", status, body.Error)
	}
	_, status = errorBody(t, http.MethodPut, r.url+"/api/documents/nope", `{"name":"Plan"}`)
	if status != http.StatusNotFound {
		t.Fatalf("unknown id status = %d", status)
	}
}

func TestDeleteDocument(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	_, status := doRequest[any](t, http.MethodDelete, r.url+"/api/documents/"+d.ID, "")
	if status != http.StatusNoContent {
		t.Fatalf("delete status = %d", status)
	}
	_, status = errorBody(t, http.MethodDelete, r.url+"/api/documents/"+d.ID, "")
	if status != http.StatusNotFound {
		t.Fatalf("second delete status = %d", status)
	}
}

func TestDownloadDocument(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	resp, err := http.Get(r.url + "/api/documents/" + d.ID + "/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "# Spec\n" {
		t.Fatalf("download: %d %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename="Design spec.md"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/markdown") {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestTicketListCarriesDocumentCount(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)

	tickets, status := doRequest[[]models.Ticket](t, http.MethodGet, r.url+"/api/tickets", "")
	if status != http.StatusOK || len(tickets) != 1 || tickets[0].ID != tk.ID || tickets[0].DocumentCount != 1 {
		t.Fatalf("tickets = %d %+v", status, tickets)
	}
}

func TestDownloadDocumentWithANonASCIIName(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)
	if _, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Étude", Content: "x"}); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(r.url + "/api/documents/" + url.PathEscape("étude.md") + "/download?ticket=DOC-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download by name: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Disposition"); got != "attachment; filename*=utf-8''%C3%89tude.md" {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestDeleteDocumentByTicketAndName(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	_, status := doRequest[any](t, http.MethodDelete, r.url+"/api/documents/"+url.PathEscape("design spec")+"?ticket=DOC-1", "")
	if status != http.StatusNoContent {
		t.Fatalf("delete by name status = %d", status)
	}
	if got, _ := r.srv.store.GetDocument(d.ID); got != nil {
		t.Fatal("the document is still there")
	}
	body, status := errorBody(t, http.MethodGet, r.url+"/api/documents/Plan?ticket=DOC-99", "")
	if status != http.StatusNotFound || body.Error == "" {
		t.Fatalf("unknown ticket: %d %q", status, body.Error)
	}
}
