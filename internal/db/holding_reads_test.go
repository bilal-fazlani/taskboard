package db

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// GetTicket and ListTickets carry the holding agent, with its staleness, and
// the open request; a ticket with neither leaves both out.
func TestTicketReadsCarryTheHoldingAgentAndOpenRequest(t *testing.T) {
	f := newClaimFixture(t)
	free := seedTicket(t, f.s, f.project.ID, "Nobody holds it")
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	reqID := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?", Choices: []string{"Land", "Hold"}})
	setLastSeen(t, f.s, f.first.ID, time.Hour)

	check := func(where string, got models.Ticket) {
		t.Helper()
		if got.Agent == nil || got.Agent.ID != f.first.ID || got.Agent.Role != "implementer" ||
			got.Agent.SessionID != f.first.SessionID || !got.Agent.Stale {
			t.Errorf("%s: agent = %+v, want the stale %s", where, got.Agent, f.first.ID)
		}
		if got.OpenRequest == nil || got.OpenRequest.ID != reqID || got.OpenRequest.AgentID != f.first.ID ||
			got.OpenRequest.Prompt != "Land it?" || len(got.OpenRequest.Choices) != 2 {
			t.Errorf("%s: open request = %+v, want %s", where, got.OpenRequest, reqID)
		}
	}
	got, err := f.s.GetTicket(f.ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	check("GetTicket", *got)

	list, err := f.s.ListTickets(models.TicketFilter{ProjectID: f.project.ID})
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := f.s.ListTicketsPage(models.TicketFilter{ProjectID: f.project.ID}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for where, tickets := range map[string][]models.Ticket{"ListTickets": list, "ListTicketsPage": page} {
		if len(tickets) != 2 {
			t.Fatalf("%s: %d tickets, want 2", where, len(tickets))
		}
		for _, tk := range tickets {
			switch tk.ID {
			case f.ticket.ID:
				check(where, tk)
			case free.ID:
				if tk.Agent != nil || tk.OpenRequest != nil {
					t.Errorf("%s: the free ticket has agent %+v and request %+v", where, tk.Agent, tk.OpenRequest)
				}
			}
		}
	}

	freeRead, _ := f.s.GetTicket(free.ID)
	raw, err := json.Marshal(freeRead)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"agent"`) || strings.Contains(string(raw), `"openRequest"`) {
		t.Errorf("a free ticket's JSON names an agent or request: %s", raw)
	}

	// Answered, the request is no longer open; given back, the agent is
	// gone.
	if _, err := f.s.AnswerRequest(reqID, "Land", "Bilal"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReleaseTicket(f.ticket.ID, models.ReleaseTicketRequest{AgentID: f.first.ID,
		Outcome: models.ReleaseGiveBack, HandOff: "Approved; landing is next"}); err != nil {
		t.Fatal(err)
	}
	got, _ = f.s.GetTicket(f.ticket.ID)
	if got.Agent != nil || got.OpenRequest != nil {
		t.Fatalf("after the answer and release: agent %+v, open request %+v", got.Agent, got.OpenRequest)
	}
}

// A ticket in needs_user_input, which no write sets but a request, breaks no
// read and shows everywhere: the ticket, the lists (filtered on its status
// too), its epic's count, activity, the board's needs_user_input column and
// Now's waiting group.
func TestReadsDoNotBreakOnATicketWaitingOnThePerson(t *testing.T) {
	f := newClaimFixture(t)
	epic := seedEpic(t, f.s, f.project.ID, "Agents")
	epicName := epic.Name
	if _, err := f.s.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Epic: &epicName}); err != nil {
		t.Fatal(err)
	}
	seedTicket(t, f.s, f.project.ID, "Still todo")
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputQuestion, Prompt: "Which port?"})

	if got, err := f.s.GetTicket(f.ticket.ID); err != nil || got.Status != models.StatusNeedsUserInput {
		t.Fatalf("GetTicket: %+v, %v", got, err)
	}
	list, err := f.s.ListTickets(models.TicketFilter{})
	if err != nil || len(list) != 2 {
		t.Fatalf("ListTickets: %d tickets, %v", len(list), err)
	}
	listed := false
	for _, tk := range list {
		listed = listed || (tk.ID == f.ticket.ID && tk.Status == models.StatusNeedsUserInput)
	}
	if !listed {
		t.Error("ListTickets leaves out the ticket waiting on the person")
	}
	if epics, err := f.s.ListEpics(f.project.ID); err != nil || len(epics) != 1 || epics[0].Total != 1 ||
		epics[0].Counts[models.StatusNeedsUserInput] != 1 {
		t.Fatalf("ListEpics: %+v, %v; want the waiting ticket counted", epics, err)
	}
	activity, err := f.s.ListActivity(f.project.ID, nil, "", 20)
	if err != nil || len(activity.Entries) == 0 || activity.Entries[0].ToStatus != models.StatusNeedsUserInput {
		t.Fatalf("ListActivity: %+v, %v; want the move to needs_user_input first", activity, err)
	}

	filtered, err := f.s.ListTickets(models.TicketFilter{Statuses: []string{models.StatusNeedsUserInput}})
	if err != nil || len(filtered) != 1 || filtered[0].ID != f.ticket.ID {
		t.Fatalf("ListTickets filtered on needs_user_input: %+v, %v; want just the waiting ticket", filtered, err)
	}

	board, err := f.s.GetBoard(f.project.ID)
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	for _, c := range board.Columns {
		for _, tk := range c.Tickets {
			if tk.ID == f.ticket.ID && c.Status != models.StatusNeedsUserInput {
				t.Errorf("the board shows the waiting ticket in %s", c.Status)
			}
		}
		if c.Status == models.StatusNeedsUserInput && (len(c.Tickets) != 1 || c.Tickets[0].ID != f.ticket.ID ||
			c.Tickets[0].OpenRequest == nil || c.Tickets[0].Agent == nil) {
			t.Errorf("the needs_user_input column = %+v, want the waiting ticket with its agent and request", c.Tickets)
		}
	}
	now, err := f.s.Now("", time.Now())
	if err != nil {
		t.Fatalf("Now: %v", err)
	}
	if len(now.Waiting) != 1 || now.Waiting[0].ID != f.ticket.ID || now.Waiting[0].Status != models.StatusNeedsUserInput {
		t.Errorf("Now waiting = %+v, want the waiting ticket", now.Waiting)
	}
	for _, tk := range append(now.InProgress, now.InReview...) {
		if tk.ID == f.ticket.ID {
			t.Error("Now shows the waiting ticket in progress or in review")
		}
	}
	// An ordinary edit of the waiting ticket keeps its status.
	title := "Claim rules, renamed"
	if got, err := f.s.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Title: &title}); err != nil ||
		got.Status != models.StatusNeedsUserInput {
		t.Fatalf("UpdateTicket: %+v, %v", got, err)
	}
}
