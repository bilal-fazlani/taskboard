package db

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

func mustCreateRequest(t *testing.T, s *Store, req models.CreateUserInputRequest) string {
	t.Helper()
	id, err := s.CreateRequest(req)
	if err != nil {
		t.Fatalf("creating request %q: %v", req.Prompt, err)
	}
	return id
}

// A request records the agent that asked, answers with its id, and moves
// the ticket to needs_user_input whatever its type.
func TestCreateRequestRecordsTheAskingAgentAndWaitsOnThePerson(t *testing.T) {
	for _, typ := range models.UserInputTypes {
		t.Run(typ, func(t *testing.T) {
			f := newClaimFixture(t)
			mustClaim(t, f.s, f.ticket.ID, f.first.ID)
			setLastSeen(t, f.s, f.first.ID, 10*time.Minute)

			id := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: "ACP-1", AgentID: f.first.ID,
				Type: typ, Prompt: " Land it? ", Choices: []string{"Land", " Hold "}})
			r, err := f.s.GetRequest(id)
			if err != nil || r == nil {
				t.Fatalf("reading request %s: %+v, %v", id, r, err)
			}
			if r.TicketID != f.ticket.ID || r.AgentID != f.first.ID || r.Type != typ || r.Prompt != "Land it?" ||
				!reflect.DeepEqual(r.Choices, []string{"Land", "Hold"}) || r.Answered() {
				t.Fatalf("request = %+v", r)
			}
			got, _ := f.s.GetTicket(f.ticket.ID)
			if got.Status != models.StatusNeedsUserInput {
				t.Fatalf("ticket status %s, want needs_user_input", got.Status)
			}
			wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusNeedsUserInput, "request "+id)
			if time.Since(lastSeen(t, f.s, f.first.ID)) > time.Minute {
				t.Fatal("asking did not touch the agent")
			}
		})
	}
}

// A ticket has one open request at a time; an unknown type, a blank prompt
// or choice, an unknown agent or ticket, or a ticket no agent holds, is
// refused.
func TestCreateRequestRefusesASecondOpenRequestOrAnUnknownType(t *testing.T) {
	f := newClaimFixture(t)
	free := seedTicket(t, f.s, f.project.ID, "Nobody holds it")
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	first := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputQuestion, Prompt: "Which port?"})
	for _, tc := range []struct {
		name string
		req  models.CreateUserInputRequest
		want string
	}{
		{"a second open request", models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.second.ID,
			Type: models.UserInputApproval, Prompt: "Ship?"}, "ticket ACP-1 already waits on request " + first},
		{"unknown type", models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
			Type: "needs_approval", Prompt: "Ship?"}, `type "needs_approval" is not a type of user input: use one of approval, question`},
		{"no prompt", models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
			Type: models.UserInputQuestion}, "prompt is required"},
		{"blank choice", models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
			Type: models.UserInputQuestion, Prompt: "Which?", Choices: []string{"a", " "}}, "a choice is blank"},
		{"unknown agent", models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: "ghost",
			Type: models.UserInputQuestion, Prompt: "Which?"}, `"ghost" is not an agent`},
		{"unknown ticket", models.CreateUserInputRequest{TicketID: "ACP-42", AgentID: f.first.ID,
			Type: models.UserInputQuestion, Prompt: "Which?"}, `no ticket matches "ACP-42"`},
		{"a ticket no agent holds", models.CreateUserInputRequest{TicketID: free.ID, AgentID: f.first.ID,
			Type: models.UserInputApproval, Prompt: "Ship?"}, "ticket ACP-2 is not held by any agent: a request goes on a ticket an agent is working"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.s.CreateRequest(tc.req)
			wantInvalidContaining(t, err, tc.want)
		})
	}
	var n int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM ticket_requests`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("requests = %d (%v), want the first only", n, err)
	}
	if got, _ := f.s.GetTicket(free.ID); got.Status != models.StatusTodo {
		t.Fatalf("the free ticket moved to %s", got.Status)
	}
}

// An answer stores what was answered and who answered, and moves the
// ticket back to in_progress; a request with choices takes one of them.
func TestAnswerRequestStoresTheAnswerAndWhoAnswered(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	id := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?", Choices: []string{"Land", "Hold"}})

	_, err := f.s.AnswerRequest(id, "maybe", "Bilal")
	wantInvalidContaining(t, err, `answer "maybe" is not one of the request's choices: Land, Hold`)
	_, err = f.s.AnswerRequest(id, "land", "")
	wantInvalidContaining(t, err, "answeredBy is required")
	_, err = f.s.AnswerRequest(id, " ", "Bilal")
	wantInvalidContaining(t, err, "answer is required")

	r, err := f.s.AnswerRequest(id, "land", "Bilal")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Answered() || r.Answer != "Land" || r.AnsweredBy != "Bilal" {
		t.Fatalf("answered request = %+v, want Land by Bilal", r)
	}
	got, _ := f.s.GetTicket(f.ticket.ID)
	if got.Status != models.StatusInProgress || got.OpenRequest != nil || got.Agent == nil || got.Agent.ID != f.first.ID {
		t.Fatalf("after the answer: status %s, open request %+v, agent %+v", got.Status, got.OpenRequest, got.Agent)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusNeedsUserInput, models.StatusInProgress, "Answered by Bilal")

	_, err = f.s.AnswerRequest(id, "Hold", "Bilal")
	wantInvalidContaining(t, err, "is already answered, by Bilal")
	_, err = f.s.AnswerRequest("no-such-request", "yes", "Bilal")
	wantInvalidContaining(t, err, `request not found: "no-such-request"`)

	// With the first answered, the ticket can ask again, and a free answer
	// takes any text.
	next := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputQuestion, Prompt: "Which port?"})
	if r, err := f.s.AnswerRequest(next, "3012", "Bilal"); err != nil || r.Answer != "3012" {
		t.Fatalf("free answer: %+v, %v", r, err)
	}
}

