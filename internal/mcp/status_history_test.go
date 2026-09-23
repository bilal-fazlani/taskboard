package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func seedTicketInReview(t *testing.T, s *MCPServer) *models.Ticket {
	t.Helper()
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Agent Control Plane", Prefix: "ACP"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Reviewed", Status: models.StatusAgentReview})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

// callToolText runs a tool the way an MCP client does and returns the text it
// gets back, and whether it was flagged as an error.
func callToolText(t *testing.T, s *MCPServer, name string, args map[string]any) (string, bool) {
	t.Helper()
	params := mustJSON(t, map[string]any{"name": name, "arguments": args})
	resp := s.handleRequest(jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
	result := resp.Result.(map[string]any)
	content := result["content"].([]textContent)
	isError, _ := result["isError"].(bool)
	return content[0].Text, isError
}

func TestMoveAndUpdateToolsRequireANoteLeavingReview(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"move_ticket", map[string]any{"status": "in_progress"}},
		{"move_ticket", map[string]any{"status": "done", "note": "   "}},
		{"update_ticket", map[string]any{"status": "done"}},
		{"update_ticket", map[string]any{"status": "todo", "note": ""}},
	} {
		s := newTestServer(t)
		tk := seedTicketInReview(t, s)
		tc.args["id"] = "ACP-1"

		text, isError := callToolText(t, s, tc.tool, tc.args)
		if !isError {
			t.Fatalf("%s %v: succeeded without a note, want an error", tc.tool, tc.args)
		}
		// The error is the agent's documentation for the rule: it says a note
		// is required leaving agent_review, and what to put in it.
		for _, want := range []string{"note is required", "out of agent_review", "approved and landed", "findings"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s error %q does not mention %q", tc.tool, text, want)
			}
		}
		got, _ := s.store.GetTicket(tk.ID)
		if got.Status != models.StatusAgentReview {
			t.Fatalf("%s without a note moved the ticket to %q", tc.tool, got.Status)
		}
	}
}

func TestMoveAndUpdateToolsSaveTheNote(t *testing.T) {
	s := newTestServer(t)
	tk := seedTicketInReview(t, s)

	if _, err := s.callTool("move_ticket", mustJSON(t, map[string]any{
		"id": "ACP-1", "status": "in_progress", "note": "bounced: the migration has no index",
	})); err != nil {
		t.Fatalf("move_ticket with a note: %v", err)
	}
	// Into review, and anything else not leaving it, needs no note.
	if _, err := s.callTool("update_ticket", mustJSON(t, map[string]any{"id": "ACP-1", "status": "agent_review"})); err != nil {
		t.Fatalf("update_ticket into agent_review: %v", err)
	}
	if _, err := s.callTool("update_ticket", mustJSON(t, map[string]any{
		"id": "ACP-1", "status": "done", "note": "approved and landed",
	})); err != nil {
		t.Fatalf("update_ticket with a note: %v", err)
	}

	changes, err := s.store.ListStatusChanges(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range changes {
		got = append(got, c.FromStatus+">"+c.ToStatus+":"+c.Note)
	}
	want := []string{
		"agent_review>done:approved and landed",
		"in_progress>agent_review:",
		"agent_review>in_progress:bounced: the migration has no index",
		">agent_review:",
	}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("history = %q, want %q", got, want)
	}
}

func TestGetTicketToolReturnsHistory(t *testing.T) {
	s := newTestServer(t)
	tk := seedTicketInReview(t, s)
	if _, err := s.store.MoveTicket(tk.ID, models.MoveTicketRequest{Status: "in_progress", Note: "needs tests"}); err != nil {
		t.Fatal(err)
	}

	result, err := s.callTool("get_ticket", mustJSON(t, map[string]any{"id": "acp-1"}))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	var shape struct {
		ReviewRounds int `json:"reviewRounds"`
		History      []struct {
			FromStatus string `json:"fromStatus"`
			ToStatus   string `json:"toStatus"`
			Note       string `json:"note"`
			CreatedAt  string `json:"createdAt"`
		} `json:"history"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		t.Fatal(err)
	}
	if shape.ReviewRounds != 1 {
		t.Fatalf("reviewRounds = %d, want 1", shape.ReviewRounds)
	}
	if len(shape.History) != 2 {
		t.Fatalf("history = %s, want 2 entries", data)
	}
	first, birth := shape.History[0], shape.History[1]
	if first.FromStatus != "agent_review" || first.ToStatus != "in_progress" || first.Note != "needs tests" || first.CreatedAt == "" {
		t.Fatalf("newest entry = %+v", first)
	}
	if birth.FromStatus != "" || birth.ToStatus != "agent_review" {
		t.Fatalf("birth entry = %+v", birth)
	}

	// Other tools leave history out.
	listed, err := s.callTool("list_tickets", mustJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if listed.([]models.Ticket)[0].History != nil {
		t.Fatal("list_tickets returned history")
	}
}

func TestNoteParamDescribesTheRule(t *testing.T) {
	s := newTestServer(t)
	for _, def := range s.toolDefinitions() {
		if def.Name != "move_ticket" && def.Name != "update_ticket" {
			continue
		}
		note, ok := def.InputSchema.Properties["note"]
		if !ok {
			t.Fatalf("%s takes no note", def.Name)
		}
		if note.Type != "string" || !strings.Contains(note.Description, "Required when moving a ticket out of agent_review") {
			t.Fatalf("%s note = %+v", def.Name, note)
		}
		for _, r := range def.InputSchema.Required {
			if r == "note" {
				t.Fatalf("%s marks note required for every call", def.Name)
			}
		}
	}
}
