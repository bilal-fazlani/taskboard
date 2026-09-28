package mcp

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// agentServer is an MCP server over a throwaway database holding a project
// (ACP) with one ticket (ACP-1). path opens a second connection to the same
// database, the way the web UI's server answers a request.
type agentServer struct {
	s      *MCPServer
	db     *sql.DB
	path   string
	ticket *models.Ticket
}

func newAgentServer(t *testing.T) agentServer {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	f := agentServer{s: NewServer(db.NewStore(database)), db: database, path: path}
	p, err := f.s.store.CreateProject(models.CreateProjectRequest{Name: "Agent Control Plane", Prefix: "ACP"})
	if err != nil {
		t.Fatal(err)
	}
	if f.ticket, err = f.s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Agent tools"}); err != nil {
		t.Fatal(err)
	}
	return f
}

// identify runs identify_agent for a session and role, and answers with the
// agent's id.
func (f agentServer) identify(t *testing.T, session, role string) string {
	t.Helper()
	got := callJSON(t, f.s, "identify_agent", map[string]any{
		"vendor": "claude_code", "vendorSessionId": session, "resumeCommand": "claude --resume " + session,
		"role": role, "model": "opus", "provider": "anthropic",
	})
	wantKeys(t, "identify_agent", got, "agentId")
	return got["agentId"].(string)
}

// claim gives the ticket to the agent, as the one-call start will.
func (f agentServer) claim(t *testing.T, agentID string) {
	t.Helper()
	if _, err := f.s.store.ClaimTicket(f.ticket.ID, agentID); err != nil {
		t.Fatalf("claiming ACP-1 for %s: %v", agentID, err)
	}
}

