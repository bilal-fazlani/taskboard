package db

import (
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// seedFilterTicket creates a ticket with the given status, labels and
// dependencies, for the list filter tests.
func seedFilterTicket(t *testing.T, s *Store, projectID, title, status string, labels []string, dependsOn ...string) *models.Ticket {
	t.Helper()
	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: projectID, Title: title, Status: status, Labels: labels, DependsOn: models.DependOn(dependsOn...),
	})
	if err != nil {
		t.Fatalf("seeding ticket %q: %v", title, err)
	}
	return tk
}

// Several statuses in one filter match a ticket in any of them.
func TestListTicketsFilterBySeveralStatuses(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedFilterTicket(t, s, p.ID, "todo", models.StatusTodo, nil)
	seedFilterTicket(t, s, p.ID, "doing", models.StatusInProgress, nil)
	seedFilterTicket(t, s, p.ID, "reviewing", models.StatusAgentReview, nil)
	seedFilterTicket(t, s, p.ID, "finished", models.StatusDone, nil)

	assertTitles(t, "todo + in_progress + agent_review",
		ticketTitles(t, s, models.TicketFilter{Statuses: []string{models.StatusTodo, models.StatusInProgress, models.StatusAgentReview}}),
		"todo", "doing", "reviewing")
	assertTitles(t, "one status", ticketTitles(t, s, models.TicketFilter{Statuses: []string{models.StatusDone}}), "finished")
	// Blank values are ignored rather than matching nothing, and surrounding
	// spaces are trimmed.
	assertTitles(t, "blank and padded", ticketTitles(t, s, models.TicketFilter{Statuses: []string{"", " done "}}), "finished")
	assertTitles(t, "only blank", ticketTitles(t, s, models.TicketFilter{Statuses: []string{""}}),
		"todo", "doing", "reviewing", "finished")
	// A mistyped status is a clear error, not a silent empty list, naming the
	// allowed values.
	if _, err := s.ListTickets(models.TicketFilter{Statuses: []string{"nope"}}); err == nil {
		t.Fatal("ListTickets with an unknown status: want an error, got nil")
	} else if msg := err.Error(); !strings.Contains(msg, `"nope"`) {
		t.Fatalf("ListTickets with an unknown status: err = %q, want it to name %q", msg, "nope")
	} else {
		for _, st := range models.Statuses {
			if !strings.Contains(msg, st) {
				t.Fatalf("ListTickets with an unknown status: err = %q, want it to name %q", msg, st)
			}
		}
	}
	// A comma-separated value splits the same as several separate ones, so
	// the store behaves the same whichever surface (HTTP's query string, or
	// MCP's single-string form) hands it a joined value.
	assertTitles(t, "comma-separated",
		ticketTitles(t, s, models.TicketFilter{Statuses: []string{"todo,in_progress"}}),
		"todo", "doing")
	// A mistyped status is still the error even paired with an unknown
	// project: the status filter is validated before the project's own
	// "unknown project matches nothing" short-circuit runs, so bad input
	// is never hidden behind it.
	if _, err := s.ListTickets(models.TicketFilter{ProjectID: "NOPE", Statuses: []string{"in-progress"}}); err == nil {
		t.Fatal("ListTickets with an unknown project and an unknown status: want an error, got nil")
	} else if msg := err.Error(); !strings.Contains(msg, `"in-progress"`) {
		t.Fatalf("ListTickets with an unknown project and an unknown status: err = %q, want it to name %q", msg, "in-progress")
	}
	// Combines with the other filters.
	other := seedProject(t, s, "Search", "SRCH")
	seedFilterTicket(t, s, other.ID, "other doing", models.StatusInProgress, nil)
	assertTitles(t, "statuses + project",
		ticketTitles(t, s, models.TicketFilter{ProjectID: "srch", Statuses: []string{models.StatusTodo, models.StatusInProgress}}),
		"other doing")
}

