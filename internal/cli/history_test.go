package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// The CLI never requires a note, not even out of agent_review; it takes one
// when given and shows it in the ticket's history.
func TestTicketNotesAndHistory(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	run := func(args ...string) string {
		t.Helper()
		return captureStdout(t, func() {
			if _, err := runCLI(t, append([]string{"--db", path}, args...)...); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
		})
	}
	run("project", "create", "Agent control plane", "--prefix", "ACP")
	run("ticket", "create", "--project", "acp", "--title", "Status history")
	run("ticket", "move", "ACP-1", "--status", "agent_review", "--note", "ready for review")
	run("ticket", "move", "ACP-1", "--status", "in_progress")
	run("ticket", "update", "ACP-1", "--status", "done", "--note", "landed\nafter a second look")
	run("ticket", "update", "ACP-1", "--title", "Renamed", "--note", "no status change, so no row")

	out := run("ticket", "history", "acp-1")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	want := []string{
		"Status history for ACP-1, newest first:",
		"in_progress -> done",
		"      landed",
		"      after a second look",
		"agent_review -> in_progress",
		"todo -> agent_review",
		"      ready for review",
		"created in todo",
	}
	if len(lines) != len(want) {
		t.Fatalf("history printed %d lines, want %d:\n%s", len(lines), len(want), out)
	}
	today := time.Now().Format("2006-01-02")
	for i, w := range want {
		if !strings.HasSuffix(lines[i], w) {
			t.Fatalf("line %d = %q, want it to end with %q\n%s", i, lines[i], w, out)
		}
		if !strings.HasPrefix(w, " ") && i > 0 && !strings.Contains(lines[i], today) {
			t.Fatalf("line %d = %q has no date", i, lines[i])
		}
	}
	if strings.Contains(out, "no status change") {
		t.Fatalf("a note without a status change reached the history:\n%s", out)
	}
}

func TestTicketHistoryUnknownKeyIsAnError(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	if _, err := runCLI(t, "--db", path, "ticket", "history", "ACP-9"); err == nil {
		t.Fatal("expected an error for an unknown ticket")
	}
}

func TestTicketMoveHelpListsStatuses(t *testing.T) {
	out, err := runCLI(t, "ticket", "move", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join(models.Statuses, "|"); !strings.Contains(out, want) {
		t.Fatalf("ticket move --help does not list %q:\n%s", want, out)
	}
	for _, cmd := range []string{"move", "update"} {
		out, err := runCLI(t, "ticket", cmd, "--help")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "--note") {
			t.Fatalf("ticket %s --help has no --note:\n%s", cmd, out)
		}
	}
}
