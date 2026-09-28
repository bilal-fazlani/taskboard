package mcp

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// An approval still open when the person stops work closes with the fixed
// stop text, neither approved nor declined: await_answer hands the agent
// that text, a wait in progress included, and its next write on the ticket
// is refused as stopped by the person while its hand-off is still accepted.
// Starting the ticket again says who stopped it and when.
func TestAwaitAnswerOnAStoppedApproval(t *testing.T) {
	f := newAgentServer(t)
	agent := f.identify(t, "3da2c294", "implementer")
	f.claim(t, agent)
	id := callJSON(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "approval",
		"prompt": "Land the branch on main?"})["id"].(string)

	web := f.secondStore(t)
	stopErr := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, err := web.StopWork("ACP-1", "bilal")
		stopErr <- err
	}()
	got := callJSON(t, f.s, "await_answer", map[string]any{"request": id, "agentId": agent, "timeoutSeconds": 20})
	if err := <-stopErr; err != nil {
		t.Fatalf("stopping work on ACP-1: %v", err)
	}
	if got["answered"] != true || got["answer"] != models.StoppedAnswer || got["answeredBy"] != "bilal" {
		t.Fatalf("await_answer on a stopped approval = %v, want answered with %q by bilal", got, models.StoppedAnswer)
	}
	if got["answer"] == models.ApprovalApproved || got["answer"] == models.ApprovalDeclined {
		t.Fatalf("a stopped approval reads as %v", got["answer"])
	}

	if text := callError(t, f.s, "write_entry", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "learning",
		"text": "Carried on anyway"}); !strings.Contains(text, "stopped by the person") {
		t.Errorf("the stopped agent's entry: %s; want it refused as stopped by the person", text)
	}
	got = callJSON(t, f.s, "release_ticket", map[string]any{"ticket": "ACP-1", "agentId": agent, "outcome": "give_back",
		"handOff": "Branch ready; landing waits on the person"})
	if got["status"] != models.StatusTodo {
		t.Fatalf("the stopped agent's hand-off = %v, want it accepted with the ticket in todo", got)
	}

	got = callJSON(t, f.s, "start_ticket", map[string]any{"ticket": "ACP-1", "agentId": agent})
	stopped, _ := got["stopped"].(map[string]any)
	if stopped == nil || stopped["by"] != "bilal" || stopped["at"] == nil {
		t.Fatalf("start_ticket after the stop: stopped = %v, want by bilal with its time", got["stopped"])
	}

	// The request Stop work closed shows as stopped, not as an answer: no
	// answer or note, only who stopped the work and when. get_ticket carries
	// the same.
	ticket := got["ticket"].(map[string]any)
	answered, _ := ticket["answeredRequests"].([]any)
	if len(answered) != 1 {
		t.Fatalf("start_ticket's answeredRequests = %v, want the one closed by the stop", ticket["answeredRequests"])
	}
	entry := answered[0].(map[string]any)
	if entry["type"] != "approval" || entry["prompt"] != "Land the branch on main?" || entry["stopped"] != true ||
		entry["answer"] != nil || entry["note"] != nil || entry["answeredBy"] != "bilal" || entry["answeredAt"] == nil {
		t.Fatalf("the stopped request's answeredRequests entry = %v, want it stopped with no answer", entry)
	}

	fromGetTicket := callJSON(t, f.s, "get_ticket", map[string]any{"id": "ACP-1"})
	if !reflect.DeepEqual(fromGetTicket["answeredRequests"], ticket["answeredRequests"]) {
		t.Fatalf("get_ticket's answeredRequests = %v, want the same as start_ticket's %v",
			fromGetTicket["answeredRequests"], ticket["answeredRequests"])
	}
}

// Once the person stops the work, the stopped session's update_ticket,
// move_ticket and toggle_subtask on the ticket are refused like its other
// writes, pointing it at its hand-off, and change nothing; the hand-off is
// still accepted. The person's own writes, which take no agent, go through.
func TestAStoppedAgentsTicketWritesAreRefused(t *testing.T) {
	f := newAgentServer(t)
	agent := f.identify(t, "3da2c294", "implementer")
	orchestrator := f.identify(t, "3da2c294", "orchestrator")
	sub, err := f.s.store.AddSubtask(f.ticket.ID, models.CreateSubtaskRequest{Title: "Store"})
	if err != nil {
		t.Fatal(err)
	}
	f.claim(t, agent)
	if _, err := f.secondStore(t).StopWork("ACP-1", "bilal"); err != nil {
		t.Fatal(err)
	}

	// The whole session is stopped on the ticket, its orchestrator too.
	for _, id := range []string{agent, orchestrator} {
		for _, c := range []struct {
			tool string
			args map[string]any
		}{
			{"update_ticket", map[string]any{"id": "ACP-1", "title": "Renamed", "agentId": id}},
			{"move_ticket", map[string]any{"id": "ACP-1", "status": "agent_review", "agentId": id}},
			{"toggle_subtask", map[string]any{"id": sub.ID, "completed": true, "agentId": id}},
		} {
			text := callError(t, f.s, c.tool, c.args)
			if !strings.Contains(text, "stopped by the person") || !strings.Contains(text, "hand-off") {
				t.Errorf("%s by stopped agent %s: %s; want it refused as stopped by the person", c.tool, id, text)
			}
		}
	}
	got, err := f.s.store.GetTicket(f.ticket.ID)
	if err != nil || got.Title != "Agent tools" || got.Status != models.StatusTodo || got.Subtasks[0].Completed {
		t.Fatalf("the refused writes changed the ticket: %+v, %v", got, err)
	}

	callJSON(t, f.s, "release_ticket", map[string]any{"ticket": "ACP-1", "agentId": agent, "outcome": "give_back",
		"handOff": "Store done; the MCP tools are next"})
	title := "Renamed by the person"
	if _, err := f.s.store.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Title: &title}); err != nil {
		t.Fatalf("the person's update after the stop: %v", err)
	}
	if _, err := f.s.store.SetSubtaskState(sub.ID, true); err != nil {
		t.Fatalf("the person's tick after the stop: %v", err)
	}
}
