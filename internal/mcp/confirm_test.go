package mcp

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// changeTools are the MCP tools that change something and answer with the
// record they changed: short by default, the whole record with full: true.
var changeTools = []string{
	"create_project", "update_project",
	"create_epic", "update_epic",
	"create_label", "update_label",
	"create_ticket", "update_ticket", "move_ticket",
	"create_subtask", "batch_create_subtasks", "toggle_subtask", "delete_subtask",
	"create_document", "update_document",
}

// callJSON runs a tool the way an MCP client does and decodes the JSON text
// it answers with, failing the test on a tool error.
func callJSON(t *testing.T, s *MCPServer, tool string, args map[string]any) map[string]any {
	t.Helper()
	text, isError := callToolText(t, s, tool, args)
	if isError {
		t.Fatalf("%s: %s", tool, text)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("%s returned %q: %v", tool, text, err)
	}
	return got
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// wantKeys pins a short confirmation's exact fields, so nothing from the
// whole record creeps back into it.
func wantKeys(t *testing.T, tool string, got map[string]any, want ...string) {
	t.Helper()
	sort.Strings(want)
	if keys := keysOf(got); !reflect.DeepEqual(keys, want) {
		t.Fatalf("%s short answer has fields %v, want %v\n%v", tool, keys, want, got)
	}
}

func wantChanged(t *testing.T, tool string, got map[string]any, want ...string) {
	t.Helper()
	raw, ok := got["changed"].([]any)
	if !ok {
		t.Fatalf("%s changed = %#v, want a list", tool, got["changed"])
	}
	fields := make([]string, len(raw))
	for i, f := range raw {
		fields[i] = f.(string)
	}
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("%s changed = %v, want %v", tool, fields, want)
	}
}

func TestTicketToolsAnswerShortByDefault(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}

	created := callJSON(t, s, "create_ticket", map[string]any{
		"projectId": "BILL", "title": "Invoice", "description": "A long description nobody reads back.",
		"labels": []string{"api"}, "priority": "high",
	})
	wantKeys(t, "create_ticket", created, "id", "key", "title", "status", "created", "updatedAt", "url")
	if created["key"] != "BILL-1" || created["title"] != "Invoice" || created["status"] != "todo" ||
		created["created"] != true || created["url"] != "http://board.test/?ticket=BILL-1" {
		t.Fatalf("create_ticket = %v", created)
	}

	updated := callJSON(t, s, "update_ticket", map[string]any{
		"id": "BILL-1", "priority": "low", "labels": []string{"api", "bug"}, "title": "Invoice",
	})
	wantKeys(t, "update_ticket", updated, "id", "key", "title", "status", "changed", "updatedAt", "url")
	// title was sent unchanged, so it is not listed.
	wantChanged(t, "update_ticket", updated, "priority", "labels")

	// An update that changes nothing says so with an empty list.
	same := callJSON(t, s, "update_ticket", map[string]any{"id": "BILL-1", "priority": "low"})
	wantChanged(t, "update_ticket", same)

	moved := callJSON(t, s, "move_ticket", map[string]any{"id": "BILL-1", "status": "in_progress"})
	wantKeys(t, "move_ticket", moved, "id", "key", "title", "status", "changed", "updatedAt", "url")
	wantChanged(t, "move_ticket", moved, "status")
	if moved["status"] != "in_progress" {
		t.Fatalf("move_ticket status = %v", moved["status"])
	}
	// A move to the column it is already in changes nothing a caller set.
	wantChanged(t, "move_ticket", callJSON(t, s, "move_ticket", map[string]any{"id": "BILL-1", "status": "in_progress"}))

	// The update's changes and the move landed, whatever the answer holds.
	tk, err := s.store.GetTicket(created["id"].(string))
	if err != nil || tk.Priority != "low" || len(tk.Labels) != 2 || tk.Status != "in_progress" {
		t.Fatalf("stored ticket = %+v, %v", tk, err)
	}
}

