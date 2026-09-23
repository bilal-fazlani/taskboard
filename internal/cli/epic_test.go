package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// epic list, create, update and delete work end to end, including the "No
// epic" line that list always prints.
func TestEpicCreateListUpdateDelete(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}

	created := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "epic", "create", "BILL", "Launch", "--description", "Go live"); err != nil {
			t.Fatalf("epic create: %v", err)
		}
	})
	if !strings.Contains(created, "Created epic Launch") {
		t.Fatalf("epic create printed %q, want it to mention Launch", created)
	}
	epicID := lastParenthesized(t, created)

	// list shows the new epic (0/0 done, not complete: it has no tickets yet)
	// and always a trailing "No epic" line.
	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "epic", "list", "BILL"); err != nil {
			t.Fatalf("epic list: %v", err)
		}
	})
	if !strings.Contains(listed, "Launch") {
		t.Fatalf("epic list = %q, want it to mention Launch", listed)
	}
	if !strings.Contains(listed, "No epic") {
		t.Fatalf("epic list = %q, want a No epic line", listed)
	}

	// Update by id.
	updated := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "epic", "update", epicID, "--name", "Launch v2"); err != nil {
			t.Fatalf("epic update by id: %v", err)
		}
	})
	if !strings.Contains(updated, "Launch v2") {
		t.Fatalf("epic update printed %q, want it to mention Launch v2", updated)
	}

	// Update by name with --project.
	if _, err := runCLI(t, "--db", path, "epic", "update", "Launch v2", "--project", "BILL", "--description", "Go live for real"); err != nil {
		t.Fatalf("epic update by name: %v", err)
	}

	// A ticket placed in the epic shows up in its progress and is cleared, not
	// deleted, when the epic is deleted.
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Ship it", "--epic", "Launch v2"); err != nil {
		t.Fatalf("ticket create --epic: %v", err)
	}
	listedWithTicket := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "epic", "list", "BILL"); err != nil {
			t.Fatalf("epic list: %v", err)
		}
	})
	if !strings.Contains(listedWithTicket, "0/1 done") {
		t.Fatalf("epic list = %q, want 0/1 done for Launch v2", listedWithTicket)
	}

	deleted := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "epic", "delete", "Launch v2", "--project", "BILL"); err != nil {
			t.Fatalf("epic delete by name: %v", err)
		}
	})
	if !strings.Contains(deleted, "Cleared from 1 ticket(s)") {
		t.Fatalf("epic delete printed %q, want it to report 1 cleared ticket", deleted)
	}

	// The ticket survived and now shows no epic.
	ticketList := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "list"); err != nil {
			t.Fatalf("ticket list: %v", err)
		}
	})
	if !strings.Contains(ticketList, "Ship it") {
		t.Fatalf("ticket list = %q, want the ticket to still be there", ticketList)
	}
	if strings.Contains(ticketList, "epic:") {
		t.Fatalf("ticket list = %q, want no epic reference after the epic was deleted", ticketList)
	}

	// A name without --project cannot resolve, since it is read as an id; the
	// error says --project is needed to address an epic by name.
	if _, err := runCLI(t, "--db", path, "epic", "update", "Launch v2", "--name", "x"); err == nil {
		t.Fatal("expected an error updating an epic by name without --project")
	} else if want := `epic not found: "Launch v2" (to use a name, pass --project)`; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}

	// An unknown id is a clear error, not a silent no-op.
	if _, err := runCLI(t, "--db", path, "epic", "delete", "does-not-exist"); err == nil {
		t.Fatal("expected an error deleting an unknown epic")
	}
}

