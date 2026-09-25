package mcp

import (
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