func TestTicketToolsAnswerWholeTicketOnRequest(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}
	// The whole ticket: the fields the store sends, url included.
	wholeTicket := func(tool string, got map[string]any) {
		t.Helper()
		for _, key := range []string{"id", "projectId", "number", "title", "description", "status", "priority", "labels", "url", "createdAt", "updatedAt"} {
			if _, ok := got[key]; !ok {
				t.Fatalf("%s full answer has no %s: %v", tool, key, got)
			}
		}
		for _, key := range []string{"created", "changed", "key"} {
			if _, ok := got[key]; ok {
				t.Fatalf("%s full answer carries the short form's %s", tool, key)
			}
		}
	}
	wholeTicket("create_ticket", callJSON(t, s, "create_ticket", map[string]any{
		"projectId": "BILL", "title": "Invoice", "description": "Details.", "labels": []string{"api"}, "full": true,
	}))
	wholeTicket("update_ticket", callJSON(t, s, "update_ticket", map[string]any{"id": "BILL-1", "priority": "low", "full": true}))
	wholeTicket("move_ticket", callJSON(t, s, "move_ticket", map[string]any{"id": "BILL-1", "status": "done", "full": true}))

	// full: false is the default, spelled out.
	short := callJSON(t, s, "move_ticket", map[string]any{"id": "BILL-1", "status": "todo", "full": false})
	if _, ok := short["description"]; ok || short["changed"] == nil {
		t.Fatalf("move_ticket with full: false = %v, want the short form", short)
	}
}

func TestSubtaskToolsAnswerShortByDefault(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	url := "http://board.test/?ticket=DOC-1"

	one := callJSON(t, s, "create_subtask", map[string]any{"ticketId": "DOC-1", "title": "Step one"})
	wantKeys(t, "create_subtask", one, "id", "title", "completed", "ticket", "created", "url")
	if one["title"] != "Step one" || one["completed"] != false || one["ticket"] != "DOC-1" || one["created"] != true || one["url"] != url {
		t.Fatalf("create_subtask = %v", one)
	}

	batch := callJSON(t, s, "batch_create_subtasks", map[string]any{
		"ticketId": "DOC-1", "subtasks": []map[string]any{{"title": "Step two"}, {"title": "Step three"}},
	})
	wantKeys(t, "batch_create_subtasks", batch, "ticket", "created", "subtasks", "url")
	subs := batch["subtasks"].([]any)
	if batch["ticket"] != "DOC-1" || batch["created"] != true || batch["url"] != url || len(subs) != 2 {
		t.Fatalf("batch_create_subtasks = %v", batch)
	}
	for i, title := range []string{"Step two", "Step three"} {
		sub := subs[i].(map[string]any)
		wantKeys(t, "batch_create_subtasks subtask", sub, "id", "title")
		if sub["title"] != title || sub["id"] == "" {
			t.Fatalf("batch subtask %d = %v", i, sub)
		}
	}

	id := one["id"].(string)
	ticked := callJSON(t, s, "toggle_subtask", map[string]any{"id": id, "completed": true})
	wantKeys(t, "toggle_subtask", ticked, "id", "title", "completed", "ticket", "changed", "url")
	wantChanged(t, "toggle_subtask", ticked, "completed")
	if ticked["completed"] != true {
		t.Fatalf("toggle_subtask = %v", ticked)
	}
	// Already ticked: nothing changed.
	wantChanged(t, "toggle_subtask", callJSON(t, s, "toggle_subtask", map[string]any{"id": id, "completed": true}))
	// Flipping always changes it.
	flipped := callJSON(t, s, "toggle_subtask", map[string]any{"id": id})
	wantChanged(t, "toggle_subtask", flipped, "completed")
	if flipped["completed"] != false {
		t.Fatalf("flipped toggle_subtask = %v", flipped)
	}

	gone := callJSON(t, s, "delete_subtask", map[string]any{"id": id})
	wantKeys(t, "delete_subtask", gone, "id", "title", "completed", "ticket", "deleted", "url")
	if gone["id"] != id || gone["deleted"] != true || gone["ticket"] != "DOC-1" {
		t.Fatalf("delete_subtask = %v", gone)
	}
	if got, _ := s.store.GetTicket(tk.ID); len(got.Subtasks) != 2 {
		t.Fatalf("ticket has %d subtasks after the delete, want 2", len(got.Subtasks))
	}
}

