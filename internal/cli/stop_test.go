package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// ticket stop, the person's own command, frees a ticket an agent holds: back
// in todo with no agent, recorded as stopped by the local user. The stopped
// agent's next write is refused as stopped by the person, all but its
// hand-off. A ticket no agent holds has nothing to stop.
func TestTicketStopCommand(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	run := func(args ...string) string {
		t.Helper()
		out, err := runCLI(t, append([]string{"--db", path}, args...)...)
		if err != nil {
			t.Fatalf("%v: %q, %v", args, out, err)
		}
		return out
	}
	run("project", "create", "Billing", "--prefix", "BILL")
	run("ticket", "create", "--project", "BILL", "--title", "Invoice")
	run("ticket", "create", "--project", "BILL", "--title", "Refund")
	agentOut := run("agent", "identify", "--vendor", "claude_code", "--session-id", "3da2c294",
		"--role", "implementer", "--model", "claude-sonnet", "--provider", "anthropic")
	agent := identifyIDRe.FindStringSubmatch(agentOut)[1]

	if _, err := runCLI(t, "--db", path, "ticket", "stop", "BILL-1"); err == nil || !strings.Contains(err.Error(), "not held by any agent") {
		t.Fatalf("stop an unheld ticket = %v, want it refused", err)
	}

	claimForTest(t, path, "BILL-1", agent)
	if out := run("ticket", "stop", "BILL-1"); !strings.Contains(out, "Stopped work on BILL-1: now todo") {
		t.Fatalf("ticket stop = %q", out)
	}
	if history := run("ticket", "history", "BILL-1"); !strings.Contains(history, "Stopped by the person, "+defaultAuthor()+".") ||
		!strings.Contains(history, agent) {
		t.Fatalf("history after the stop = %q, want the person and the agent named", history)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "release", "BILL-1", "--agent", agent, "--done",
		"--proof", "go test passes"); err == nil || !strings.Contains(err.Error(), "stopped by the person") {
		t.Fatalf("the stopped agent finishing = %v, want it refused as stopped by the person", err)
	}
	if out := run("ticket", "release", "BILL-1", "--agent", agent, "--stopped", "totals", "--next", "tax"); !strings.Contains(out, "now todo") {
		t.Fatalf("the stopped agent's hand-off = %q, want it accepted", out)
	}

	// The person's own writes on the stopped ticket take no agent and are
	// never refused: editing it, moving it, ticking its subtasks.
	run("ticket", "update", "BILL-1", "--title", "Invoice v2")
	run("ticket", "move", "BILL-1", "--status", "in_progress")
	added := run("ticket", "subtask", "add", "BILL-1", "Tax")
	subtaskID := added[strings.LastIndex(added, "(")+1 : strings.LastIndex(added, ")")]
	if out := run("ticket", "subtask", "toggle", subtaskID); !strings.Contains(out, "Tax is now done") {
		t.Fatalf("the person's subtask toggle after the stop = %q", out)
	}

	// --json prints the stopped ticket itself.
	claimForTest(t, path, "BILL-2", agent)
	var stopped models.Ticket
	if out := run("ticket", "stop", "BILL-2", "--json"); json.Unmarshal([]byte(out), &stopped) != nil ||
		stopped.Status != models.StatusTodo || stopped.Agent != nil {
		t.Fatalf("ticket stop --json = %q", out)
	}
}
