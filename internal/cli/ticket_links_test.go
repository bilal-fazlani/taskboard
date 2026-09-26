package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// seedLinksCLI makes a board with ACP-1 and ACP-2 and returns a runner for
// more commands against it.
func seedLinksCLI(t *testing.T) (run func(args ...string) string, fail func(args ...string) error) {
	t.Helper()
	setLiveBuild(t, false)
	sandboxHome(t)
	t.Setenv(weburl.BaseEnv, "http://board.test")
	path := filepath.Join(t.TempDir(), "dev.db")
	run = func(args ...string) string {
		t.Helper()
		out, err := runCLI(t, append([]string{"--db", path}, args...)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	fail = func(args ...string) error {
		t.Helper()
		_, err := runCLI(t, append([]string{"--db", path}, args...)...)
		if err == nil {
			t.Fatalf("%v succeeded, want an error", args)
		}
		return err
	}
	run("project", "create", "Agent control plane", "--prefix", "ACP")
	run("ticket", "create", "--project", "ACP", "--title", "Delivery fields")
	run("ticket", "create", "--project", "ACP", "--title", "Project journal")
	return run, fail
}

func getJSONTicket(t *testing.T, run func(args ...string) string, key string) models.Ticket {
	t.Helper()
	var tk models.Ticket
	if err := json.Unmarshal([]byte(run("ticket", "get", key, "--json")), &tk); err != nil {
		t.Fatalf("ticket get %s --json: %v", key, err)
	}
	return tk
}

func TestTicketCLISetsAndReadsDependencyKindsAndNotes(t *testing.T) {
	run, fail := seedLinksCLI(t)
	run("ticket", "create", "--project", "ACP", "--title", "Typed links",
		"--depends-on", "ACP-1:conflict_only,ACP-2",
		"--depends-on-note", "acp-1=internal/db/store.go, internal/mcp/mcp.go")

	tk := getJSONTicket(t, run, "ACP-3")
	if len(tk.DependsOn) != 2 ||
		tk.DependsOn[0].Kind != models.DependencyConflictOnly || tk.DependsOn[0].Note != "internal/db/store.go, internal/mcp/mcp.go" ||
		tk.DependsOn[1].Kind != models.DependencyNeedsWork || tk.DependsOn[1].Note != "" {
		t.Fatalf("dependsOn = %+v, want ACP-1 conflict only with its note and ACP-2 needs work", tk.DependsOn)
	}

	out := run("ticket", "get", "ACP-3")
	if !strings.Contains(out, "Depends on: ACP-1 (conflict only; note: internal/db/store.go, internal/mcp/mcp.go), ACP-2\n") {
		t.Fatalf("ticket get output %q does not mark the conflict-only dependency and its note", out)
	}
	if out := run("ticket", "get", "ACP-1"); !strings.Contains(out, "Blocks: ACP-3 (conflict only; note: internal/db/store.go, internal/mcp/mcp.go)\n") {
		t.Fatalf("ticket get ACP-1 output %q does not mark the conflict-only block", out)
	}
	if out := run("ticket", "list", "--project", "ACP"); !strings.Contains(out, "depends on ACP-1 (conflict only; note: internal/db/store.go, internal/mcp/mcp.go), ACP-2") {
		t.Fatalf("ticket list output %q does not mark the conflict-only dependency", out)
	}
	if out := run("ticket", "list", "--project", "ACP", "--summary"); !strings.Contains(out, "depends on: ACP-1 todo (conflict only; note: internal/db/store.go, internal/mcp/mcp.go), ACP-2 todo") {
		t.Fatalf("ticket list --summary output %q does not mark the conflict-only dependency", out)
	}

	// The list is the whole set: plain keys need work and have no note.
	run("ticket", "update", "ACP-3", "--depends-on", "ACP-1", "--depends-on", "ACP-2")
	if tk := getJSONTicket(t, run, "ACP-3"); tk.DependsOn[0].Kind != models.DependencyNeedsWork || tk.DependsOn[0].Note != "" {
		t.Fatalf("after a plain update dependsOn = %+v, want ACP-1 needing work with no note", tk.DependsOn)
	}
	// A note on a needs-work dependency, by id.
	run("ticket", "update", "ACP-3", "--depends-on", "ACP-1,ACP-2:conflict_only", "--depends-on-note", tk.DependsOn[0].ID+"=needs the new column")
	if tk := getJSONTicket(t, run, "ACP-3"); tk.DependsOn[0].Note != "needs the new column" || tk.DependsOn[1].Kind != models.DependencyConflictOnly {
		t.Fatalf("after explicit kinds and a note dependsOn = %+v", tk.DependsOn)
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--depends-on", "ACP-1:later"}, "needs_work, conflict_only"},
		{[]string{"--depends-on-note", "ACP-1=x"}, "needs --depends-on"},
		{[]string{"--depends-on", "ACP-1", "--depends-on-note", "ACP-2=x"}, "does not list"},
		{[]string{"--depends-on", "ACP-1", "--depends-on-note", "no equals sign"}, "KEY=text"},
	} {
		err := fail(append([]string{"ticket", "update", "ACP-3"}, tc.args...)...)
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v error = %v, want %q", tc.args, err, tc.want)
		}
	}
	if tk := getJSONTicket(t, run, "ACP-3"); tk.DependsOn[0].Note != "needs the new column" {
		t.Fatalf("a rejected update changed dependsOn: %+v", tk.DependsOn)
	}

	// An empty --depends-on clears them.
	run("ticket", "update", "ACP-3", "--depends-on=")
	if tk := getJSONTicket(t, run, "ACP-3"); len(tk.DependsOn) != 0 {
		t.Fatalf("dependsOn = %+v after clearing, want none", tk.DependsOn)
	}
}

func TestTicketCLISetsAndReadsSurfacedFrom(t *testing.T) {
	run, fail := seedLinksCLI(t)
	run("ticket", "create", "--project", "ACP", "--title", "Found in the run", "--surfaced-from", "acp-1")

	if tk := getJSONTicket(t, run, "ACP-3"); tk.SurfacedFrom == nil || tk.SurfacedFrom.Key != "ACP-1" {
		t.Fatalf("surfacedFrom = %+v, want ACP-1", tk.SurfacedFrom)
	}
	if out := run("ticket", "get", "ACP-3"); !strings.Contains(out, "Surfaced from: ACP-1\n") {
		t.Fatalf("ticket get output %q missing the surfaced-from line", out)
	}
	if out := run("ticket", "get", "ACP-1"); !strings.Contains(out, "Surfaced: ACP-3\n") {
		t.Fatalf("ticket get ACP-1 output %q missing the surfaced line", out)
	}
	if out := run("ticket", "list", "--project", "ACP"); !strings.Contains(out, "surfaced from ACP-1") {
		t.Fatalf("ticket list output %q missing surfaced from", out)
	}
	if out := run("ticket", "list", "--project", "ACP", "--summary"); !strings.Contains(out, "surfaced from: ACP-1") {
		t.Fatalf("ticket list --summary output %q missing surfaced from", out)
	}

	// Leaving the flag out keeps it; another key moves it; none removes it.
	run("ticket", "update", "ACP-3", "--title", "Renamed")
	if tk := getJSONTicket(t, run, "ACP-3"); tk.SurfacedFrom == nil {
		t.Fatal("an update without --surfaced-from dropped the link")
	}
	run("ticket", "update", "ACP-3", "--surfaced-from", "ACP-2")
	if tk := getJSONTicket(t, run, "ACP-3"); tk.SurfacedFrom == nil || tk.SurfacedFrom.Key != "ACP-2" {
		t.Fatalf("surfacedFrom = %+v, want ACP-2", tk.SurfacedFrom)
	}
	for _, clear := range []string{"--surfaced-from=", "--surfaced-from=none"} {
		run("ticket", "update", "ACP-3", "--surfaced-from", "ACP-2")
		run("ticket", "update", "ACP-3", clear)
		if tk := getJSONTicket(t, run, "ACP-3"); tk.SurfacedFrom != nil {
			t.Fatalf("surfacedFrom = %+v after %s, want none", tk.SurfacedFrom, clear)
		}
	}

	if err := fail("ticket", "update", "ACP-3", "--surfaced-from", "ACP-3"); !strings.Contains(err.Error(), "itself") {
		t.Fatalf("surfaced from itself error = %v", err)
	}
}
