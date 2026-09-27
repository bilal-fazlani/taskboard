package mcp

import (
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// get_now answers Store.Now's three groups, compacted: no ids, descriptions,
// subtask lists or commit repos; no status (the group already says it) or
// project prefix (key already starts with it); timestamps truncated to
// whole seconds.
func TestGetNowTool(t *testing.T) {
	s := newTestServer(t)
	acp, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Control plane", Prefix: "ACP"})
	if err != nil {
		t.Fatal(err)
	}
	ldr, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Ledger", Prefix: "LDR"})
	if err != nil {
		t.Fatal(err)
	}
	create := func(projectID, title, status string) *models.Ticket {
		t.Helper()
		tk, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: projectID, Title: title, Status: status})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}

	working := create(acp.ID, "Working", models.StatusInProgress)
	st, err := s.store.AddSubtask(working.ID, models.CreateSubtaskRequest{Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.SetSubtaskState(st.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.AddSubtask(working.ID, models.CreateSubtaskRequest{Title: "two"}); err != nil {
		t.Fatal(err)
	}

	reviewed := create(ldr.ID, "Reviewed", models.StatusAgentReview)
	if _, err := s.store.CreateDocument(models.CreateDocumentRequest{TicketID: reviewed.ID, Name: "Review 1", Content: "VERDICT: APPROVE\n"}); err != nil {
		t.Fatal(err)
	}

	shipped := create(acp.ID, "Shipped", models.StatusDone)
	if _, err := s.store.UpdateTicket(shipped.ID, models.UpdateTicketRequest{Delivery: &models.DeliveryUpdate{
		LandedCommits: &[]models.LandedCommit{{SHA: "39a07fd", Repo: "acme/app"}},
	}}); err != nil {
		t.Fatal(err)
	}

	create(acp.ID, "Waiting", models.StatusTodo)

	out, err := s.callTool("get_now", mustJSON(t, map[string]any{}))
	if err != nil {
		t.Fatalf("get_now: %v", err)
	}
	n, ok := out.(nowAnswer)
	if !ok {
		t.Fatalf("get_now result type = %T, want nowAnswer", out)
	}

	if len(n.InProgress) != 1 {
		t.Fatalf("inProgress = %+v, want one ticket", n.InProgress)
	}
	w := n.InProgress[0]
	if w.Key != "ACP-1" || w.Title != "Working" {
		t.Errorf("inProgress[0] = %+v, want ACP-1 Working", w)
	}
	if w.Subtasks != "1/2" {
		t.Errorf("inProgress[0].Subtasks = %q, want 1/2", w.Subtasks)
	}
	if w.ReviewRounds != 0 || w.Review != "" {
		t.Errorf("inProgress[0] = %+v, want no review rounds and no review state", w)
	}
	if w.Since.IsZero() {
		t.Error("inProgress[0].Since is zero")
	}
	if w.Since.Nanosecond() != 0 {
		t.Errorf("inProgress[0].Since = %v, want truncated to whole seconds", w.Since)
	}

	if len(n.InReview) != 1 {
		t.Fatalf("inReview = %+v, want one ticket", n.InReview)
	}
	r := n.InReview[0]
	if r.Key != "LDR-1" || r.Review != models.NowReviewApproved || r.ReviewRounds != 1 {
		t.Errorf("inReview[0] = %+v, want LDR-1 approved in round 1", r)
	}
	if r.Subtasks != "" {
		t.Errorf("inReview[0].Subtasks = %q, want empty (no subtasks)", r.Subtasks)
	}

	if len(n.Landed) != 1 {
		t.Fatalf("landed = %+v, want one ticket", n.Landed)
	}
	l := n.Landed[0]
	if l.Key != "ACP-2" || l.Title != "Shipped" {
		t.Errorf("landed[0] = %+v, want ACP-2 Shipped", l)
	}
	if len(l.Shas) != 1 || l.Shas[0] != "39a07fd" {
		t.Errorf("landed[0].Shas = %v, want [39a07fd]", l.Shas)
	}
	if l.DoneAt.IsZero() {
		t.Error("landed[0].DoneAt is zero")
	}
	if l.DoneAt.Nanosecond() != 0 {
		t.Errorf("landed[0].DoneAt = %v, want truncated to whole seconds", l.DoneAt)
	}

	// Compact: the JSON an agent actually reads carries no id, description,
	// subtask list, url, commit repo, status, project prefix or fractional
	// seconds on a timestamp.
	text, isError := callToolText(t, s, "get_now", map[string]any{})
	if isError {
		t.Fatalf("get_now errored: %s", text)
	}
	for _, absent := range []string{
		`"id"`, `"description"`, `"subtasksDone"`, `"subtasksTotal"`, `"url"`, `"repo"`, `acme/app`,
		`"status"`, `"projectPrefix"`, `.`,
	} {
		if strings.Contains(text, absent) {
			t.Errorf("get_now text contains %s, want it left out: %s", absent, text)
		}
	}
	assertCompactJSON(t, "get_now", text)
}

// A project filter narrows all three groups; an unknown project matches
// nothing, like get_board.
func TestGetNowToolProjectFilter(t *testing.T) {
	s := newTestServer(t)
	acp, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Control plane", Prefix: "ACP"})
	if err != nil {
		t.Fatal(err)
	}
	ldr, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Ledger", Prefix: "LDR"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: acp.ID, Title: "A", Status: models.StatusInProgress}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: ldr.ID, Title: "L", Status: models.StatusAgentReview}); err != nil {
		t.Fatal(err)
	}

	out, err := s.callTool("get_now", mustJSON(t, map[string]any{"projectId": "ldr"}))
	if err != nil {
		t.Fatalf("get_now(projectId=ldr): %v", err)
	}
	n := out.(nowAnswer)
	if len(n.InProgress) != 0 || len(n.InReview) != 1 || n.InReview[0].Key != "LDR-1" {
		t.Errorf("get_now(projectId=ldr) = %+v, want only LDR-1 in review", n)
	}

	// project is projectId's alias.
	out, err = s.callTool("get_now", mustJSON(t, map[string]any{"project": "acp"}))
	if err != nil {
		t.Fatalf("get_now(project=acp): %v", err)
	}
	n = out.(nowAnswer)
	if len(n.InProgress) != 1 || n.InProgress[0].Key != "ACP-1" {
		t.Errorf("get_now(project=acp) = %+v, want ACP-1 in progress", n)
	}

	out, err = s.callTool("get_now", mustJSON(t, map[string]any{"projectId": "NOPE"}))
	if err != nil {
		t.Fatalf("get_now(projectId=NOPE): %v", err)
	}
	n = out.(nowAnswer)
	if len(n.InProgress) != 0 || len(n.InReview) != 0 || len(n.Landed) != 0 {
		t.Errorf("get_now(projectId=NOPE) = %+v, want everything empty", n)
	}
}

// An empty board answers with [] for every group, never null, the same
// convention every other MCP list tool follows (ACP-148).
func TestGetNowToolEmptyBoardIsArrays(t *testing.T) {
	s := newTestServer(t)

	text, isError := callToolText(t, s, "get_now", map[string]any{})
	if isError {
		t.Fatalf("get_now errored: %s", text)
	}
	want := `{"inProgress":[],"inReview":[],"landed":[]}`
	if text != want {
		t.Errorf("get_now on an empty board = %s, want %s", text, want)
	}
}

// get_now's description tells an agent when to reach for it instead of
// list_tickets.
func TestGetNowToolDescriptionSaysWhenToUseIt(t *testing.T) {
	s := newTestServer(t)
	for _, def := range s.toolDefinitions() {
		if def.Name != "get_now" {
			continue
		}
		if !strings.Contains(def.Description, "list_tickets") {
			t.Errorf("get_now description doesn't say when to use it instead of list_tickets: %s", def.Description)
		}
		return
	}
	t.Fatal("no get_now tool definition")
}
