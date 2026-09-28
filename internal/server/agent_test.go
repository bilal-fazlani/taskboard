package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// identify calls POST /api/agents with vendor, session and agent fields that
// all differ by suffix, so several calls in one test never collide on the
// session's (vendor, vendorSessionId) uniqueness.
func identify(t *testing.T, r *running, suffix, role string) map[string]any {
	t.Helper()
	agent, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/agents", fmt.Sprintf(
		`{"vendor":"claude_code","vendorSessionId":"session-%s","machine":"mac","resumeCommand":"claude --resume %s",
		"role":%q,"model":"opus","provider":"anthropic"}`, suffix, suffix, role))
	if status != http.StatusCreated {
		t.Fatalf("identify: status %d, %#v", status, agent)
	}
	return agent
}

// TestIdentifyAgent covers POST /api/agents: a valid identify returns the new
// agent, not stale, in a fresh session; missing fields are refused by name.
func TestIdentifyAgent(t *testing.T) {
	r := serve(t)
	agent := identify(t, r, "1", "implementer")
	if agent["role"] != "implementer" || agent["model"] != "opus" || agent["provider"] != "anthropic" || agent["stale"] != false {
		t.Fatalf("identify = %#v", agent)
	}
	if agent["sessionId"] == nil || agent["sessionId"] == "" {
		t.Fatalf("identify has no sessionId: %#v", agent)
	}

	for _, c := range []struct {
		name, body string
	}{
		{"no vendor", `{"vendorSessionId":"s","role":"implementer","model":"opus","provider":"anthropic"}`},
		{"no session id", `{"vendor":"claude_code","role":"implementer","model":"opus","provider":"anthropic"}`},
		{"no role", `{"vendor":"claude_code","vendorSessionId":"s2","model":"opus","provider":"anthropic"}`},
		{"no model", `{"vendor":"claude_code","vendorSessionId":"s2","role":"implementer","provider":"anthropic"}`},
		{"bad provider", `{"vendor":"claude_code","vendorSessionId":"s2","role":"implementer","model":"opus","provider":"acme"}`},
		{"not json", `not json`},
	} {
		if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/agents", c.body); status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", c.name, status)
		}
	}
}

// TestListAndGetAgents covers GET /api/agents and GET /api/agents/{id}: each
// agent carries its session and the tickets it currently holds, and an
// unknown id is a 404.
func TestListAndGetAgents(t *testing.T) {
	r := serve(t)
	agent := identify(t, r, "2", "orchestrator")
	agentID := agent["id"].(string)

	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))

	// Nothing creates a claim over HTTP (ACP-202's one-call start does), so
	// the store is called directly, same package, to hold the ticket.
	if _, err := r.srv.store.ClaimTicket(ticket["id"].(string), agentID); err != nil {
		t.Fatal(err)
	}

	items, status := doRequest[[]map[string]any](t, http.MethodGet, r.url+"/api/agents", "")
	if status != http.StatusOK {
		t.Fatalf("list: status %d, %#v", status, items)
	}
	var found map[string]any
	for _, item := range items {
		if item["id"] == agentID {
			found = item
		}
	}
	if found == nil {
		t.Fatalf("agent %s missing from list %#v", agentID, items)
	}
	session, ok := found["session"].(map[string]any)
	if !ok || session["vendor"] != "claude_code" {
		t.Fatalf("list session = %#v", found["session"])
	}
	held, ok := found["heldTickets"].([]any)
	if !ok || len(held) != 1 || held[0].(map[string]any)["key"] != "BILL-1" {
		t.Fatalf("list heldTickets = %#v", found["heldTickets"])
	}

	got, status := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/agents/"+agentID, "")
	if status != http.StatusOK || got["id"] != agentID {
		t.Fatalf("get: status %d, %#v", status, got)
	}
	if _, status := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/agents/nope", ""); status != http.StatusNotFound {
		t.Fatalf("get unknown: status %d, want 404", status)
	}
}

