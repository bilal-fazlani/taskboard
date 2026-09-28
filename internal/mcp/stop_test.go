package mcp

import (
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
	go func() {
		time.Sleep(300 * time.Millisecond)
		web.StopWork("ACP-1", "bilal")
	}()
	got := callJSON(t, f.s, "await_answer", map[string]any{"request": id, "agentId": agent, "timeoutSeconds": 20})
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
}
