package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// doRequest sends a request with the given method, URL and raw JSON body (or
// no body when raw is ""), and returns the parsed response along with its
// status code, so a test can assert on error statuses without doJSON's
// built-in "status must be < 300" check.
func doRequest[T any](t *testing.T, method, url, raw string) (T, int) {
	t.Helper()
	var payload io.Reader
	if raw != "" {
		payload = bytes.NewReader([]byte(raw))
	}
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded T
	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		_ = json.Unmarshal(body, &decoded)
	}
	return decoded, resp.StatusCode
}

type apiError struct {
	Error string `json:"error"`
}

func errorBody(t *testing.T, method, url, raw string) (apiError, int) {
	t.Helper()
	return doRequest[apiError](t, method, url, raw)
}

func TestListEpicsRequiresProjectID(t *testing.T) {
	r := serve(t)

	body, status := errorBody(t, http.MethodGet, r.url+"/api/epics", "")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if body.Error == "" {
		t.Fatal("want an error message")
	}
}

func TestListEpicsUnknownProjectIs400(t *testing.T) {
	r := serve(t)

	body, status := errorBody(t, http.MethodGet, r.url+"/api/epics?projectId=nope", "")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if body.Error == "" {
		t.Fatal("want the store's error message")
	}
}

// TestListEpicsReturnsProgressAndNoEpic builds a small board — two epics and
// some unassigned tickets — and checks the counts, complete flag and
// lastActivityAt the list computes, plus the "no epic" summary that rides
// along in the same response for the Epics view.
func TestListEpicsReturnsProgressAndNoEpic(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Billing",
		"prefix": "BILL",
	})

	invoices := doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": project.ID,
		"name":      "Invoices",
	})
	doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": project.ID,
		"name":      "Payouts",
	})

	// Invoices: one done, one todo -> not complete, total 2.
	doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Send receipts",
		"status":    "done",
		"epic":      invoices.Name,
	})
	doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Export CSV",
		"epic":      invoices.ID,
	})
	// A ticket with no epic.
	doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Unfiled",
	})

	resp := doJSON[epicsResponse](t, http.MethodGet, r.url+"/api/epics?projectId="+project.ID, nil)

	if len(resp.Epics) != 2 {
		t.Fatalf("epics = %d, want 2", len(resp.Epics))
	}

	byName := map[string]models.Epic{}
	for _, e := range resp.Epics {
		byName[e.Name] = e
	}

	inv, ok := byName["Invoices"]
	if !ok {
		t.Fatalf("Invoices missing from %+v", resp.Epics)
	}
	if inv.Total != 2 {
		t.Errorf("Invoices.Total = %d, want 2", inv.Total)
	}
	if inv.Complete {
		t.Error("Invoices.Complete = true, want false (one ticket is still todo)")
	}
	if inv.Counts["done"] != 1 || inv.Counts["todo"] != 1 {
		t.Errorf("Invoices.Counts = %+v, want done:1 todo:1", inv.Counts)
	}
	if inv.LastActivityAt == nil {
		t.Error("Invoices.LastActivityAt = nil, want a time")
	}

	pay, ok := byName["Payouts"]
	if !ok {
		t.Fatalf("Payouts missing from %+v", resp.Epics)
	}
	if pay.Total != 0 || pay.Complete {
		t.Errorf("Payouts = %+v, want an empty, incomplete epic", pay)
	}
	if pay.LastActivityAt != nil {
		t.Errorf("Payouts.LastActivityAt = %v, want nil", pay.LastActivityAt)
	}

	if resp.NoEpic == nil {
		t.Fatal("NoEpic = nil, want the project's unfiled-ticket summary")
	}
	if resp.NoEpic.Total != 1 {
		t.Errorf("NoEpic.Total = %d, want 1", resp.NoEpic.Total)
	}
}

// TestListEpicsWireFormat checks the raw JSON the Epics view depends on
// rather than the Go struct the handler happens to use to build it: the
// top-level object has exactly the keys "epics" and "noEpic", and "epics" is
// an empty array — not null — for a project with none yet, since a client
// that calls .map() on it must never see null.
func TestListEpicsWireFormat(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Search",
		"prefix": "SRCH",
	})

	req, err := http.NewRequest(http.MethodGet, r.url+"/api/epics?projectId="+project.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("decoding response as an object: %v", err)
	}
	epicsRaw, hasEpics := asMap["epics"]
	_, hasNoEpic := asMap["noEpic"]
	if len(asMap) != 2 || !hasEpics || !hasNoEpic {
		t.Fatalf("top-level keys = %v, want exactly \"epics\" and \"noEpic\"", keysOf(asMap))
	}
	if string(epicsRaw) != "[]" {
		t.Errorf(`"epics" = %s, want [] for a project with no epics yet, not null`, epicsRaw)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestEpicsAcceptProjectPrefix checks that both the list and the create route
// take a project prefix, in any case, exactly as POST /api/tickets does.
func TestEpicsAcceptProjectPrefix(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Search",
		"prefix": "SRCH",
	})

	e := doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": "srch",
		"name":      "Ranking",
	})
	if e.ProjectID != project.ID {
		t.Fatalf("ProjectID = %q, want %q (resolved from the lowercase prefix)", e.ProjectID, project.ID)
	}

	resp := doJSON[epicsResponse](t, http.MethodGet, r.url+"/api/epics?projectId=srch", nil)
	if len(resp.Epics) != 1 || resp.Epics[0].ID != e.ID {
		t.Fatalf("epics via prefix = %+v, want just %s", resp.Epics, e.ID)
	}
}