// The store's own rules (empty name, duplicate name, reserved "none") come
// back unchanged through the CLI.
func TestEpicCreateEnforcesStoreRules(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "epic", "create", "BILL", "Launch"); err != nil {
		t.Fatalf("epic create: %v", err)
	}

	if _, err := runCLI(t, "--db", path, "epic", "create", "BILL", "launch"); err == nil {
		t.Fatal("expected an error creating a duplicate epic name (case-insensitive)")
	} else if !strings.Contains(err.Error(), "already has an epic called") {
		t.Fatalf("unexpected error for duplicate epic name: %v", err)
	}

	if _, err := runCLI(t, "--db", path, "epic", "create", "BILL", "none"); err == nil {
		t.Fatal(`expected an error creating an epic named "none"`)
	} else if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("unexpected error for reserved epic name: %v", err)
	}

	if _, err := runCLI(t, "--db", path, "epic", "create", "BILL", "   "); err == nil {
		t.Fatal("expected an error creating an epic with a blank name")
	}
}

// ticket create and update's --epic flag sets, keeps and clears the ticket's
// epic, with "none" (any case) meaning the same as an empty string.
func TestTicketEpicFlagSetsKeepsAndClears(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "epic", "create", "BILL", "Launch"); err != nil {
		t.Fatalf("epic create: %v", err)
	}

	created := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Ship it", "--epic", "Launch"); err != nil {
			t.Fatalf("ticket create --epic: %v", err)
		}
	})
	if !strings.Contains(created, "Epic: Launch") {
		t.Fatalf("ticket create printed %q, want it to show Epic: Launch", created)
	}

	// Leaving --epic out on update keeps it.
	kept := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "update", "BILL-1", "--priority", "high"); err != nil {
			t.Fatalf("ticket update without --epic: %v", err)
		}
	})
	if !strings.Contains(kept, "Epic: Launch") {
		t.Fatalf("ticket update printed %q, want the epic kept", kept)
	}

	// ticket list shows the epic.
	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "list"); err != nil {
			t.Fatalf("ticket list: %v", err)
		}
	})
	if !strings.Contains(listed, "epic:Launch") {
		t.Fatalf("ticket list = %q, want it to show epic:Launch", listed)
	}

	// --epic none (any case) clears it, same as "".
	cleared := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "update", "BILL-1", "--epic", "NONE"); err != nil {
			t.Fatalf("ticket update --epic NONE: %v", err)
		}
	})
	if strings.Contains(cleared, "Epic:") {
		t.Fatalf("ticket update printed %q, want no epic after clearing", cleared)
	}

	listedAfterClear := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "list"); err != nil {
			t.Fatalf("ticket list: %v", err)
		}
	})
	if strings.Contains(listedAfterClear, "epic:") {
		t.Fatalf("ticket list = %q, want no epic reference after clearing", listedAfterClear)
	}

	// --epic "" (an explicit empty string, not just an omitted flag) also
	// clears it. This must go through the same "flag was changed" check as
	// --due, not a "value is non-empty" check: since "" is the zero value,
	// checking for non-empty would silently treat this the same as omitting
	// the flag and leave the epic untouched instead of clearing it.
	if _, err := runCLI(t, "--db", path, "ticket", "update", "BILL-1", "--epic", "Launch"); err != nil {
		t.Fatalf("re-setting the epic: %v", err)
	}
	clearedEmpty := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "update", "BILL-1", "--epic", ""); err != nil {
			t.Fatalf(`ticket update --epic "": %v`, err)
		}
	})
	if strings.Contains(clearedEmpty, "Epic:") {
		t.Fatalf(`ticket update printed %q, want no epic after --epic ""`, clearedEmpty)
	}
	listedAfterEmptyClear := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "list"); err != nil {
			t.Fatalf("ticket list: %v", err)
		}
	})
	if strings.Contains(listedAfterEmptyClear, "epic:") {
		t.Fatalf(`ticket list = %q, want no epic reference after --epic ""`, listedAfterEmptyClear)
	}

	// A whitespace-only value is rejected, not read as a clear.
	if _, err := runCLI(t, "--db", path, "ticket", "update", "BILL-1", "--epic", "   "); err == nil {
		t.Fatal("expected an error for a whitespace-only --epic value")
	}

	// An unknown epic name is a clear error.
	if _, err := runCLI(t, "--db", path, "ticket", "update", "BILL-1", "--epic", "Nope"); err == nil {
		t.Fatal("expected an error for an unknown epic name")
	}
}

