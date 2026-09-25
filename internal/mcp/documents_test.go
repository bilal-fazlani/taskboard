package mcp

import (
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

func seedMCPTicket(t *testing.T, s *MCPServer) *models.Ticket {
	t.Helper()
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Has docs"})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestGetTicketToolListsDocuments(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	if _, err := s.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Plan", Content: "x"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.callTool("get_ticket", mustJSON(t, map[string]any{"id": "DOC-1"}))
	if err != nil {
		t.Fatal(err)
	}
	full := got.(*models.Ticket)
	if full.DocumentCount != 1 || len(full.Documents) != 1 ||
		full.Documents[0].URL != "http://board.test/?ticket=DOC-1&doc=Plan.md" {
		t.Fatalf("get_ticket documents = %d %+v", full.DocumentCount, full.Documents)
	}
	if full.Documents[0].Size != 1 || full.Documents[0].Name != "Plan" || full.Documents[0].Format != models.DocumentFormatMarkdown {
		t.Fatalf("get_ticket document meta = %+v", full.Documents[0])
	}
}

func TestDocumentTools(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)

	created, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"ticket": "doc-1", "name": "Design spec", "content": "# v1",
	}))
	if err != nil {
		t.Fatalf("create_document: %v", err)
	}
	d := created.(*models.Document)
	if d.URL != "http://board.test/?ticket=DOC-1&doc=Design+spec.md" || d.Format != models.DocumentFormatMarkdown {
		t.Fatalf("created = %+v", d.DocumentMeta)
	}

	for _, args := range []map[string]any{
		{"id": d.ID},
		{"id": "design spec", "ticket": "DOC-1"},
		{"id": "Design spec.md", "ticket": "DOC-1"},
	} {
		got, err := s.callTool("get_document", mustJSON(t, args))
		if err != nil || got.(*models.Document).Content != "# v1" {
			t.Fatalf("get_document %v = %+v, %v", args, got, err)
		}
	}

	updated, err := s.callTool("update_document", mustJSON(t, map[string]any{
		"id": "Design spec", "ticket": "DOC-1", "content": "# v2", "name": "Plan",
	}))
	if err != nil {
		t.Fatalf("update_document: %v", err)
	}
	u := updated.(*models.Document)
	if u.Name != "Plan" || u.Content != "# v2" || u.Revision != 2 || !strings.HasSuffix(u.URL, "doc=Plan.md") {
		t.Fatalf("updated = %+v", u)
	}

	if _, err := s.callTool("delete_document", mustJSON(t, map[string]any{"id": u.ID})); err != nil {
		t.Fatalf("delete_document: %v", err)
	}
	if _, err := s.callTool("get_document", mustJSON(t, map[string]any{"id": u.ID})); err == nil {
		t.Fatal("get_document after delete should fail")
	}
}

func TestCreateHTMLDocumentTool(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)
	got, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"ticket": "DOC-1", "name": "Report", "format": "html", "content": "<h1>x</h1>",
	}))
	if err != nil {
		t.Fatal(err)
	}
	d := got.(*models.Document)
	if d.Format != models.DocumentFormatHTML || !strings.HasSuffix(d.URL, "doc=Report.html") {
		t.Fatalf("created = %+v", d.DocumentMeta)
	}
	read, err := s.callTool("get_document", mustJSON(t, map[string]any{"id": "Report.html", "ticket": "DOC-1"}))
	if err != nil || read.(*models.Document).Content != "<h1>x</h1>" {
		t.Fatalf("get_document Report.html = %+v, %v", read, err)
	}
	_, err = s.callTool("create_document", mustJSON(t, map[string]any{
		"ticket": "DOC-1", "name": "Deck", "format": "pdf", "content": "x",
	}))
	if err == nil || err.Error() != `Format must be "markdown" or "html".` {
		t.Fatalf("unknown format error = %v", err)
	}
}

func TestCreateDocumentToolOffersHTML(t *testing.T) {
	s := newTestServer(t)
	for _, def := range s.toolDefinitions() {
		if def.Name != "create_document" {
			continue
		}
		enum := def.InputSchema.Properties["format"].Enum
		if len(enum) != 2 || enum[0] != models.DocumentFormatMarkdown || enum[1] != models.DocumentFormatHTML {
			t.Fatalf("create_document format enum = %v", enum)
		}
		return
	}
	t.Fatal("create_document not listed")
}

func TestDocumentToolErrors(t *testing.T) {
	s := newTestServer(t)
	seedMCPTicket(t, s)

	_, err := s.callTool("create_document", mustJSON(t, map[string]any{"ticket": "DOC-1", "name": "plan.md"}))
	if err == nil || err.Error() != "Use letters, digits, spaces, _ and - only." {
		t.Fatalf("bad name error = %v", err)
	}
	_, err = s.callTool("create_document", mustJSON(t, map[string]any{"name": "Plan"}))
	if err == nil || err.Error() != "ticket or epic is required" {
		t.Fatalf("missing ticket error = %v", err)
	}
	_, err = s.callTool("get_document", mustJSON(t, map[string]any{"id": "Plan"}))
	if err == nil || !strings.Contains(err.Error(), "pass ticket") {
		t.Fatalf("name without ticket error = %v", err)
	}
	_, err = s.callTool("update_document", mustJSON(t, map[string]any{"id": "x"}))
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("empty update error = %v", err)
	}
}

