package db

import (
	"fmt"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// Pages of ListTicketsPage, read one after another, are the whole filtered
// list in its order, each ticket once, with the total on every page and
// the page's details (labels, subtasks, dependencies) loaded.
func TestListTicketsPage(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	other := seedProject(t, s, "Search", "SRCH")
	first := seedFilterTicket(t, s, p.ID, "t0", models.StatusTodo, nil)
	for i := 1; i < 7; i++ {
		seedFilterTicket(t, s, p.ID, fmt.Sprintf("t%d", i), models.StatusTodo, []string{"api"}, first.ID)
	}
	seedFilterTicket(t, s, p.ID, "finished", models.StatusDone, nil)
	seedFilterTicket(t, s, other.ID, "elsewhere", models.StatusTodo, nil)

	filter := models.TicketFilter{ProjectID: "BILL", Statuses: []string{models.StatusTodo}}
	all, err := s.ListTickets(filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 7 {
		t.Fatalf("ListTickets = %d tickets, want 7", len(all))
	}

	var paged []models.Ticket
	for offset := 0; offset < 9; offset += 3 {
		page, total, err := s.ListTicketsPage(filter, 3, offset)
		if err != nil {
			t.Fatal(err)
		}
		if total != 7 {
			t.Fatalf("page at %d: total = %d, want 7", offset, total)
		}
		want := 3
		if offset == 6 {
			want = 1
		}
		if len(page) != want {
			t.Fatalf("page at %d: %d tickets, want %d", offset, len(page), want)
		}
		paged = append(paged, page...)
	}
	for i := range all {
		if paged[i].ID != all[i].ID {
			t.Fatalf("paged ticket %d = %s, want %s: pages must follow the list's order", i, paged[i].Title, all[i].Title)
		}
		if paged[i].Title != "t0" && (len(paged[i].Labels) != 1 || len(paged[i].DependsOn) != 1) {
			t.Fatalf("paged ticket %s: labels %v, dependsOn %v, want its details loaded", paged[i].Title, paged[i].Labels, paged[i].DependsOn)
		}
	}

	// A page past the end is empty and still counts every match.
	page, total, err := s.ListTicketsPage(filter, 3, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 0 || total != 7 {
		t.Fatalf("page past the end = %d tickets of %d, want 0 of 7", len(page), total)
	}

	// A filter that resolves to nothing is an empty page of none.
	page, total, err = s.ListTicketsPage(models.TicketFilter{ProjectID: "NOPE"}, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 0 || total != 0 {
		t.Fatalf("unknown project = %d tickets of %d, want 0 of 0", len(page), total)
	}
}