// secondStore is another connection to the same database.
func (f agentServer) secondStore(t *testing.T) *db.Store {
	t.Helper()
	database, err := db.OpenAt(f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return db.NewStore(database)
}

// ageAgent sets the agent's last seen an hour back, so a touch shows.
func (f agentServer) ageAgent(t *testing.T, agentID string) time.Time {
	t.Helper()
	back := time.Now().UTC().Add(-time.Hour)
	if _, err := f.db.Exec(`UPDATE agents SET last_seen_at = ? WHERE id = ?`, back.Format("2006-01-02T15:04:05.000000000Z"), agentID); err != nil {
		t.Fatal(err)
	}
	a, err := f.s.store.GetAgent(agentID)
	if err != nil || a == nil {
		t.Fatalf("reading agent %s: %+v, %v", agentID, a, err)
	}
	if !a.LastSeenAt.Before(time.Now().Add(-50 * time.Minute)) {
		t.Fatalf("agent %s last seen %s after ageing it", agentID, a.LastSeenAt)
	}
	return a.LastSeenAt
}

func (f agentServer) lastSeen(t *testing.T, agentID string) time.Time {
	t.Helper()
	a, err := f.s.store.GetAgent(agentID)
	if err != nil || a == nil {
		t.Fatalf("reading agent %s: %+v, %v", agentID, a, err)
	}
	return a.LastSeenAt
}

// identify_agent creates the session with its link and the agent in it,
// and answers with only the agent's id. A subagent passing its parent's
// session ID is a new agent in the same session. Without a machine, the
// server's host is the session's.
func TestIdentifyAgentNamesTheSessionAndTheAgent(t *testing.T) {
	f := newAgentServer(t)
	got := callJSON(t, f.s, "identify_agent", map[string]any{
		"vendor": "claude_code", "vendorSessionId": "3da2c294", "machine": "studio",
		"resumeCommand": "claude --resume 3da2c294", "webUrl": "https://claude.ai/code/3da2c294",
		"role": "orchestrator", "model": "opus", "provider": "anthropic",
	})
	wantKeys(t, "identify_agent", got, "agentId")
	parent, err := f.s.store.GetAgent(got["agentId"].(string))
	if err != nil || parent == nil || parent.Role != "orchestrator" || parent.Model != "opus" || parent.Provider != "anthropic" {
		t.Fatalf("identified agent = %+v, %v", parent, err)
	}
	var vendor, vendorSession, machine, resume, web string
	if err := f.db.QueryRow(`SELECT vendor, vendor_session_id, machine, resume_command, web_url FROM sessions WHERE id = ?`,
		parent.SessionID).Scan(&vendor, &vendorSession, &machine, &resume, &web); err != nil {
		t.Fatal(err)
	}
	if vendor != "claude_code" || vendorSession != "3da2c294" || machine != "studio" ||
		resume != "claude --resume 3da2c294" || web != "https://claude.ai/code/3da2c294" {
		t.Fatalf("session = %s %s %s %q %s", vendor, vendorSession, machine, resume, web)
	}

	sub := f.identify(t, "3da2c294", "implementer")
	child, err := f.s.store.GetAgent(sub)
	if err != nil || child == nil || child.ID == parent.ID || child.SessionID != parent.SessionID || child.Role != "implementer" {
		t.Fatalf("subagent = %+v, %v; want a new agent in session %s", child, err, parent.SessionID)
	}

	other, _ := f.s.store.GetAgent(f.identify(t, "b10d8a02", "reviewer"))
	host, _ := os.Hostname()
	if err := f.db.QueryRow(`SELECT machine FROM sessions WHERE id = ?`, other.SessionID).Scan(&machine); err != nil {
		t.Fatal(err)
	}
	if machine != host {
		t.Errorf("session without a machine has %q, want the server's host %q", machine, host)
	}

	for _, missing := range []string{"vendor", "vendorSessionId", "role", "model", "provider"} {
		args := map[string]any{"vendor": "codex", "vendorSessionId": "x", "role": "implementer", "model": "gpt-5", "provider": "openai"}
		delete(args, missing)
		if text := callError(t, f.s, "identify_agent", args); !strings.Contains(text, missing) {
			t.Errorf("identify_agent without %s: %s; want it named", missing, text)
		}
	}
}

// Every agent tool takes the calling agent's id: without one, or with one
// that is no agent, it is refused, and each call touches its agent. Two
// agents of one session, sharing the connection, each pass their own.
func TestAgentToolsTakeAndTouchTheCallingAgent(t *testing.T) {
	f := newAgentServer(t)
	parent := f.identify(t, "3da2c294", "orchestrator")
	sub := f.identify(t, "3da2c294", "implementer")
	f.claim(t, sub)

	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"start_ticket", map[string]any{"ticket": "ACP-1"}},
		{"request_user_input", map[string]any{"ticket": "ACP-1", "type": "question", "prompt": "Which port?"}},
		{"await_answer", map[string]any{"request": "nope", "timeoutSeconds": 0}},
		{"release_ticket", map[string]any{"ticket": "ACP-1", "outcome": "give_back", "handOff": "Stopped."}},
	} {
		if text := callError(t, f.s, c.tool, c.args); !strings.Contains(text, "agentId is required") {
			t.Errorf("%s without agentId: %s", c.tool, text)
		}
		c.args["agentId"] = "ghost"
		if text := callError(t, f.s, c.tool, c.args); !strings.Contains(text, "identify first") {
			t.Errorf("%s with an unknown agentId: %s", c.tool, text)
		}
	}

	// The orchestrator asks on its implementer's ticket; the request is its.
	before := f.ageAgent(t, parent)
	id := callJSON(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": parent,
		"type": "approval", "prompt": "Merge acp-12 into main?"})["id"].(string)
	if !f.lastSeen(t, parent).After(before) {
		t.Error("request_user_input did not touch its agent")
	}
	if r, _ := f.s.store.GetRequest(id); r == nil || r.AgentID != parent {
		t.Fatalf("request = %+v, want it asked by the orchestrator %s", r, parent)
	}

	// The implementer, of the same session, collects it, and is touched.
	before = f.ageAgent(t, sub)
	got := callJSON(t, f.s, "await_answer", map[string]any{"request": id, "agentId": sub, "timeoutSeconds": 0})
	if got["answered"] != false || !f.lastSeen(t, sub).After(before) {
		t.Errorf("await_answer by the subagent = %v, last seen %s", got, f.lastSeen(t, sub))
	}
	if _, err := f.s.store.AnswerRequest(id, "yes", "Bilal"); err != nil {
		t.Fatal(err)
	}

	before = f.ageAgent(t, sub)
	callJSON(t, f.s, "release_ticket", map[string]any{"ticket": "ACP-1", "agentId": sub, "outcome": "finish", "proof": "go test ./... passed."})
	if !f.lastSeen(t, sub).After(before) {
		t.Error("release_ticket did not touch its agent")
	}
}