func TestSubtaskToolsAnswerWholeTicketOnRequest(t *testing.T) {
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	subtaskCount := func(tool string, got map[string]any) int {
		t.Helper()
		if got["id"] != tk.ID || got["title"] != tk.Title || got["url"] == nil {
			t.Fatalf("%s full answer = %v, want the whole ticket", tool, got)
		}
		subs, _ := got["subtasks"].([]any)
		return len(subs)
	}

	one := callJSON(t, s, "create_subtask", map[string]any{"ticketId": "DOC-1", "title": "Step one", "full": true})
	if n := subtaskCount("create_subtask", one); n != 1 {
		t.Fatalf("create_subtask full answer lists %d subtasks, want 1", n)
	}
	batch := callJSON(t, s, "batch_create_subtasks", map[string]any{
		"ticketId": "DOC-1", "subtasks": []map[string]any{{"title": "Step two"}}, "full": true,
	})
	if n := subtaskCount("batch_create_subtasks", batch); n != 2 {
		t.Fatalf("batch_create_subtasks full answer lists %d subtasks, want 2", n)
	}
	id := one["subtasks"].([]any)[0].(map[string]any)["id"].(string)
	ticked := callJSON(t, s, "toggle_subtask", map[string]any{"id": id, "completed": true, "full": true})
	if first := ticked["subtasks"].([]any)[0].(map[string]any); first["completed"] != true {
		t.Fatalf("toggle_subtask full answer's subtask = %v, want completed", first)
	}
	gone := callJSON(t, s, "delete_subtask", map[string]any{"id": id, "full": true})
	if n := subtaskCount("delete_subtask", gone); n != 1 {
		t.Fatalf("delete_subtask full answer lists %d subtasks, want 1", n)
	}
}

func TestProjectToolsAnswerShortByDefault(t *testing.T) {
	s := newTestServer(t)
	created := callJSON(t, s, "create_project", map[string]any{
		"name": "Billing", "prefix": "BILL", "description": "Long.", "agentInstructions": "Longer.",
	})
	wantKeys(t, "create_project", created, "id", "prefix", "name", "status", "created", "updatedAt")
	if created["prefix"] != "BILL" || created["name"] != "Billing" || created["status"] != "active" || created["created"] != true {
		t.Fatalf("create_project = %v", created)
	}

	updated := callJSON(t, s, "update_project", map[string]any{"id": "BILL", "agentInstructions": "New rules.", "name": "Billing"})
	wantKeys(t, "update_project", updated, "id", "prefix", "name", "status", "changed", "updatedAt")
	wantChanged(t, "update_project", updated, "agentInstructions")

	full := callJSON(t, s, "update_project", map[string]any{"id": "BILL", "status": "archived", "full": true})
	if full["agentInstructions"] != "New rules." || full["description"] != "Long." || full["status"] != "archived" {
		t.Fatalf("update_project full answer = %v", full)
	}
	full = callJSON(t, s, "create_project", map[string]any{"name": "Support", "prefix": "SUP", "agentInstructions": "Rules.", "full": true})
	if full["agentInstructions"] != "Rules." || full["createdAt"] == nil {
		t.Fatalf("create_project full answer = %v", full)
	}
}

func TestEpicToolsAnswerShortByDefault(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}
	created := callJSON(t, s, "create_epic", map[string]any{"projectId": "BILL", "name": "Payments", "description": "Long."})
	wantKeys(t, "create_epic", created, "id", "name", "project", "created", "updatedAt", "url")
	if created["name"] != "Payments" || created["project"] != "BILL" || created["created"] != true ||
		created["url"] != weburl.Epic("http://board.test", "BILL", "Payments") {
		t.Fatalf("create_epic = %v", created)
	}

	updated := callJSON(t, s, "update_epic", map[string]any{"id": "Payments", "project": "BILL", "description": "Longer."})
	wantKeys(t, "update_epic", updated, "id", "name", "project", "changed", "updatedAt", "url")
	wantChanged(t, "update_epic", updated, "description")

	full := callJSON(t, s, "update_epic", map[string]any{"id": created["id"], "name": "Payouts", "full": true})
	if full["name"] != "Payouts" || full["description"] != "Longer." || full["counts"] == nil {
		t.Fatalf("update_epic full answer = %v", full)
	}
	full = callJSON(t, s, "create_epic", map[string]any{"projectId": "BILL", "name": "Refunds", "full": true})
	if full["projectId"] == nil || full["counts"] == nil {
		t.Fatalf("create_epic full answer = %v", full)
	}
}

func TestLabelToolsAnswerShortByDefault(t *testing.T) {
	s := newTestServer(t)
	created := callJSON(t, s, "create_label", map[string]any{"name": "bug", "color": "#FF0000"})
	wantKeys(t, "create_label", created, "id", "name", "color", "created")
	if created["name"] != "bug" || created["color"] != "#FF0000" || created["created"] != true {
		t.Fatalf("create_label = %v", created)
	}

	updated := callJSON(t, s, "update_label", map[string]any{"id": "bug", "color": "#00FF00", "name": "bug"})
	wantKeys(t, "update_label", updated, "id", "name", "color", "changed")
	wantChanged(t, "update_label", updated, "color")

	full := callJSON(t, s, "update_label", map[string]any{"id": "bug", "name": "defect", "full": true})
	if full["name"] != "defect" || full["ticketCount"] == nil {
		t.Fatalf("update_label full answer = %v", full)
	}
	if full := callJSON(t, s, "create_label", map[string]any{"name": "chore", "full": true}); full["ticketCount"] == nil {
		t.Fatalf("create_label full answer = %v", full)
	}
}

