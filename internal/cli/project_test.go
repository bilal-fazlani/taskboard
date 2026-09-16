package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// project delete must accept a project prefix (case-insensitive), the same
// as ticket create's --project flag, and reject an unknown one with a clear
// error rather than silently doing nothing.
func TestProjectDeleteAcceptsPrefixAndRejectsUnknownOne(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}

	if _, err := runCLI(t, "--db", path, "project", "delete", "NOPE"); err == nil {
		t.Fatal("expected an error for an unknown project prefix")
	}

	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "project", "list"); err != nil {
			t.Fatalf("project list: %v", err)
		}
	})
	if !strings.Contains(listed, "BILL") {
		t.Fatalf("project list after a failed delete = %q, want BILL still present", listed)
	}

	if _, err := runCLI(t, "--db", path, "project", "delete", "bill"); err != nil {
		t.Fatalf("project delete bill: %v", err)
	}

	listedAfter := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "project", "list"); err != nil {
			t.Fatalf("project list: %v", err)
		}
	})
	if !strings.Contains(listedAfter, "No projects found") {
		t.Fatalf("project list after delete by prefix = %q, want no projects", listedAfter)
	}
}

// projects.prefix is UNIQUE only under SQLite's default binary collation, so
// two projects can differ only by the case of their prefix. "project delete"
// must refuse to guess which one a case-insensitive-only match means,
// rather than deleting whichever one the database returns first (this
// previously deleted the wrong project, and every ticket under it).
func TestProjectDeleteRejectsAmbiguousPrefix(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Glow Upper", "--prefix", "GLOW"); err != nil {
		t.Fatalf("project create GLOW: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "project", "create", "Glow Lower", "--prefix", "glow"); err != nil {
		t.Fatalf("project create glow: %v", err)
	}

	if _, err := runCLI(t, "--db", path, "project", "delete", "Glow"); err == nil {
		t.Fatal("expected an error for a prefix matching two projects with no exact case match")
	}

	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "project", "list"); err != nil {
			t.Fatalf("project list: %v", err)
		}
	})
	if !strings.Contains(listed, "[GLOW]") || !strings.Contains(listed, "[glow]") {
		t.Fatalf("project list after a rejected ambiguous delete = %q, want both GLOW and glow still present", listed)
	}
}
