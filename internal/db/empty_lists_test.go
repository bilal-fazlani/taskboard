package db

import (
	"encoding/json"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// wantEmptyJSONArray fails unless v marshals to exactly "[]": not "null",
// and not some other empty representation. That is the wire guarantee
// ACP-148 asks for: a list result with no matches is [] to every caller,
// never null.
func wantEmptyJSONArray(t *testing.T, label string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s: marshaling %#v: %v", label, v, err)
	}
	if string(data) != "[]" {
		t.Errorf("%s = %s, want [] (not null)", label, data)
	}
}

// A ListTickets filter matching nothing, whether because no ticket has the
// status or because the project/label/epic filter resolves to nothing,
// gives [] rather than a nil slice.
func TestListTicketsEmptyIsNeverNil(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "Invoice")

	for label, filter := range map[string]models.TicketFilter{
		"status matches nothing":   {Statuses: []string{models.StatusInProgress}},
		"unknown project":          {ProjectID: "NOPE"},
		"unknown label":            {Label: "nosuchlabel"},
		"unknown epic":             {Epic: "nosuchepic", ProjectID: p.ID},
		"priority matches nothing": {Priority: "urgent"},
	} {
		tickets, err := s.ListTickets(filter)
		if err != nil {
			t.Fatalf("ListTickets(%s): %v", label, err)
		}
		if tickets == nil {
			t.Fatalf("ListTickets(%s) = nil, want a non-nil empty slice", label)
		}
		wantEmptyJSONArray(t, "ListTickets("+label+")", tickets)
	}
}

// ListTicketsPage has the same guarantee, for both a filter matching nothing
// and a page past the end of a non-empty list.
func TestListTicketsPageEmptyIsNeverNil(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "Invoice")

	page, total, err := s.ListTicketsPage(models.TicketFilter{ProjectID: "NOPE"}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page == nil {
		t.Fatal("ListTicketsPage(unknown project) = nil, want a non-nil empty slice")
	}
	wantEmptyJSONArray(t, "ListTicketsPage(unknown project)", page)
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}

	page, total, err = s.ListTicketsPage(models.TicketFilter{ProjectID: p.ID}, 10, 50)
	if err != nil {
		t.Fatal(err)
	}
	if page == nil {
		t.Fatal("ListTicketsPage(past the end) = nil, want a non-nil empty slice")
	}
	wantEmptyJSONArray(t, "ListTicketsPage(past the end)", page)
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
}

// ListLabels with no labels created gives [] rather than null.
func TestListLabelsEmptyIsNeverNil(t *testing.T) {
	s := newTestStore(t)

	labels, err := s.ListLabels()
	if err != nil {
		t.Fatal(err)
	}
	if labels == nil {
		t.Fatal("ListLabels() = nil, want a non-nil empty slice")
	}
	wantEmptyJSONArray(t, "ListLabels()", labels)
}

// ListProjects with no projects created, or a status filter matching none,
// gives [] rather than null.
func TestListProjectsEmptyIsNeverNil(t *testing.T) {
	s := newTestStore(t)

	projects, err := s.ListProjects("")
	if err != nil {
		t.Fatal(err)
	}
	if projects == nil {
		t.Fatal(`ListProjects("") = nil, want a non-nil empty slice`)
	}
	wantEmptyJSONArray(t, `ListProjects("")`, projects)

	seedProject(t, s, "Billing", "BILL")
	archived, err := s.ListProjects("archived")
	if err != nil {
		t.Fatal(err)
	}
	if archived == nil {
		t.Fatal(`ListProjects("archived") = nil, want a non-nil empty slice`)
	}
	wantEmptyJSONArray(t, `ListProjects("archived")`, archived)
}