// release_ticket finishes with proof, or gives back with a hand-off, and
// answers with a short confirmation; without its record, or with the wrong
// one, it is refused, naming what is missing, and changes nothing.
func TestReleaseTicketNeedsItsRecord(t *testing.T) {
	f := newAgentServer(t)
	agent := f.identify(t, "3da2c294", "implementer")
	f.claim(t, agent)

	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"outcome": "give_back"}, "hand-off: where the work stopped and the next step"},
		{map[string]any{"outcome": "finish"}, "proof: what was verified, how, and the result"},
		{map[string]any{"outcome": "finish", "handOff": "Stopped."}, "proof"},
		{map[string]any{}, "give_back"},
	} {
		c.args["ticket"], c.args["agentId"] = "ACP-1", agent
		if text := callError(t, f.s, "release_ticket", c.args); !strings.Contains(text, c.want) {
			t.Errorf("release_ticket(%v): %s; want it to say %q", c.args, text, c.want)
		}
	}
	if text := callError(t, f.s, "release_ticket", map[string]any{"agentId": agent, "outcome": "finish", "proof": "x"}); !strings.Contains(text, "ticket is required") {
		t.Errorf("release_ticket without a ticket: %s", text)
	}
	if tk, _ := f.s.store.GetTicket(f.ticket.ID); tk.Status != models.StatusInProgress || tk.Agent == nil {
		t.Fatalf("after refused releases the ticket is %s held by %+v", tk.Status, tk.Agent)
	}

	got := callJSON(t, f.s, "release_ticket", map[string]any{"ticket": "acp-1", "agentId": agent, "outcome": "give_back",
		"handOff": "Tools written; next, the descriptions."})
	wantKeys(t, "release_ticket", got, "key", "status", "released")
	if got["key"] != "ACP-1" || got["status"] != "todo" || got["released"] != true {
		t.Fatalf("give_back answered %v", got)
	}

	f.claim(t, agent)
	got = callJSON(t, f.s, "release_ticket", map[string]any{"ticket": "ACP-1", "agentId": agent, "outcome": "finish",
		"proof": "go test ./... passed."})
	if got["status"] != "done" {
		t.Fatalf("finish answered %v", got)
	}
	if tk, _ := f.s.store.GetTicket(f.ticket.ID); tk.Status != models.StatusDone || tk.Agent != nil {
		t.Fatalf("finished ticket is %s held by %+v", tk.Status, tk.Agent)
	}
}

// request_user_input answers with the request's id at once and the ticket
// waits on the person; await_answer on that id returns unanswered when its
// timeout passes, and the answer once the person gives it, from another
// connection, while it waits. The asking agent stays live all the while.
func TestRequestUserInputThenAwaitTheAnswer(t *testing.T) {
	f := newAgentServer(t)
	agent := f.identify(t, "3da2c294", "implementer")
	f.claim(t, agent)

	got := callJSON(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "question",
		"prompt": "Which port for the dev server?", "choices": []string{"3011", "3014"}})
	wantKeys(t, "request_user_input", got, "id", "created")
	id := got["id"].(string)
	if tk, _ := f.s.store.GetTicket(f.ticket.ID); tk.Status != models.StatusNeedsUserInput || tk.OpenRequest == nil || tk.OpenRequest.ID != id {
		t.Fatalf("after the request the ticket is %s with open request %+v", tk.Status, tk.OpenRequest)
	}
	if text := callError(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "question", "prompt": "Again?"}); !strings.Contains(text, "one open request") {
		t.Errorf("a second open request: %s", text)
	}
	if text := callError(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "vote", "prompt": "?"}); !strings.Contains(text, "approval, question") {
		t.Errorf("an unknown type: %s", text)
	}

	start := time.Now()
	got = callJSON(t, f.s, "await_answer", map[string]any{"request": id, "agentId": agent, "timeoutSeconds": 1})
	wantKeys(t, "await_answer", got, "id", "answered")
	if got["id"] != id || got["answered"] != false || time.Since(start) < time.Second {
		t.Fatalf("await_answer timing out = %v after %s", got, time.Since(start))
	}

	before := f.ageAgent(t, agent)
	web := f.secondStore(t)
	go func() {
		time.Sleep(300 * time.Millisecond)
		web.AnswerRequest(id, "3014", "Bilal")
	}()
	got = callJSON(t, f.s, "await_answer", map[string]any{"request": id, "agentId": agent, "timeoutSeconds": 20})
	if got["answered"] != true || got["answer"] != "3014" || got["answeredBy"] != "Bilal" {
		t.Fatalf("await_answer after the answer = %v", got)
	}
	if !f.lastSeen(t, agent).After(before) {
		t.Error("waiting did not keep the asking agent live")
	}
	if tk, _ := f.s.store.GetTicket(f.ticket.ID); tk.Status != models.StatusInProgress || tk.OpenRequest != nil {
		t.Fatalf("after the answer the ticket is %s with open request %+v", tk.Status, tk.OpenRequest)
	}
}

