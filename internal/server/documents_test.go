package server

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

func TestCreateDocumentOverHTTP(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)

	created, status := doRequest[models.Document](t, http.MethodPost, r.url+"/api/documents",
		`{"ticketId":"`+tk.ID+`","name":"Notes","format":"markdown","content":""}`)
	if status != http.StatusCreated || created.Name != "Notes" || created.Revision != 1 || created.TicketID != tk.ID {
		t.Fatalf("create: %d %+v", status, created)
	}
	body, status := errorBody(t, http.MethodPost, r.url+"/api/documents",
		`{"ticketId":"`+tk.ID+`","name":"design SPEC","content":""}`)
	if status != http.StatusBadRequest || body.Error != `This ticket already has a document called "Design spec.md".` {
		t.Fatalf("taken name: %d %q", status, body.Error)
	}
	body, status = errorBody(t, http.MethodPost, r.url+"/api/documents",
		`{"ticketId":"`+tk.ID+`","name":"notes.md","content":""}`)
	if status != http.StatusBadRequest || body.Error != "Use letters, digits, spaces, _ and - only." {
		t.Fatalf("bad name: %d %q", status, body.Error)
	}
	_, status = errorBody(t, http.MethodPost, r.url+"/api/documents", `{"ticketId":"nope","name":"X"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("unknown ticket status = %d", status)
	}
	_, status = errorBody(t, http.MethodPost, r.url+"/api/documents", `{`)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d", status)
	}
}

func TestStaleSaveIsAConflict(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	// An agent's save sends no revision and always lands.
	_, status := doRequest[models.Document](t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"content":"theirs"}`)
	if status != http.StatusOK {
		t.Fatalf("agent save status = %d", status)
	}
	type conflict struct {
		Error   string          `json:"error"`
		Current models.Document `json:"current"`
	}
	got, status := doRequest[conflict](t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"content":"mine","expectedRevision":1}`)
	if status != http.StatusConflict || got.Current.Content != "theirs" || got.Current.Revision != 2 ||
		got.Error != "This document changed since you started editing." {
		t.Fatalf("stale save: %d %+v", status, got)
	}

	saved, status := doRequest[models.Document](t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"content":"mine","expectedRevision":2}`)
	if status != http.StatusOK || saved.Content != "mine" || saved.Revision != 3 {
		t.Fatalf("save at the current revision: %d %+v", status, saved)
	}
}

// The sandbox tokens the raw route may grant, and those it must never grant.
// allow-forms, allow-modals and allow-downloads were ruled out on purpose:
// the page's forms don't submit, alert/confirm/prompt/print are blocked, and
// it cannot start a download of its own.
var forbiddenSandboxTokens = []string{
	"allow-same-origin", "allow-top-navigation", "allow-popups",
	"allow-forms", "allow-modals", "allow-downloads",
}

func TestRawDocumentIsSandboxed(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)
	page, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{
		TicketID: tk.ID, Name: "Report", Format: models.DocumentFormatHTML, Content: "<script>1</script>",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/api/documents/" + page.ID + "/raw?rev=1",
		"/api/documents/" + url.PathEscape("Report.html") + "/raw?ticket=DOC-1",
	} {
		resp, err := http.Get(r.url + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != documentGuard+"<script>1</script>" {
			t.Fatalf("%s: %d %q", path, resp.StatusCode, body)
		}
		for header, want := range map[string]string{
			"Content-Type":            "text/html; charset=utf-8",
			"Content-Security-Policy": "sandbox allow-scripts",
			"X-Content-Type-Options":  "nosniff",
			"Cache-Control":           "no-store",
			"Referrer-Policy":         "no-referrer",
		} {
			if got := resp.Header.Get(header); got != want {
				t.Errorf("%s: %s = %q, want %q", path, header, got, want)
			}
		}
		csp := resp.Header.Get("Content-Security-Policy")
		for _, token := range forbiddenSandboxTokens {
			if strings.Contains(csp, token) {
				t.Errorf("%s: Content-Security-Policy %q grants %s", path, csp, token)
			}
		}
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s: Access-Control-Allow-Origin = %q, want none", path, got)
		}
		if got := resp.Header.Get("Content-Disposition"); got != "" {
			t.Errorf("%s: Content-Disposition = %q, want none (shown inline)", path, got)
		}
	}
}

