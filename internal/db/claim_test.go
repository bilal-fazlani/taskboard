package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// claimFixture is a project with one todo ticket and two agents in
// different sessions.
type claimFixture struct {
	s             *Store
	project       *models.Project
	ticket        *models.Ticket
	first, second *models.Agent
}

func newClaimFixture(t *testing.T) claimFixture {
	t.Helper()
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	return claimFixture{
		s: s, project: p, ticket: seedTicket(t, s, p.ID, "Claim rules"),
		first:  identify(t, s, "3da2c294", "implementer"),
		second: identify(t, s, "b10d8a02", "implementer"),
	}
}

func mustClaim(t *testing.T, s *Store, ticketID, agentID string) *models.Claim {
	t.Helper()
	c, err := s.ClaimTicket(ticketID, agentID)
	if err != nil {
		t.Fatalf("claiming %s for %s: %v", ticketID, agentID, err)
	}
	return c
}

func historyLen(t *testing.T, s *Store, ticketID string) int {
	t.Helper()
	changes, err := s.ListStatusChanges(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	return len(changes)
}

// A claim sets the agent, moves the ticket to in_progress and touches the
// agent; the display key works as well as the id.
func TestClaimTicketHoldsItAndMovesItToInProgress(t *testing.T) {
	f := newClaimFixture(t)
	setLastSeen(t, f.s, f.first.ID, 10*time.Minute)
	c := mustClaim(t, f.s, "ACP-1", f.first.ID)
	if c.Ticket.Status != models.StatusInProgress || c.Ticket.Agent == nil || c.Ticket.Agent.ID != f.first.ID {
		t.Fatalf("claimed ticket: status %s, agent %+v; want in_progress held by %s", c.Ticket.Status, c.Ticket.Agent, f.first.ID)
	}
	if c.TakenFrom != nil || c.HandOff != nil {
		t.Fatalf("a claim of a free ticket took it from %+v with hand-off %+v", c.TakenFrom, c.HandOff)
	}
	if time.Since(lastSeen(t, f.s, f.first.ID)) > time.Minute {
		t.Fatal("claiming did not touch the agent")
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusTodo, models.StatusInProgress, "")

	// Claiming it again changes nothing.
	n := historyLen(t, f.s, f.ticket.ID)
	c = mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	if c.Ticket.Agent.ID != f.first.ID || historyLen(t, f.s, f.ticket.ID) != n {
		t.Fatal("claiming a ticket the agent already holds changed it")
	}
}

// A live holder keeps its ticket: the claim is refused, naming the holder
// and when it was last seen, and nothing changes.
func TestClaimTicketRefusesATicketALiveAgentHolds(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	setLastSeen(t, f.s, f.first.ID, 29*time.Minute)
	n := historyLen(t, f.s, f.ticket.ID)

	_, err := f.s.ClaimTicket(f.ticket.ID, f.second.ID)
	var held *ErrTicketHeld
	if !errors.As(err, &held) {
		t.Fatalf("claim of a live agent's ticket: %v, want ErrTicketHeld", err)
	}
	if held.Holder.ID != f.first.ID || held.Ticket != "ACP-1" {
		t.Fatalf("ErrTicketHeld names %s on %s, want %s on ACP-1", held.Holder.ID, held.Ticket, f.first.ID)
	}
	wantInvalidContaining(t, err, "held by agent "+f.first.ID)
	if !strings.Contains(err.Error(), "whose session was last seen "+held.Holder.SessionLastSeenAt.UTC().Format(time.RFC3339)) ||
		!strings.Contains(err.Error(), "29m") {
		t.Fatalf("message %q does not say when the holder was last seen", err)
	}
	got, _ := f.s.GetTicket(f.ticket.ID)
	if got.Agent.ID != f.first.ID || historyLen(t, f.s, f.ticket.ID) != n {
		t.Fatal("a refused claim changed the ticket")
	}
}

// A stale holder's ticket is taken over: the status history records who it
// was taken from and its latest hand-off, which stays readable, and the
// claim answers with both.
func TestClaimTicketTakesOverAStaleHoldersTicket(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: f.ticket.ID},
		Type: models.EntryHandOff, Text: "Stopped mid-way: the claim test is next", AgentID: f.first.ID})
	latest := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: f.ticket.ID},
		Type: models.EntryHandOff, Text: "Claim works; release is next", AgentID: f.first.ID})
	setLastSeen(t, f.s, f.first.ID, 31*time.Minute)

	c := mustClaim(t, f.s, f.ticket.ID, f.second.ID)
	if c.Ticket.Agent == nil || c.Ticket.Agent.ID != f.second.ID || c.Ticket.Status != models.StatusInProgress {
		t.Fatalf("after the takeover: agent %+v, status %s", c.Ticket.Agent, c.Ticket.Status)
	}
	if c.TakenFrom == nil || c.TakenFrom.ID != f.first.ID || !c.TakenFrom.Stale {
		t.Fatalf("taken from %+v, want the stale %s", c.TakenFrom, f.first.ID)
	}
	if c.HandOff == nil || c.HandOff.ID != latest.ID {
		t.Fatalf("hand-off %+v, want the latest, %s", c.HandOff, latest.ID)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusInProgress,
		"Taken over by agent "+f.second.ID+" (implementer, claude-opus-5-5) from agent "+f.first.ID)
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusInProgress, "entry "+latest.ID)
	if e, err := f.s.GetEntry(latest.ID); err != nil || e == nil || e.Text != "Claim works; release is next" {
		t.Fatalf("the previous holder's hand-off after the takeover: %+v, %v", e, err)
	}
}

