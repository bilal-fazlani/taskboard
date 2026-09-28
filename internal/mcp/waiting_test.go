package mcp

import (
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// move_ticket's and update_ticket's descriptions say a waiting ticket can't
// be moved out while its request is open, and the two ways out.
func TestMoveAndUpdateTicketDescribeTheWaitingRule(t *testing.T) {
	f := newAgentServer(t)
	for _, tool := range []string{"move_ticket", "update_ticket"} {
		desc := findToolDef(t, f.s, tool).Description
		for _, want := range []string{"needs_user_input cannot be moved out while its request is open",
			"answering the request on the ticket page", "Stop work"} {
			if !strings.Contains(desc, want) {
				t.Fatalf("%s's description has no %q: %s", tool, want, desc)
			}
		}
	}
}

// move_ticket and update_ticket refuse to take a ticket out of
// needs_user_input while its request is open, with a message naming the
// request and the two ways out; the ticket stays waiting. Once the person
// answers, the ticket moves again.
func TestMovingAWaitingTicketOutIsRefused(t *testing.T) {
	f := newAgentServer(t)
	agent := f.identify(t, "3da2c294", "implementer")
	f.claim(t, agent)
	id := callJSON(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "approval",
		"prompt": "Land the branch on main?"})["id"].(string)

	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"move_ticket", map[string]any{"id": "ACP-1", "status": models.StatusTodo, "agentId": agent}},
		{"update_ticket", map[string]any{"id": "ACP-1", "status": models.StatusDone, "title": "Renamed", "note": "landed", "agentId": agent}},
	} {
		text := callError(t, f.s, call.tool, call.args)
		for _, want := range []string{"ACP-1", id, "answering the request on the ticket page", "Stop work"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: %s; want it to contain %q", call.tool, text, want)
			}
		}
	}
	got, err := f.s.store.GetTicket(f.ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusNeedsUserInput || got.Title != "Agent tools" || got.OpenRequest == nil || got.OpenRequest.ID != id {
		t.Fatalf("ticket after the refused calls: status %s, title %q, open request %+v", got.Status, got.Title, got.OpenRequest)
	}

	if _, err := f.s.store.AnswerRequest(id, "approved", "bilal", ""); err != nil {
		t.Fatal(err)
	}
	moved := callJSON(t, f.s, "move_ticket", map[string]any{"id": "ACP-1", "status": models.StatusAgentReview, "agentId": agent})
	if moved["key"] != "ACP-1" {
		t.Fatalf("move_ticket after the answer = %v", moved)
	}
	if got, _ := f.s.store.GetTicket(f.ticket.ID); got.Status != models.StatusAgentReview {
		t.Fatalf("after the answer and a move: status %s, want agent_review", got.Status)
	}
}