func TestRawMarkdownDocumentIsPlainText(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)
	resp, err := http.Get(r.url + "/api/documents/" + d.ID + "/raw")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "# Spec\n" {
		t.Fatalf("raw markdown: %d %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "sandbox allow-scripts" {
		t.Errorf("Content-Security-Policy = %q", got)
	}

	_, status := errorBody(t, http.MethodGet, r.url+"/api/documents/nope/raw", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown document raw status = %d, want 404", status)
	}
}

// Downloading an HTML document through the board's own route is untouched
// by the sandbox: it is an attachment named with the display name.
func TestDownloadHTMLDocument(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)
	page, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{
		TicketID: tk.ID, Name: "Report", Format: models.DocumentFormatHTML, Content: "<h1>x</h1>",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(r.url + "/api/documents/" + page.ID + "/download")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "<h1>x</h1>" {
		t.Fatalf("download: %d %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename=Report.html` {
		t.Errorf("Content-Disposition = %q", got)
	}
}

// A sandboxed frame has an opaque origin, which the browser sends as
// "Origin: null". Its scripts must not be able to write to the board, even
// with a "simple" request that needs no CORS preflight.
func TestWriteFromASandboxedFrameIsRefused(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/documents/" + d.ID, `{"content":"pwned"}`},
		{http.MethodDelete, "/api/documents/" + d.ID, ""},
		{http.MethodPost, "/api/tickets", `{"projectId":"DOC","title":"pwned"}`},
	} {
		req, err := http.NewRequest(tc.method, r.url+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "null")
		req.Header.Set("Content-Type", "text/plain")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s from Origin: null = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
	if got, _ := r.srv.store.GetDocument(d.ID); got == nil || got.Content != "# Spec\n" {
		t.Fatal("the document changed")
	}
	tickets, err := r.srv.store.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("tickets = %d, want only the seeded one", len(tickets))
	}
}

func TestEpicDocumentsOverHTTP(t *testing.T) {
	r := serve(t)
	p, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.srv.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := r.srv.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Has docs"})
	if err != nil {
		t.Fatal(err)
	}

	created, status := doRequest[models.Document](t, http.MethodPost, r.url+"/api/documents",
		`{"epicId":"`+e.ID+`","name":"Rollout","format":"markdown","content":"# r"}`)
	if status != http.StatusCreated || created.EpicID != e.ID || created.TicketID != "" {
		t.Fatalf("create: %d %+v", status, created)
	}
	body, status := errorBody(t, http.MethodPost, r.url+"/api/documents",
		`{"epicId":"`+e.ID+`","name":"rollout","content":""}`)
	if status != http.StatusBadRequest || body.Error != `This epic already has a document called "Rollout.md".` {
		t.Fatalf("taken name: %d %q", status, body.Error)
	}
	body, status = errorBody(t, http.MethodPost, r.url+"/api/documents",
		`{"epicId":"`+e.ID+`","ticketId":"`+tk.ID+`","name":"Both","content":""}`)
	if status != http.StatusBadRequest || body.Error != "A document belongs to one ticket or one epic." {
		t.Fatalf("both owners on create: %d %q", status, body.Error)
	}

	docs, status := doRequest[[]models.DocumentMeta](t, http.MethodGet, r.url+"/api/epics/"+e.ID+"/documents", "")
	if status != http.StatusOK || len(docs) != 1 || docs[0].EpicID != e.ID {
		t.Fatalf("list: %d %+v", status, docs)
	}
	_, status = errorBody(t, http.MethodGet, r.url+"/api/epics/nope/documents", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown epic documents status = %d, want 404", status)
	}
	epic, status := doRequest[models.Epic](t, http.MethodGet, r.url+"/api/epics/"+e.ID, "")
	if status != http.StatusOK || epic.DocumentCount != 1 || len(epic.Documents) != 1 || epic.Name != "Launch" {
		t.Fatalf("get epic: %d %+v", status, epic)
	}
	_, status = errorBody(t, http.MethodGet, r.url+"/api/epics/nope", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown epic status = %d", status)
	}
	listed, status := doRequest[struct {
		Epics []models.Epic `json:"epics"`
	}](t, http.MethodGet, r.url+"/api/epics?projectId=DOC", "")
	if status != http.StatusOK || len(listed.Epics) != 1 || listed.Epics[0].DocumentCount != 1 || listed.Epics[0].Documents != nil {
		t.Fatalf("list epics: %d %+v", status, listed)
	}

	byName, status := doRequest[models.Document](t, http.MethodGet, r.url+"/api/documents/rollout.md?epic=launch&project=doc", "")
	if status != http.StatusOK || byName.ID != created.ID {
		t.Fatalf("by epic name: %d %+v", status, byName)
	}
	byEpicID, status := doRequest[models.Document](t, http.MethodGet, r.url+"/api/documents/Rollout?epic="+e.ID, "")
	if status != http.StatusOK || byEpicID.ID != created.ID {
		t.Fatalf("by epic id: %d %+v", status, byEpicID)
	}
	_, status = errorBody(t, http.MethodGet, r.url+"/api/documents/Rollout?epic=Launch", "")
	if status != http.StatusNotFound {
		t.Fatalf("epic name without project status = %d, want 404", status)
	}
	body, status = errorBody(t, http.MethodGet, r.url+"/api/documents/Rollout?epic="+e.ID+"&ticket=DOC-1", "")
	if status != http.StatusBadRequest || body.Error != "pass ticket or epic, not both" {
		t.Fatalf("both owners: %d %q", status, body.Error)
	}

	renamed, status := doRequest[models.Document](t, http.MethodPut,
		r.url+"/api/documents/Rollout?epic=Launch&project=DOC", `{"name":"Go live"}`)
	if status != http.StatusOK || renamed.Name != "Go live" {
		t.Fatalf("rename by epic name: %d %+v", status, renamed)
	}
	resp, err := http.Get(r.url + "/api/documents/go%20live.md/download?epic=" + e.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Disposition"), `filename="Go live.md"`) {
		t.Fatalf("download by epic: %d %q", resp.StatusCode, resp.Header.Get("Content-Disposition"))
	}
	req, _ := http.NewRequest(http.MethodDelete, r.url+"/api/documents/Go%20live?epic=launch&project=DOC", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete by epic name status = %d", resp.StatusCode)
	}
}

