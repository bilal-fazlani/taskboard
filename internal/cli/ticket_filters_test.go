package cli

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// listedTitles runs `ticket list` with the extra args and returns the
// titles it printed, sorted.
func listedTitles(t *testing.T, path string, args ...string) []string {
	t.Helper()
	out, err := runCLI(t, append([]string{"--db", path, "ticket", "list"}, args...)...)
	if err != nil {
		t.Fatalf("ticket list %v: %v", args, err)
	}
	var titles []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "[") {
			continue
		}
		rest := line[strings.Index(line, "] ")+2:]
		titles = append(titles, rest[:strings.Index(rest, " - ")])
	}
	sort.Strings(titles)
	return titles
}

func assertListedTitles(t *testing.T, path string, args []string, want ...string) {
	t.Helper()
	got := listedTitles(t, path, args...)
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ticket list %v = %v, want %v", args, got, want)
	}
}

// ticket list takes several statuses (repeated or comma-separated),
// --ready and --exclude-label, alone and with the existing filters.
func TestTicketListFilters(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	run := func(args ...string) {
		t.Helper()
		if _, err := runCLI(t, append([]string{"--db", path}, args...)...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	run("project", "create", "Billing", "--prefix", "BILL")
	run("project", "create", "Search", "--prefix", "SRCH")
	run("ticket", "create", "--project", "BILL", "--title", "Finished")                                 // BILL-1, moved to done below
	run("ticket", "create", "--project", "BILL", "--title", "Doing")                                    // BILL-2, moved to in_progress below
	run("ticket", "create", "--project", "BILL", "--title", "Reviewing")                                // BILL-3, moved to agent_review below
	run("ticket", "create", "--project", "BILL", "--title", "Startable", "--depends-on", "BILL-1")      // BILL-4
	run("ticket", "create", "--project", "BILL", "--title", "Waits on doing", "--depends-on", "BILL-2") // BILL-5
	run("ticket", "create", "--project", "BILL", "--title", "Held", "--label", "hold")                  // BILL-6
	run("ticket", "create", "--project", "SRCH", "--title", "Elsewhere")
	// ticket create takes no --status, so the first three move into place.
	run("ticket", "move", "BILL-1", "--status", "done")
	run("ticket", "move", "BILL-2", "--status", "in_progress")
	run("ticket", "move", "BILL-3", "--status", "agent_review")

	assertListedTitles(t, path, []string{"--status", "done"}, "Finished")
	assertListedTitles(t, path, []string{"--status", "in_progress", "--status", "agent_review"}, "Doing", "Reviewing")
	assertListedTitles(t, path, []string{"--status", "in_progress,agent_review", "--project", "BILL"}, "Doing", "Reviewing")
	assertListedTitles(t, path, []string{"--ready"}, "Startable", "Held", "Elsewhere")
	assertListedTitles(t, path, []string{"--ready", "--exclude-label", "HOLD"}, "Startable", "Elsewhere")
	assertListedTitles(t, path, []string{"--ready", "--exclude-label", "hold", "--project", "bill", "--epic", "none"}, "Startable")
	assertListedTitles(t, path, []string{"--ready", "--status", "in_progress"})
	assertListedTitles(t, path, []string{"--status", "todo", "--exclude-label", "hold", "--project", "BILL"}, "Startable", "Waits on doing")

	// A mistyped status is a clear error naming the allowed values, not a
	// silent empty list.
	_, err := runCLI(t, "--db", path, "ticket", "list", "--status", "in-progress")
	if err == nil {
		t.Fatal("ticket list --status in-progress: want an error, got nil")
	}
	if msg := err.Error(); !strings.Contains(msg, `"in-progress"`) {
		t.Fatalf("ticket list --status in-progress: err = %q, want it to name %q", msg, "in-progress")
	} else {
		for _, st := range models.Statuses {
			if !strings.Contains(msg, st) {
				t.Fatalf("ticket list --status in-progress: err = %q, want it to name %q", msg, st)
			}
		}
	}
}
