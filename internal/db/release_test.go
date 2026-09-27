package db

import (
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

func ticketEntries(t *testing.T, s *Store, ticketID, entryType string) []models.Entry {
	t.Helper()
	page, err := s.ListEntries(models.EntryOwner{TicketID: ticketID}, models.EntryFilter{Types: []string{entryType}}, "", EntryMaxLimit)
	if err != nil {
		t.Fatal(err)
	}
	return page.Entries
}

// Giving a ticket back writes the hand-off as the agent's entry, clears the
// agent and returns the ticket to todo.
func TestReleaseTicketGivingBackNeedsAHandOff(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	setLastSeen(t, f.s, f.first.ID, 10*time.Minute)

	got, err := f.s.ReleaseTicket("ACP-1", models.ReleaseTicketRequest{AgentID: f.first.ID, Outcome: models.ReleaseGiveBack,
		HandOff: "Claim is done; release is next"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusTodo || got.Agent != nil {
		t.Fatalf("given back: status %s, agent %+v; want todo and no agent", got.Status, got.Agent)
	}
	handOffs := ticketEntries(t, f.s, f.ticket.ID, models.EntryHandOff)
	if len(handOffs) != 1 || handOffs[0].AgentID != f.first.ID || handOffs[0].Text != "Claim is done; release is next" {
		t.Fatalf("hand-offs = %+v", handOffs)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusTodo, "Hand-off: entry "+handOffs[0].ID)
	if time.Since(lastSeen(t, f.s, f.first.ID)) > time.Minute {
		t.Fatal("releasing did not touch the agent")
	}
}

// Finishing writes the proof and moves the ticket to done.
func TestReleaseTicketFinishingNeedsProof(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	got, err := f.s.ReleaseTicket(f.ticket.ID, models.ReleaseTicketRequest{AgentID: f.first.ID, Outcome: models.ReleaseFinish,
		Proof: "go test ./internal/db passes, run with -count=1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusDone || got.Agent != nil {
		t.Fatalf("finished: status %s, agent %+v; want done and no agent", got.Status, got.Agent)
	}
	proofs := ticketEntries(t, f.s, f.ticket.ID, models.EntryProof)
	if len(proofs) != 1 || proofs[0].AgentID != f.first.ID {
		t.Fatalf("proofs = %+v", proofs)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusDone, "Proof: entry "+proofs[0].ID)
}

// A release without its record, by an agent not holding the ticket, or
// while the ticket waits on the person, is refused, and writes nothing.
func TestReleaseTicketRefusesWithoutItsRecord(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	for _, tc := range []struct {
		name string
		req  models.ReleaseTicketRequest
		want string
	}{
		{"bare give back", models.ReleaseTicketRequest{AgentID: f.first.ID, Outcome: models.ReleaseGiveBack},
			"giving a ticket back needs its hand-off: where the work stopped and the next step"},
		{"bare finish", models.ReleaseTicketRequest{AgentID: f.first.ID, Outcome: models.ReleaseFinish, HandOff: "  "},
			"finishing a ticket needs its proof: what was verified, how, and the result"},
		{"no outcome", models.ReleaseTicketRequest{AgentID: f.first.ID, HandOff: "x"}, `outcome "" is not a way to release a ticket`},
		{"give back with proof", models.ReleaseTicketRequest{AgentID: f.first.ID, Outcome: models.ReleaseGiveBack, HandOff: "x", Proof: "y"},
			"proof is for finishing"},
		{"finish with a hand-off", models.ReleaseTicketRequest{AgentID: f.first.ID, Outcome: models.ReleaseFinish, HandOff: "x", Proof: "y"},
			"a hand-off is for giving a ticket back"},
		{"another session", models.ReleaseTicketRequest{AgentID: f.second.ID, Outcome: models.ReleaseGiveBack, HandOff: "x"},
			"ticket ACP-1 is held by agent " + f.first.ID + " of another session, not " + f.second.ID},
		{"no agent", models.ReleaseTicketRequest{Outcome: models.ReleaseGiveBack, HandOff: "x"}, "agentId is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.s.ReleaseTicket(f.ticket.ID, tc.req)
			wantInvalidContaining(t, err, tc.want)
		})
	}

	if _, err := f.s.CreateRequest(models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.s.ReleaseTicket(f.ticket.ID, models.ReleaseTicketRequest{AgentID: f.first.ID, Outcome: models.ReleaseFinish, Proof: "tests pass"})
	wantInvalidContaining(t, err, "waits on the person's answer to request")

	got, _ := f.s.GetTicket(f.ticket.ID)
	if got.Agent == nil || got.Agent.ID != f.first.ID {
		t.Fatalf("refused releases freed the ticket: agent %+v", got.Agent)
	}
	if n := len(ticketEntries(t, f.s, f.ticket.ID, models.EntryHandOff)) + len(ticketEntries(t, f.s, f.ticket.ID, models.EntryProof)); n != 0 {
		t.Fatalf("refused releases wrote %d entries", n)
	}

	free := seedTicket(t, f.s, f.project.ID, "Nobody holds it")
	_, err = f.s.ReleaseTicket(free.ID, models.ReleaseTicketRequest{AgentID: f.first.ID, Outcome: models.ReleaseGiveBack, HandOff: "x"})
	wantInvalidContaining(t, err, "is not held by any agent")
}