// TestReleaseTicket covers POST /api/tickets/{id}/release: giving back needs
// a hand-off and returns the ticket to todo; finishing needs proof and moves
// it to done; a bare release, or one missing the ticket, is refused.
func TestReleaseTicket(t *testing.T) {
	r := serve(t)
	agent := identify(t, r, "3", "implementer")
	agentID := agent["id"].(string)

	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))
	ticketID := ticket["id"].(string)

	if _, err := r.srv.store.ClaimTicket(ticketID, agentID); err != nil {
		t.Fatal(err)
	}

	// A release from an agent outside the holder's session is a 409, not a
	// 400: it is a live conflict over the ticket, not bad input.
	other := identify(t, r, "3-other", "implementer")
	if body, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/release",
		fmt.Sprintf(`{"agentId":%q,"outcome":"give_back","handOff":"stole it"}`, other["id"])); status != http.StatusConflict {
		t.Fatalf("release from another session: status %d, %#v, want 409", status, body)
	}

	// A bare release, and one that mixes up hand-off and proof, are refused.
	for _, c := range []struct {
		name, body string
	}{
		{"no outcome", fmt.Sprintf(`{"agentId":%q}`, agentID)},
		{"give back with no hand-off", fmt.Sprintf(`{"agentId":%q,"outcome":"give_back"}`, agentID)},
		{"give back with proof instead", fmt.Sprintf(`{"agentId":%q,"outcome":"give_back","proof":"done"}`, agentID)},
		{"finish with no proof", fmt.Sprintf(`{"agentId":%q,"outcome":"finish"}`, agentID)},
	} {
		body, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/release", c.body)
		if status != http.StatusBadRequest {
			t.Errorf("%s: status %d, %#v", c.name, status, body)
		}
	}
	if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/nope/release",
		fmt.Sprintf(`{"agentId":%q,"outcome":"give_back","handOff":"stopped here"}`, agentID)); status != http.StatusNotFound {
		t.Fatalf("release unknown ticket: status %d, want 404", status)
	}

	back, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/release",
		fmt.Sprintf(`{"agentId":%q,"outcome":"give_back","handOff":"stopped after the migration; next: the store"}`, agentID))
	if status != http.StatusOK || back["status"] != "todo" || back["agent"] != nil {
		t.Fatalf("give back: status %d, %#v", status, back)
	}

	if _, err := r.srv.store.ClaimTicket(ticketID, agentID); err != nil {
		t.Fatal(err)
	}
	done, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/release",
		fmt.Sprintf(`{"agentId":%q,"outcome":"finish","proof":"go test ./... passes"}`, agentID))
	if status != http.StatusOK || done["status"] != "done" {
		t.Fatalf("finish: status %d, %#v", status, done)
	}
}

// TestStartTicket covers POST /api/tickets/{id}/start: it claims the ticket
// and answers the store's start; another session's live agent is a 409
// naming the holder and its last seen, and once that agent is stale the
// ticket is taken over and the answer says from whom.
func TestStartTicket(t *testing.T) {
	r := serve(t)
	first := identify(t, r, "7", "implementer")["id"].(string)
	second := identify(t, r, "8", "implementer")["id"].(string)
	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))
	ticketID := ticket["id"].(string)
	start := func(ref, body string) (map[string]any, int) {
		return doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ref+"/start", body)
	}

	got, status := start("bill-1", fmt.Sprintf(`{"agentId":%q}`, first))
	if status != http.StatusOK || len(got) != 2 || got["ticket"] == nil || got["project"] == nil {
		t.Fatalf("start: status %d, %#v; want only ticket and project", status, got)
	}
	if tk := got["ticket"].(map[string]any); tk["id"] != ticketID || tk["status"] != "in_progress" || tk["agent"] != nil {
		t.Fatalf("start's ticket = %#v, want BILL-1 in progress with no agent", tk)
	}
	if held, _ := r.srv.store.GetTicket(ticketID); held.Agent == nil || held.Agent.ID != first {
		t.Fatalf("after the start the ticket is held by %+v, want %s", held.Agent, first)
	}

	got, status = start(ticketID, fmt.Sprintf(`{"agentId":%q}`, second))
	if msg, _ := got["error"].(string); status != http.StatusConflict || !strings.Contains(msg, "held by agent "+first) ||
		!strings.Contains(msg, "last seen") {
		t.Fatalf("start of a live agent's ticket: status %d, %#v; want 409 naming the holder and its last seen", status, got)
	}

	execOnServed(t, r, `UPDATE agents SET last_seen_at = ? WHERE id = ?`, storedTime(time.Now().Add(-time.Hour)), first)
	got, status = start(ticketID, fmt.Sprintf(`{"agentId":%q}`, second))
	if from, _ := got["takenFrom"].(map[string]any); status != http.StatusOK || from == nil || from["id"] != first {
		t.Fatalf("start of a stale agent's ticket: status %d, %#v; want takenFrom %s", status, got, first)
	}

	for _, c := range []struct {
		name, ref, body string
		want            int
	}{
		{"unknown ticket", "BILL-99", fmt.Sprintf(`{"agentId":%q}`, second), http.StatusNotFound},
		{"no agent", ticketID, `{}`, http.StatusBadRequest},
		{"unknown agent", ticketID, `{"agentId":"ghost"}`, http.StatusBadRequest},
		{"not json", ticketID, `not json`, http.StatusBadRequest},
	} {
		if body, status := start(c.ref, c.body); status != c.want {
			t.Errorf("%s: status %d, %#v; want %d", c.name, status, body, c.want)
		}
	}
}

