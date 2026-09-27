package cli

import (
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
)

// seedCLIAgent inserts an agent in a session into the database at path:
// nothing creates agents from the CLI yet.
func seedCLIAgent(t *testing.T, path string) string {
	t.Helper()
	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, q := range []string{
		`INSERT INTO sessions (id, vendor, vendor_session_id, machine, resume_command, created_at)
			VALUES ('s1', 'claude_code', '3da2c294', 'mac', 'claude --resume 3da2c294', CURRENT_TIMESTAMP)`,
		`INSERT INTO agents (id, session_id, role, model, provider, created_at, last_seen_at)
			VALUES ('a1', 's1', 'implementer', 'opus', 'anthropic', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return "a1"
}

// entry add writes entries on a ticket, an epic and a project, by an agent
// or by the person (--author, or the user name); entry list reads them
// newest first, current only unless --all, a page at a time; entry handle
// marks a note handled.
func TestEntryCommands(t *testing.T) {
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
	run("epic", "create", "BILL", "Launch")
	run("ticket", "create", "--project", "BILL", "--title", "Invoice")
	agent := seedCLIAgent(t, path)
	idOf := regexp.MustCompile(`\((\S+)\)\n$`)

	if out := run("entry", "list", "--ticket", "BILL-1"); !strings.Contains(out, "No entries on BILL-1.") {
		t.Fatalf("empty list = %q", out)
	}
	out := run("entry", "add", "--ticket", "bill-1", "--type", "learning", "--agent", agent, "Foreign keys are off in the shell.")
	if !strings.HasPrefix(out, "Added learning on bill-1 by agent a1 (") {
		t.Fatalf("add by agent = %q", out)
	}
	old := idOf.FindStringSubmatch(out)[1]
	run("entry", "add", "--ticket", "BILL-1", "--type", "learning", "--agent", agent, "--replaces", old, "They are on in the app.")
	out = run("entry", "add", "--ticket", "BILL-1", "--type", "note", "--author", "Bilal", "Mind the port.")
	note := idOf.FindStringSubmatch(out)[1]
	run("entry", "add", "--epic", "Launch", "--project", "BILL", "--type", "decision", "--source", "person", "--author", "Bilal", "Ship in May.")
	out, err := runCLIWithInput(t, "Keep it lean.\n", "--db", path, "entry", "add", "--project", "BILL", "--type", "note", "-")
	if err != nil {
		t.Fatal(err)
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, " by "+me.Username+" (") {
		t.Fatalf("add without an author = %q, want it by %s", out, me.Username)
	}

	stamp := `\d{4}-\d{2}-\d{2} \d{2}:\d{2}`
	out = run("entry", "list", "--ticket", "BILL-1")
	want := regexp.MustCompile(`(?s)^Entries on BILL-1, newest first \(2 of 2\):\n` +
		`  ` + stamp + `  note  Bilal  \(\S+\)  \[open\]\n      Mind the port\.\n` +
		`  ` + stamp + `  learning  agent a1  \(\S+\)\n      They are on in the app\.\n$`)
	if !want.MatchString(out) {
		t.Fatalf("ticket entries =\n%s", out)
	}
	out = run("entry", "list", "--ticket", "BILL-1", "--all", "--type", "learning")
	if !strings.Contains(out, "(2 of 2)") || !strings.Contains(out, "[replaced by ") {
		t.Fatalf("all learnings =\n%s", out)
	}
	out = run("entry", "list", "--epic", "launch", "--project", "bill")
	if !strings.Contains(out, "decision  Bilal") || !strings.Contains(out, "[person's call]") {
		t.Fatalf("epic entries =\n%s", out)
	}
	out = run("entry", "list", "--ticket", "BILL-1", "--limit", "1")
	next := regexp.MustCompile(`Older entries: taskboard entry list --ticket "BILL-1" --before (\S+)\n$`).FindStringSubmatch(out)
	if next == nil {
		t.Fatalf("first page of 1 =\n%s", out)
	}
	if out = run("entry", "list", "--ticket", "BILL-1", "--limit", "1", "--before", next[1]); !strings.Contains(out, "They are on in the app.") {
		t.Fatalf("second page =\n%s", out)
	}

	if out = run("entry", "handle", note, "--agent", agent); !strings.Contains(out, "Marked note "+note+" handled by agent a1") {
		t.Fatalf("handle = %q", out)
	}
	if out = run("entry", "list", "--ticket", "BILL-1", "--type", "note"); !strings.Contains(out, "[handled by agent a1]") {
		t.Fatalf("handled note =\n%s", out)
	}

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"entry", "list"}, "pass one of --ticket"},
		{[]string{"entry", "list", "--ticket", "BILL-1", "--project", "BILL"}, "pass one of --ticket"},
		{[]string{"entry", "add", "--epic", "Launch", "--project", "BILL", "--type", "proof", "t"}, "goes on a ticket only"},
		{[]string{"entry", "add", "--ticket", "BILL-1", "--type", "learning", "--agent", agent, "--author", "Bilal", "t"}, "not both"},
		{[]string{"entry", "add", "--ticket", "BILL-1", "--type", "review", "--agent", agent, "--verdict", "approve", "--finding", "minor", "t"}, "severity=count"},
		{[]string{"entry", "handle", note, "--agent", agent}, "already handled"},
		{[]string{"entry", "handle", note}, "agentId is required"},
	} {
		out, err := runCLI(t, append([]string{"--db", path}, c.args...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %q, %v; want an error saying %q", c.args, out, err, c.want)
		}
	}
}