// ticket list --epic filters by name and by "none" for tickets without one.
func TestTicketListEpicFilter(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "epic", "create", "BILL", "Launch"); err != nil {
		t.Fatalf("epic create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "In epic", "--epic", "Launch"); err != nil {
		t.Fatalf("ticket create in epic: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "No epic ticket"); err != nil {
		t.Fatalf("ticket create without epic: %v", err)
	}

	inEpic := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "list", "--epic", "Launch"); err != nil {
			t.Fatalf("ticket list --epic Launch: %v", err)
		}
	})
	if !strings.Contains(inEpic, "In epic") || strings.Contains(inEpic, "No epic ticket") {
		t.Fatalf("ticket list --epic Launch = %q, want only the ticket in the epic", inEpic)
	}

	noEpic := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "list", "--epic", "none"); err != nil {
			t.Fatalf("ticket list --epic none: %v", err)
		}
	})
	if !strings.Contains(noEpic, "No epic ticket") || strings.Contains(noEpic, "In epic") {
		t.Fatalf("ticket list --epic none = %q, want only the ticket without an epic", noEpic)
	}
}

// epic list shows accurate done/total counts, marks a fully-done epic
// complete, and the "No epic" line carries its own counts for the project's
// tickets that have no epic.
func TestEpicListShowsProgressAndCompleteness(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "epic", "create", "BILL", "Launch"); err != nil {
		t.Fatalf("epic create: %v", err)
	}

	// One ticket in the epic, moved to done: the epic is complete.
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Ship it", "--epic", "Launch"); err != nil {
		t.Fatalf("ticket create in epic: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "move", "BILL-1", "--status", "done"); err != nil {
		t.Fatalf("ticket move to done: %v", err)
	}

	// One ticket with no epic, left in todo: the "No epic" summary must not
	// be marked complete.
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Unassigned"); err != nil {
		t.Fatalf("ticket create without epic: %v", err)
	}

	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "epic", "list", "BILL"); err != nil {
			t.Fatalf("epic list: %v", err)
		}
	})
	lines := strings.Split(strings.TrimSpace(listed), "\n")
	if len(lines) != 2 {
		t.Fatalf("epic list = %q, want exactly 2 lines (Launch, No epic)", listed)
	}
	if !strings.Contains(lines[0], "Launch") || !strings.Contains(lines[0], "1/1 done") || !strings.Contains(lines[0], "- complete") {
		t.Fatalf("epic list Launch line = %q, want it to show 1/1 done and complete", lines[0])
	}
	if !strings.Contains(lines[1], "No epic") || !strings.Contains(lines[1], "0/1 done") || strings.Contains(lines[1], "complete") {
		t.Fatalf("epic list No epic line = %q, want it to show 0/1 done and not complete", lines[1])
	}
}

// epic create and update's --description isn't reflected in any CLI output,
// so this reads the store directly to confirm it is actually persisted.
func TestEpicDescriptionIsSaved(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	created := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "epic", "create", "BILL", "Launch", "--description", "Go live"); err != nil {
			t.Fatalf("epic create: %v", err)
		}
	})
	epicID := lastParenthesized(t, created)

	getEpic := func() *models.Epic {
		t.Helper()
		database, err := db.OpenAt(path)
		if err != nil {
			t.Fatalf("opening db to verify: %v", err)
		}
		defer database.Close()
		e, err := db.NewStore(database).GetEpic(epicID)
		if err != nil {
			t.Fatalf("GetEpic: %v", err)
		}
		return e
	}

	if e := getEpic(); e == nil || e.Description != "Go live" {
		t.Fatalf("epic after create = %+v, want description %q", e, "Go live")
	}

	if _, err := runCLI(t, "--db", path, "epic", "update", epicID, "--description", "Go live for real"); err != nil {
		t.Fatalf("epic update --description: %v", err)
	}
	if e := getEpic(); e == nil || e.Description != "Go live for real" {
		t.Fatalf("epic after update = %+v, want description %q", e, "Go live for real")
	}
}
