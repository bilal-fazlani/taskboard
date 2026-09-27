package server

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/db"
)

// seedEntryAgent inserts an agent in a session into the served database:
// nothing creates agents over HTTP yet.
func seedEntryAgent(t *testing.T, r *running) string {
	t.Helper()
	seedAgentInSession(t, r, "a1", "s1", "3da2c294", "implementer")
	return "a1"
}

// storedTime is a time as the store writes it (db's sortable format), which
// the store's agent reads parse; SQLite's CURRENT_TIMESTAMP is not.
func storedTime(at time.Time) string {
	return at.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

// seedAgentInSession inserts a session and an agent in it, seen now.
func seedAgentInSession(t *testing.T, r *running, agentID, sessionID, vendorSessionID, role string) {
	t.Helper()
	now := storedTime(time.Now())
	execOnServed(t, r,
		`INSERT INTO sessions (id, vendor, vendor_session_id, machine, resume_command, created_at)
			VALUES (?, 'claude_code', ?, 'mac', 'claude --resume ' || ?, ?)`, sessionID, vendorSessionID, vendorSessionID, now)
	execOnServed(t, r,
		`INSERT INTO agents (id, session_id, role, model, provider, created_at, last_seen_at)
			VALUES (?, ?, ?, 'opus', 'anthropic', ?, ?)`, agentID, sessionID, role, now, now)
}

// execOnServed runs one statement on the served database.
func execOnServed(t *testing.T, r *running, query string, args ...any) {
	t.Helper()
	database, err := db.OpenAt(r.path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func entryTexts(page map[string]any) []string {
	var texts []string
	for _, e := range page["entries"].([]any) {
		texts = append(texts, e.(map[string]any)["text"].(string))
	}
	return texts
}

// Entries are written and read on a project (by id or prefix), an epic and a
// ticket (by id or key); a replaced entry drops out of reads unless asked for;
// a note is marked handled; and a ticket's read carries its entries and the
// open notes on it, its epic and its project.
func TestEntryEndpoints(t *testing.T) {
	r := serve(t)
	agent := seedEntryAgent(t, r)
	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	epic, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/epics", fmt.Sprintf(`{"projectId":%q,"name":"Launch"}`, project["id"]))
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice","epic":%q}`, project["id"], epic["id"]))

	owners := map[string]string{
		"project": r.url + "/api/projects/bill/entries",
		"epic":    r.url + "/api/epics/" + epic["id"].(string) + "/entries",
		"ticket":  r.url + "/api/tickets/BILL-1/entries",
	}
	for kind, url := range owners {
		learning, status := doRequest[map[string]any](t, http.MethodPost, url,
			fmt.Sprintf(`{"type":"learning","text":"%s learning.","agentId":%q}`, kind, agent))
		if status != http.StatusCreated || learning["agentId"] != agent {
			t.Fatalf("%s learning: status %d, %#v", kind, status, learning)
		}
		note, status := doRequest[map[string]any](t, http.MethodPost, url,
			fmt.Sprintf(`{"type":"note","text":"%s note.","authorName":"Bilal"}`, kind))
		if status != http.StatusCreated || note["authorName"] != "Bilal" {
			t.Fatalf("%s note: status %d, %#v", kind, status, note)
		}
		page, status := doRequest[map[string]any](t, http.MethodGet, url, "")
		if status != http.StatusOK || fmt.Sprint(entryTexts(page)) != fmt.Sprintf("[%s note. %s learning.]", kind, kind) {
			t.Fatalf("%s entries: status %d, %#v", kind, status, page)
		}
		page, _ = doRequest[map[string]any](t, http.MethodGet, url+"?type=learning&limit=1", "")
		if fmt.Sprint(entryTexts(page)) != fmt.Sprintf("[%s learning.]", kind) || page["hasMore"] != false {
			t.Fatalf("%s learnings: %#v", kind, page)
		}
	}

	// Replacing.
	old, _ := doRequest[map[string]any](t, http.MethodPost, owners["ticket"],
		fmt.Sprintf(`{"type":"decision","source":"agent","text":"Columns.","agentId":%q}`, agent))
	current, status := doRequest[map[string]any](t, http.MethodPost, owners["ticket"],
		fmt.Sprintf(`{"type":"decision","source":"agent","text":"JSON.","agentId":%q,"replaces":%q}`, agent, old["id"]))
	if status != http.StatusCreated || current["replaces"] != old["id"] {
		t.Fatalf("replacing: status %d, %#v", status, current)
	}
	page, _ := doRequest[map[string]any](t, http.MethodGet, owners["ticket"]+"?type=decision", "")
	if fmt.Sprint(entryTexts(page)) != "[JSON.]" {
		t.Fatalf("current decisions = %v", entryTexts(page))
	}
	page, _ = doRequest[map[string]any](t, http.MethodGet, owners["ticket"]+"?type=decision&includeReplaced=true", "")
	if fmt.Sprint(entryTexts(page)) != "[JSON. Columns.]" {
		t.Fatalf("all decisions = %v", entryTexts(page))
	}

	// The ticket's read.
	got, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/tickets/"+ticket["id"].(string), "")
	entries := got["entries"].(map[string]any)
	if entries["total"] != float64(3) || got["openNotes"] != float64(1) || got["epicOpenNotes"] != float64(1) ||
		got["projectOpenNotes"] != float64(1) || got["title"] != "Invoice" {
		t.Fatalf("ticket read = %#v", got)
	}

	// Marking the ticket's note handled.
	notes, _ := doRequest[map[string]any](t, http.MethodGet, owners["ticket"]+"?type=note", "")
	noteID := notes["entries"].([]any)[0].(map[string]any)["id"].(string)
	handled, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/entries/"+noteID+"/handled",
		fmt.Sprintf(`{"agentId":%q}`, agent))
	if status != http.StatusOK || handled["handledBy"] != agent || handled["handledAt"] == nil {
		t.Fatalf("handling: status %d, %#v", status, handled)
	}
	got, _ = doRequest[map[string]any](t, http.MethodGet, r.url+"/api/tickets/BILL-1", "")
	if _, ok := got["openNotes"]; ok {
		t.Fatalf("ticket read after handling carries openNotes: %#v", got["openNotes"])
	}

	// Mistakes.
	for _, c := range []struct {
		method, url, body string
		want              int
	}{
		{http.MethodGet, r.url + "/api/projects/NOPE/entries", "", http.StatusNotFound},
		{http.MethodGet, r.url + "/api/epics/nope/entries", "", http.StatusNotFound},
		{http.MethodPost, r.url + "/api/tickets/BILL-9/entries", `{"type":"learning","text":"t","authorName":"Bilal"}`, http.StatusNotFound},
		{http.MethodPost, owners["epic"], `{"type":"proof","text":"t","agentId":"a1"}`, http.StatusBadRequest},
		{http.MethodPost, owners["ticket"], `{"type":"learning","text":"t"}`, http.StatusBadRequest},
		{http.MethodPost, owners["ticket"], `not json`, http.StatusBadRequest},
		{http.MethodGet, owners["ticket"] + "?type=gossip", "", http.StatusBadRequest},
		{http.MethodGet, owners["ticket"] + "?limit=many", "", http.StatusBadRequest},
		{http.MethodGet, owners["ticket"] + "?includeReplaced=maybe", "", http.StatusBadRequest},
		{http.MethodPost, r.url + "/api/entries/" + noteID + "/handled", `{"agentId":"a1"}`, http.StatusBadRequest},
		{http.MethodPost, r.url + "/api/entries/nope/handled", `{"agentId":"a1"}`, http.StatusNotFound},
	} {
		if body, status := doRequest[map[string]any](t, c.method, c.url, c.body); status != c.want {
			t.Errorf("%s %s %s: status %d (%v), want %d", c.method, c.url, c.body, status, body, c.want)
		}
	}
}
