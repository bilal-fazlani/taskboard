package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLIWithInput runs the CLI with stdin set to input, for --file -.
func runCLIWithInput(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCmd(nil)
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(input))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestDocCommands(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	if _, err := runCLI(t, "--db", path, "project", "create", "Docs", "--prefix", "DOC"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "DOC", "--title", "Has docs"); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(t.TempDir(), "api-design_v1.2.md")
	if err := os.WriteFile(file, []byte("# Design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1", "--file", file); err != nil {
			t.Fatalf("doc add: %v", err)
		}
	})
	if !strings.Contains(added, "Added document api-design_v1 2.md") || !strings.Contains(added, "doc=api-design_v1+2.md") {
		t.Fatalf("doc add printed %q", added)
	}
	docID := lastParenthesized(t, added)

	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "list", "DOC-1"); err != nil {
			t.Fatalf("doc list: %v", err)
		}
	})
	if !strings.Contains(listed, "api-design_v1 2.md") || !strings.Contains(listed, "9 B") || !strings.Contains(listed, docID) {
		t.Fatalf("doc list printed %q", listed)
	}

	shown := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "show", "api-design_v1 2.md", "--ticket", "doc-1"); err != nil {
			t.Fatalf("doc show: %v", err)
		}
	})
	if shown != "# Design\n" {
		t.Fatalf("doc show printed %q", shown)
	}

	if _, err := runCLIWithInput(t, "# Design v2\n", "--db", path, "doc", "write", docID, "--file", "-"); err != nil {
		t.Fatalf("doc write from stdin: %v", err)
	}
	shown = captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "show", docID); err != nil {
			t.Fatalf("doc show: %v", err)
		}
	})
	if shown != "# Design v2\n" {
		t.Fatalf("after write, doc show printed %q", shown)
	}

	renamed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "rename", docID, "Plan"); err != nil {
			t.Fatalf("doc rename: %v", err)
		}
	})
	if !strings.Contains(renamed, "Renamed to Plan.md") || !strings.Contains(renamed, "/?ticket=DOC-1&doc=Plan.md") {
		t.Fatalf("doc rename printed %q", renamed)
	}

	if _, err := runCLIWithInput(t, "notes", "--db", path, "doc", "add", "DOC-1", "--file", "-", "--name", "Notes"); err != nil {
		t.Fatalf("doc add from stdin: %v", err)
	}
	deleted := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "delete", "notes", "--ticket", "DOC-1"); err != nil {
			t.Fatalf("doc delete: %v", err)
		}
	})
	if !strings.Contains(deleted, "Deleted Notes.md") {
		t.Fatalf("doc delete printed %q", deleted)
	}

	page := filepath.Join(t.TempDir(), "Load test.HTM")
	if err := os.WriteFile(page, []byte("<h1>ok</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	addedHTML := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1", "--file", page); err != nil {
			t.Fatalf("doc add html: %v", err)
		}
	})
	if !strings.Contains(addedHTML, "Added document Load test.html") || !strings.Contains(addedHTML, "doc=Load+test.html") {
		t.Fatalf("doc add html printed %q", addedHTML)
	}
	shownHTML := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "show", "Load test.html", "--ticket", "DOC-1"); err != nil {
			t.Fatalf("doc show html: %v", err)
		}
	})
	if shownHTML != "<h1>ok</h1>" {
		t.Fatalf("doc show html printed %q", shownHTML)
	}
	namedHTML := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1", "--file", page, "--name", "Chart"); err != nil {
			t.Fatalf("doc add html with --name: %v", err)
		}
	})
	if !strings.Contains(namedHTML, "Added document Chart.html") {
		t.Fatalf("doc add html with --name printed %q", namedHTML)
	}
}

