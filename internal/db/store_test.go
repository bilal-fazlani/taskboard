package db

import (
	"path/filepath"
	"strings"
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

func TestDependencyDirections(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Subscription lifecycle")
	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Portal UI",
		DependsOn: []string{blocker.ID},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	// Forward direction on the ticket that declared the dependency.
	got, err := s.GetTicket(dependent.ID)
	if err != nil {
		t.Fatalf("GetTicket(dependent): %v", err)
	}
	if len(got.DependsOn) != 1 {
		t.Fatalf("dependsOn = %d refs, want 1", len(got.DependsOn))
	}
	ref := got.DependsOn[0]
	if ref.ID != blocker.ID {
		t.Fatalf("dependsOn[0].ID = %q, want %q", ref.ID, blocker.ID)
	}
	if ref.Key != "BILL-1" {
		t.Fatalf("dependsOn[0].Key = %q, want BILL-1", ref.Key)
	}
	if ref.Title != "Subscription lifecycle" {
		t.Fatalf("dependsOn[0].Title = %q", ref.Title)
	}
	if ref.Status != "todo" {
		t.Fatalf("dependsOn[0].Status = %q, want todo", ref.Status)
	}

	// Reverse direction appears on the blocker without any write to it.
	back, err := s.GetTicket(blocker.ID)
	if err != nil {
		t.Fatalf("GetTicket(blocker): %v", err)
	}
	if len(back.Blocks) != 1 || back.Blocks[0].ID != dependent.ID {
		t.Fatalf("blocks = %+v, want one ref to the dependent ticket", back.Blocks)
	}
	if len(back.DependsOn) != 0 {
		t.Fatalf("blocker should depend on nothing, got %d", len(back.DependsOn))
	}
}

func TestDependencyStatusReflectsBlocker(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker")
	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{blocker.ID},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	if _, err := s.MoveTicket(blocker.ID, models.MoveTicketRequest{Status: "done"}); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	got, err := s.GetTicket(dependent.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.DependsOn[0].Status != "done" {
		t.Fatalf("status = %q, want done", got.DependsOn[0].Status)
	}
}

func TestDeletingBlockerRemovesReverseLink(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker")
	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{blocker.ID},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	if err := s.DeleteTicket(blocker.ID); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}

	got, err := s.GetTicket(dependent.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if len(got.DependsOn) != 0 {
		t.Fatalf("dependsOn = %d, want 0 after the blocker was deleted", len(got.DependsOn))
	}
}

func TestLabelsResolveByNameAndAutoCreate(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Invoice export",
		Labels:    []string{"bug", "backend"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(tk.Labels) != 2 {
		t.Fatalf("labels = %d, want 2", len(tk.Labels))
	}

	all, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("global labels = %d, want 2", len(all))
	}
	for _, l := range all {
		if l.Color != "#6B7280" {
			t.Fatalf("auto-created label %q color = %q, want #6B7280", l.Name, l.Color)
		}
	}
}

func TestLabelMatchingIsCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "First", Labels: []string{"Backend"},
	}); err != nil {
		t.Fatalf("CreateTicket(first): %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Second", Labels: []string{"backend"},
	}); err != nil {
		t.Fatalf("CreateTicket(second): %v", err)
	}

	all, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("labels = %d, want 1; casing must not create a duplicate", len(all))
	}
	if all[0].Name != "Backend" {
		t.Fatalf("stored name = %q, want the original casing Backend", all[0].Name)
	}
}

func TestLabelUpdateReplaceSemantics(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "T", Labels: []string{"bug", "backend"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	// A nil slice leaves labels untouched.
	title := "Renamed"
	got, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title})
	if err != nil {
		t.Fatalf("UpdateTicket(title): %v", err)
	}
	if len(got.Labels) != 2 {
		t.Fatalf("nil labels cleared the set: got %d, want 2", len(got.Labels))
	}

	// A non-nil slice replaces the whole set.
	got, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Labels: []string{"docs"}})
	if err != nil {
		t.Fatalf("UpdateTicket(labels): %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "docs" {
		t.Fatalf("labels = %+v, want exactly [docs]", got.Labels)
	}

	// An empty non-nil slice clears the set.
	got, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Labels: []string{}})
	if err != nil {
		t.Fatalf("UpdateTicket(empty): %v", err)
	}
	if len(got.Labels) != 0 {
		t.Fatalf("labels = %d, want 0", len(got.Labels))
	}
}

func TestLabelMatchingIsUnicodeCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "First", Labels: []string{"Étude"},
	}); err != nil {
		t.Fatalf("CreateTicket(first): %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Second", Labels: []string{"étude"},
	}); err != nil {
		t.Fatalf("CreateTicket(second): %v", err)
	}

	all, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("labels = %d, want 1; unicode casing must not create a duplicate", len(all))
	}
	if all[0].Name != "Étude" {
		t.Fatalf("stored name = %q, want the original casing Étude", all[0].Name)
	}
}

func TestDependencyResolvesByDisplayKey(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker") // BILL-1

	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{"BILL-1"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(dependent.DependsOn) != 1 || dependent.DependsOn[0].ID != blocker.ID {
		t.Fatalf("dependsOn = %+v, want one ref to %s", dependent.DependsOn, blocker.ID)
	}
}

func TestDependencyKeyIsCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "Blocker")

	got, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{"bill-1"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(got.DependsOn) != 1 {
		t.Fatalf("lowercase key did not resolve: %+v", got.DependsOn)
	}
}

func TestDependencyCrossProject(t *testing.T) {
	s := newTestStore(t)
	billing := seedProject(t, s, "Billing", "BILL")
	auth := seedProject(t, s, "Auth", "AUTH")
	authTicket := seedTicket(t, s, auth.ID, "Login") // AUTH-1

	got, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: billing.ID, Title: "Portal", DependsOn: []string{"AUTH-1"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(got.DependsOn) != 1 || got.DependsOn[0].ID != authTicket.ID {
		t.Fatalf("cross-project dependency did not resolve: %+v", got.DependsOn)
	}
	if got.DependsOn[0].Key != "AUTH-1" {
		t.Fatalf("key = %q, want AUTH-1", got.DependsOn[0].Key)
	}
}

func TestDependencyUnresolvableIsAnError(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	_, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{"NOPE-42"},
	})
	if err == nil {
		t.Fatal("expected an error for an unresolvable dependency")
	}
	if !strings.Contains(err.Error(), "NOPE-42") {
		t.Fatalf("error %q must name the offending value NOPE-42", err)
	}
}

func TestDependencySelfReferenceIsAnError(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Solo")

	_, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{
		DependsOn: []string{tk.ID},
	})
	if err == nil {
		t.Fatal("expected an error for a self-dependency")
	}
	if !strings.Contains(err.Error(), "itself") {
		t.Fatalf("error %q should explain the self-reference", err)
	}
}

func TestDependencyDuplicatesCollapse(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker")

	got, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent",
		DependsOn: []string{blocker.ID, "BILL-1", blocker.ID},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(got.DependsOn) != 1 {
		t.Fatalf("dependsOn = %d, want 1 after duplicates collapse", len(got.DependsOn))
	}
}
