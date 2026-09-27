package cli

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// identifyIDRe pulls the new agent's id out of `agent identify`'s readable
// output: "Identified agent <id>: ...".
var identifyIDRe = regexp.MustCompile(`^Identified agent (\S+):`)

// claimForTest claims ticketRef for agentID directly through the store,
// bypassing the CLI: no CLI command claims a ticket (ACP-202's one-call
// start claims; there is no separate claim command), so a test that needs a
// ticket some agent holds sets that up here.
func claimForTest(t *testing.T, path, ticketRef, agentID string) {
	t.Helper()
	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := db.NewStore(database).ClaimTicket(ticketRef, agentID); err != nil {
		t.Fatalf("claiming %s for %s: %v", ticketRef, agentID, err)
	}
}

// agent identify creates a session (found by --vendor and --session-id, or
// created) and always a fresh agent in it; agent list shows every agent
// with its stale state and the tickets it holds.
func TestAgentCommands(t *testing.T) {
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

	// An empty board prints [], not null, so a script never has to special-case it.
	if out := run("agent", "list", "--json"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("agent list --json on an empty board = %q, want []", out)
	}
	if out := run("agent", "list"); !strings.Contains(out, "No agents.") {
		t.Fatalf("agent list on an empty board = %q", out)
	}

	out := run("agent", "identify", "--vendor", "claude_code", "--session-id", "3da2c294",
		"--machine", "bilal-mbp", "--resume-command", "claude --resume 3da2c294",
		"--role", "orchestrator", "--model", "claude-opus-5-5", "--provider", "anthropic")
	if !strings.Contains(out, "orchestrator") || !strings.Contains(out, "claude_code") {
		t.Fatalf("identify = %q", out)
	}
	m := identifyIDRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("identify output has no agent id: %q", out)
	}
	orchestrator := m[1]

	// The same session again creates a second, new agent in it: agents have
	// no name of their own to upsert by.
	out = run("agent", "identify", "--vendor", "claude_code", "--session-id", "3da2c294",
		"--role", "implementer", "--model", "claude-sonnet", "--provider", "anthropic")
	m = identifyIDRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("second identify output has no agent id: %q", out)
	}
	implementer := m[1]
	if implementer == orchestrator {
		t.Fatal("a second identify in the same session returned the same agent id")
	}

	// --json prints the agent itself.
	jsonOut := run("agent", "identify", "--vendor", "codex", "--session-id", "x1",
		"--role", "implementer", "--model", "gpt-6", "--provider", "openai", "--json")
	var identified models.Agent
	if err := json.Unmarshal([]byte(jsonOut), &identified); err != nil || identified.Provider != "openai" || identified.Role != "implementer" {
		t.Fatalf("identify --json = %q: %v", jsonOut, err)
	}

	claimForTest(t, path, "BILL-1", implementer)

	listed := run("agent", "list")
	if !strings.Contains(listed, implementer) || !strings.Contains(listed, "holding: BILL-1") {
		t.Fatalf("agent list = %q, want %s holding BILL-1", listed, implementer)
	}
	if !strings.Contains(listed, orchestrator) {
		t.Fatalf("agent list = %q, want the orchestrator listed too", listed)
	}

	listedJSON := run("agent", "list", "--json")
	var agents []models.AgentListItem
	if err := json.Unmarshal([]byte(listedJSON), &agents); err != nil {
		t.Fatalf("agent list --json = %q: %v", listedJSON, err)
	}
	var found bool
	for _, a := range agents {
		if a.ID != implementer {
			continue
		}
		found = true
		if len(a.HeldTickets) != 1 || a.HeldTickets[0].Key != "BILL-1" {
			t.Fatalf("implementer's held tickets = %+v", a.HeldTickets)
		}
		if a.Session.Vendor != "claude_code" {
			t.Fatalf("implementer's session = %+v, want vendor claude_code", a.Session)
		}
	}
	if !found {
		t.Fatalf("agent list --json = %v, missing the implementer", agents)
	}

	// The readable list shows the session's vendor and resume command, not
	// its ULID.
	if !strings.Contains(listed, "claude_code: claude --resume 3da2c294") {
		t.Fatalf("agent list = %q, want the session's vendor and resume command, not its id", listed)
	}

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"agent", "identify", "--session-id", "z", "--role", "r", "--model", "m", "--provider", "anthropic"},
			`"vendor" not set`},
		{[]string{"agent", "identify", "--vendor", "v", "--session-id", "z", "--role", "r", "--model", "m", "--provider", "acme"},
			`provider "acme" is not a provider`},
	} {
		_, err := runCLI(t, append([]string{"--db", path}, c.args...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want an error saying %q", c.args, err, c.want)
		}
	}
}