// A takeover of a ticket with no hand-off says so.
func TestClaimTicketTakeoverWithoutAHandOff(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	setLastSeen(t, f.s, f.first.ID, 2*time.Hour)
	c := mustClaim(t, f.s, f.ticket.ID, f.second.ID)
	if c.TakenFrom == nil || c.HandOff != nil {
		t.Fatalf("taken from %+v with hand-off %+v, want the first agent and none", c.TakenFrom, c.HandOff)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusInProgress, "The ticket has no hand-off.")
}

// A takeover reports where the work stood: the ticket's latest hand-off,
// whichever agent wrote it. A gives the ticket back with a hand-off, B
// claims it and goes stale without one, and C's takeover gets A's.
func TestClaimTicketTakeoverReportsTheTicketsLatestHandOff(t *testing.T) {
	f := newClaimFixture(t)
	third := identify(t, f.s, "c0ffee00", "implementer")
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	given, err := f.s.ReleaseTicket(f.ticket.ID, models.ReleaseTicketRequest{AgentID: f.first.ID,
		Outcome: models.ReleaseGiveBack, HandOff: "Settings are in; the claim rules are next"})
	if err != nil || given.Status != models.StatusTodo {
		t.Fatalf("A gives back: %+v, %v", given, err)
	}
	handOff := ticketEntries(t, f.s, f.ticket.ID, models.EntryHandOff)[0]

	mustClaim(t, f.s, f.ticket.ID, f.second.ID)
	setLastSeen(t, f.s, f.second.ID, time.Hour)
	c := mustClaim(t, f.s, f.ticket.ID, third.ID)
	if c.TakenFrom == nil || c.TakenFrom.ID != f.second.ID {
		t.Fatalf("taken from %+v, want B, %s", c.TakenFrom, f.second.ID)
	}
	if c.HandOff == nil || c.HandOff.ID != handOff.ID || c.HandOff.AgentID != f.first.ID {
		t.Fatalf("hand-off %+v, want A's, %s", c.HandOff, handOff.ID)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusInProgress, "Latest hand-off: entry "+handOff.ID)
}

// A session owns its tickets: a resumed chat is a new agent in the same
// session, and it claims and releases the ticket its old agent holds, while
// that agent is still live, as its own, with no refusal and no takeover.
func TestAResumedChatClaimsAndReleasesItsSessionsTicket(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	resumed := identify(t, f.s, "3da2c294", "implementer")
	if resumed.SessionID != f.first.SessionID || resumed.ID == f.first.ID {
		t.Fatalf("the resumed chat's agent %+v, want a new agent in session %s", resumed, f.first.SessionID)
	}
	if got, _ := f.s.GetAgent(f.first.ID); got.Stale {
		t.Fatal("the old agent is stale; the test needs it live")
	}
	n := historyLen(t, f.s, f.ticket.ID)

	c := mustClaim(t, f.s, f.ticket.ID, resumed.ID)
	if c.TakenFrom != nil || c.HandOff != nil {
		t.Fatalf("a claim within the session took the ticket over from %+v, with hand-off %+v", c.TakenFrom, c.HandOff)
	}
	if c.Ticket.Agent == nil || c.Ticket.Agent.ID != resumed.ID || c.Ticket.Status != models.StatusInProgress {
		t.Fatalf("after the resumed chat's claim: agent %+v, status %s", c.Ticket.Agent, c.Ticket.Status)
	}
	if historyLen(t, f.s, f.ticket.ID) != n {
		t.Fatal("a claim within the session wrote status history")
	}

	// The session's other live agent can still release it, and an agent of
	// another session cannot.
	_, err := f.s.ReleaseTicket(f.ticket.ID, models.ReleaseTicketRequest{AgentID: f.second.ID,
		Outcome: models.ReleaseGiveBack, HandOff: "x"})
	wantInvalidContaining(t, err, "of another session")
	got, err := f.s.ReleaseTicket(f.ticket.ID, models.ReleaseTicketRequest{AgentID: f.first.ID,
		Outcome: models.ReleaseFinish, Proof: "go test ./internal/db passes"})
	if err != nil {
		t.Fatalf("the old agent of the same session releasing: %v", err)
	}
	if got.Status != models.StatusDone || got.Agent != nil {
		t.Fatalf("after the release: status %s, agent %+v", got.Status, got.Agent)
	}
	if proofs := ticketEntries(t, f.s, f.ticket.ID, models.EntryProof); len(proofs) != 1 || proofs[0].AgentID != f.first.ID {
		t.Fatalf("proofs = %+v, want one by the releasing agent", proofs)
	}
}

