package mcp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/ticketlist"
	"github.com/tcarac/taskboard/internal/weburl"
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
			ProjectID: projectID, Title: title, Status: status, Labels: labels, DependsOn: models.DependOn(dependsOn...),
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
	// A single string with several comma-separated statuses behaves the same
	// as an array, matching the CLI's --status.
	check(map[string]any{"status": "in_progress,agent_review"}, "doing", "reviewing")

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

	// A mistyped status is a clear error naming the allowed values, not a
	// silent empty list.
	for _, bad := range []map[string]any{
		{"status": "in-progress"},
		{"status": []string{"todo", "in-progress"}},
	} {
		_, err := s.callTool("list_tickets", mustJSON(t, bad))
		if err == nil || !strings.Contains(err.Error(), `"in-progress"`) {
			t.Fatalf("list_tickets %v: err = %v, want it to name %q", bad, err, "in-progress")
		}
		for _, st := range models.Statuses {
			if !strings.Contains(err.Error(), st) {
				t.Fatalf("list_tickets %v: err = %v, want it to name %q", bad, err, st)
			}
		}
	}
}

// The tool definition describes the new filters, status takes an array, and
// its items carry an enum of the valid statuses (restored after ACP-147 lost
// it, since jsonSchema.Items had no Enum field).
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
		if !reflect.DeepEqual(props["status"].Items.Enum, models.Statuses) {
			t.Fatalf("list_tickets status items enum = %v, want %v", props["status"].Items.Enum, models.Statuses)
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

// toJSONMap is v as the JSON object a client reads.
func toJSONMap(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("answer %s is not a JSON object: %v", data, err)
	}
	return m
}

// summary: true answers with a page of summaries: key, title, status,
// priority, epic, labels, dependency keys with their status, subtask
// progress and url, and none of the full form's other fields.
func TestListTicketsToolSummary(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Invoices"}); err != nil {
		t.Fatal(err)
	}
	dep, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Schema", Status: models.StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	epic := "Invoices"
	tk, err := s.store.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Send invoices", Description: "A long description that the summary leaves out",
		Priority: "high", Epic: &epic, Labels: []string{"api", "backend"}, DependsOn: models.DependOn(dep.ID), Repos: []string{"acme/billing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, title := range []string{"draft", "send", "retry"} {
		st, err := s.store.AddSubtask(tk.ID, models.CreateSubtaskRequest{Title: title})
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			if _, err := s.store.SetSubtaskState(st.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}

	out, err := s.callTool("list_tickets", mustJSON(t, map[string]any{"summary": true, "projectId": "BILL", "status": "todo"}))
	if err != nil {
		t.Fatal(err)
	}
	got := toJSONMap(t, out)
	want := map[string]any{
		"tickets": []any{map[string]any{
			"key":       "BILL-2",
			"title":     "Send invoices",
			"status":    "todo",
			"priority":  "high",
			"epic":      "Invoices",
			"labels":    []any{"api", "backend"},
			"dependsOn": []any{map[string]any{"key": "BILL-1", "status": "done"}},
			"subtasks":  "2/3",
			"url":       "http://board.test/?ticket=BILL-2",
		}},
		"total":   float64(1),
		"offset":  float64(0),
		"limit":   float64(ticketlist.DefaultLimit),
		"hasMore": false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary answer\n got %#v\nwant %#v", got, want)
	}

	// A ticket with no epic, labels, dependencies or subtasks leaves those
	// fields out rather than sending them empty.
	out, err = s.callTool("list_tickets", mustJSON(t, map[string]any{"summary": true, "status": "done"}))
	if err != nil {
		t.Fatal(err)
	}
	bare := toJSONMap(t, out)["tickets"].([]any)[0].(map[string]any)
	wantBare := map[string]any{"key": "BILL-1", "title": "Schema", "status": "done", "priority": "medium", "url": "http://board.test/?ticket=BILL-1"}
	if !reflect.DeepEqual(bare, wantBare) {
		t.Fatalf("bare summary\n got %#v\nwant %#v", bare, wantBare)
	}

	// No match is an empty page, with tickets as [] rather than null.
	out, err = s.callTool("list_tickets", mustJSON(t, map[string]any{"summary": true, "status": "agent_review"}))
	if err != nil {
		t.Fatal(err)
	}
	if tickets, ok := toJSONMap(t, out)["tickets"].([]any); !ok || len(tickets) != 0 {
		t.Fatalf("empty summary page tickets = %#v, want []", toJSONMap(t, out)["tickets"])
	}
}

