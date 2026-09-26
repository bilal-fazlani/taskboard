package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// An agent reading CLI output should be able to hand a person a link, so every
// command that prints a ticket prints its URL.
func TestTicketCommandsPrintTheTicketURL(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	t.Setenv(weburl.BaseEnv, "http://localhost:3013")
	path := filepath.Join(t.TempDir(), "dev.db")

	out, err := runCLI(t, "--db", path, "project", "create", "Agent control plane", "--prefix", "ACP")
	if err != nil {
		t.Fatalf("project create: %v", err)
	}
	projectID := lastParenthesized(t, out)

	want := "http://localhost:3013/?ticket=ACP-1"

	created, err := runCLI(t, "--db", path, "ticket", "create", "--project", projectID, "--title", "URL-addressable tickets")
	if err != nil {
		t.Fatalf("ticket create: %v", err)
	}
	if !strings.Contains(created, want) {
		t.Fatalf("ticket create printed %q, want it to contain %q", created, want)
	}
	ticketID := lastParenthesized(t, created)

	listed, err := runCLI(t, "--db", path, "ticket", "list")
	if err != nil {
		t.Fatalf("ticket list: %v", err)
	}
	if !strings.Contains(listed, want) {
		t.Fatalf("ticket list printed %q, want it to contain %q", listed, want)
	}

	updated, err := runCLI(t, "--db", path, "ticket", "update", ticketID, "--priority", "high")
	if err != nil {
		t.Fatalf("ticket update: %v", err)
	}
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
	created, err := runCLI(t, "--db", path, "ticket", "create", "--project", "bill", "--title", "Invoice")
	if err != nil {
		t.Fatalf("ticket create --project bill: %v", err)
	}
	if !strings.Contains(created, "BILL-1") {
		t.Fatalf("ticket create --project bill printed %q, want it to mention BILL-1", created)
	}

	// ticket update by display key, case-insensitively.
	updated, err := runCLI(t, "--db", path, "ticket", "update", "bill-1", "--priority", "high")
	if err != nil {
		t.Fatalf("ticket update bill-1: %v", err)
	}
	if !strings.Contains(updated, "BILL-1") {
		t.Fatalf("ticket update bill-1 printed %q, want it to mention BILL-1", updated)
	}

	// ticket move by display key.
	moved, err := runCLI(t, "--db", path, "ticket", "move", "BILL-1", "--status", "in_progress")
	if err != nil {
		t.Fatalf("ticket move BILL-1: %v", err)
	}
	if !strings.Contains(moved, "BILL-1") {
		t.Fatalf("ticket move BILL-1 printed %q, want it to mention BILL-1", moved)
	}

	// ticket delete by display key.
	if _, err := runCLI(t, "--db", path, "ticket", "delete", "BILL-1"); err != nil {
		t.Fatalf("ticket delete BILL-1: %v", err)
	}
	listed, err := runCLI(t, "--db", path, "ticket", "list")
	if err != nil {
		t.Fatalf("ticket list: %v", err)
	}
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

// ticket get must print labels, dependsOn and subtasks, as readable text by
// default and as JSON with --json, and accept a display key.
func TestTicketGetPrintsLabelsDependsOnAndSubtasks(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	t.Setenv(weburl.BaseEnv, "http://localhost:3013")
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Blocker"); err != nil {
		t.Fatalf("ticket create blocker: %v", err)
	}
	invoiceCreated, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Invoice",
		"--label", "api", "--depends-on", "BILL-1")
	if err != nil {
		t.Fatalf("ticket create: %v", err)
	}
	invoiceID := lastParenthesized(t, invoiceCreated)
	if _, err := runCLI(t, "--db", path, "ticket", "subtask", "add", "BILL-2", "Write the handler"); err != nil {
		t.Fatalf("subtask add: %v", err)
	}

	// Readable text, addressed by display key, case-insensitively.
	out, err := runCLI(t, "--db", path, "ticket", "get", "bill-2")
	if err != nil {
		t.Fatalf("ticket get: %v", err)
	}
	for _, want := range []string{
		"[BILL-2]", "Invoice", "Labels: api", "Depends on: BILL-1",
		"Subtasks:", "Write the handler", "http://localhost:3013/?ticket=BILL-2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ticket get output %q missing %q", out, want)
		}
	}

	// JSON, addressed by the ticket's raw id.
	jsonOut, err := runCLI(t, "--db", path, "ticket", "get", invoiceID, "--json")
	if err != nil {
		t.Fatalf("ticket get --json: %v", err)
	}
	var got models.Ticket
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatalf("ticket get --json produced invalid JSON: %v\n%s", err, jsonOut)
	}
	if got.DisplayKey() != "BILL-2" {
		t.Fatalf("ticket get --json key = %q, want BILL-2", got.DisplayKey())
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "api" {
		t.Fatalf("ticket get --json labels = %+v, want [api]", got.Labels)
	}
	if len(got.DependsOn) != 1 || got.DependsOn[0].Key != "BILL-1" {
		t.Fatalf("ticket get --json dependsOn = %+v, want [BILL-1]", got.DependsOn)
	}
	if len(got.Subtasks) != 1 || got.Subtasks[0].Title != "Write the handler" {
		t.Fatalf("ticket get --json subtasks = %+v, want [Write the handler]", got.Subtasks)
	}
	if got.URL != "http://localhost:3013/?ticket=BILL-2" {
		t.Fatalf("ticket get --json url = %q", got.URL)
	}
}

