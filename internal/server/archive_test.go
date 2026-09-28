package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// archiveSeed is a project GONE with a ticket, an epic, a document, a
// subtask and a project entry, and a project KEEP whose ticket depends on
// GONE's and shares its label.
type archiveSeed struct {
	gone, keep *models.Project
	g1, k1     *models.Ticket
	epic       *models.Epic
	doc        *models.Document
	subtask    *models.Subtask
}

func seedArchiveHTTP(t *testing.T, store *db.Store) archiveSeed {
	t.Helper()
	var f archiveSeed
	var err error
	if f.gone, err = store.CreateProject(models.CreateProjectRequest{Name: "Gone", Prefix: "GONE"}); err != nil {
		t.Fatal(err)
	}
	if f.keep, err = store.CreateProject(models.CreateProjectRequest{Name: "Kept", Prefix: "KEEP"}); err != nil {
		t.Fatal(err)
	}
	if f.epic, err = store.CreateEpic(models.CreateEpicRequest{ProjectID: "GONE", Name: "Plans"}); err != nil {
		t.Fatal(err)
	}
	epic := "Plans"
	if f.g1, err = store.CreateTicket(models.CreateTicketRequest{
		ProjectID: "GONE", Title: "Gone", Status: models.StatusInProgress, Epic: &epic, Labels: []string{"shared"},
	}); err != nil {
		t.Fatal(err)
	}
	if f.k1, err = store.CreateTicket(models.CreateTicketRequest{
		ProjectID: "KEEP", Title: "Kept", Labels: []string{"shared"},
		DependsOn: []models.DependencyInput{{Ticket: "GONE-1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if f.doc, err = store.CreateDocument(models.CreateDocumentRequest{TicketID: f.g1.ID, Name: "Plan", Content: "needle"}); err != nil {
		t.Fatal(err)
	}
	if f.subtask, err = store.AddSubtask(f.g1.ID, models.CreateSubtaskRequest{Title: "Step"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateEntry(models.CreateEntryRequest{EntryOwner: models.EntryOwner{ProjectID: "GONE"},
		Type: models.EntryDecision, Text: "entry", AuthorName: "Bilal", Source: models.DecisionSourcePerson}); err != nil {
		t.Fatal(err)
	}
	return f
}

func wantStatus(t *testing.T, method, url, body string, want int) []byte {
	t.Helper()
	code, got := send(t, method, url, body)
	if code != want {
		t.Fatalf("%s %s = %d %s, want %d", method, url, code, got, want)
	}
	return got
}

// Deleting a project over HTTP archives it: every read leaves it and
// everything in it out, every write to it is a 404, and its rows stay.
func TestDeletedProjectIsGoneFromTheAPI(t *testing.T) {
	r := serve(t)
	f := seedArchiveHTTP(t, r.srv.store)
	base := r.url + "/api"

	wantStatus(t, http.MethodDelete, base+"/projects/"+f.gone.ID, "", http.StatusNoContent)
	wantStatus(t, http.MethodDelete, base+"/projects/"+f.gone.ID, "", http.StatusNotFound)

	// Reads.
	for _, url := range []string{base + "/projects", base + "/projects?status=archived"} {
		var projects []models.Project
		if err := json.Unmarshal(wantStatus(t, http.MethodGet, url, "", http.StatusOK), &projects); err != nil {
			t.Fatal(err)
		}
		if len(projects) != 1 || projects[0].Prefix != "KEEP" {
			t.Fatalf("GET %s = %+v, want KEEP only", url, projects)
		}
	}
	for _, url := range []string{
		base + "/projects/" + f.gone.ID,
		base + "/projects/GONE/entries",
		base + "/projects/GONE/activity",
		base + "/tickets/" + f.g1.ID,
		base + "/tickets/" + f.g1.ID + "/history",
		base + "/tickets/" + f.g1.ID + "/documents",
		base + "/epics/" + f.epic.ID,
		base + "/epics/" + f.epic.ID + "/documents",
		base + "/documents/" + f.doc.ID,
		base + "/documents/Plan?ticket=GONE-1",
	} {
		wantStatus(t, http.MethodGet, url, "", http.StatusNotFound)
	}
	wantStatus(t, http.MethodGet, base+"/epics?projectId=GONE", "", http.StatusBadRequest)
	for _, url := range []string{
		base + "/tickets", base + "/tickets?projectId=GONE", base + "/board", base + "/now",
		base + "/documents/search?q=needle",
	} {
		if body := string(wantStatus(t, http.MethodGet, url, "", http.StatusOK)); strings.Contains(body, f.g1.ID) {
			t.Fatalf("GET %s still names GONE-1: %s", url, body)
		}
	}
	var k1 models.Ticket
	if err := json.Unmarshal(wantStatus(t, http.MethodGet, base+"/tickets/"+f.k1.ID, "", http.StatusOK), &k1); err != nil {
		t.Fatal(err)
	}
	if len(k1.DependsOn) != 0 {
		t.Fatalf("KEEP-1 dependsOn = %v, want its link into GONE left out", k1.DependsOn)
	}
	var labels []models.Label
	if err := json.Unmarshal(wantStatus(t, http.MethodGet, base+"/labels", "", http.StatusOK), &labels); err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || labels[0].TicketCount != 1 {
		t.Fatalf("labels = %+v, want shared on 1 ticket", labels)
	}

	// Writes.
	for _, w := range []struct{ method, url, body string }{
		{http.MethodPut, base + "/projects/" + f.gone.ID, `{"name":"Back"}`},
		{http.MethodPost, base + "/projects/GONE/entries", `{"type":"note","text":"more"}`},
		{http.MethodPut, base + "/tickets/" + f.g1.ID, `{"title":"Changed"}`},
		{http.MethodPost, base + "/tickets/" + f.g1.ID + "/move", `{"status":"done"}`},
		{http.MethodPost, base + "/tickets/" + f.g1.ID + "/subtasks", `{"title":"More"}`},
		{http.MethodPost, base + "/subtasks/" + f.subtask.ID + "/toggle", ""},
		{http.MethodDelete, base + "/subtasks/" + f.subtask.ID, ""},
		{http.MethodPut, base + "/epics/" + f.epic.ID, `{"name":"Changed"}`},
		{http.MethodDelete, base + "/epics/" + f.epic.ID, ""},
		{http.MethodPut, base + "/documents/" + f.doc.ID, `{"content":"changed"}`},
		{http.MethodDelete, base + "/documents/" + f.doc.ID, ""},
		{http.MethodDelete, base + "/tickets/" + f.g1.ID, ""},
	} {
		wantStatus(t, w.method, w.url, w.body, http.StatusNotFound)
	}
	for _, w := range []struct{ url, body string }{
		{base + "/tickets", `{"projectId":"GONE","title":"New"}`},
		{base + "/epics", `{"projectId":"GONE","name":"New"}`},
		{base + "/documents?ticket=GONE-1", `{"ticketId":"` + f.g1.ID + `","name":"New","content":"x"}`},
	} {
		code, _ := send(t, http.MethodPost, w.url, w.body)
		if code != http.StatusBadRequest && code != http.StatusNotFound {
			t.Fatalf("POST %s = %d, want it refused as not found", w.url, code)
		}
	}

	// The rows are all still there.
	database, err := db.OpenAt(r.path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, q := range []string{
		"SELECT COUNT(*) FROM projects",
		"SELECT COUNT(*) FROM tickets",
		"SELECT COUNT(*) FROM epics",
		"SELECT COUNT(*) FROM documents",
		"SELECT COUNT(*) FROM subtasks",
		"SELECT COUNT(*) FROM entries",
		"SELECT COUNT(*) FROM ticket_dependencies",
	} {
		var n int
		if err := database.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Fatalf("%s = 0 after the delete, want the rows kept", q)
		}
	}
}

// Only DELETE archives: a project update refuses any status but active,
// pointing at delete, and a deleted project's prefix stays taken.
func TestProjectStatusAndPrefixAfterDeleteHTTP(t *testing.T) {
	r := serve(t)
	f := seedArchiveHTTP(t, r.srv.store)
	base := r.url + "/api"

	body := string(wantStatus(t, http.MethodPut, base+"/projects/"+f.keep.ID, `{"status":"archived"}`, http.StatusBadRequest))
	if !strings.Contains(body, "to archive a project, delete it") {
		t.Fatalf("PUT status archived = %s, want an error pointing at delete", body)
	}
	wantStatus(t, http.MethodPut, base+"/projects/"+f.keep.ID, `{"status":"active"}`, http.StatusOK)

	wantStatus(t, http.MethodDelete, base+"/projects/"+f.gone.ID, "", http.StatusNoContent)
	body = string(wantStatus(t, http.MethodPost, base+"/projects", `{"name":"Again","prefix":"GONE"}`, http.StatusBadRequest))
	if !strings.Contains(body, `project prefix \"GONE\" is already used by a deleted project`) {
		t.Fatalf("POST a deleted project's prefix = %s, want it refused as a deleted project's", body)
	}
}