// await_answer waits only on its own session's requests, and refuses a
// negative timeout and a request that does not exist.
func TestAwaitAnswerIsForTheAskingSession(t *testing.T) {
	f := newAgentServer(t)
	agent := f.identify(t, "3da2c294", "implementer")
	stranger := f.identify(t, "b10d8a02", "implementer")
	f.claim(t, agent)
	id := callJSON(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "approval",
		"prompt": "Merge?"})["id"].(string)

	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"request": id, "agentId": stranger, "timeoutSeconds": 0}, "another session"},
		{map[string]any{"request": id, "agentId": agent, "timeoutSeconds": -1}, "timeoutSeconds is -1"},
		{map[string]any{"request": "nope", "agentId": agent, "timeoutSeconds": 0}, "request not found"},
		{map[string]any{"agentId": agent}, "request is required"},
	} {
		if text := callError(t, f.s, "await_answer", c.args); !strings.Contains(text, c.want) {
			t.Errorf("await_answer(%v): %s; want %q", c.args, text, c.want)
		}
	}
}

// The wait is capped, and left out it is the default.
func TestAwaitTimeoutIsCapped(t *testing.T) {
	for _, c := range []struct {
		seconds *int
		want    time.Duration
	}{
		{nil, awaitDefaultTimeout},
		{ptr(0), 0},
		{ptr(30), 30 * time.Second},
		{ptr(100000), awaitMaxTimeout},
	} {
		got, err := awaitTimeout(c.seconds)
		if err != nil || got != c.want {
			t.Errorf("awaitTimeout(%v) = %s, %v; want %s", c.seconds, got, err, c.want)
		}
	}
	if awaitDefaultTimeout >= 60*time.Second {
		t.Errorf("the default wait %s is not under a 60s client timeout", awaitDefaultTimeout)
	}
}

func ptr(n int) *int { return &n }

// A wait on the person does not hold up the other calls on the connection,
// which subagents share, and ends when the connection closes.
func TestServeAnswersOtherCallsWhileOneWaits(t *testing.T) {
	f := newAgentServer(t)
	agent := f.identify(t, "3da2c294", "implementer")
	f.claim(t, agent)
	id := callJSON(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "question",
		"prompt": "Which port?"})["id"].(string)

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- f.s.serve(inR, outW)
		outW.Close()
	}()
	lines := make(chan map[string]any)
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			var resp map[string]any
			json.Unmarshal(sc.Bytes(), &resp)
			lines <- resp
		}
		close(lines)
	}()
	send := func(id int, tool string, args map[string]any) {
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": args}})
		if _, err := inW.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	next := func() map[string]any {
		select {
		case resp := <-lines:
			return resp
		case <-time.After(10 * time.Second):
			t.Fatal("no answer within 10s")
		}
		return nil
	}

	send(1, "await_answer", map[string]any{"request": id, "agentId": agent, "timeoutSeconds": 30})
	send(2, "list_projects", nil)
	if resp := next(); resp["id"] != float64(2) {
		t.Fatalf("first answer = %v, want list_projects' while the wait runs", resp)
	}
	if _, err := f.secondStore(t).AnswerRequest(id, "3014", "Bilal"); err != nil {
		t.Fatal(err)
	}
	resp := next()
	if resp["id"] != float64(1) || !strings.Contains(mustString(t, resp), `\"answered\":true`) {
		t.Fatalf("second answer = %v, want the answered wait", resp)
	}

	// A wait still running when the connection closes ends with it.
	f.claim(t, agent)
	second := callJSON(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "question",
		"prompt": "And the host?"})["id"].(string)
	send(3, "await_answer", map[string]any{"request": second, "agentId": agent, "timeoutSeconds": 600})
	time.Sleep(100 * time.Millisecond)
	inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve still running 5s after its input closed")
	}
}

