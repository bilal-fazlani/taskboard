package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// ticket move --help and ticket update --help say a waiting ticket can't be
// moved out while its request is open, and the two ways out.
func TestTicketMoveAndUpdateHelpSayAWaitingTicketStays(t *testing.T) {
	for _, cmd := range []string{"move", "update"} {
		out, err := runCLI(t, "ticket", cmd, "--help")
		if err != nil {
			t.Fatal(err)
		}
		flat := strings.Join(strings.Fields(out), " ")
		for _, want := range []string{"needs_user_input cannot be moved out while its request is open", "'request answer'", "'ticket stop'"} {
			if !strings.Contains(flat, want) {
				t.Fatalf("ticket %s --help has no %q:\n%s", cmd, want, out)
			}
		}
	}
}

// ticket move and ticket update --status refuse to take a ticket out of
// needs_user_input while its request is open, printing the error that names
// the request and the two ways out; ticket stop still takes it out.
func TestMovingAWaitingTicketOutIsRefused(t *testing.T) {
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
	agentOut := run("agent", "identify", "--vendor", "claude_code", "--session-id", "3da2c294",
		"--role", "implementer", "--model", "claude-sonnet", "--provider", "anthropic")
	agent := identifyIDRe.FindStringSubmatch(agentOut)[1]
	claimForTest(t, path, "BILL-1", agent)
	requestID := askIDRe.FindStringSubmatch(run("ticket", "ask", "BILL-1", "--agent", agent, "--type", "approval",
		"--prompt", "Land it?"))[1]

	for _, args := range [][]string{
		{"ticket", "move", "BILL-1", "--status", "todo"},
		{"ticket", "update", "BILL-1", "--status", "done", "--title", "Renamed"},
	} {
		_, err := runCLI(t, append([]string{"--db", path}, args...)...)
		if err == nil {
			t.Fatalf("%v succeeded, want it refused", args)
		}
		for _, want := range []string{"BILL-1", requestID, "answering the request on the ticket page", "Stop work"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%v = %q, want it to contain %q", args, err, want)
			}
		}
	}
	if got := run("ticket", "get", "BILL-1"); !strings.Contains(got, "needs_user_input") || !strings.Contains(got, "Invoice") {
		t.Fatalf("ticket get after the refused writes = %q, want it still waiting, title unchanged", got)
	}

	if out := run("ticket", "stop", "BILL-1"); !strings.Contains(out, "now todo") {
		t.Fatalf("ticket stop = %q, want the ticket back in todo", out)
	}
	if out := run("ticket", "move", "BILL-1", "--status", "in_progress"); !strings.Contains(out, "Moved BILL-1 to in_progress") {
		t.Fatalf("ticket move after the stop = %q", out)
	}
}