func TestDocumentToolsAreListed(t *testing.T) {
	s := newTestServer(t)
	names := map[string]bool{}
	for _, def := range s.toolDefinitions() {
		names[def.Name] = true
	}
	for _, want := range []string{"list_documents", "get_document", "create_document", "update_document", "delete_document"} {
		if !names[want] {
			t.Errorf("tools/list is missing %s", want)
		}
	}
}

func TestEpicDocumentTools(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	if _, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: tk.ProjectID, Name: "Launch"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"epic": "launch", "project": "DOC", "name": "Rollout", "content": "# r",
	}))
	if err != nil {
		t.Fatalf("create_document on epic: %v", err)
	}
	d := got.(*models.Document)
	if d.URL != "http://board.test/epics?project=DOC&epic=Launch&doc=Rollout.md" || d.EpicID == "" || d.TicketID != "" {
		t.Fatalf("created = %+v", d.DocumentMeta)
	}
	_, err = s.callTool("create_document", mustJSON(t, map[string]any{
		"epic": "Launch", "project": "DOC", "name": "rollout", "content": "",
	}))
	if err == nil || err.Error() != `This epic already has a document called "Rollout.md".` {
		t.Fatalf("taken name on epic error = %v", err)
	}

	listed, err := s.callTool("list_documents", mustJSON(t, map[string]any{"epic": "Launch", "project": "doc"}))
	if err != nil {
		t.Fatal(err)
	}
	docs := listed.([]models.DocumentMeta)
	if len(docs) != 1 || docs[0].URL != d.URL {
		t.Fatalf("list_documents on epic = %+v", docs)
	}
	listed, err = s.callTool("list_documents", mustJSON(t, map[string]any{"ticket": "DOC-1"}))
	if err != nil || len(listed.([]models.DocumentMeta)) != 0 {
		t.Fatalf("list_documents on ticket = %+v, %v", listed, err)
	}
	if _, err := s.callTool("list_documents", mustJSON(t, map[string]any{})); err == nil || err.Error() != "ticket or epic is required" {
		t.Fatalf("list_documents with no owner error = %v", err)
	}

	read, err := s.callTool("get_document", mustJSON(t, map[string]any{"id": "rollout.md", "epic": "Launch", "project": "DOC"}))
	if err != nil || read.(*models.Document).Content != "# r" || read.(*models.Document).URL != d.URL {
		t.Fatalf("get_document by epic name = %+v, %v", read, err)
	}
	if _, err := s.callTool("get_document", mustJSON(t, map[string]any{"id": "Rollout", "epic": d.EpicID})); err != nil {
		t.Fatalf("get_document by epic id: %v", err)
	}
	_, err = s.callTool("get_document", mustJSON(t, map[string]any{"id": "Rollout", "epic": "Launch", "project": "DOC", "ticket": "DOC-1"}))
	if err == nil || err.Error() != "pass ticket or epic, not both" {
		t.Fatalf("both owners error = %v", err)
	}
	_, err = s.callTool("get_document", mustJSON(t, map[string]any{"id": "Rollout", "epic": "Launch"}))
	if err == nil || !strings.Contains(err.Error(), "pass project") {
		t.Fatalf("epic name without project error = %v", err)
	}

	updated, err := s.callTool("update_document", mustJSON(t, map[string]any{
		"id": "Rollout", "epic": "Launch", "project": "DOC", "name": "Go live", "content": "# v2",
	}))
	if err != nil {
		t.Fatalf("update_document on epic: %v", err)
	}
	if u := updated.(*models.Document); u.URL != "http://board.test/epics?project=DOC&epic=Launch&doc=Go+live.md" || u.Content != "# v2" {
		t.Fatalf("updated = %+v", u)
	}
	if _, err := s.callTool("delete_document", mustJSON(t, map[string]any{"id": "go live", "epic": "Launch", "project": "DOC"})); err != nil {
		t.Fatalf("delete_document on epic: %v", err)
	}
	listed, _ = s.callTool("list_documents", mustJSON(t, map[string]any{"epic": "Launch", "project": "DOC"}))
	if len(listed.([]models.DocumentMeta)) != 0 {
		t.Fatalf("after delete, list_documents = %+v", listed)
	}
}

func TestDeleteEpicToolSaysItsDocumentsAreDeleted(t *testing.T) {
	s := newTestServer(t)
	for _, def := range s.toolDefinitions() {
		if def.Name != "delete_epic" {
			continue
		}
		if !strings.Contains(def.Description, "Its own documents are deleted for good") {
			t.Fatalf("delete_epic description = %q", def.Description)
		}
		return
	}
	t.Fatal("delete_epic not listed")
}