// TestRequestLifecycle covers the request routes: creating a request moves
// the ticket to needs_user_input, its history lists it, answering moves the
// ticket back to in_progress, and awaiting an already-answered request
// returns at once.
func TestRequestLifecycle(t *testing.T) {
	r := serve(t)
	agent := identify(t, r, "4", "implementer")
	agentID := agent["id"].(string)

	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))
	ticketID := ticket["id"].(string)

	// A request needs a held ticket.
	if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/requests",
		fmt.Sprintf(`{"agentId":%q,"type":"approval","prompt":"Ship it?"}`, agentID)); status != http.StatusBadRequest {
		t.Fatalf("request on an unheld ticket: status %d, want 400", status)
	}
	if _, err := r.srv.store.ClaimTicket(ticketID, agentID); err != nil {
		t.Fatal(err)
	}

	created, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/requests",
		fmt.Sprintf(`{"agentId":%q,"type":"approval","prompt":"Ship it?","choices":["yes","no"]}`, agentID))
	if status != http.StatusCreated || created["id"] == nil || created["type"] != "approval" {
		t.Fatalf("create request: status %d, %#v", status, created)
	}
	requestID := created["id"].(string)

	waiting, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/tickets/"+ticketID, "")
	if waiting["status"] != "needs_user_input" {
		t.Fatalf("ticket after request: status %#v", waiting["status"])
	}
	open, ok := waiting["openRequest"].(map[string]any)
	if !ok || open["id"] != requestID {
		t.Fatalf("ticket openRequest = %#v", waiting["openRequest"])
	}

	// A second open request is refused.
	if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/requests",
		fmt.Sprintf(`{"agentId":%q,"type":"question","prompt":"Again?"}`, agentID)); status != http.StatusBadRequest {
		t.Fatalf("second open request: status %d, want 400", status)
	}

	history, status := doRequest[[]map[string]any](t, http.MethodGet, r.url+"/api/tickets/"+ticketID+"/requests", "")
	if status != http.StatusOK || len(history) != 1 || history[0]["id"] != requestID {
		t.Fatalf("history: status %d, %#v", status, history)
	}

	// An approval's answer is always approved or declined, whatever it is
	// answered with and whatever choices the request offers: free text and
	// one of the request's own choices are both refused.
	for _, bad := range []string{"maybe", "yes"} {
		if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/requests/"+requestID+"/answer",
			fmt.Sprintf(`{"answer":%q,"answeredBy":"Bilal"}`, bad)); status != http.StatusBadRequest {
			t.Fatalf("answer %q: status %d, want 400", bad, status)
		}
	}
	if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/requests/nope/answer",
		`{"answer":"approved","answeredBy":"Bilal"}`); status != http.StatusNotFound {
		t.Fatalf("answer unknown request: status %d, want 404", status)
	}

	answered, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/requests/"+requestID+"/answer",
		`{"answer":"approved","answeredBy":"Bilal","note":"Ship it once CI is green."}`)
	if status != http.StatusOK || answered["answer"] != "approved" || answered["answeredBy"] != "Bilal" ||
		answered["note"] != "Ship it once CI is green." {
		t.Fatalf("answer: status %d, %#v", status, answered)
	}

	backToWork, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/tickets/"+ticketID, "")
	if backToWork["status"] != "in_progress" || backToWork["openRequest"] != nil {
		t.Fatalf("ticket after answer: %#v", backToWork["status"])
	}

	// Awaiting an already-answered request returns at once, answered, note
	// included.
	awaited, status := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/requests/"+requestID+"/await?timeout=5s", "")
	if status != http.StatusOK || awaited["answer"] != "approved" || awaited["note"] != "Ship it once CI is green." {
		t.Fatalf("await answered: status %d, %#v", status, awaited)
	}
	if _, status := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/requests/nope/await", ""); status != http.StatusNotFound {
		t.Fatalf("await unknown request: status %d, want 404", status)
	}
	if _, status := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/requests/"+requestID+"/await?timeout=nonsense", ""); status != http.StatusBadRequest {
		t.Fatalf("await bad timeout: status %d, want 400", status)
	}

	// A second, unanswered request times out server-side rather than erroring.
	if _, err := r.srv.store.ClaimTicket(ticketID, agentID); err != nil {
		t.Fatal(err)
	}
	unanswered, err := r.srv.store.CreateRequest(models.CreateUserInputRequest{TicketID: ticketID, AgentID: agentID, Type: "question", Prompt: "Still there?"})
	if err != nil {
		t.Fatal(err)
	}
	timedOut, status := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/requests/"+unanswered+"/await?timeout=30ms", "")
	if status != http.StatusOK || timedOut["answer"] != nil {
		t.Fatalf("await timeout: status %d, %#v", status, timedOut)
	}

	// answeredBy left out of the body defaults to the local person
	// (localPerson, entries.go), since an answer is today always the local
	// person's.
	want := localPerson()
	if want == "" {
		t.Fatal("localPerson() is empty in this environment; the test cannot tell it apart from a missing value")
	}
	byDefault, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/requests/"+unanswered+"/answer", `{"answer":"still here"}`)
	if status != http.StatusOK || byDefault["answeredBy"] != want {
		t.Fatalf("answer with no answeredBy: status %d, answeredBy %#v, want %q", status, byDefault["answeredBy"], want)
	}
}

