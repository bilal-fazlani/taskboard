package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

const (
	fullSHA1 = "6bafa19c0ffee0000000000000000000000000a1"
	fullSHA2 = "a198cc5deadbeef000000000000000000000b1b1"
)

// update_ticket sets the delivery fields, get_ticket reads them back, and
// the short answer names delivery among the changed fields.
func TestUpdateTicketToolSetsDelivery(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "BILL", "title": "Invoice", "repos": []string{"acme/api", "acme/web"}})

	if got := callJSON(t, s, "get_ticket", map[string]any{"id": "BILL-1"}); got["delivery"] != nil {
		t.Fatalf("new ticket delivery = %v, want it left out", got["delivery"])
	}

	short := callJSON(t, s, "update_ticket", map[string]any{"id": "BILL-1", "delivery": map[string]any{
		"branch": "bill-1", "worktree": "/w/bill-1", "prUrl": "https://github.com/acme/api/pull/3",
		"landedCommits": []map[string]any{
			{"sha": fullSHA1, "repo": "acme/api"},
			{"sha": fullSHA2, "repo": "acme/web"},
			{"sha": "77aa0bc", "repo": "acme/web"},
		},
	}})
	wantChanged(t, "update_ticket", short, "delivery")

	got := callJSON(t, s, "get_ticket", map[string]any{"id": "bill-1"})
	want := map[string]any{
		"branch": "bill-1", "worktree": "/w/bill-1", "prUrl": "https://github.com/acme/api/pull/3",
		"landedCommits": []any{
			map[string]any{"sha": fullSHA1, "repo": "acme/api"},
			map[string]any{"sha": fullSHA2, "repo": "acme/web"},
			map[string]any{"sha": "77aa0bc", "repo": "acme/web"},
		},
	}
	if !reflect.DeepEqual(got["delivery"], want) {
		t.Fatalf("get_ticket delivery = %v, want %v", got["delivery"], want)
	}

	// Only the fields passed change.
	wantChanged(t, "update_ticket", callJSON(t, s, "update_ticket", map[string]any{"id": "BILL-1", "delivery": map[string]any{"prUrl": ""}}), "delivery")
	wantChanged(t, "update_ticket", callJSON(t, s, "update_ticket", map[string]any{"id": "BILL-1", "title": "Invoice v2"}), "title")
	delete(want, "prUrl")
	if got := callJSON(t, s, "get_ticket", map[string]any{"id": "BILL-1"}); !reflect.DeepEqual(got["delivery"], want) {
		t.Fatalf("get_ticket delivery = %v, want %v", got["delivery"], want)
	}

	// A commit without a repo on a two-repo ticket is refused.
	text, isError := callToolText(t, s, "update_ticket", map[string]any{"id": "BILL-1", "delivery": map[string]any{
		"landedCommits": []map[string]any{{"sha": "0123abc"}},
	}})
	if !isError || !strings.Contains(text, "has no repo") {
		t.Fatalf("update_ticket without a repo = %q (error %v), want a refusal", text, isError)
	}
}

// find_tickets_by_commit finds a ticket by a full or short sha, across two
// repos, with each ticket's url and the commits that matched.
func TestFindTicketsByCommitTool(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "BILL", "title": "API", "repos": []string{"acme/api"}})
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "BILL", "title": "Web", "repos": []string{"acme/web"}})
	callJSON(t, s, "update_ticket", map[string]any{"id": "BILL-1", "delivery": map[string]any{
		"landedCommits": []map[string]any{{"sha": fullSHA1}, {"sha": "77aa0bc"}},
	}})
	callJSON(t, s, "update_ticket", map[string]any{"id": "BILL-2", "delivery": map[string]any{
		"landedCommits": []map[string]any{{"sha": fullSHA2}, {"sha": "77aa0bc", "repo": "acme/api"}},
	}})

	find := func(args map[string]any) []models.CommitTicket {
		t.Helper()
		text, isError := callToolText(t, s, "find_tickets_by_commit", args)
		if isError {
			t.Fatalf("find_tickets_by_commit %v: %s", args, text)
		}
		var found []models.CommitTicket
		if err := json.Unmarshal([]byte(text), &found); err != nil {
			t.Fatalf("find_tickets_by_commit answered %q: %v", text, err)
		}
		return found
	}

	byShort := find(map[string]any{"sha": "6bafa19"})
	if len(byShort) != 1 || byShort[0].Key != "BILL-1" || byShort[0].Title != "API" || byShort[0].Status != "todo" ||
		byShort[0].URL != "http://board.test/?ticket=BILL-1" ||
		!reflect.DeepEqual(byShort[0].Commits, []models.LandedCommit{{SHA: fullSHA1, Repo: "acme/api"}}) {
		t.Fatalf("by short sha = %+v, want BILL-1 with its url and the full commit", byShort)
	}
	if byFull := find(map[string]any{"sha": strings.ToUpper(fullSHA2)}); len(byFull) != 1 || byFull[0].Key != "BILL-2" {
		t.Fatalf("by full sha = %+v, want BILL-2", byFull)
	}
	if both := find(map[string]any{"sha": "77aa0bc"}); len(both) != 2 || both[0].Key != "BILL-1" || both[1].Key != "BILL-2" {
		t.Fatalf("a sha on two tickets = %+v, want BILL-1 and BILL-2", both)
	}
	if one := find(map[string]any{"sha": "77aa0bc", "repo": "acme/api"}); len(one) != 2 {
		t.Fatalf("repo acme/api = %+v, want both tickets (each landed 77aa0bc in acme/api)", one)
	}

	text, isError := callToolText(t, s, "find_tickets_by_commit", map[string]any{"sha": "0000000"})
	if isError || text != "[]" {
		t.Fatalf("no match = %q (error %v), want []", text, isError)
	}
	text, isError = callToolText(t, s, "find_tickets_by_commit", map[string]any{"sha": "abc"})
	if !isError || !strings.Contains(text, "not a commit sha") {
		t.Fatalf("a bad sha = %q (error %v), want a refusal", text, isError)
	}
}

func TestDeliveryToolDefinitions(t *testing.T) {
	s := newTestServer(t)
	byName := map[string]toolDef{}
	for _, def := range s.toolDefinitions() {
		byName[def.Name] = def
	}
	delivery, ok := byName["update_ticket"].InputSchema.Properties["delivery"]
	if !ok || delivery.Type != "object" {
		t.Fatalf("update_ticket delivery = %+v, want an object", delivery)
	}
	for _, field := range []string{"branch", "worktree", "prUrl", "landedCommits"} {
		if _, ok := delivery.Properties[field]; !ok {
			t.Fatalf("update_ticket delivery has no %s", field)
		}
	}
	find, ok := byName["find_tickets_by_commit"]
	if !ok || !reflect.DeepEqual(find.InputSchema.Required, []string{"sha"}) {
		t.Fatalf("find_tickets_by_commit = %+v, want a tool that requires sha", find)
	}
	if !strings.Contains(byName["get_ticket"].Description, "delivery") {
		t.Fatal("get_ticket does not mention delivery")
	}
}