// A client that gives up on a wait cancels it with notifications/cancelled:
// the wait stops, so the answer that comes later is never sent, and neither
// that notification nor any other gets a response.
func TestServeStopsACancelledWaitAndAnswersNoNotification(t *testing.T) {
	f := newAgentServer(t)
	agent := f.identify(t, "3da2c294", "implementer")
	f.claim(t, agent)
	id := callJSON(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "question",
		"prompt": "Which port?"})["id"].(string)

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- f.s.serve(inR, outW)
		outW.Close()
	}()
	lines := make(chan map[string]any, 10)
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			var resp map[string]any
			json.Unmarshal(sc.Bytes(), &resp)
			lines <- resp
		}
		close(lines)
	}()
	send := func(msg map[string]any) {
		data, _ := json.Marshal(msg)
		if _, err := inW.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}

	send(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{"name": "await_answer", "arguments": map[string]any{"request": id, "agentId": agent, "timeoutSeconds": 600}}})
	// A cancellation naming another request, one with the id as a string,
	// and one with none change nothing.
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 99}})
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": "7"}})
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{}})
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 7, "reason": "timed out"}})
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/unknown"})

	// The answer comes after the cancellation: a wait still running would
	// send it within a poll or two.
	time.Sleep(200 * time.Millisecond)
	if _, err := f.secondStore(t).AnswerRequest(id, "3014", "Bilal"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	send(map[string]any{"jsonrpc": "2.0", "id": 8, "method": "tools/call", "params": map[string]any{"name": "list_projects"}})
	select {
	case resp := <-lines:
		if resp["id"] != float64(8) {
			t.Fatalf("first line written = %v, want list_projects' answer and nothing for the cancelled wait or the notifications", resp)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no answer to list_projects within 10s")
	}

	inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve still running 5s after its input closed")
	}
	for resp := range lines {
		t.Errorf("written after list_projects' answer: %v", resp)
	}
}

// A number id and a string id never name the same request.
func TestRequestKeyTellsNumbersFromStrings(t *testing.T) {
	if requestKey(float64(7)) == requestKey("7") {
		t.Fatal("7 and \"7\" share a key")
	}
	if requestKey(float64(7)) != requestKey(float64(7)) {
		t.Fatal("the same id has two keys")
	}
}

func mustString(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A ticket waiting on the person shows everywhere an agent reads: filtered
// by list_tickets, with its agent and open request on get_ticket, in the
// board's needs_user_input column and in get_now's waiting group. No write
// moves a ticket there.
func TestWaitingTicketShowsInEveryRead(t *testing.T) {
	f := newAgentServer(t)
	agent := f.identify(t, "3da2c294", "implementer")
	f.claim(t, agent)
	id := callJSON(t, f.s, "request_user_input", map[string]any{"ticket": "ACP-1", "agentId": agent, "type": "approval",
		"prompt": "Merge acp-12?"})["id"].(string)

	list, err := f.s.callTool("list_tickets", mustJSON(t, map[string]any{"status": "needs_user_input"}))
	if err != nil {
		t.Fatal(err)
	}
	if text := mustString(t, list); !strings.Contains(text, `[{"id":"`+f.ticket.ID) || !strings.Contains(text, `"openRequest":{"id":"`+id) ||
		!strings.Contains(text, `"agent":{"id":"`+agent) {
		t.Errorf("list_tickets status needs_user_input = %s", text)
	}
	got := callJSON(t, f.s, "get_ticket", map[string]any{"id": "ACP-1"})
	agentField, _ := got["agent"].(map[string]any)
	request, _ := got["openRequest"].(map[string]any)
	if got["status"] != "needs_user_input" || agentField["id"] != agent || request["id"] != id || request["type"] != "approval" {
		t.Errorf("get_ticket = status %v, agent %v, openRequest %v", got["status"], agentField, request)
	}

	board, err := f.s.callTool("get_board", mustJSON(t, map[string]any{"project": "ACP"}))
	if err != nil {
		t.Fatal(err)
	}
	var column []models.Ticket
	for _, c := range board.(*models.Board).Columns {
		if c.Status == models.StatusNeedsUserInput {
			column = c.Tickets
		}
	}
	if len(column) != 1 || column[0].ID != f.ticket.ID {
		t.Errorf("the board's needs_user_input column = %+v", column)
	}

	now, err := f.s.callTool("get_now", nil)
	if err != nil {
		t.Fatal(err)
	}
	n := now.(nowAnswer)
	if len(n.Waiting) != 1 || n.Waiting[0].Key != "ACP-1" || len(n.InProgress) != 0 {
		t.Errorf("get_now = %+v, want ACP-1 waiting", n)
	}

	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"move_ticket", map[string]any{"id": "ACP-1", "status": "needs_user_input"}},
		{"update_ticket", map[string]any{"id": "ACP-1", "status": "needs_user_input"}},
		{"create_ticket", map[string]any{"project": "ACP", "title": "x", "status": "needs_user_input"}},
	} {
		if text := callError(t, f.s, c.tool, c.args); !strings.Contains(text, "request user input") {
			t.Errorf("%s to needs_user_input: %s", c.tool, text)
		}
	}
	for _, def := range f.s.toolDefinitions() {
		if def.Name == "move_ticket" || def.Name == "update_ticket" || def.Name == "create_ticket" {
			if contains(def.InputSchema.Properties["status"].Enum, models.StatusNeedsUserInput) {
				t.Errorf("%s offers needs_user_input as a status to set", def.Name)
			}
		}
	}
}