// TestTicketHeldMapsTo409 covers the shared error mapping: a ticket held by
// another live session (db.ErrTicketHeld) is a 409, not the 400 every other
// ErrInvalidInput gets. No route in this ticket calls ClaimTicket a second
// time (that is ACP-202's one-call start), so this exercises writeStoreError
// directly with the store's own error.
func TestTicketHeldMapsTo409(t *testing.T) {
	r := serve(t)
	holder := identify(t, r, "5", "implementer")
	other := identify(t, r, "6", "implementer")

	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))

	if _, err := r.srv.store.ClaimTicket(ticket["id"].(string), holder["id"].(string)); err != nil {
		t.Fatal(err)
	}
	_, err := r.srv.store.ClaimTicket(ticket["id"].(string), other["id"].(string))
	if err == nil {
		t.Fatal("claim by another live session should be refused")
	}

	w := httptest.NewRecorder()
	writeStoreError(w, err)
	if w.Code != http.StatusConflict {
		t.Fatalf("writeStoreError(ErrTicketHeld) = %d, want %d", w.Code, http.StatusConflict)
	}
}

// TestShutdownEndsPendingAwait covers Review 1's major finding: a pending
// GET /api/requests/{id}/await must not hold up server shutdown.
// http.Server.Shutdown never cancels a request's own context, so an await
// with a long timeout would otherwise keep the handler running past the
// shutdown grace period; the fix watches s.events.closed() too, the same
// signal /api/events streams already end on.
func TestShutdownEndsPendingAwait(t *testing.T) {
	r := serve(t)
	agent := identify(t, r, "7", "implementer")
	agentID := agent["id"].(string)

	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))
	ticketID := ticket["id"].(string)
	if _, err := r.srv.store.ClaimTicket(ticketID, agentID); err != nil {
		t.Fatal(err)
	}
	created, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/requests",
		fmt.Sprintf(`{"agentId":%q,"type":"approval","prompt":"Ship it?"}`, agentID))
	if status != http.StatusCreated {
		t.Fatalf("create request: status %d, %#v", status, created)
	}
	requestID := created["id"].(string)

	// A signal that the request has really reached awaitAnswerRequest and is
	// about to block on the store, rather than a guess based on a sleep.
	entered := make(chan struct{})
	awaitAnswerRequestEntered = func() { close(entered) }
	t.Cleanup(func() { awaitAnswerRequestEntered = nil })

	type awaitResponse struct {
		status int
		body   string
		err    error
	}
	awaitDone := make(chan awaitResponse, 1)
	go func() {
		resp, err := http.Get(r.url + "/api/requests/" + requestID + "/await?timeout=5m")
		if err != nil {
			awaitDone <- awaitResponse{err: err}
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		awaitDone <- awaitResponse{status: resp.StatusCode, body: string(body)}
	}()
	select {
	case <-entered:
	case <-time.After(waitFor):
		t.Fatal("the await request never reached the handler")
	}

	start := time.Now()
	r.stop()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
		// Keep t.Cleanup's receive from blocking.
		r.done <- nil
	case <-time.After(waitFor):
		t.Fatal("Serve did not return while an await was pending")
	}
	if d := time.Since(start); d > shutdownTimeout/2 {
		t.Fatalf("shutdown took %v; the pending await held it up", d)
	}

	select {
	case got := <-awaitDone:
		if got.err != nil {
			t.Fatalf("await request failed: %v", got.err)
		}
		if got.status != http.StatusServiceUnavailable || !strings.Contains(strings.ToLower(got.body), "retry") {
			t.Fatalf("await response on shutdown = status %d, body %q, want 503 telling the caller to retry",
				got.status, got.body)
		}
	case <-time.After(waitFor):
		t.Fatal("the pending await never returned")
	}
}