func TestDocumentToolsAnswerShortByDefault(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	desc := "![shot](Login screen.png)"
	if _, err := s.store.UpdateTicket(tk.ID, models.UpdateTicketRequest{Description: &desc}); err != nil {
		t.Fatal(err)
	}

	created := callJSON(t, s, "create_document", map[string]any{"ticket": "DOC-1", "name": "Plan", "content": "# A long plan"})
	wantKeys(t, "create_document", created, "id", "name", "format", "created", "updatedAt", "url")
	if created["name"] != "Plan" || created["format"] != "markdown" || created["created"] != true ||
		created["url"] != "http://board.test/?ticket=DOC-1&doc=Plan.md" {
		t.Fatalf("create_document = %v", created)
	}

	updated := callJSON(t, s, "update_document", map[string]any{"ticket": "DOC-1", "id": "Plan", "content": "# A longer plan"})
	wantKeys(t, "update_document", updated, "id", "name", "format", "changed", "updatedAt", "url")
	wantChanged(t, "update_document", updated, "content")
	renamed := callJSON(t, s, "update_document", map[string]any{"ticket": "DOC-1", "id": "Plan", "name": "Plan v2", "content": "# A longer plan"})
	wantChanged(t, "update_document", renamed, "name")

	full := callJSON(t, s, "update_document", map[string]any{"ticket": "DOC-1", "id": "Plan v2", "content": "# Final", "full": true})
	if full["content"] != "# Final" || full["revision"] == nil {
		t.Fatalf("update_document full answer = %v", full)
	}
	full = callJSON(t, s, "create_document", map[string]any{"ticket": "DOC-1", "name": "Notes", "content": "Hi", "full": true})
	if full["content"] != "Hi" {
		t.Fatalf("create_document full answer = %v", full)
	}

	// An image: a new picture is always listed as data, and a rename's
	// rewritten references stay in the short form.
	image := callJSON(t, s, "create_document", map[string]any{"ticket": "DOC-1", "name": "Login screen.png", "data": b64(imagedoctest.PNG(8, 8))})
	wantKeys(t, "create_document", image, "id", "name", "format", "created", "updatedAt", "url")
	replaced := callJSON(t, s, "update_document", map[string]any{"id": image["id"], "data": b64(imagedoctest.PNG(8, 8))})
	wantChanged(t, "update_document", replaced, "data")
	moved := callJSON(t, s, "update_document", map[string]any{"id": image["id"], "name": "Home"})
	wantKeys(t, "update_document", moved, "id", "name", "format", "changed", "updatedAt", "url", "referencesUpdated")
	wantChanged(t, "update_document", moved, "name")
	if refs := moved["referencesUpdated"].([]any); len(refs) != 1 || refs[0].(map[string]any)["kind"] != "description" {
		t.Fatalf("update_document referencesUpdated = %v", moved["referencesUpdated"])
	}
}

func TestChangeToolDescriptionsExplainBothAnswers(t *testing.T) {
	s := newTestServer(t)
	defs := map[string]toolDef{}
	for _, def := range s.toolDefinitions() {
		defs[def.Name] = def
	}
	for _, name := range changeTools {
		def, ok := defs[name]
		if !ok {
			t.Fatalf("no tool %s", name)
		}
		if !strings.Contains(def.Description, "By default it answers with a short confirmation: ") ||
			!strings.Contains(def.Description, "Pass full: true to get the whole ") {
			t.Errorf("%s description doesn't explain the short and full answers: %s", name, def.Description)
		}
		full, ok := def.InputSchema.Properties["full"]
		if !ok || full.Type != "boolean" || !strings.Contains(full.Description, "instead of the short confirmation") {
			t.Errorf("%s full argument = %+v (present %v)", name, full, ok)
		}
	}
	// Tools that change nothing, or delete what they would describe, take no full.
	for name, def := range defs {
		if _, ok := def.InputSchema.Properties["full"]; ok && !contains(changeTools, name) {
			t.Errorf("%s takes full but is not a change tool", name)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
