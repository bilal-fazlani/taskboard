package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

func mustStop(t *testing.T, s *Store, ticketID string) *models.Ticket {
	t.Helper()
	got, err := s.StopWork(ticketID, "bilal")
	if err != nil {
		t.Fatalf("stopping work on %s: %v", ticketID, err)
	}
	return got
}

// wantStopped fails unless err is an ErrStopped that says the work was
// stopped by the person.
func wantStopped(t *testing.T, what string, err error) {
	t.Helper()
	var stopped *ErrStopped
	if !errors.As(err, &stopped) {
		t.Fatalf("%s: err = %v, want an ErrStopped", what, err)
	}
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("%s: an ErrStopped must also be an ErrInvalidInput", what)
	}
	if !strings.Contains(err.Error(), "stopped by the person") {
		t.Fatalf("%s: err = %q, want it to say the work was stopped by the person", what, err)
	}
}

// Stopping a live agent's work frees the ticket at once: the agent is
// cleared, the ticket is back in todo, and its status history names the
// person who stopped it and the agent that held it.
func TestStopWorkFreesATicketALiveAgentHolds(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)

	got := mustStop(t, f.s, "ACP-1")
	if got.Status != models.StatusTodo || got.Agent != nil {
		t.Fatalf("stopped: status %s, agent %+v; want todo and no agent", got.Status, got.Agent)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusTodo, "Stopped by the person, bilal.")
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusTodo,
		"Agent "+f.first.ID+" (implementer, claude-opus-5-5) held it.")
}

// A stale agent's work stops the same way: the person never waits on the
// agent, live or not.
func TestStopWorkFreesATicketAStaleAgentHolds(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	setLastSeen(t, f.s, f.first.ID, 3*time.Hour)
	if a, err := f.s.GetAgent(f.first.ID); err != nil || !a.Stale {
		t.Fatalf("agent should be stale: %+v, %v", a, err)
	}

	got := mustStop(t, f.s, f.ticket.ID)
	if got.Status != models.StatusTodo || got.Agent != nil {
		t.Fatalf("stopped: status %s, agent %+v; want todo and no agent", got.Status, got.Agent)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusTodo, "Agent "+f.first.ID)
}

// Stopping a ticket that waits on the person closes its open request, so
// the ticket no longer waits and the agent waiting on it learns on its next
// poll.
func TestStopWorkClosesTheOpenRequest(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	reqID := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?", Choices: []string{"approve", "decline"}})

	got := mustStop(t, f.s, f.ticket.ID)
	if got.Status != models.StatusTodo || got.OpenRequest != nil {
		t.Fatalf("stopped: status %s, open request %+v; want todo and none", got.Status, got.OpenRequest)
	}
	r, err := f.s.GetRequest(reqID)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Answered() || r.Answer != StoppedAnswer || r.AnsweredBy != "bilal" {
		t.Fatalf("request after the stop = %+v, want it closed with %q by bilal", r, StoppedAnswer)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusNeedsUserInput, models.StatusTodo, "request "+reqID+" (approval) was closed unanswered")
}

// There is nothing to stop on a ticket no agent holds, and a stop names who
// stopped it. Neither writes anything.
func TestStopWorkRefusesAnUnheldTicketOrNoPerson(t *testing.T) {
	f := newClaimFixture(t)
	_, err := f.s.StopWork(f.ticket.ID, "bilal")
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "not held by any agent") {
		t.Fatalf("stopping an unheld ticket: err = %v", err)
	}
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	before := historyLen(t, f.s, f.ticket.ID)
	if _, err := f.s.StopWork(f.ticket.ID, "  "); !errors.As(err, &invalid) {
		t.Fatalf("stopping with no person: err = %v", err)
	}
	if _, err := f.s.StopWork("ACP-99", "bilal"); !errors.As(err, &invalid) {
		t.Fatalf("stopping an unknown ticket: err = %v", err)
	}
	if got := historyLen(t, f.s, f.ticket.ID); got != before {
		t.Fatalf("a refused stop wrote history: %d rows, want %d", got, before)
	}
	if tk, _ := f.s.GetTicket(f.ticket.ID); tk.Agent == nil || tk.Agent.ID != f.first.ID {
		t.Fatalf("a refused stop changed the holder: %+v", tk.Agent)
	}
}

// After the stop, the stopped agent's writes on the ticket are refused with
// "stopped by the person": entries other than its hand-off, requests,
// finishing, handling a note. So are a resumed chat's, a new agent in the
// same session. Its writes elsewhere, and other sessions' on the ticket, are
// not.
func TestAStoppedAgentsWritesOnTheTicketAreRefused(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	note, err := f.s.CreateEntry(models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: f.ticket.ID},
		Type: models.EntryNote, Text: "Use the store's clock", AuthorName: "bilal"})
	if err != nil {
		t.Fatal(err)
	}
	mustStop(t, f.s, f.ticket.ID)
	resumed := identify(t, f.s, "3da2c294", "implementer")

	for _, agent := range []*models.Agent{f.first, resumed} {
		_, err := f.s.CreateEntry(models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: f.ticket.ID},
			Type: models.EntryDecision, Text: "Chose a table", Source: models.DecisionSourceAgent, AgentID: agent.ID})
		wantStopped(t, "a decision", err)
		_, err = f.s.CreateRequest(models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: agent.ID,
			Type: models.UserInputQuestion, Prompt: "Which table?"})
		wantStopped(t, "a request", err)
		_, err = f.s.ReleaseTicket(f.ticket.ID, models.ReleaseTicketRequest{AgentID: agent.ID, Outcome: models.ReleaseFinish,
			Proof: "go test passes"})
		wantStopped(t, "finishing", err)
		_, err = f.s.MarkNoteHandled(note.ID, agent.ID)
		wantStopped(t, "handling a note", err)
	}
	if n := len(ticketEntries(t, f.s, f.ticket.ID, models.EntryDecision)); n != 0 {
		t.Fatalf("a refused decision was written: %d decisions", n)
	}
	if tk, _ := f.s.GetTicket(f.ticket.ID); tk.Status != models.StatusTodo {
		t.Fatalf("a refused write moved the ticket to %s", tk.Status)
	}

	// Elsewhere, and for another session, nothing is refused.
	if _, err := f.s.CreateEntry(models.CreateEntryRequest{EntryOwner: models.EntryOwner{ProjectID: f.project.ID},
		Type: models.EntryLearning, Text: "Stops are per session", AgentID: f.first.ID}); err != nil {
		t.Fatalf("the stopped agent's entry on the project: %v", err)
	}
	if _, err := f.s.CreateEntry(models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: f.ticket.ID},
		Type: models.EntryLearning, Text: "Reviewed the stop", AgentID: f.second.ID}); err != nil {
		t.Fatalf("another session's entry on the ticket: %v", err)
	}
}

