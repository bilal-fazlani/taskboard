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
	if !strings.Contains(renamed, "Renamed to Plan.md") {
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
		err.Error() != "Only .md files can be attached." {
		t.Fatalf("txt file: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "doc", "show", "Plan"); err == nil || !strings.Contains(err.Error(), "--ticket") {
		t.Fatalf("name without --ticket: %v", err)
	}
}
