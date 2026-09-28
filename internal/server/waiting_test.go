package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestMovingAWaitingTicketOutIsAConflict covers POST /api/tickets/{id}/move
// and PUT /api/tickets/{id} on a ticket that waits on the person: a status
// change out of needs_user_input is a 409 naming the open request and the two
// ways out, and the ticket keeps its status, its request and the rest of the
// update. Answering the request takes it out, after which it moves again.
func TestMovingAWaitingTicketOutIsAConflict(t *testing.T) {
	r := serve(t)
	agentID := identify(t, r, "waiting", "implementer")["id"].(string)
	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))
	ticketID := ticket["id"].(string)
	if _, err := r.srv.store.ClaimTicket(ticketID, agentID); err != nil {
		t.Fatal(err)
	}
	created, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/requests",
		fmt.Sprintf(`{"agentId":%q,"type":"question","prompt":"Which vendor?"}`, agentID))
	if status != http.StatusCreated {
		t.Fatalf("create request: status %d, %#v", status, created)
	}
	requestID := created["id"].(string)

	wantConflict := func(what string, body map[string]any, status int) {
		t.Helper()
		msg := fmt.Sprint(body["error"])
		if status != http.StatusConflict {
			t.Fatalf("%s: status %d, %#v; want 409", what, status, body)
		}
		for _, want := range []string{"BILL-1", requestID, "answering the request on the ticket page", "Stop work"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("%s: error %q, want it to contain %q", what, msg, want)
			}
		}
	}
	body, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/move", `{"status":"todo"}`)
	wantConflict("move to todo", body, status)
	body, status = doRequest[map[string]any](t, http.MethodPut, r.url+"/api/tickets/"+ticketID,
		`{"status":"done","title":"Renamed"}`)
	wantConflict("update to done", body, status)

	got, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/tickets/"+ticketID, "")
	open, _ := got["openRequest"].(map[string]any)
	if got["status"] != "needs_user_input" || got["title"] != "Invoice" || open == nil || open["id"] != requestID {
		t.Fatalf("ticket after the refused writes = %#v, want it still waiting on %s, title unchanged", got, requestID)
	}

	if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/requests/"+requestID+"/answer",
		`{"answer":"Stripe"}`); status != http.StatusOK {
		t.Fatalf("answer: status %d", status)
	}
	moved, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/move", `{"status":"todo"}`)
	if status != http.StatusOK || moved["status"] != "todo" {
		t.Fatalf("move after the answer: status %d, %#v; want 200 and todo", status, moved)
	}
}