// A claim of a ticket waiting on the person keeps it waiting: its open
// request still needs an answer.
func TestClaimTicketKeepsATicketWaitingOnThePerson(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?"})
	setLastSeen(t, f.s, f.first.ID, time.Hour)
	c := mustClaim(t, f.s, f.ticket.ID, f.second.ID)
	if c.Ticket.Status != models.StatusNeedsUserInput || c.Ticket.OpenRequest == nil || c.TakenFrom == nil {
		t.Fatalf("taken over while waiting: status %s, open request %+v, taken from %+v",
			c.Ticket.Status, c.Ticket.OpenRequest, c.TakenFrom)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusNeedsUserInput, models.StatusNeedsUserInput, "Taken over by agent "+f.second.ID)
}

// AwaitAnswer returns as soon as the request is answered, even by another
// connection to the file, and waits on its own request only.
func TestAwaitAnswerSeesAnAnswerFromASecondConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "await.db")
	open := func() *Store {
		database, err := OpenAt(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { database.Close() })
		return NewStore(database)
	}
	agentSide, personSide := open(), open()
	agentSide.awaitPoll = 5 * time.Millisecond

	p := seedProject(t, agentSide, "Agent Control Plane", "ACP")
	tk := seedTicket(t, agentSide, p.ID, "Await")
	a := identify(t, agentSide, "3da2c294", "implementer")
	mustClaim(t, agentSide, tk.ID, a.ID)
	id := mustCreateRequest(t, agentSide, models.CreateUserInputRequest{TicketID: tk.ID, AgentID: a.ID,
		Type: models.UserInputApproval, Prompt: "Land it?"})

	type result struct {
		r   *models.TicketRequest
		err error
	}
	done := make(chan result, 1)
	go func() {
		r, err := agentSide.AwaitAnswer(context.Background(), id, 10*time.Second)
		done <- result{r, err}
	}()
	select {
	case got := <-done:
		t.Fatalf("AwaitAnswer returned before any answer: %+v, %v", got.r, got.err)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := personSide.AnswerRequest(id, "yes", "Bilal"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || got.r == nil || !got.r.Answered() || got.r.Answer != "yes" || got.r.AnsweredBy != "Bilal" {
			t.Fatalf("AwaitAnswer = %+v, %v; want the answer yes by Bilal", got.r, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AwaitAnswer did not see the answer written by the second connection")
	}
}

// Without an answer, AwaitAnswer gives the request back unanswered once the
// timeout passes, or stops with the context; it never returns another
// request's answer.
func TestAwaitAnswerTimesOutOnItsOwnRequest(t *testing.T) {
	f := newClaimFixture(t)
	f.s.awaitPoll = 5 * time.Millisecond
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	mine := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?"})
	other := seedTicket(t, f.s, f.project.ID, "Another ticket")
	mustClaim(t, f.s, other.ID, f.second.ID)
	theirs := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: other.ID, AgentID: f.second.ID,
		Type: models.UserInputApproval, Prompt: "Ship it?"})
	if _, err := f.s.AnswerRequest(theirs, "yes", "Bilal"); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	r, err := f.s.AwaitAnswer(context.Background(), mine, 40*time.Millisecond)
	if err != nil || r == nil || r.ID != mine || r.Answered() {
		t.Fatalf("AwaitAnswer after the timeout = %+v, %v; want %s unanswered", r, err, mine)
	}
	if waited := time.Since(start); waited < 40*time.Millisecond || waited > 2*time.Second {
		t.Fatalf("AwaitAnswer waited %v for a 40ms timeout", waited)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err = f.s.AwaitAnswer(ctx, mine, time.Minute)
	if !errors.Is(err, context.Canceled) || r == nil || r.Answered() {
		t.Fatalf("AwaitAnswer with a cancelled context = %+v, %v", r, err)
	}

	_, err = f.s.AwaitAnswer(context.Background(), "no-such-request", time.Millisecond)
	wantInvalidContaining(t, err, `request not found: "no-such-request"`)
}

// Waiting on an answer keeps the asking agent alive: its last seen moves up
// during the wait, so no claim can take its ticket over while it waits.
func TestAwaitAnswerKeepsTheWaitingAgentLive(t *testing.T) {
	f := newClaimFixture(t)
	f.s.awaitPoll = 5 * time.Millisecond
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	id := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?"})
	setLastSeen(t, f.s, f.first.ID, 29*time.Minute)

	type result struct {
		r   *models.TicketRequest
		err error
	}
	done := make(chan result, 1)
	go func() {
		r, err := f.s.AwaitAnswer(context.Background(), id, 5*time.Second)
		done <- result{r, err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Since(lastSeen(t, f.s, f.first.ID)) > time.Minute {
		if time.Now().After(deadline) {
			t.Fatal("the waiting agent's last seen did not move up during the wait")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Moved back again mid-wait, the next poll brings it up again.
	setLastSeen(t, f.s, f.first.ID, 29*time.Minute)
	for time.Since(lastSeen(t, f.s, f.first.ID)) > time.Minute {
		if time.Now().After(deadline) {
			t.Fatal("the waiting agent's last seen did not move up on a later poll")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := f.s.ClaimTicket(f.ticket.ID, f.second.ID); err == nil {
		t.Fatal("another session took over the ticket of an agent waiting on its answer")
	}

	if _, err := f.s.AnswerRequest(id, "yes", "Bilal"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || !got.r.Answered() {
			t.Fatalf("AwaitAnswer = %+v, %v", got.r, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AwaitAnswer did not return after the answer")
	}
}

// Liveness is the session's: the ticket a quiet implementer holds stays live
// while its orchestrator, in the same session, waits in AwaitAnswer on the
// person, so another session's claim is refused and reads show it live.
func TestAWaitingOrchestratorKeepsItsSessionsTicketLive(t *testing.T) {
	f := newClaimFixture(t)
	f.s.awaitPoll = 5 * time.Millisecond
	orchestrator := identify(t, f.s, "3da2c294", "orchestrator")
	if orchestrator.SessionID != f.first.SessionID {
		t.Fatal("the orchestrator is not in the implementer's session")
	}
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	id := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: orchestrator.ID,
		Type: models.UserInputApproval, Prompt: "Land it?"})

	// The implementer has gone quiet well past the threshold; the session is
	// judged by its most recently seen agent.
	setLastSeen(t, f.s, f.first.ID, 2*time.Hour)
	setLastSeen(t, f.s, orchestrator.ID, 31*time.Minute)
	if got, _ := f.s.GetTicket(f.ticket.ID); !got.Agent.Stale {
		t.Fatal("with every agent of the session unseen past the threshold, the holder is not stale")
	}
	setLastSeen(t, f.s, orchestrator.ID, 10*time.Minute)
	got, _ := f.s.GetTicket(f.ticket.ID)
	if got.Agent.Stale || got.Agent.ID != f.first.ID || time.Since(got.Agent.SessionLastSeenAt) > 11*time.Minute {
		t.Fatalf("with the orchestrator seen 10m ago the holder reads %+v, want live, its session seen 10m ago", got.Agent)
	}

	// The orchestrator waits on the person, past what would have been the
	// threshold for it too.
	setLastSeen(t, f.s, orchestrator.ID, 29*time.Minute)
	done := make(chan error, 1)
	go func() {
		_, err := f.s.AwaitAnswer(context.Background(), id, 5*time.Second)
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Since(lastSeen(t, f.s, orchestrator.ID)) > time.Minute {
		if time.Now().After(deadline) {
			t.Fatal("the waiting orchestrator's last seen did not move up")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, err := f.s.ClaimTicket(f.ticket.ID, f.second.ID)
	var held *ErrTicketHeld
	if !errors.As(err, &held) || held.Holder.ID != f.first.ID {
		t.Fatalf("another session's claim while the orchestrator waits: %v, want ErrTicketHeld naming %s", err, f.first.ID)
	}
	wantInvalidContaining(t, err, "whose session was last seen")
	if got, _ := f.s.GetTicket(f.ticket.ID); got.Agent == nil || got.Agent.ID != f.first.ID || got.Agent.Stale {
		t.Fatalf("while the orchestrator waits the ticket reads agent %+v, want the live implementer", got.Agent)
	}

	if _, err := f.s.AnswerRequest(id, "yes", "Bilal"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AwaitAnswer did not return after the answer")
	}
}
