package mcp

import (
	"sort"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// list_tickets takes status as one string or an array, ready and
// excludeLabel, alone and with the existing filters.
func TestListTicketsToolFilters(t *testing.T) {
	s := newTestServer(t)
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Search", Prefix: "SRCH"})
	if err != nil {
		t.Fatal(err)
	}
	create := func(projectID, title, status string, labels []string, dependsOn ...string) *models.Ticket {
		t.Helper()
		tk, err := s.store.CreateTicket(models.CreateTicketRequest{
			ProjectID: projectID, Title: title, Status: status, Labels: labels, DependsOn: dependsOn,
		})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	finished := create(p.ID, "finished", models.StatusDone, nil)
	doing := create(p.ID, "doing", models.StatusInProgress, nil)
	create(p.ID, "reviewing", models.StatusAgentReview, nil)
	create(p.ID, "startable", models.StatusTodo, []string{"api"}, finished.ID)
	create(p.ID, "waits on doing", models.StatusTodo, nil, doing.ID)
	create(p.ID, "held", models.StatusTodo, []string{"hold"})
	create(other.ID, "elsewhere", models.StatusTodo, nil)

	check := func(args map[string]any, want ...string) {
		t.Helper()
		out, err := s.callTool("list_tickets", mustJSON(t, args))
		if err != nil {
			t.Fatalf("list_tickets %v: %v", args, err)
		}
		var got []string
		for _, tk := range out.([]models.Ticket) {
			got = append(got, tk.Title)
		}
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("list_tickets %v = %v, want %v", args, got, want)
		}
	}

	check(map[string]any{"status": "done"}, "finished")
	check(map[string]any{"status": []string{"in_progress", "agent_review"}}, "doing", "reviewing")
	check(map[string]any{"status": []string{"todo", "in_progress", "agent_review"}, "projectId": "BILL"},
		"doing", "reviewing", "startable", "waits on doing", "held")
	check(map[string]any{"status": []string{}}, "finished", "doing", "reviewing", "startable", "waits on doing", "held", "elsewhere")
	check(map[string]any{"ready": true}, "startable", "held", "elsewhere")
	check(map[string]any{"ready": true, "excludeLabel": "HOLD"}, "startable", "elsewhere")
	check(map[string]any{"ready": true, "excludeLabel": "hold", "projectId": "bill", "label": "api", "epic": "none"}, "startable")
	check(map[string]any{"ready": true, "status": "in_progress"})
	check(map[string]any{"status": "todo", "excludeLabel": "hold", "projectId": "BILL"}, "startable", "waits on doing")

	for _, bad := range []map[string]any{
		{"status": 1},
		{"status": []any{"todo", 2}},
		{"ready": "yes"},
		{"excludeLabel": []string{"hold"}},
	} {
		if _, err := s.callTool("list_tickets", mustJSON(t, bad)); err == nil || !strings.Contains(err.Error(), "invalid arguments") {
			t.Fatalf("list_tickets %v: err = %v, want an invalid arguments error", bad, err)
		}
	}
}

// The tool definition describes the new filters, and status takes an array.
func TestListTicketsToolDefinitionHasNewFilters(t *testing.T) {
	s := newTestServer(t)
	for _, td := range s.toolDefinitions() {
		if td.Name != "list_tickets" {
			continue
		}
		props := td.InputSchema.Properties
		if props["status"].Type != "array" || props["status"].Items == nil || props["status"].Items.Type != "string" {
			t.Fatalf("list_tickets status = %+v, want an array of strings", props["status"])
		}
		for _, st := range models.Statuses {
			if !strings.Contains(props["status"].Description, st) {
				t.Fatalf("list_tickets status description %q does not name %q", props["status"].Description, st)
			}
		}
		if props["ready"].Type != "boolean" {
			t.Fatalf("list_tickets ready = %+v, want a boolean", props["ready"])
		}
		if props["excludeLabel"].Type != "string" {
			t.Fatalf("list_tickets excludeLabel = %+v, want a string", props["excludeLabel"])
		}
		return
	}
	t.Fatal("no list_tickets tool definition")
}
