package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// newWaitingFixture is a ticket an agent holds that waits on the person: its
// approval request is open and it is in needs_user_input. It answers the
// fixture and the request's id.
func newWaitingFixture(t *testing.T) (claimFixture, string) {
	t.Helper()
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	reqID := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?"})
	return f, reqID
}

// wantWaiting fails unless err is an ErrTicketWaiting, also an
// ErrInvalidInput, naming the ticket, its open request and both ways out.
func wantWaiting(t *testing.T, what string, err error, reqID string) {
	t.Helper()
	var waiting *ErrTicketWaiting
	if !errors.As(err, &waiting) {
		t.Fatalf("%s: err = %v, want an ErrTicketWaiting", what, err)
	}
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("%s: an ErrTicketWaiting must also be an ErrInvalidInput", what)
	}
	if waiting.Ticket != "ACP-1" || waiting.Request != reqID {
		t.Fatalf("%s: err names ticket %q and request %q, want ACP-1 and %s", what, waiting.Ticket, waiting.Request, reqID)
	}
	for _, want := range []string{"ACP-1", reqID, "(approval)", models.StatusNeedsUserInput,
		"answering the request on the ticket page", "Stop work"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %q, want it to contain %q", what, err, want)
		}
	}
}

// wantStillWaiting fails unless the ticket is still in needs_user_input with
// the request open, its title and status history untouched.
func wantStillWaiting(t *testing.T, f claimFixture, reqID string, history int) {
	t.Helper()
	got, err := f.s.GetTicket(f.ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusNeedsUserInput || got.OpenRequest == nil || got.OpenRequest.ID != reqID {
		t.Fatalf("ticket after a refused write: status %s, open request %+v; want needs_user_input waiting on %s",
			got.Status, got.OpenRequest, reqID)
	}
	if got.Title != f.ticket.Title {
		t.Fatalf("a refused update changed the title to %q", got.Title)
	}
	if n := historyLen(t, f.s, f.ticket.ID); n != history {
		t.Fatalf("a refused write wrote history: %d rows, want %d", n, history)
	}
	r, err := f.s.GetRequest(reqID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Answered() {
		t.Fatalf("a refused write closed the request: %+v", r)
	}
}

// A move out of needs_user_input, to any status a write may set, is refused
// while the ticket's request is open, whether the caller asks for the review
// note rule (MCP) or not (HTTP, CLI). Nothing is written.
func TestMoveTicketRefusesToTakeAWaitingTicketOut(t *testing.T) {
	for _, status := range models.Statuses {
		t.Run(status, func(t *testing.T) {
			f, reqID := newWaitingFixture(t)
			history := historyLen(t, f.s, f.ticket.ID)

			_, err := f.s.MoveTicket(f.ticket.ID, models.MoveTicketRequest{Status: status, Note: "moving on"})
			wantWaiting(t, "move to "+status, err, reqID)
			_, err = f.s.MoveTicket(f.ticket.ID, models.MoveTicketRequest{Status: status, Note: "moving on"}, RequireNoteLeavingReview())
			wantWaiting(t, "move to "+status+" with the note rule", err, reqID)
			wantStillWaiting(t, f, reqID, history)
		})
	}
}

// A status change through UpdateTicket is refused the same way, and the
// whole update with it: the other fields it sets are not written either. An
// update that leaves the status alone still goes through.
func TestUpdateTicketRefusesToTakeAWaitingTicketOut(t *testing.T) {
	f, reqID := newWaitingFixture(t)
	history := historyLen(t, f.s, f.ticket.ID)

	for _, status := range models.Statuses {
		title := "Renamed while waiting"
		_, err := f.s.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Status: &status, Title: &title})
		wantWaiting(t, "update to "+status, err, reqID)
	}
	wantStillWaiting(t, f, reqID, history)

	priority := "high"
	got, err := f.s.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Priority: &priority})
	if err != nil {
		t.Fatalf("an update that keeps the status: %v", err)
	}
	if got.Status != models.StatusNeedsUserInput || got.Priority != "high" || got.OpenRequest == nil {
		t.Fatalf("after an update that keeps the status: status %s, priority %s, open request %+v",
			got.Status, got.Priority, got.OpenRequest)
	}
}

// Answering and stopping still take a waiting ticket out of needs_user_input,
// and once they have, the ticket moves freely again.
func TestAnsweringOrStoppingStillTakesAWaitingTicketOut(t *testing.T) {
	t.Run("answer", func(t *testing.T) {
		f, reqID := newWaitingFixture(t)
		if _, err := f.s.AnswerRequest(reqID, "approved", "bilal", ""); err != nil {
			t.Fatalf("answering: %v", err)
		}
		got, _ := f.s.GetTicket(f.ticket.ID)
		if got.Status != models.StatusInProgress || got.OpenRequest != nil {
			t.Fatalf("answered: status %s, open request %+v; want in_progress and none", got.Status, got.OpenRequest)
		}
		if moved, err := f.s.MoveTicket(f.ticket.ID, models.MoveTicketRequest{Status: models.StatusAgentReview}); err != nil ||
			moved.Status != models.StatusAgentReview {
			t.Fatalf("moving the answered ticket: %+v, %v", moved, err)
		}
	})
	t.Run("stop", func(t *testing.T) {
		f, _ := newWaitingFixture(t)
		got := mustStop(t, f.s, f.ticket.ID)
		if got.Status != models.StatusTodo || got.OpenRequest != nil {
			t.Fatalf("stopped: status %s, open request %+v; want todo and none", got.Status, got.OpenRequest)
		}
		status := models.StatusDone
		if updated, err := f.s.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Status: &status}); err != nil ||
			updated.Status != models.StatusDone {
			t.Fatalf("updating the stopped ticket: %+v, %v", updated, err)
		}
	})
}

// A ticket left in needs_user_input with no open request, which the rule
// never produces, may still be moved out: there is nothing to answer.
func TestAWaitingTicketWithNoOpenRequestMayMove(t *testing.T) {
	f := newClaimFixture(t)
	if _, err := f.s.db.Exec("UPDATE tickets SET status = ? WHERE id = ?", models.StatusNeedsUserInput, f.ticket.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.MoveTicket(f.ticket.ID, models.MoveTicketRequest{Status: models.StatusTodo})
	if err != nil || got.Status != models.StatusTodo {
		t.Fatalf("moving a waiting ticket with no open request: %+v, %v", got, err)
	}
}