// A claim keeps a ticket in review in agent_review: leaving review takes an
// agent's note, which a claim does not give.
func TestClaimTicketKeepsATicketInReview(t *testing.T) {
	f := newClaimFixture(t)
	if _, err := f.s.MoveTicket(f.ticket.ID, models.MoveTicketRequest{Status: models.StatusAgentReview}); err != nil {
		t.Fatal(err)
	}
	n := historyLen(t, f.s, f.ticket.ID)
	c := mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	if c.Ticket.Status != models.StatusAgentReview || c.Ticket.Agent == nil || c.Ticket.Agent.ID != f.first.ID {
		t.Fatalf("claimed in review: status %s, agent %+v; want agent_review held by %s", c.Ticket.Status, c.Ticket.Agent, f.first.ID)
	}
	if historyLen(t, f.s, f.ticket.ID) != n {
		t.Fatal("claiming a ticket in review wrote status history")
	}

	// A takeover in review keeps it there too, and records the takeover,
	// but the takeover's row is not another review round, and Now still
	// dates the ticket from its move into review.
	nowSince := func() time.Time {
		t.Helper()
		now, err := f.s.Now("", time.Now())
		if err != nil || len(now.InReview) != 1 {
			t.Fatalf("Now: %+v, %v; want the ticket in review", now, err)
		}
		return now.InReview[0].Since
	}
	since := nowSince()
	setLastSeen(t, f.s, f.first.ID, time.Hour)
	c = mustClaim(t, f.s, f.ticket.ID, f.second.ID)
	if c.Ticket.Status != models.StatusAgentReview || c.TakenFrom == nil {
		t.Fatalf("taken over in review: status %s, taken from %+v", c.Ticket.Status, c.TakenFrom)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusAgentReview, models.StatusAgentReview, "Taken over by agent "+f.second.ID)
	if c.Ticket.ReviewRounds != 1 {
		t.Errorf("after a takeover in review, reviewRounds = %d, want 1", c.Ticket.ReviewRounds)
	}
	if list, err := f.s.ListTickets(models.TicketFilter{ProjectID: f.project.ID}); err != nil || list[0].ReviewRounds != 1 {
		t.Errorf("ListTickets after a takeover in review: %+v, %v; want 1 review round", list, err)
	}
	if got := nowSince(); !got.Equal(since) {
		t.Errorf("Now's since after a takeover in review = %v, want %v, the move into review", got, since)
	}
}

// Unfinished dependencies never block a claim.
func TestClaimTicketNeverChecksDependencies(t *testing.T) {
	f := newClaimFixture(t)
	blocked, err := f.s.CreateTicket(models.CreateTicketRequest{ProjectID: f.project.ID, Title: "Waits on ACP-1",
		DependsOn: []models.DependencyInput{{Ticket: "ACP-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	c := mustClaim(t, f.s, blocked.ID, f.first.ID)
	if c.Ticket.Status != models.StatusInProgress || c.Ticket.Agent.ID != f.first.ID {
		t.Fatalf("a ticket with an unfinished dependency: status %s, agent %+v", c.Ticket.Status, c.Ticket.Agent)
	}
}

func TestClaimTicketRefusesAnUnknownTicketOrAgent(t *testing.T) {
	f := newClaimFixture(t)
	_, err := f.s.ClaimTicket("ACP-99", f.first.ID)
	wantInvalidContaining(t, err, `no ticket matches "ACP-99"`)
	_, err = f.s.ClaimTicket(f.ticket.ID, "no-such-agent")
	wantInvalidContaining(t, err, "is not an agent")
	_, err = f.s.ClaimTicket(f.ticket.ID, "")
	wantInvalidContaining(t, err, "agentId is required")
	if got, _ := f.s.GetTicket(f.ticket.ID); got.Agent != nil || got.Status != models.StatusTodo {
		t.Fatalf("refused claims changed the ticket: %+v, %s", got.Agent, got.Status)
	}
}