func TestCreateEpic(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Search",
		"prefix": "SRCH",
	})

	payload, err := json.Marshal(map[string]string{
		"projectId":   project.ID,
		"name":        "  Ranking  ",
		"description": "Relevance work",
	})
	if err != nil {
		t.Fatal(err)
	}
	e, status := doRequest[models.Epic](t, http.MethodPost, r.url+"/api/epics", string(payload))
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", status)
	}
	if e.Name != "Ranking" {
		t.Errorf("Name = %q, want trimmed \"Ranking\"", e.Name)
	}
	if e.Description != "Relevance work" {
		t.Errorf("Description = %q, want \"Relevance work\"", e.Description)
	}
	if e.ProjectID != project.ID {
		t.Errorf("ProjectID = %q, want %q", e.ProjectID, project.ID)
	}
	if e.Total != 0 || e.Complete {
		t.Errorf("a fresh epic = %+v, want an empty, incomplete progress", e.EpicProgress)
	}
}

// TestCreateEpicErrorsComeFromTheStore checks that the three rules ACP-74
// names explicitly — empty name, the reserved "none", and a duplicate name —
// reach the client as 400s with the store's own wording, unchanged.
func TestCreateEpicErrorsComeFromTheStore(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Search",
		"prefix": "SRCH",
	})

	body, status := errorBody(t, http.MethodPost, r.url+"/api/epics",
		`{"projectId":"`+project.ID+`","name":"   "}`)
	if status != http.StatusBadRequest {
		t.Fatalf("whitespace name: status = %d, want 400", status)
	}
	if body.Error != "Enter a name" {
		t.Errorf("whitespace name: error = %q, want %q", body.Error, "Enter a name")
	}

	// The web dialog's new-epic form sends "" for an untouched field, not
	// whitespace, so that must give the same error.
	body, status = errorBody(t, http.MethodPost, r.url+"/api/epics",
		`{"projectId":"`+project.ID+`","name":""}`)
	if status != http.StatusBadRequest {
		t.Fatalf(`empty name: status = %d, want 400`, status)
	}
	if body.Error != "Enter a name" {
		t.Errorf("empty name: error = %q, want %q", body.Error, "Enter a name")
	}

	body, status = errorBody(t, http.MethodPost, r.url+"/api/epics",
		`{"projectId":"`+project.ID+`","name":"None"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("reserved none: status = %d, want 400", status)
	}
	if body.Error != `"none" is reserved for tickets without an epic.` {
		t.Errorf("reserved none: error = %q", body.Error)
	}

	doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": project.ID,
		"name":      "Ranking",
	})
	body, status = errorBody(t, http.MethodPost, r.url+"/api/epics",
		`{"projectId":"`+project.ID+`","name":"RANKING"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("duplicate name: status = %d, want 400", status)
	}
	if body.Error != `This project already has an epic called "Ranking".` {
		t.Errorf("duplicate name: error = %q", body.Error)
	}
}

func TestUpdateEpic(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Search",
		"prefix": "SRCH",
	})
	e := doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": project.ID,
		"name":      "Ranking",
	})

	updated := doJSON[models.Epic](t, http.MethodPut, r.url+"/api/epics/"+e.ID, map[string]string{
		"description": "New description",
	})
	if updated.Name != "Ranking" {
		t.Errorf("Name = %q, want it unchanged", updated.Name)
	}
	if updated.Description != "New description" {
		t.Errorf("Description = %q, want \"New description\"", updated.Description)
	}

	renamed := doJSON[models.Epic](t, http.MethodPut, r.url+"/api/epics/"+e.ID, map[string]string{
		"name": "Relevance",
	})
	if renamed.Name != "Relevance" {
		t.Errorf("Name = %q, want \"Relevance\"", renamed.Name)
	}
	if renamed.Description != "New description" {
		t.Errorf("Description = %q, want it kept from the earlier update", renamed.Description)
	}
}

