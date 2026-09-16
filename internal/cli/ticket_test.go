package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/weburl"
)

// captureStdout runs fn with os.Stdout replaced by a pipe and returns what it
// printed. The ticket commands print with fmt.Printf, which cobra's output
// buffer does not see.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	fn()
	os.Stdout = prev
	w.Close()
	out := <-done
	r.Close()
	return out
}

// An agent reading CLI output should be able to hand a person a link, so every
// command that prints a ticket prints its URL.
func TestTicketCommandsPrintTheTicketURL(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	t.Setenv(weburl.BaseEnv, "http://localhost:3013")
	path := filepath.Join(t.TempDir(), "dev.db")

	out := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "project", "create", "Agent control plane", "--prefix", "ACP"); err != nil {
			t.Fatalf("project create: %v", err)
		}
	})
	projectID := lastParenthesized(t, out)

	want := "http://localhost:3013/?ticket=ACP-1"

	created := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", projectID, "--title", "URL-addressable tickets"); err != nil {
			t.Fatalf("ticket create: %v", err)
		}
	})
	if !strings.Contains(created, want) {
		t.Fatalf("ticket create printed %q, want it to contain %q", created, want)
	}
	ticketID := lastParenthesized(t, created)

	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "list"); err != nil {
			t.Fatalf("ticket list: %v", err)
		}
	})
	if !strings.Contains(listed, want) {
		t.Fatalf("ticket list printed %q, want it to contain %q", listed, want)
	}

	updated := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "update", ticketID, "--priority", "high"); err != nil {
			t.Fatalf("ticket update: %v", err)
		}
	})
	if !strings.Contains(updated, want) {
		t.Fatalf("ticket update printed %q, want it to contain %q", updated, want)
	}
}

// Ticket move, delete and update, and ticket create's --project flag, must
// accept a display key or project prefix, not just the raw ULID, the same as
// --depends-on already does.
func TestTicketCommandsAcceptDisplayKeysAndProjectPrefix(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}

	// ticket create --project accepts a project prefix, case-insensitively.
	created := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "bill", "--title", "Invoice"); err != nil {
			t.Fatalf("ticket create --project bill: %v", err)
		}
	})
	if !strings.Contains(created, "BILL-1") {
		t.Fatalf("ticket create --project bill printed %q, want it to mention BILL-1", created)
	}

	// ticket update by display key, case-insensitively.
	updated := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "update", "bill-1", "--priority", "high"); err != nil {
			t.Fatalf("ticket update bill-1: %v", err)
		}
	})
	if !strings.Contains(updated, "BILL-1") {
		t.Fatalf("ticket update bill-1 printed %q, want it to mention BILL-1", updated)
	}

	// ticket move by display key.
	moved := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "move", "BILL-1", "--status", "in_progress"); err != nil {
			t.Fatalf("ticket move BILL-1: %v", err)
		}
	})
	if !strings.Contains(moved, "BILL-1") {
		t.Fatalf("ticket move BILL-1 printed %q, want it to mention BILL-1", moved)
	}

	// ticket delete by display key.
	if _, err := runCLI(t, "--db", path, "ticket", "delete", "BILL-1"); err != nil {
		t.Fatalf("ticket delete BILL-1: %v", err)
	}
	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "ticket", "list"); err != nil {
			t.Fatalf("ticket list: %v", err)
		}
	})
	if !strings.Contains(listed, "No tickets found") {
		t.Fatalf("ticket list after delete = %q, want no tickets", listed)
	}
}

func TestTicketCommandsRejectUnresolvableKeyOrProject(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}

	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "NOPE", "--title", "Invoice"); err == nil {
		t.Fatal("expected an error for an unknown project prefix")
	} else if strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("error %q leaked a raw constraint failure", err)
	}

	if _, err := runCLI(t, "--db", path, "ticket", "update", "BILL-99", "--priority", "high"); err == nil {
		t.Fatal("expected an error for an unknown display key")
	}

	if _, err := runCLI(t, "--db", path, "ticket", "move", "BILL-99", "--status", "in_progress"); err == nil {
		t.Fatal("expected an error for an unknown display key")
	}

	if _, err := runCLI(t, "--db", path, "ticket", "delete", "BILL-99"); err == nil {
		t.Fatal("expected an error for an unknown display key")
	}
}

// lastParenthesized returns the text inside the last (…) on the first line,
// which is how the CLI prints a new record's id.
func lastParenthesized(t *testing.T, out string) string {
	t.Helper()
	line := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
	open := strings.LastIndex(line, "(")
	closeIdx := strings.LastIndex(line, ")")
	if open < 0 || closeIdx < open {
		t.Fatalf("no id in %q", out)
	}
	return line[open+1 : closeIdx]
}