func TestTicketGetRejectsUnknownKey(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "get", "BILL-99"); err == nil {
		t.Fatal("expected an error for an unknown display key")
	}
}

// ticket subtask add must accept the parent ticket's display key or its raw id.
func TestTicketSubtaskAddAcceptsDisplayKeyOrRawID(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	created, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Invoice")
	if err != nil {
		t.Fatalf("ticket create: %v", err)
	}
	ticketID := lastParenthesized(t, created)

	out, err := runCLI(t, "--db", path, "ticket", "subtask", "add", "bill-1", "Write the handler")
	if err != nil {
		t.Fatalf("subtask add bill-1: %v", err)
	}
	if !strings.Contains(out, "Write the handler") {
		t.Fatalf("subtask add output %q missing the title", out)
	}

	// The ticket's raw id works too, not just its display key.
	rawOut, err := runCLI(t, "--db", path, "ticket", "subtask", "add", ticketID, "Write the tests")
	if err != nil {
		t.Fatalf("subtask add by raw id: %v", err)
	}
	if !strings.Contains(rawOut, "Write the tests") {
		t.Fatalf("subtask add by raw id output %q missing the title", rawOut)
	}

	got, err := runCLI(t, "--db", path, "ticket", "get", "BILL-1")
	if err != nil {
		t.Fatalf("ticket get: %v", err)
	}
	for _, want := range []string{"Write the handler", "Write the tests"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ticket get after subtask add = %q, missing %q", got, want)
		}
	}

	if _, err := runCLI(t, "--db", path, "ticket", "subtask", "add", "BILL-99", "Nope"); err == nil {
		t.Fatal("expected an error for an unknown display key")
	}
}

// ticket subtask add must reject an empty or whitespace-only title, the same
// as HTTP's addSubtask and MCP's create_subtask do.
func TestTicketSubtaskAddRejectsBlankTitle(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Invoice"); err != nil {
		t.Fatalf("ticket create: %v", err)
	}

	for _, title := range []string{"", "   ", "\t"} {
		if _, err := runCLI(t, "--db", path, "ticket", "subtask", "add", "BILL-1", title); err == nil {
			t.Fatalf("subtask add with title %q: expected an error", title)
		} else if !strings.Contains(err.Error(), "title is required") {
			t.Fatalf("subtask add with title %q: error = %q, want it to say a title is required", title, err)
		}
	}

	got, err := runCLI(t, "--db", path, "ticket", "get", "BILL-1")
	if err != nil {
		t.Fatalf("ticket get: %v", err)
	}
	if strings.Contains(got, "Subtasks:") {
		t.Fatalf("ticket get = %q, a blank-titled subtask was created", got)
	}
}

