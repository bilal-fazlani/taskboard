package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestStopWork covers POST /api/tickets/{id}/stop, the person's route: it
// frees the ticket at once, back in todo with no agent, recorded as stopped
// by the local person. The stopped agent's next write is then a 409 saying
// the work was stopped by the person, but its hand-off is still accepted. A
// ticket no agent holds is a 400, an unknown one a 404.
func TestStopWork(t *testing.T) {
	r := serve(t)
	agent := identify(t, r, "stop", "implementer")
	agentID := agent["id"].(string)
	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))
	ticketID := ticket["id"].(string)

	if body, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/BILL-1/stop", ""); status != http.StatusBadRequest {
		t.Fatalf("stop an unheld ticket: status %d, %#v, want 400", status, body)
	}
	if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/nope/stop", ""); status != http.StatusNotFound {
		t.Fatalf("stop an unknown ticket: status %d, want 404", status)
	}

	if _, err := r.srv.store.ClaimTicket(ticketID, agentID); err != nil {
		t.Fatal(err)
	}
	stopped, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/BILL-1/stop", "")
	if status != http.StatusOK || stopped["status"] != "todo" || stopped["agent"] != nil {
		t.Fatalf("stop: status %d, ticket %#v; want 200, todo and no agent", status, stopped)
	}
	history, _ := doRequest[[]map[string]any](t, http.MethodGet, r.url+"/api/tickets/"+ticketID+"/history", "")
	if len(history) == 0 || !strings.Contains(history[0]["note"].(string), "Stopped by the person, "+localPerson()+".") ||
		!strings.Contains(history[0]["note"].(string), agentID) {
		t.Fatalf("history after the stop = %#v, want a note naming the person and the agent", history)
	}

	body, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/entries",
		fmt.Sprintf(`{"type":"learning","text":"kept going","agentId":%q}`, agentID))
	if status != http.StatusConflict || !strings.Contains(fmt.Sprint(body["error"]), "stopped by the person") {
		t.Fatalf("the stopped agent's entry: status %d, %#v; want 409 stopped by the person", status, body)
	}
	body, status = doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/release",
		fmt.Sprintf(`{"agentId":%q,"outcome":"give_back","handOff":"Totals done; tax is next"}`, agentID))
	if status != http.StatusOK || body["status"] != "todo" {
		t.Fatalf("the stopped agent's hand-off: status %d, %#v; want 200 and todo", status, body)
	}
}
