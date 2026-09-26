package cli

import (
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// project journal append writes entries (text from an argument or standard
// input, author from --author or the user name), and project journal list
// reads them newest first, a page at a time.
func TestProjectJournalCommands(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "--db", path, "project", "journal", "list", "BILL")
	if err != nil || !strings.Contains(out, "No journal entries for BILL.") {
		t.Fatalf("empty journal: %q, %v", out, err)
	}

	out, err = runCLI(t, "--db", path, "project", "journal", "append", "bill", "Run started.", "--author", "orchestrator")
	if err != nil || !strings.Contains(out, "Appended to the journal as orchestrator") {
		t.Fatalf("append with --author: %q, %v", out, err)
	}
	out, err = runCLIWithInput(t, "Line one.\n\nLine two.\n", "--db", path, "project", "journal", "append", "BILL", "-")
	if err != nil {
		t.Fatalf("append from stdin: %q, %v", out, err)
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "as "+me.Username+" ") {
		t.Fatalf("append without --author = %q, want it written as %s", out, me.Username)
	}
	out, err = runCLI(t, "--db", path, "project", "journal", "append", "BILL", "Third.", "--author", "reviewer")
	if err != nil {
		t.Fatal(err)
	}

	out, err = runCLI(t, "--db", path, "project", "journal", "list", "BILL")
	if err != nil {
		t.Fatal(err)
	}
	stamp := `\d{4}-\d{2}-\d{2} \d{2}:\d{2}`
	want := regexp.MustCompile(`(?s)^Journal for BILL, newest first \(3 of 3\):\n` +
		`  ` + stamp + `  reviewer\n      Third\.\n` +
		`  ` + stamp + `  ` + regexp.QuoteMeta(me.Username) + `\n      Line one\.\n\n      Line two\.\n` +
		`  ` + stamp + `  orchestrator\n      Run started\.\n$`)
	if !want.MatchString(out) {
		t.Fatalf("journal list =\n%s", out)
	}

	// A page that stops short names the command for the next one.
	out, err = runCLI(t, "--db", path, "project", "journal", "list", "BILL", "--limit", "2")
	if err != nil {
		t.Fatal(err)
	}
	next := regexp.MustCompile(`Older entries: taskboard project journal list BILL --before (\S+)\n$`).FindStringSubmatch(out)
	if next == nil || !strings.Contains(out, "(2 of 3)") || strings.Contains(out, "Run started.") {
		t.Fatalf("first page of 2 =\n%s", out)
	}
	out, err = runCLI(t, "--db", path, "project", "journal", "list", "BILL", "--limit", "2", "--before", next[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(1 of 3)") || !strings.Contains(out, "Run started.") || strings.Contains(out, "Older entries") {
		t.Fatalf("second page =\n%s", out)
	}
}

func TestProjectJournalCommandErrors(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"append", "NOPE", "text"}, "project not found"},
		{[]string{"append", "BILL", "  "}, "text is required"},
		{[]string{"list", "NOPE"}, "project not found"},
		{[]string{"list", "BILL", "--limit", "0"}, "limit must be between 1 and 100"},
		{[]string{"list", "BILL", "--before", "nope"}, "is not an entry"},
	} {
		args := append([]string{"--db", path, "project", "journal"}, c.args...)
		_, err := runCLI(t, args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: error %v, want one containing %q", c.args, err, c.want)
		}
	}
	out, _ := runCLI(t, "--db", path, "project", "journal", "list", "BILL")
	if !strings.Contains(out, "No journal entries") {
		t.Fatalf("journal after failed appends = %q", out)
	}
}