// ticket subtask toggle flips a subtask's state, and delete removes it.
func TestTicketSubtaskToggleAndDelete(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "BILL", "--title", "Invoice"); err != nil {
		t.Fatalf("ticket create: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "subtask", "add", "BILL-1", "Write the handler"); err != nil {
		t.Fatalf("subtask add: %v", err)
	}

	jsonOut, err := runCLI(t, "--db", path, "ticket", "get", "BILL-1", "--json")
	if err != nil {
		t.Fatalf("ticket get --json: %v", err)
	}
	var ticket models.Ticket
	if err := json.Unmarshal([]byte(jsonOut), &ticket); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(ticket.Subtasks) != 1 {
		t.Fatalf("subtasks = %+v, want exactly one", ticket.Subtasks)
	}
	subtaskID := ticket.Subtasks[0].ID

	toggled, err := runCLI(t, "--db", path, "ticket", "subtask", "toggle", subtaskID)
	if err != nil {
		t.Fatalf("subtask toggle: %v", err)
	}
	if !strings.Contains(toggled, "now done") {
		t.Fatalf("subtask toggle output %q, want it to say the subtask is now done", toggled)
	}

	// A second toggle flips it back.
	toggledBack, err := runCLI(t, "--db", path, "ticket", "subtask", "toggle", subtaskID)
	if err != nil {
		t.Fatalf("subtask toggle again: %v", err)
	}
	if !strings.Contains(toggledBack, "now not done") {
		t.Fatalf("subtask toggle again output %q, want it to say the subtask is now not done", toggledBack)
	}

	if _, err := runCLI(t, "--db", path, "ticket", "subtask", "delete", subtaskID); err != nil {
		t.Fatalf("subtask delete: %v", err)
	}

	afterDelete, err := runCLI(t, "--db", path, "ticket", "get", "BILL-1")
	if err != nil {
		t.Fatalf("ticket get after delete: %v", err)
	}
	if strings.Contains(afterDelete, "Write the handler") {
		t.Fatalf("ticket get after subtask delete = %q, still shows the deleted subtask", afterDelete)
	}
}

// ticket subtask toggle and delete must both fail clearly, with a non-zero
// exit, on an id that matches no subtask, rather than silently succeeding.
// ToggleSubtask already reported this itself; delete used to rely on the CLI
// checking existence first (store.DeleteSubtask never reported whether it
// matched a row), so a false "Subtask deleted." was still possible through
// MCP and HTTP. store.DeleteSubtask now reports the same not-found error
// directly (ACP-121), and the CLI relies on that instead of its own check.
func TestTicketSubtaskToggleAndDeleteRejectUnknownID(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Billing", "--prefix", "BILL"); err != nil {
		t.Fatalf("project create: %v", err)
	}

	const unknownID = "01UNKNOWNSUBTASKIDXXXXXXXX"

	if _, err := runCLI(t, "--db", path, "ticket", "subtask", "toggle", unknownID); err == nil {
		t.Fatal("subtask toggle with an unknown id: expected an error")
	} else if !strings.Contains(err.Error(), "subtask not found") {
		t.Fatalf("subtask toggle with an unknown id: error = %q, want it to say the subtask was not found", err)
	}

	if _, err := runCLI(t, "--db", path, "ticket", "subtask", "delete", unknownID); err == nil {
		t.Fatal("subtask delete with an unknown id: expected an error")
	} else if !strings.Contains(err.Error(), "subtask not found") {
		t.Fatalf("subtask delete with an unknown id: error = %q, want it to say the subtask was not found", err)
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