// Without summary, limit or offset the answer is the array of full tickets
// it has always been, and summary: false alone asks for nothing new.
func TestListTicketsToolFullFormUnchanged(t *testing.T) {
	s := newTestServer(t)
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < ticketlist.DefaultLimit+3; i++ {
		if _, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "t", Description: "full"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range []map[string]any{{}, {"summary": false}, {"projectId": "BILL"}} {
		out, err := s.callTool("list_tickets", mustJSON(t, args))
		if err != nil {
			t.Fatal(err)
		}
		tickets, ok := out.([]models.Ticket)
		if !ok {
			t.Fatalf("list_tickets %v answered %T, want []models.Ticket", args, out)
		}
		if len(tickets) != ticketlist.DefaultLimit+3 {
			t.Fatalf("list_tickets %v = %d tickets, want every one of %d", args, len(tickets), ticketlist.DefaultLimit+3)
		}
		if tickets[0].Description != "full" || tickets[0].URL == "" {
			t.Fatalf("list_tickets %v ticket = %+v, want the full form with its url", args, tickets[0])
		}
	}
}

// limit and offset page through the list, in summary or full form: the
// answer says whether more follow and where the next page starts, and the
// second page picks up where the first ended.
func TestListTicketsToolPaging(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: fmt.Sprintf("t%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.store.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatal(err)
	}
	keys := func(from, to int) []any {
		var out []any
		for _, tk := range all[from:to] {
			out = append(out, tk.DisplayKey())
		}
		return out
	}
	page := func(args map[string]any) map[string]any {
		t.Helper()
		out, err := s.callTool("list_tickets", mustJSON(t, args))
		if err != nil {
			t.Fatalf("list_tickets %v: %v", args, err)
		}
		return toJSONMap(t, out)
	}
	pageKeys := func(m map[string]any, field string) []any {
		var out []any
		for _, tk := range m["tickets"].([]any) {
			out = append(out, tk.(map[string]any)[field])
		}
		return out
	}

	first := page(map[string]any{"summary": true, "limit": 2})
	if !reflect.DeepEqual(pageKeys(first, "key"), keys(0, 2)) || first["total"] != float64(5) ||
		first["hasMore"] != true || first["nextOffset"] != float64(2) || first["limit"] != float64(2) {
		t.Fatalf("first page = %#v, want %v of 5 with nextOffset 2", first, keys(0, 2))
	}
	second := page(map[string]any{"summary": true, "limit": 2, "offset": first["nextOffset"]})
	if !reflect.DeepEqual(pageKeys(second, "key"), keys(2, 4)) || second["offset"] != float64(2) ||
		second["hasMore"] != true || second["nextOffset"] != float64(4) {
		t.Fatalf("second page = %#v, want %v with nextOffset 4", second, keys(2, 4))
	}
	last := page(map[string]any{"summary": true, "limit": 2, "offset": 4})
	if _, has := last["nextOffset"]; !reflect.DeepEqual(pageKeys(last, "key"), keys(4, 5)) || last["hasMore"] != false || has {
		t.Fatalf("last page = %#v, want %v and no more", last, keys(4, 5))
	}

	// Paging without summary pages the full form.
	full := page(map[string]any{"limit": 2, "offset": 2})
	if !reflect.DeepEqual(pageKeys(full, "number"), []any{float64(all[2].Number), float64(all[3].Number)}) || full["nextOffset"] != float64(4) {
		t.Fatalf("full second page = %#v, want tickets %d and %d", full, all[2].Number, all[3].Number)
	}
	if tk := full["tickets"].([]any)[0].(map[string]any); tk["id"] != all[2].ID || tk["url"] == nil || tk["projectId"] == nil {
		t.Fatalf("full page ticket = %#v, want the full form", tk)
	}
	// An offset alone pages with the default limit.
	if m := page(map[string]any{"offset": 1}); m["limit"] != float64(ticketlist.DefaultLimit) || len(m["tickets"].([]any)) != 4 || m["hasMore"] != false {
		t.Fatalf("offset alone = %#v, want 4 tickets under the default limit", m)
	}
	// Past the end is an empty page.
	if m := page(map[string]any{"summary": true, "offset": 9}); len(m["tickets"].([]any)) != 0 || m["total"] != float64(5) || m["hasMore"] != false {
		t.Fatalf("past the end = %#v, want an empty page of 5", m)
	}

	for _, bad := range []map[string]any{
		{"limit": 0},
		{"limit": ticketlist.MaxLimit + 1},
		{"summary": true, "offset": -1},
	} {
		if _, err := s.callTool("list_tickets", mustJSON(t, bad)); err == nil {
			t.Fatalf("list_tickets %v: want an error", bad)
		}
	}
	for _, bad := range []map[string]any{{"limit": "10"}, {"offset": 1.5}, {"summary": "yes"}} {
		if _, err := s.callTool("list_tickets", mustJSON(t, bad)); err == nil || !strings.Contains(err.Error(), "invalid arguments") {
			t.Fatalf("list_tickets %v: err = %v, want an invalid arguments error", bad, err)
		}
	}
}

// The tool definition offers summary, limit and offset, and says when to
// use the summary and how to get the next page.
func TestListTicketsToolDefinitionHasSummaryAndPaging(t *testing.T) {
	s := newTestServer(t)
	for _, td := range s.toolDefinitions() {
		if td.Name != "list_tickets" {
			continue
		}
		props := td.InputSchema.Properties
		if props["summary"].Type != "boolean" || props["limit"].Type != "integer" || props["offset"].Type != "integer" {
			t.Fatalf("list_tickets summary/limit/offset = %+v / %+v / %+v", props["summary"], props["limit"], props["offset"])
		}
		for _, want := range []string{"summary: true", "get_ticket", "nextOffset", "hasMore", "array of every match in full"} {
			if !strings.Contains(td.Description, want) {
				t.Fatalf("list_tickets description does not mention %q: %s", want, td.Description)
			}
		}
		return
	}
	t.Fatal("no list_tickets tool definition")
}
