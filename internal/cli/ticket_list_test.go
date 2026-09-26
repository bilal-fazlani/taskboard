package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/weburl"
)

// seedListCLI makes a board for the ticket list tests: BILL-1 done, BILL-2
// with an epic, labels, a dependency on BILL-1 and one of two subtasks done,
// and BILL-3 to BILL-5 plain.
func seedListCLI(t *testing.T) string {
	t.Helper()
	setLiveBuild(t, false)
	sandboxHome(t)
	t.Setenv(weburl.BaseEnv, "http://board.test")
	path := filepath.Join(t.TempDir(), "dev.db")
	run := func(args ...string) string {
		t.Helper()
		out, err := runCLI(t, append([]string{"--db", path}, args...)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	run("project", "create", "Billing", "--prefix", "BILL")
	run("epic", "create", "BILL", "Invoices")
	run("ticket", "create", "--project", "BILL", "--title", "Schema")
	run("ticket", "move", "BILL-1", "--status", "done")
	run("ticket", "create", "--project", "BILL", "--title", "Send invoices", "--priority", "high",
		"--epic", "Invoices", "--label", "api", "--label", "backend", "--depends-on", "BILL-1")
	added := strings.TrimSpace(run("ticket", "subtask", "add", "BILL-2", "draft"))
	run("ticket", "subtask", "toggle", added[strings.LastIndex(added, "(")+1:len(added)-1])
	run("ticket", "subtask", "add", "BILL-2", "send")
	for i := 3; i <= 5; i++ {
		run("ticket", "create", "--project", "BILL", "--title", fmt.Sprintf("Plain %d", i))
	}
	return path
}

func listCLI(t *testing.T, path string, args ...string) []string {
	t.Helper()
	out, err := runCLI(t, append([]string{"--db", path, "ticket", "list"}, args...)...)
	if err != nil {
		t.Fatalf("ticket list %v: %v", args, err)
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// --summary prints one short line per ticket with the summary's fields,
// and ends with which tickets the page showed.
func TestTicketListSummary(t *testing.T) {
	path := seedListCLI(t)
	got := listCLI(t, path, "--summary", "--status", "todo", "--label", "api")
	want := []string{
		"BILL-2  Send invoices  (todo, high)  epic: Invoices  labels: api, backend  depends on: BILL-1 done  subtasks: 1/2  http://board.test/?ticket=BILL-2",
		"Showing 1-1 of 1 tickets.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ticket list --summary =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	got = listCLI(t, path, "--summary", "--status", "done")
	if got[0] != "BILL-1  Schema  (done, medium)  http://board.test/?ticket=BILL-1" {
		t.Fatalf("bare summary line = %q", got[0])
	}
	if got := listCLI(t, path, "--summary", "--status", "agent_review"); len(got) != 1 || got[0] != "No tickets found." {
		t.Fatalf("empty summary = %q", got)
	}
}

// --limit and --offset page through the list in either form, and each page
// says where the next one starts; without them the list is unchanged.
func TestTicketListPaging(t *testing.T) {
	path := seedListCLI(t)
	keys := func(lines []string) []string {
		var out []string
		for _, l := range lines {
			if strings.HasPrefix(l, "BILL-") {
				out = append(out, l[:strings.Index(l, " ")])
			} else if strings.HasPrefix(l, "[") {
				out = append(out, l[1:strings.Index(l, "]")])
			}
		}
		return out
	}
	all := keys(listCLI(t, path))
	if len(all) != 5 {
		t.Fatalf("ticket list = %v, want all 5 tickets", all)
	}
	if lines := listCLI(t, path); strings.HasPrefix(lines[len(lines)-1], "Showing") {
		t.Fatalf("ticket list without paging flags ends %q, want no page line", lines[len(lines)-1])
	}

	first := listCLI(t, path, "--summary", "--limit", "2")
	if strings.Join(keys(first), ",") != strings.Join(all[:2], ",") || first[len(first)-1] != "Showing 1-2 of 5 tickets. Next page: --offset 2" {
		t.Fatalf("first page = %q, want %v and the next offset", first, all[:2])
	}
	second := listCLI(t, path, "--summary", "--limit", "2", "--offset", "2")
	if strings.Join(keys(second), ",") != strings.Join(all[2:4], ",") || second[len(second)-1] != "Showing 3-4 of 5 tickets. Next page: --offset 4" {
		t.Fatalf("second page = %q, want %v", second, all[2:4])
	}
	last := listCLI(t, path, "--summary", "--limit", "2", "--offset", "4")
	if strings.Join(keys(last), ",") != all[4] || last[len(last)-1] != "Showing 5-5 of 5 tickets." {
		t.Fatalf("last page = %q, want %v and no next page", last, all[4:])
	}

	// Paging without --summary pages the full lines.
	full := listCLI(t, path, "--limit", "2", "--offset", "2")
	if !strings.HasPrefix(full[0], "[") || strings.Join(keys(full), ",") != strings.Join(all[2:4], ",") {
		t.Fatalf("full second page = %q, want full lines for %v", full, all[2:4])
	}
	if got := listCLI(t, path, "--offset", "9"); len(got) != 1 || got[0] != "No tickets at offset 9; 5 match in all." {
		t.Fatalf("past the end = %q", got)
	}

	for _, bad := range [][]string{{"--limit", "0"}, {"--limit", "201"}, {"--offset", "-1"}} {
		if _, err := runCLI(t, append([]string{"--db", path, "ticket", "list"}, bad...)...); err == nil {
			t.Fatalf("ticket list %v: want an error", bad)
		}
	}
}
