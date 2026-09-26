package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// ticket update --append-description adds a paragraph without the caller
// resending the description, and keeps what was there byte for byte.
func TestTicketUpdateAppendsToDescription(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Invoice"); err != nil {
		t.Fatalf("ticket create: %v", err)
	}
	description := func() string {
		t.Helper()
		out, err := runCLI(t, "--db", path, "ticket", "get", "BILL-1", "--json")
		if err != nil {
			t.Fatalf("ticket get: %v", err)
		}
		var got models.Ticket
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("ticket get --json: %v\n%s", err, out)
		}
		return got.Description
	}

	// Onto an empty description, the text is the description.
	if _, err := runCLI(t, "--db", path, "ticket", "update", "BILL-1", "--append-description", "The plan.\n  - keep this indent"); err != nil {
		t.Fatalf("ticket update: %v", err)
	}
	if got, want := description(), "The plan.\n  - keep this indent"; got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}

	// Onto a non-empty one, it starts a new paragraph.
	out, err := runCLI(t, "--db", path, "ticket", "update", "bill-1", "--append-description", "Worktree: /w/bill-1")
	if err != nil {
		t.Fatalf("ticket update: %v", err)
	}
	if !strings.Contains(out, "Updated BILL-1") {
		t.Fatalf("ticket update printed %q", out)
	}
	if got, want := description(), "The plan.\n  - keep this indent\n\nWorktree: /w/bill-1"; got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}

	// Replacing and appending at once, or appending nothing, is refused.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--description", "New", "--append-description", "More"}, "append-description"},
		{[]string{"--append-description", ""}, "appendDescription is empty"},
	} {
		_, err := runCLI(t, append([]string{"--db", path, "ticket", "update", "BILL-1"}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("ticket update %v: err = %v, want a refusal containing %q", tc.args, err, tc.want)
		}
	}
	if got, want := description(), "The plan.\n  - keep this indent\n\nWorktree: /w/bill-1"; got != want {
		t.Fatalf("description after refusals = %q, want %q", got, want)
	}
}