// TestAwaitAnswerRequestGoneMidWaitIs404 covers Review 3's minor finding: a
// pending await must not be treated as a shutdown when its own request
// vanishes for an unrelated reason. Deleting the ticket mid-wait cascades
// away its request (ticket_requests.ticket_id ON DELETE CASCADE), so
// AwaitAnswer's own error (its context never ends) must answer 404, not the
// shutdown 503.
func TestAwaitAnswerRequestGoneMidWaitIs404(t *testing.T) {
	r := serve(t)
	agent := identify(t, r, "9", "implementer")
	agentID := agent["id"].(string)

	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	ticket, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets",
		fmt.Sprintf(`{"projectId":%q,"title":"Invoice"}`, project["id"]))
	ticketID := ticket["id"].(string)
	if _, err := r.srv.store.ClaimTicket(ticketID, agentID); err != nil {
		t.Fatal(err)
	}
	created, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticketID+"/requests",
		fmt.Sprintf(`{"agentId":%q,"type":"approval","prompt":"Ship it?"}`, agentID))
	if status != http.StatusCreated {
		t.Fatalf("create request: status %d, %#v", status, created)
	}
	requestID := created["id"].(string)

	entered := make(chan struct{})
	awaitAnswerRequestEntered = func() { close(entered) }
	t.Cleanup(func() { awaitAnswerRequestEntered = nil })

	type awaitResponse struct {
		status int
		body   string
		err    error
	}
	awaitDone := make(chan awaitResponse, 1)
	go func() {
		resp, err := http.Get(r.url + "/api/requests/" + requestID + "/await?timeout=5s")
		if err != nil {
			awaitDone <- awaitResponse{err: err}
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		awaitDone <- awaitResponse{status: resp.StatusCode, body: string(body)}
	}()
	select {
	case <-entered:
	case <-time.After(waitFor):
		t.Fatal("the await request never reached the handler")
	}

	if _, status := doRequest[map[string]any](t, http.MethodDelete, r.url+"/api/tickets/"+ticketID, ""); status != http.StatusNoContent {
		t.Fatalf("delete ticket mid-wait: status %d, want 204", status)
	}

	select {
	case got := <-awaitDone:
		if got.err != nil {
			t.Fatalf("await request failed: %v", got.err)
		}
		if got.status != http.StatusNotFound {
			t.Fatalf("await response after the ticket vanished = status %d, body %q, want 404, not the shutdown 503",
				got.status, got.body)
		}
	case <-time.After(waitFor):
		t.Fatal("the pending await never returned after its ticket was deleted")
	}
}
