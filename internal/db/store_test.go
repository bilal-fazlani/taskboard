package db

import (
	"path/filepath"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// newTestStore returns a Store backed by a throwaway database in the test's
// temp dir. It must never call db.Open(), which resolves to the user's real
// database.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := OpenAt(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewStore(database)
}

func seedProject(t *testing.T, s *Store, name, prefix string) *models.Project {
	t.Helper()
	p, err := s.CreateProject(models.CreateProjectRequest{Name: name, Prefix: prefix})
	if err != nil {
		t.Fatalf("seeding project %s: %v", prefix, err)
	}
	return p
}

func seedTicket(t *testing.T, s *Store, projectID, title string) *models.Ticket {
	t.Helper()
	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: projectID, Title: title})
	if err != nil {
		t.Fatalf("seeding ticket %q: %v", title, err)
	}
	return tk
}

func TestMigrationsDropTeams(t *testing.T) {
	s := newTestStore(t)

	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='teams'`,
	).Scan(&n)
	if err != nil {
		t.Fatalf("querying sqlite_master: %v", err)
	}
	if n != 0 {
		t.Fatalf("teams table still exists after migrations")
	}

	var col int
	err = s.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('tickets') WHERE name='team_id'`,
	).Scan(&col)
	if err != nil {
		t.Fatalf("querying pragma_table_info: %v", err)
	}
	if col != 0 {
		t.Fatalf("tickets.team_id still exists after migrations")
	}
}

func TestTicketRoundTripAfterTeamsRemoval(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice export")

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got == nil {
		t.Fatal("GetTicket returned nil")
	}
	if got.Title != "Invoice export" {
		t.Fatalf("title = %q, want %q", got.Title, "Invoice export")
	}
	if got.DisplayKey() != "BILL-1" {
		t.Fatalf("key = %q, want BILL-1", got.DisplayKey())
	}
}

func TestTicketRepoRoundTrip(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Invoice export",
		Repo:      "acme/billing-api",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if tk.Repo != "acme/billing-api" {
		t.Fatalf("repo after create = %q, want acme/billing-api", tk.Repo)
	}

	newRepo := "acme/billing-web"
	updated, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Repo: &newRepo})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if updated.Repo != "acme/billing-web" {
		t.Fatalf("repo after update = %q, want acme/billing-web", updated.Repo)
	}

	// A nil Repo must leave the existing value alone.
	title := "Renamed"
	untouched, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title})
	if err != nil {
		t.Fatalf("UpdateTicket (title only): %v", err)
	}
	if untouched.Repo != "acme/billing-web" {
		t.Fatalf("repo was cleared by an unrelated update: %q", untouched.Repo)
	}
}

func TestListTicketsFilterByRepo(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "API work", Repo: "acme/billing-api",
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Web work", Repo: "acme/billing-web",
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := s.ListTickets(models.TicketFilter{Repo: "acme/billing-api"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(got) != 1 || got[0].Title != "API work" {
		t.Fatalf("repo filter returned %d tickets, want 1 (API work)", len(got))
	}
}