func TestDocCommandErrors(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	if _, err := runCLI(t, "--db", path, "project", "create", "Docs", "--prefix", "DOC"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "DOC", "--title", "Has docs"); err != nil {
		t.Fatal(err)
	}

	if _, err := runCLIWithInput(t, "x", "--db", path, "doc", "add", "DOC-1", "--file", "-"); err == nil ||
		!strings.Contains(err.Error(), "--name") {
		t.Fatalf("stdin without --name: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1"); err == nil || !strings.Contains(err.Error(), "--file") {
		t.Fatalf("no --file: %v", err)
	}
	txt := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(txt, []byte("x"), 0o644)
	if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1", "--file", txt); err == nil ||
		err.Error() != "Only .md, .html and .htm files can be attached." {
		t.Fatalf("txt file: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "doc", "show", "Plan"); err == nil || !strings.Contains(err.Error(), "--ticket") {
		t.Fatalf("name without --ticket: %v", err)
	}
}

func TestDocCommandsOnEpics(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	for _, args := range [][]string{
		{"project", "create", "Docs", "--prefix", "DOC"},
		{"epic", "create", "DOC", "Launch"},
		{"ticket", "create", "--project", "DOC", "--title", "Has docs"},
	} {
		if _, err := runCLI(t, append([]string{"--db", path}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	added := captureStdout(t, func() {
		if _, err := runCLIWithInput(t, "# r", "--db", path, "doc", "add", "--epic", "Launch", "--project", "DOC", "--name", "Rollout", "--file", "-"); err != nil {
			t.Fatalf("doc add on epic: %v", err)
		}
	})
	if !strings.Contains(added, "Added document Rollout.md") || !strings.Contains(added, "/epics?project=DOC&epic=Launch&doc=Rollout.md") {
		t.Fatalf("doc add printed %q", added)
	}
	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "list", "--epic", "launch", "--project", "doc"); err != nil {
			t.Fatalf("doc list on epic: %v", err)
		}
	})
	if !strings.Contains(listed, "Rollout.md") || !strings.Contains(listed, "/epics?project=DOC&epic=Launch&doc=Rollout.md") {
		t.Fatalf("doc list printed %q", listed)
	}
	shown := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "show", "Rollout", "--epic", "Launch", "--project", "DOC"); err != nil {
			t.Fatalf("doc show on epic: %v", err)
		}
	})
	if shown != "# r" {
		t.Fatalf("doc show printed %q", shown)
	}
	if _, err := runCLIWithInput(t, "# v2", "--db", path, "doc", "write", "rollout.md", "--epic", "Launch", "--project", "DOC", "--file", "-"); err != nil {
		t.Fatalf("doc write on epic: %v", err)
	}
	renamed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "rename", "Rollout", "Go live", "--epic", "Launch", "--project", "DOC"); err != nil {
			t.Fatalf("doc rename on epic: %v", err)
		}
	})
	if !strings.Contains(renamed, "Renamed to Go live.md") || !strings.Contains(renamed, "&epic=Launch&doc=Go+live.md") {
		t.Fatalf("doc rename printed %q", renamed)
	}
	// The epic's "Go live" and a ticket's "Go live" live side by side.
	if _, err := runCLIWithInput(t, "t", "--db", path, "doc", "add", "DOC-1", "--name", "Go live", "--file", "-"); err != nil {
		t.Fatalf("doc add on ticket beside the epic's: %v", err)
	}
	deleted := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "delete", "go live", "--epic", "Launch", "--project", "DOC"); err != nil {
			t.Fatalf("doc delete on epic: %v", err)
		}
	})
	if !strings.Contains(deleted, "Deleted Go live.md") {
		t.Fatalf("doc delete printed %q", deleted)
	}
	ticketDocs := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "list", "DOC-1"); err != nil {
			t.Fatalf("doc list on ticket: %v", err)
		}
	})
	if !strings.Contains(ticketDocs, "Go live.md") {
		t.Fatalf("deleting the epic's document touched the ticket's: %q", ticketDocs)
	}

	if _, err := runCLI(t, "--db", path, "doc", "list"); err == nil || !strings.Contains(err.Error(), "--epic") {
		t.Fatalf("doc list with no owner: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "doc", "list", "DOC-1", "--epic", "Launch", "--project", "DOC"); err == nil ||
		!strings.Contains(err.Error(), "not both") {
		t.Fatalf("doc list with both owners: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "doc", "show", "Go live", "--ticket", "DOC-1", "--epic", "Launch", "--project", "DOC"); err == nil ||
		!strings.Contains(err.Error(), "not both") {
		t.Fatalf("doc show with both owners: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "doc", "list", "--epic", "Launch"); err == nil ||
		!strings.Contains(err.Error(), "--project") {
		t.Fatalf("epic name without --project: %v", err)
	}
}

func TestEpicDeleteHelpSaysItsDocumentsAreDeleted(t *testing.T) {
	cmd, _, err := NewRootCmd(nil).Find([]string{"epic", "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd.Short, "its documents are deleted for good") {
		t.Fatalf("epic delete Short = %q", cmd.Short)
	}
}
