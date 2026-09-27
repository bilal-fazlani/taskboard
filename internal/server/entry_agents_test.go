package server

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// A read of entries carries the agents its entries name, each once with its
// role, model, provider and session, for the web's author line; a page naming
// no agent leaves agents out. A note sent with no author is the server's
// user's, which is how the web leaves one; any other type still needs one.
func TestEntryReadsCarryTheirAgentsAndNotesDefaultToThePerson(t *testing.T) {
	r := serve(t)
	agent := seedEntryAgent(t, r)
	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets", fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))
	url := r.url + "/api/tickets/BILL-1/entries"

	note, status := doRequest[map[string]any](t, http.MethodPost, url, `{"type":"note","text":"Aim for 100 ms."}`)
	if status != http.StatusCreated || note["authorName"] == "" || note["authorName"] == nil || note["agentId"] != nil {
		t.Fatalf("note with no author: status %d, %#v", status, note)
	}
	if _, status := doRequest[map[string]any](t, http.MethodPost, url, `{"type":"learning","text":"t"}`); status != http.StatusBadRequest {
		t.Fatalf("learning with no author: status %d, want 400", status)
	}
	page, _ := doRequest[map[string]any](t, http.MethodGet, url, "")
	if _, ok := page["agents"]; ok {
		t.Fatalf("a page naming no agent carries agents: %#v", page["agents"])
	}

	for _, body := range []string{
		fmt.Sprintf(`{"type":"learning","text":"Commit before waiting.","agentId":%q}`, agent),
		fmt.Sprintf(`{"type":"decision","source":"person","text":"Poll.","agentId":%q}`, agent),
	} {
		if _, status := doRequest[map[string]any](t, http.MethodPost, url, body); status != http.StatusCreated {
			t.Fatalf("%s: status %d", body, status)
		}
	}
	page, _ = doRequest[map[string]any](t, http.MethodGet, url, "")
	agents, ok := page["agents"].(map[string]any)
	if !ok || len(agents) != 1 {
		t.Fatalf("agents = %#v, want the one agent", page["agents"])
	}
	a := agents[agent].(map[string]any)
	session, _ := a["session"].(map[string]any)
	if a["role"] != "implementer" || a["model"] != "opus" || a["provider"] != "anthropic" ||
		session["resumeCommand"] != "claude --resume 3da2c294" || session["machine"] != "mac" {
		t.Fatalf("agent = %#v", a)
	}
}

// The agents map reads each agent the way every other read does: an agent
// whose session was last seen long ago is stale, with that last seen, and
// one seen just now is not.
func TestEntryReadsReportAStaleAgentAsStale(t *testing.T) {
	r := serve(t)
	fresh := seedEntryAgent(t, r)
	seedAgentInSession(t, r, "a2", "s2", "0ld5e55", "reviewer")
	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets", fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))
	url := r.url + "/api/tickets/BILL-1/entries"
	for _, agent := range []string{fresh, "a2"} {
		body := fmt.Sprintf(`{"type":"learning","text":"From %s.","agentId":%q}`, agent, agent)
		if _, status := doRequest[map[string]any](t, http.MethodPost, url, body); status != http.StatusCreated {
			t.Fatalf("%s: status %d", body, status)
		}
	}
	// Writing touched a2; send its session back 30 days, past any threshold.
	longAgo := time.Now().Add(-30 * 24 * time.Hour)
	execOnServed(t, r, `UPDATE agents SET last_seen_at = ? WHERE id = 'a2'`, storedTime(longAgo))

	page, status := doRequest[map[string]any](t, http.MethodGet, url, "")
	agents, ok := page["agents"].(map[string]any)
	if status != http.StatusOK || !ok || len(agents) != 2 {
		t.Fatalf("status %d, agents = %#v", status, page["agents"])
	}
	stale := agents["a2"].(map[string]any)
	seen, err := time.Parse(time.RFC3339Nano, fmt.Sprint(stale["sessionLastSeenAt"]))
	if stale["stale"] != true || err != nil || !seen.Equal(longAgo.UTC().Truncate(time.Nanosecond)) {
		t.Errorf("a2 = stale %v, sessionLastSeenAt %v (%v); want stale, last seen %v",
			stale["stale"], stale["sessionLastSeenAt"], err, longAgo.UTC())
	}
	live := agents[fresh].(map[string]any)
	if live["stale"] != false || live["sessionLastSeenAt"] == "0001-01-01T00:00:00Z" {
		t.Errorf("%s = stale %v, sessionLastSeenAt %v; want not stale, seen now", fresh, live["stale"], live["sessionLastSeenAt"])
	}
}
