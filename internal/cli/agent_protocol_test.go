package cli

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// askIDRe pulls the new request's id out of `ticket ask`'s readable output:
// "Request <id> (<type>) created on <ticket>".
var askIDRe = regexp.MustCompile(`^Request (\S+) \(approval\) created on`)

// ticket ask creates a request for user input and prints its id, refusing a
// second one while the first is open; request answer (the person's own
// command) answers it, refusing an answer outside the request's choices;
// request await returns once it is answered, or says it is still waiting
// after --timeout.
func TestTicketAskAndRequestCommands(t *testing.T) {
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
	claimForTest(t, path, "BILL-1", agent)
	claimForTest(t, path, "BILL-2", agent)

	// --json prints the new request itself.
	jsonAsk := run("ticket", "ask", "BILL-2", "--agent", agent, "--type", "question", "--prompt", "Which vendor?", "--json")
	var asked models.TicketRequest
	if err := json.Unmarshal([]byte(jsonAsk), &asked); err != nil || asked.Type != "question" || asked.Prompt != "Which vendor?" {
		t.Fatalf("ask --json = %q: %v", jsonAsk, err)
	}

	askOut := run("ticket", "ask", "BILL-1", "--agent", agent, "--type", "approval",
		"--prompt", "Land it?", "--choice", "yes", "--choice", "no")
	m := askIDRe.FindStringSubmatch(askOut)
	if m == nil {
		t.Fatalf("ask = %q, want a request id", askOut)
	}
	requestID := m[1]
	if got := run("ticket", "get", "BILL-1"); !strings.Contains(got, "needs_user_input") {
		t.Fatalf("ticket get after ask = %q, want needs_user_input", got)
	}
	// --status accepts needs_user_input, a status only a request puts a
	// ticket in (ACP-12): it is a filter, not a write target.
	if got := run("ticket", "list", "--status", "needs_user_input", "--project", "BILL", "--summary"); !strings.Contains(got, "BILL-1") {
		t.Fatalf("ticket list --status needs_user_input = %q, want BILL-1", got)
	}

	// A ticket has one open request at a time.
	if _, err := runCLI(t, "--db", path, "ticket", "ask", "BILL-1", "--agent", agent,
		"--type", "question", "--prompt", "Sure?"); err == nil || !strings.Contains(err.Error(), "already waits on request") {
		t.Fatalf("a second open request = %v, want it refused", err)
	}

	// await times out fast when nobody has answered yet.
	if out := run("request", "await", requestID, "--timeout", "50ms"); !strings.Contains(out, "still waiting") {
		t.Fatalf("await before an answer = %q, want still waiting", out)
	}

	// A zero or negative timeout is refused, the way the HTTP await is.
	for _, timeout := range []string{"0s", "-5s"} {
		_, err := runCLI(t, "--db", path, "request", "await", requestID, "--timeout", timeout)
		if err == nil || !strings.Contains(err.Error(), "timeout must be a positive duration") {
			t.Fatalf("await --timeout %s = %v, want it refused", timeout, err)
		}
	}

	// answer is the person's, and must be one of the request's choices.
	if _, err := runCLI(t, "--db", path, "request", "answer", requestID, "--answer", "maybe"); err == nil ||
		!strings.Contains(err.Error(), "is not one of the request's choices") {
		t.Fatalf("an answer outside the choices = %v, want it refused", err)
	}
	answerOut := run("request", "answer", requestID, "--answer", "yes")
	if !strings.Contains(answerOut, "Answered request "+requestID) || !strings.Contains(answerOut, "yes") {
		t.Fatalf("answer = %q", answerOut)
	}
	if _, err := runCLI(t, "--db", path, "request", "answer", requestID, "--answer", "yes"); err == nil ||
		!strings.Contains(err.Error(), "already answered") {
		t.Fatalf("answering twice = %v, want it refused", err)
	}

	// await now returns immediately with the answer.
	awaitOut := run("request", "await", requestID, "--timeout", "5s")
	if !strings.Contains(awaitOut, "answered by") || !strings.Contains(awaitOut, "yes") {
		t.Fatalf("await after an answer = %q", awaitOut)
	}

	// --json prints the answered request.
	jsonOut := run("request", "await", requestID, "--json")
	var answered models.TicketRequest
	if err := json.Unmarshal([]byte(jsonOut), &answered); err != nil || answered.Answer != "yes" {
		t.Fatalf("await --json = %q: %v", jsonOut, err)
	}
}

// ticket release gives a ticket back to todo with --stopped and --next, or
// finishes it with --done and --proof, refusing a bare or contradictory call
// and naming what is missing.
func TestTicketReleaseCommand(t *testing.T) {
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

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"ticket", "release", "BILL-1", "--agent", agent}, "give the ticket back"},
		{[]string{"ticket", "release", "BILL-1", "--agent", agent, "--stopped", "half done"}, "needs --next too"},
		{[]string{"ticket", "release", "BILL-1", "--agent", agent, "--next", "add tests"}, "needs --stopped too"},
		// A blank flag (only whitespace) is refused by name, the same as an
		// omitted one, rather than trimmed away and silently joined into a
		// hollow hand-off (Review 1).
		{[]string{"ticket", "release", "BILL-1", "--agent", agent, "--stopped", "   ", "--next", "add tests"}, "needs --stopped too"},
		{[]string{"ticket", "release", "BILL-1", "--agent", agent, "--stopped", "half done", "--next", "   "}, "needs --next too"},
		{[]string{"ticket", "release", "BILL-1", "--agent", agent, "--done"}, "finishing a ticket needs --proof"},
		{[]string{"ticket", "release", "BILL-1", "--agent", agent, "--proof", "tests pass"}, "pass --done too"},
		{[]string{"ticket", "release", "BILL-1", "--agent", agent, "--done", "--stopped", "x"}, "not both"},
	} {
		_, err := runCLI(t, append([]string{"--db", path}, c.args...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want an error saying %q", c.args, err, c.want)
		}
	}

	giveBack := run("ticket", "release", "BILL-1", "--agent", agent, "--stopped", "wired the approval flow", "--next", "add tests")
	if !strings.Contains(giveBack, "Gave back BILL-1: now todo") {
		t.Fatalf("give back = %q", giveBack)
	}
	// The hand-off is stored as one entry, "<stopped>. Next: <next>".
	handOffs := run("entry", "list", "--ticket", "BILL-1", "--type", "hand_off")
	if !strings.Contains(handOffs, "wired the approval flow. Next: add tests") {
		t.Fatalf("hand-off entries = %q, want the joined stopped/next text", handOffs)
	}

	// A stopped text that already ends in punctuation gets no extra period.
	claimForTest(t, path, "BILL-1", agent)
	run("ticket", "release", "BILL-1", "--agent", agent, "--stopped", "Land it?", "--next", "wait for review")
	handOffs = run("entry", "list", "--ticket", "BILL-1", "--type", "hand_off", "--all")
	if !strings.Contains(handOffs, "Land it? Next: wait for review") {
		t.Fatalf("hand-off entries = %q, want no period after a question mark", handOffs)
	}
	if strings.Contains(handOffs, "Land it?. Next:") {
		t.Fatalf("hand-off entries = %q, want no double punctuation", handOffs)
	}

	claimForTest(t, path, "BILL-1", agent)
	finishOut := run("ticket", "release", "BILL-1", "--agent", agent, "--done", "--proof", "go test ./... passes", "--json")
	var finished models.Ticket
	if err := json.Unmarshal([]byte(finishOut), &finished); err != nil || finished.Status != models.StatusDone {
		t.Fatalf("finish --json = %q: %v", finishOut, err)
	}
}