// The stopped agent's hand-off is still accepted, through a release or as an
// entry of its own, so where the work stood is not lost. It changes nothing
// else: the ticket stays where the stop left it, even once another session
// holds it.
func TestAStoppedAgentsHandOffIsStillAccepted(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	mustStop(t, f.s, f.ticket.ID)
	before := historyLen(t, f.s, f.ticket.ID)

	got, err := f.s.ReleaseTicket(f.ticket.ID, models.ReleaseTicketRequest{AgentID: f.first.ID, Outcome: models.ReleaseGiveBack,
		HandOff: "Store done; the HTTP route is next"})
	if err != nil {
		t.Fatalf("the stopped agent's hand-off: %v", err)
	}
	if got.Status != models.StatusTodo || got.Agent != nil {
		t.Fatalf("after the hand-off: status %s, agent %+v; want todo and no agent", got.Status, got.Agent)
	}
	if n := historyLen(t, f.s, f.ticket.ID); n != before {
		t.Fatalf("the hand-off wrote status history: %d rows, want %d", n, before)
	}

	// Another session takes the ticket up; the stopped agent's hand-off, as
	// an entry of its own, is still accepted and leaves the new holder be.
	mustClaim(t, f.s, f.ticket.ID, f.second.ID)
	if _, err := f.s.CreateEntry(models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: f.ticket.ID},
		Type: models.EntryHandOff, Text: "Also: the CLI command is untested", AgentID: f.first.ID}); err != nil {
		t.Fatalf("the stopped agent's hand-off entry: %v", err)
	}
	handOffs := ticketEntries(t, f.s, f.ticket.ID, models.EntryHandOff)
	if len(handOffs) != 2 || handOffs[0].AgentID != f.first.ID || handOffs[1].AgentID != f.first.ID {
		t.Fatalf("hand-offs = %+v, want both of the stopped agent's", handOffs)
	}
	if tk, _ := f.s.GetTicket(f.ticket.ID); tk.Agent == nil || tk.Agent.ID != f.second.ID || tk.Status != models.StatusInProgress {
		t.Fatalf("the hand-off disturbed the new holder: status %s, agent %+v", tk.Status, tk.Agent)
	}
}

// Claiming the ticket again is how the stopped session takes it up: from
// then on its writes are accepted again.
func TestClaimingAgainLiftsTheStop(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	mustStop(t, f.s, f.ticket.ID)
	resumed := identify(t, f.s, "3da2c294", "implementer")
	mustClaim(t, f.s, f.ticket.ID, resumed.ID)

	for _, agent := range []*models.Agent{f.first, resumed} {
		if _, err := f.s.CreateEntry(models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: f.ticket.ID},
			Type: models.EntryDecision, Text: "Picked up again", Source: models.DecisionSourceAgent, AgentID: agent.ID}); err != nil {
			t.Fatalf("a write after claiming again: %v", err)
		}
	}
}

// Starting the ticket again from the stopped session, here a resumed chat,
// takes it back and lifts the stop: the start says who stopped the work and
// when, and the session's writes on the ticket are accepted again. A later
// start with no stop to lift says nothing about one.
func TestStartingAgainLiftsTheStopAndSaysSo(t *testing.T) {
	f := newClaimFixture(t)
	mustStart(t, f.s, f.ticket.ID, f.first.ID)
	mustStop(t, f.s, f.ticket.ID)
	resumed := identify(t, f.s, "3da2c294", "implementer")

	start := mustStart(t, f.s, f.ticket.ID, resumed.ID)
	if start.Stopped == nil || start.Stopped.By != "bilal" || time.Since(start.Stopped.At) > time.Minute {
		t.Fatalf("start after the stop: stopped = %+v, want by bilal just now", start.Stopped)
	}
	if start.Ticket.Status != models.StatusInProgress {
		t.Fatalf("start after the stop: status %s, want in_progress", start.Ticket.Status)
	}
	for _, agent := range []*models.Agent{f.first, resumed} {
		if _, err := f.s.CreateEntry(models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: f.ticket.ID},
			Type: models.EntryDecision, Text: "Picked up again", Source: models.DecisionSourceAgent, AgentID: agent.ID}); err != nil {
			t.Fatalf("a write after starting again: %v", err)
		}
	}
	if again := mustStart(t, f.s, f.ticket.ID, resumed.ID); again.Stopped != nil {
		t.Fatalf("a start with no stop to lift: stopped = %+v, want none", again.Stopped)
	}
}