func TestSearchDocumentsOverHTTP(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)
	if _, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{
		TicketID: tk.ID, Name: "Report", Format: models.DocumentFormatHTML,
		Content: "<style>p { margin: 0 }</style><p>Readable words</p>",
	}); err != nil {
		t.Fatal(err)
	}

	type result struct {
		TicketIDs []string `json:"ticketIds"`
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"q=SPEC&projectId=DOC", []string{tk.ID}},
		{"q=design+spec.md&projectId=doc", []string{tk.ID}},
		{"q=readable+WORDS&projectId=DOC", []string{tk.ID}},
		{"q=style&projectId=DOC", []string{}},
		{"q=margin&projectId=DOC", []string{}},
		{"q=spec&projectId=NOPE", []string{}},
		{"q=", []string{}},
	} {
		got, status := doRequest[result](t, http.MethodGet, r.url+"/api/documents/search?"+tc.query, "")
		if status != http.StatusOK || got.TicketIDs == nil || !slices.Equal(got.TicketIDs, tc.want) {
			t.Errorf("search %s: %d %#v, want %#v", tc.query, status, got.TicketIDs, tc.want)
		}
	}
}

// TestDocumentAPITakesNoPath: the HTTP API has no path argument, since it can
// be reached from elsewhere (through ngrok) and a path would let a remote
// caller make the server read its local files. A path sent anyway is ignored:
// the file is never read into a document.
func TestDocumentAPITakesNoPath(t *testing.T) {
	r := serve(t)
	tk, d := seedTicketDocument(t, r)
	secret := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(secret, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, status := doRequest[models.Document](t, http.MethodPost, r.url+"/api/documents",
		`{"ticketId":"`+tk.ID+`","name":"Notes","path":`+strconv.Quote(secret)+`}`)
	if status != http.StatusCreated || created.Content != "" || created.Format != models.DocumentFormatMarkdown {
		t.Fatalf("create with path: %d %+v", status, created)
	}

	body, status := errorBody(t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"path":`+strconv.Quote(secret)+`}`)
	if status != http.StatusBadRequest || !strings.HasPrefix(body.Error, "nothing to update") {
		t.Fatalf("update with path: %d %q", status, body.Error)
	}
	if cur, _ := r.srv.store.GetDocument(d.ID); cur.Content != "# Spec\n" {
		t.Fatalf("update with path changed the content: %q", cur.Content)
	}
}