// The descriptions are the only documentation most agents read: each says
// when to use the tool and what it answers.
func TestAgentToolDescriptionsSayWhenToUseThem(t *testing.T) {
	descriptions := map[string]string{}
	for _, def := range newTestServer(t).toolDefinitions() {
		descriptions[def.Name] = def.Description
	}
	for tool, want := range map[string][]string{
		"identify_agent":     {"Call once when you start", "{agentId}", "parent's vendor and vendorSessionId"},
		"release_ticket":     {"finish with proof", "give_back with handOff", "{key, status, released: true}"},
		"request_user_input": {"await_answer", "{id, created: true}", "act only on that approval", "own words"},
		"await_answer":       {"request you made", "answered: false", "call again", "keeps your session live"},
		"get_now":            {"waiting on the person"},
		"start_ticket": {"How you begin work on a ticket", "claims it for you", "Refused while an agent of another session",
			"last seen", "takes the ticket over", "takenFrom", "handOff", "never block you"},
	} {
		for _, w := range want {
			if !strings.Contains(descriptions[tool], w) {
				t.Errorf("%s's description does not say %q:\n%s", tool, w, descriptions[tool])
			}
		}
	}
}

// start_ticket claims the ticket and answers the store's start, the ticket
// with its link; another session's live agent is refused, naming the
// holder, and once it is stale the ticket is taken over and the answer says
// from whom.
func TestStartTicketToolBeginsWork(t *testing.T) {
	f := newAgentServer(t)
	first := f.identify(t, "3da2c294", "implementer")
	second := f.identify(t, "b10d8a02", "implementer")

	if text := callError(t, f.s, "start_ticket", map[string]any{"agentId": first}); !strings.Contains(text, "ticket is required") {
		t.Errorf("start_ticket without a ticket: %s", text)
	}

	got := callJSON(t, f.s, "start_ticket", map[string]any{"ticket": "acp-1", "agentId": first})
	wantKeys(t, "start_ticket", got, "ticket", "project")
	ticket := got["ticket"].(map[string]any)
	if ticket["id"] != f.ticket.ID || ticket["status"] != models.StatusInProgress || ticket["url"] == nil || ticket["agent"] != nil {
		t.Fatalf("start_ticket's ticket = %v, want ACP-1 in progress with its url and no agent", ticket)
	}
	if held, _ := f.s.store.GetTicket(f.ticket.ID); held.Agent == nil || held.Agent.ID != first {
		t.Fatalf("after start_ticket the ticket is held by %+v, want %s", held.Agent, first)
	}

	text := callError(t, f.s, "start_ticket", map[string]any{"ticket": "ACP-1", "agentId": second})
	if !strings.Contains(text, "held by agent "+first) || !strings.Contains(text, "last seen") {
		t.Errorf("start_ticket on a live agent's ticket: %s; want the holder and its last seen", text)
	}

	f.ageAgent(t, first)
	got = callJSON(t, f.s, "start_ticket", map[string]any{"ticket": "ACP-1", "agentId": second})
	if from, _ := got["takenFrom"].(map[string]any); from == nil || from["id"] != first {
		t.Fatalf("start_ticket on a stale agent's ticket: takenFrom %v, want %s", got["takenFrom"], first)
	}
}