func TestUpdateEpicNotFound(t *testing.T) {
	r := serve(t)

	_, status := errorBody(t, http.MethodPut, r.url+"/api/epics/01ARZ3NDEKTSV4RRFFQ69G5FAV",
		`{"name":"Whatever"}`)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestUpdateEpicDuplicateNameIs400(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Search",
		"prefix": "SRCH",
	})
	doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": project.ID,
		"name":      "Ranking",
	})
	payouts := doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": project.ID,
		"name":      "Payouts",
	})

	body, status := errorBody(t, http.MethodPut, r.url+"/api/epics/"+payouts.ID,
		`{"name":"ranking"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if body.Error != `This project already has an epic called "Ranking".` {
		t.Errorf("error = %q", body.Error)
	}
}

func TestDeleteEpicClearsItFromTickets(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Search",
		"prefix": "SRCH",
	})
	e := doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": project.ID,
		"name":      "Ranking",
	})
	ticket := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Tune BM25",
		"epic":      e.ID,
	})
	if ticket.Epic == nil || ticket.Epic.ID != e.ID {
		t.Fatalf("ticket.Epic = %+v, want %s", ticket.Epic, e.ID)
	}

	req, err := http.NewRequest(http.MethodDelete, r.url+"/api/epics/"+e.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}

	after := doJSON[models.Ticket](t, http.MethodGet, r.url+"/api/tickets/"+ticket.ID, nil)
	if after.Epic != nil {
		t.Errorf("ticket.Epic = %+v after deleting its epic, want nil", after.Epic)
	}
}

func TestDeleteEpicNotFound(t *testing.T) {
	r := serve(t)

	req, err := http.NewRequest(http.MethodDelete, r.url+"/api/epics/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestTicketCarriesEpicWithDueDateContract checks the ticket JSON contract
// ACP-74 asks for: the epic rides along as {id, name}, and create/update
// treat it exactly like dueDate — omitted leaves it alone, "" and "none"
// (any case) clear it, and an unresolvable value is a 400 that changes
// nothing else in the request.
func TestTicketCarriesEpicWithDueDateContract(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Search",
		"prefix": "SRCH",
	})
	e := doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": project.ID,
		"name":      "Ranking",
	})

	// Create with an epic name.
	ticket := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Tune BM25",
		"epic":      e.Name,
	})
	if ticket.Epic == nil || ticket.Epic.ID != e.ID || ticket.Epic.Name != "Ranking" {
		t.Fatalf("ticket.Epic = %+v, want {%s Ranking}", ticket.Epic, e.ID)
	}

	url := r.url + "/api/tickets/" + ticket.ID

	// Omitted epic on update leaves it unchanged.
	untouched := rawUpdate(t, url, `{"title":"Tune BM25 v2"}`)
	if untouched.Epic == nil || untouched.Epic.ID != e.ID {
		t.Errorf("Epic = %+v after an update that omitted it, want it unchanged", untouched.Epic)
	}

	// A JSON null is the same as omitted.
	stillThere := rawUpdate(t, url, `{"epic":null}`)
	if stillThere.Epic == nil || stillThere.Epic.ID != e.ID {
		t.Errorf("Epic = %+v after epic:null, want it unchanged", stillThere.Epic)
	}

	// "" clears it.
	cleared := rawUpdate(t, url, `{"epic":""}`)
	if cleared.Epic != nil {
		t.Errorf("Epic = %+v after epic:\"\", want nil", cleared.Epic)
	}

	// Put it back, then clear with "none" (any case).
	rawUpdate(t, url, `{"epic":"`+e.ID+`"}`)
	clearedByNone := rawUpdate(t, url, `{"epic":"NONE"}`)
	if clearedByNone.Epic != nil {
		t.Errorf("Epic = %+v after epic:\"NONE\", want nil", clearedByNone.Epic)
	}

	// An unresolvable epic is a 400 from the store, unchanged, and does not
	// touch the rest of the request.
	body, status := errorBody(t, http.MethodPut, url, `{"title":"should not stick","epic":"Nope"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if want := `This project has no epic called "Nope".`; body.Error != want {
		t.Errorf("error = %q, want %q", body.Error, want)
	}
	after := doJSON[models.Ticket](t, http.MethodGet, url, nil)
	if after.Title == "should not stick" {
		t.Error("a rejected update must not apply any of its other fields")
	}
}

func TestTicketListFiltersByEpic(t *testing.T) {
	r := serve(t)

	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name":   "Search",
		"prefix": "SRCH",
	})
	e := doJSON[models.Epic](t, http.MethodPost, r.url+"/api/epics", map[string]string{
		"projectId": project.ID,
		"name":      "Ranking",
	})
	inEpic := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Tune BM25",
		"epic":      e.Name,
	})
	unfiled := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID,
		"title":     "Unfiled ticket",
	})

	byEpic := doJSON[[]models.Ticket](t, http.MethodGet, r.url+"/api/tickets?epic=Ranking", nil)
	if len(byEpic) != 1 || byEpic[0].ID != inEpic.ID {
		t.Fatalf("?epic=Ranking = %+v, want just %s", byEpic, inEpic.ID)
	}

	byNone := doJSON[[]models.Ticket](t, http.MethodGet, r.url+"/api/tickets?epic=none", nil)
	if len(byNone) != 1 || byNone[0].ID != unfiled.ID {
		t.Fatalf("?epic=none = %+v, want just %s", byNone, unfiled.ID)
	}

	byNoneCaps := doJSON[[]models.Ticket](t, http.MethodGet, r.url+"/api/tickets?epic=NONE", nil)
	if len(byNoneCaps) != 1 || byNoneCaps[0].ID != unfiled.ID {
		t.Fatalf("?epic=NONE = %+v, want just %s", byNoneCaps, unfiled.ID)
	}
}