// Ready keeps todo tickets whose dependencies are all done; an unfinished
// dependency, in this project or another, holds a ticket back until it is
// done.
func TestListTicketsFilterReady(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	other := seedProject(t, s, "Search", "SRCH")
	finished := seedFilterTicket(t, s, p.ID, "finished dep", models.StatusDone, nil)
	unfinished := seedFilterTicket(t, s, p.ID, "unfinished dep", models.StatusInProgress, nil)
	elsewhere := seedFilterTicket(t, s, other.ID, "unfinished elsewhere", models.StatusAgentReview, nil)
	seedFilterTicket(t, s, p.ID, "no deps", models.StatusTodo, nil)
	seedFilterTicket(t, s, p.ID, "deps done", models.StatusTodo, nil, finished.ID)
	seedFilterTicket(t, s, p.ID, "waits on unfinished", models.StatusTodo, nil, unfinished.ID)
	seedFilterTicket(t, s, p.ID, "waits on one of two", models.StatusTodo, nil, finished.ID, unfinished.ID)
	seedFilterTicket(t, s, p.ID, "waits across projects", models.StatusTodo, nil, elsewhere.ID)
	seedFilterTicket(t, s, p.ID, "started, deps done", models.StatusInProgress, nil, finished.ID)

	assertTitles(t, "ready", ticketTitles(t, s, models.TicketFilter{Ready: true}), "no deps", "deps done")
	assertTitles(t, "ready + project", ticketTitles(t, s, models.TicketFilter{ProjectID: "srch", Ready: true}))

	// Ready is ANDed with a status list: with todo in it nothing changes,
	// without todo nothing is ready.
	assertTitles(t, "ready + todo,in_progress",
		ticketTitles(t, s, models.TicketFilter{Ready: true, Statuses: []string{models.StatusTodo, models.StatusInProgress}}),
		"no deps", "deps done")
	assertTitles(t, "ready + in_progress",
		ticketTitles(t, s, models.TicketFilter{Ready: true, Statuses: []string{models.StatusInProgress}}))

	// Finishing the dependencies makes their dependents ready.
	for _, id := range []string{unfinished.ID, elsewhere.ID} {
		if _, err := s.MoveTicket(id, models.MoveTicketRequest{Status: models.StatusDone}); err != nil {
			t.Fatalf("moving dependency to done: %v", err)
		}
	}
	assertTitles(t, "ready after the deps finish", ticketTitles(t, s, models.TicketFilter{Ready: true}),
		"no deps", "deps done", "waits on unfinished", "waits on one of two", "waits across projects")
}

// ExcludeLabel leaves out held tickets, matching the label name
// case-insensitively, and combines with the other filters.
func TestListTicketsFilterExcludeLabel(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedFilterTicket(t, s, p.ID, "held", models.StatusTodo, []string{"hold"})
	seedFilterTicket(t, s, p.ID, "held bug", models.StatusTodo, []string{"bug", "Hold"})
	seedFilterTicket(t, s, p.ID, "bug", models.StatusTodo, []string{"bug"})
	seedFilterTicket(t, s, p.ID, "plain", models.StatusInProgress, nil)
	seedFilterTicket(t, s, p.ID, "Étude", models.StatusTodo, []string{"étude"})

	assertTitles(t, "exclude hold", ticketTitles(t, s, models.TicketFilter{ExcludeLabel: "hold"}), "bug", "plain", "Étude")
	assertTitles(t, "exclude HOLD", ticketTitles(t, s, models.TicketFilter{ExcludeLabel: " HOLD "}), "bug", "plain", "Étude")
	assertTitles(t, "exclude ÉTUDE", ticketTitles(t, s, models.TicketFilter{ExcludeLabel: "ÉTUDE"}), "held", "held bug", "bug", "plain")
	// A label nobody has excludes nothing.
	assertTitles(t, "exclude unknown", ticketTitles(t, s, models.TicketFilter{ExcludeLabel: "nosuchlabel"}),
		"held", "held bug", "bug", "plain", "Étude")
	// With the label filter and a status list.
	assertTitles(t, "bug without hold", ticketTitles(t, s, models.TicketFilter{Label: "bug", ExcludeLabel: "hold"}), "bug")
	assertTitles(t, "todo without hold",
		ticketTitles(t, s, models.TicketFilter{Statuses: []string{models.StatusTodo}, ExcludeLabel: "hold"}), "bug", "Étude")
	// Including and excluding the same label leaves nothing.
	assertTitles(t, "hold without hold", ticketTitles(t, s, models.TicketFilter{Label: "hold", ExcludeLabel: "HOLD"}))
}

// Ready and ExcludeLabel together give the tickets an agent can start: a
// held ticket stays out even with its dependencies done, and a ticket
// waiting on an unfinished dependency stays out even without the hold.
func TestListTicketsFilterReadyWithoutHeld(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	finished := seedFilterTicket(t, s, p.ID, "finished dep", models.StatusDone, nil)
	unfinished := seedFilterTicket(t, s, p.ID, "unfinished dep", models.StatusTodo, nil)
	seedFilterTicket(t, s, p.ID, "startable", models.StatusTodo, nil, finished.ID)
	seedFilterTicket(t, s, p.ID, "held", models.StatusTodo, []string{"hold"}, finished.ID)
	seedFilterTicket(t, s, p.ID, "blocked", models.StatusTodo, nil, unfinished.ID)

	assertTitles(t, "ready without hold",
		ticketTitles(t, s, models.TicketFilter{ProjectID: "BILL", Ready: true, ExcludeLabel: "hold"}),
		"unfinished dep", "startable")
}
