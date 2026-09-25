package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// openLegacyDB builds a database at the state it would have been in just
// before migration `upTo` ran, so a migration's effect on existing data can be
// exercised rather than assumed.
func openLegacyDB(t *testing.T, path string, upTo string) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("opening legacy database: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("creating schema_migrations: %v", err)
	}

	entries, err := migrationsFS.ReadDir(migrations)
	if err != nil {
		t.Fatalf("reading migrations: %v", err)
	}
	applied := 0
	for _, entry := range entries {
		if entry.Name() >= upTo {
			break
		}
		content, err := migrationsFS.ReadFile(filepath.Join(migrations, entry.Name()))
		if err != nil {
			t.Fatalf("reading migration %s: %v", entry.Name(), err)
		}
		if _, err := raw.Exec(string(content)); err != nil {
			t.Fatalf("executing migration %s: %v", entry.Name(), err)
		}
		if _, err := raw.Exec("INSERT INTO schema_migrations (version) VALUES (?)", entry.Name()); err != nil {
			t.Fatalf("recording migration %s: %v", entry.Name(), err)
		}
		applied++
	}
	if applied == 0 {
		t.Fatalf("no migrations applied before %s; is the filename right?", upTo)
	}
	return raw
}

func TestMigrationBackfillsRepoColumnIntoTicketRepos(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	legacy := openLegacyDB(t, path, "005_ticket_repos.sql")
	if _, err := legacy.Exec(
		`INSERT INTO projects (id, name, prefix) VALUES ('p1', 'Billing', 'BILL')`,
	); err != nil {
		t.Fatalf("seeding legacy project: %v", err)
	}
	if _, err := legacy.Exec(
		`INSERT INTO tickets (id, project_id, number, title, repo)
		 VALUES ('t1', 'p1', 1, 'Has a repo', 'acme/billing-api'),
		        ('t2', 'p1', 2, 'Has no repo', '')`,
	); err != nil {
		t.Fatalf("seeding legacy tickets: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("closing legacy database: %v", err)
	}

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating legacy database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewStore(database)

	withRepo, err := s.GetTicket("t1")
	if err != nil {
		t.Fatalf("GetTicket t1: %v", err)
	}
	assertRepos(t, "backfilled ticket", withRepo.Repos, []string{"acme/billing-api"})

	withoutRepo, err := s.GetTicket("t2")
	if err != nil {
		t.Fatalf("GetTicket t2: %v", err)
	}
	assertRepos(t, "ticket with an empty repo", withoutRepo.Repos, nil)

	var col int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('tickets') WHERE name='repo'`,
	).Scan(&col); err != nil {
		t.Fatalf("querying pragma_table_info: %v", err)
	}
	if col != 0 {
		t.Fatal("tickets.repo still exists after migration 005")
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

// assertRepos compares a ticket's repos against the exact sequence wanted,
// order included: repos come back sorted by name.
func assertRepos(t *testing.T, context string, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s: repos = %v, want %v", context, got, want)
	}
}

func TestCreateTicketStoresMultipleRepos(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Invoice export",
		Repos:     []string{"acme/billing-web", "acme/billing-api"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	assertRepos(t, "after create", tk.Repos, []string{"acme/billing-api", "acme/billing-web"})
}

func TestGetTicketReturnsRepos(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Invoice export",
		Repos:     []string{"acme/billing-api", "acme/billing-web"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	assertRepos(t, "GetTicket", got.Repos, []string{"acme/billing-api", "acme/billing-web"})
}

func TestListTicketsReturnsRepos(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Invoice export",
		Repos:     []string{"acme/billing-api", "acme/billing-web"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListTickets returned %d tickets, want 1", len(got))
	}
	assertRepos(t, "ListTickets", got[0].Repos, []string{"acme/billing-api", "acme/billing-web"})
}

func TestCreateTicketTrimsAndDedupesRepos(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Invoice export",
		Repos:     []string{"  acme/billing-api  ", "acme/billing-api", "", "   "},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	assertRepos(t, "after create", tk.Repos, []string{"acme/billing-api"})
}

// Repos are matched exactly, unlike labels: two hosts can legitimately
// disagree about case, so acme/API is not acme/api.
func TestCreateTicketTreatsRepoCaseAsSignificant(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Invoice export",
		Repos:     []string{"acme/API", "acme/api"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	assertRepos(t, "after create", tk.Repos, []string{"acme/API", "acme/api"})
}

func TestUpdateTicketReplacesRepos(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Invoice export",
		Repos: []string{"acme/billing-api"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	updated, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{
		Repos: []string{"acme/billing-web", "acme/billing-worker"},
	})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	assertRepos(t, "after update", updated.Repos, []string{"acme/billing-web", "acme/billing-worker"})
}

func TestUpdateTicketWithNilReposLeavesThemAlone(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Invoice export",
		Repos: []string{"acme/billing-api", "acme/billing-web"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	title := "Renamed"
	untouched, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title})
	if err != nil {
		t.Fatalf("UpdateTicket (title only): %v", err)
	}
	assertRepos(t, "after unrelated update", untouched.Repos,
		[]string{"acme/billing-api", "acme/billing-web"})
}

func TestUpdateTicketWithEmptyReposClearsThem(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Invoice export",
		Repos: []string{"acme/billing-api"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	cleared, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Repos: []string{}})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	assertRepos(t, "after clearing", cleared.Repos, nil)
}

func TestCreateTicketParsesDueDate(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	due := "2026-10-01"
	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Invoice export", DueDate: &due,
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if tk.DueDate == nil {
		t.Fatal("DueDate = nil, want 2026-10-01")
	}
	if got := tk.DueDate.Format("2006-01-02"); got != due {
		t.Fatalf("DueDate = %q, want %q", got, due)
	}
}

// A malformed due date must reject the whole create rather than silently
// storing the ticket without one.
func TestCreateTicketRejectsMalformedDueDate(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	bad := "not-a-date"
	_, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Invoice export", DueDate: &bad,
	})
	if err == nil {
		t.Fatal("expected an error for a malformed due date")
	}
	if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput so the HTTP layer returns 400", err)
	}

	tickets, err := s.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(tickets) != 0 {
		t.Fatalf("tickets = %d, want 0; a rejected create must leave nothing behind", len(tickets))
	}
}

func TestUpdateTicketSetsDueDate(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice export")

	due := "2026-12-24"
	updated, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{DueDate: &due})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if updated.DueDate == nil {
		t.Fatal("DueDate = nil, want 2026-12-24")
	}
	if got := updated.DueDate.Format("2006-01-02"); got != due {
		t.Fatalf("DueDate = %q, want %q", got, due)
	}
}

// Omitting the field (a nil pointer) must leave an existing due date alone —
// this is the "omitted field = unchanged" contract that lets a caller send
// only the fields it actually edited, rather than the whole ticket.
func TestUpdateTicketWithNilDueDateLeavesItAlone(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	due := "2026-10-01"
	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Invoice export", DueDate: &due,
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	title := "Renamed"
	untouched, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title})
	if err != nil {
		t.Fatalf("UpdateTicket (title only): %v", err)
	}
	if untouched.DueDate == nil || untouched.DueDate.Format("2006-01-02") != due {
		t.Fatalf("DueDate = %v, want unchanged at %q", untouched.DueDate, due)
	}
}

// A pointer to an explicit empty string clears an existing due date, the
// only way the editor (or any other caller) can remove one.
func TestUpdateTicketWithEmptyDueDateClearsIt(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	due := "2026-10-01"
	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Invoice export", DueDate: &due,
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	empty := ""
	cleared, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{DueDate: &empty})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if cleared.DueDate != nil {
		t.Fatalf("DueDate = %v, want nil after clearing", cleared.DueDate)
	}
}

// A malformed due date must reject the whole update rather than silently
// leaving the previous value (or clearing it) in place.
func TestUpdateTicketRejectsMalformedDueDate(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	due := "2026-10-01"
	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Invoice export", DueDate: &due,
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	bad := "10/01/2026"
	_, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{DueDate: &bad})
	if err == nil {
		t.Fatal("expected an error for a malformed due date")
	}
	if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput so the HTTP layer returns 400", err)
	}

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.DueDate == nil || got.DueDate.Format("2006-01-02") != due {
		t.Fatalf("DueDate = %v, want unchanged at %q after a rejected update", got.DueDate, due)
	}
}

func TestListTicketsFilterByRepoMatchesOneOfSeveral(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Cross-cutting work",
		Repos: []string{"acme/billing-api", "acme/billing-web"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Worker work",
		Repos: []string{"acme/billing-worker"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := s.ListTickets(models.TicketFilter{Repo: "acme/billing-web"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Cross-cutting work" {
		t.Fatalf("repo filter returned %d tickets, want 1 (Cross-cutting work)", len(got))
	}
}

// A ticket that lists the same repo twice must appear once, not once per row
// of the join table.
func TestListTicketsFilterByRepoDoesNotDuplicateTickets(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "API work",
		Repos: []string{"acme/billing-api", "acme/billing-web"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := s.ListTickets(models.TicketFilter{Repo: "acme/billing-api"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("repo filter returned %d tickets, want 1", len(got))
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

func TestResolveTicketIDByULID(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice") // BILL-1

	id, err := s.ResolveTicketID(tk.ID)
	if err != nil {
		t.Fatalf("ResolveTicketID(ulid): %v", err)
	}
	if id != tk.ID {
		t.Fatalf("resolved id = %q, want %q", id, tk.ID)
	}
}

func TestResolveTicketIDByDisplayKeyIsCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice") // BILL-1

	for _, ref := range []string{"BILL-1", "bill-1", "Bill-1"} {
		id, err := s.ResolveTicketID(ref)
		if err != nil {
			t.Fatalf("ResolveTicketID(%q): %v", ref, err)
		}
		if id != tk.ID {
			t.Fatalf("ResolveTicketID(%q) = %q, want %q", ref, id, tk.ID)
		}
	}
}

func TestResolveTicketIDUnresolvableIsAnError(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.ResolveTicketID("NOPE-42"); err == nil {
		t.Fatal("expected an error for an unresolvable display key")
	}
	// A 26-character, Crockford-shaped string is treated as a ULID and looked
	// up as a literal id, not split as a display key.
	if _, err := s.ResolveTicketID("01ARZ3NDEKTSV4RRFFQ69G5FAV"); err == nil {
		t.Fatal("expected an error for a well-formed but unknown ULID")
	}
	if _, err := s.ResolveTicketID(""); err == nil {
		t.Fatal("expected an error for an empty ref")
	}
}

// The number half of a display key must be its canonical decimal form: a
// signed or zero-padded number must not resolve, even though strconv.Atoi
// would happily parse either.
func TestResolveTicketIDRejectsNonCanonicalNumber(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Glow", "GLOW")
	seedTicket(t, s, p.ID, "First") // GLOW-1

	for _, ref := range []string{"GLOW-+1", "GLOW-001", "GLOW-1.0", "GLOW-"} {
		if _, err := s.ResolveTicketID(ref); err == nil {
			t.Fatalf("ResolveTicketID(%q) resolved, want an error", ref)
		}
	}
}

func TestResolveProjectRefByIDOrPrefix(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	id, err := s.ResolveProjectRef(p.ID)
	if err != nil {
		t.Fatalf("ResolveProjectRef(id): %v", err)
	}
	if id != p.ID {
		t.Fatalf("resolved id = %q, want %q", id, p.ID)
	}

	for _, ref := range []string{"BILL", "bill", "Bill"} {
		id, err := s.ResolveProjectRef(ref)
		if err != nil {
			t.Fatalf("ResolveProjectRef(%q): %v", ref, err)
		}
		if id != p.ID {
			t.Fatalf("ResolveProjectRef(%q) = %q, want %q", ref, id, p.ID)
		}
	}
}

func TestResolveProjectRefUnknownIsAnError(t *testing.T) {
	s := newTestStore(t)

	_, err := s.ResolveProjectRef("NOPE")
	if err == nil {
		t.Fatal("expected an error for an unknown project prefix")
	}
	if !strings.Contains(err.Error(), "project not found") {
		t.Fatalf("error %q should say project not found", err)
	}
}

// projects.prefix is UNIQUE only under SQLite's default binary collation, so
// "GLOW" and "glow" can coexist as two different projects. Matching
// case-insensitively must prefer an exact case match when there is exactly
// one, and refuse to guess when there isn't — never silently resolve to
// whichever row the database happens to return first.
func TestResolveProjectRefPrefixCaseAmbiguity(t *testing.T) {
	s := newTestStore(t)
	upper := seedProject(t, s, "Glow Upper", "GLOW")
	lower := seedProject(t, s, "Glow Lower", "glow")

	if id, err := s.ResolveProjectRef("glow"); err != nil {
		t.Fatalf("ResolveProjectRef(glow): %v", err)
	} else if id != lower.ID {
		t.Fatalf("ResolveProjectRef(glow) = %q, want the exact-case match %q", id, lower.ID)
	}

	if id, err := s.ResolveProjectRef("GLOW"); err != nil {
		t.Fatalf("ResolveProjectRef(GLOW): %v", err)
	} else if id != upper.ID {
		t.Fatalf("ResolveProjectRef(GLOW) = %q, want the exact-case match %q", id, upper.ID)
	}

	_, err := s.ResolveProjectRef("Glow")
	if err == nil {
		t.Fatal("expected an error for a prefix matching two projects with no exact case match")
	}
	if !strings.Contains(err.Error(), "more than one project prefix") {
		t.Fatalf("error %q should explain the ambiguity", err)
	}
}

// The same ambiguity guard must apply to lookupTicketRef's prefix join, the
// pre-existing code resolveProjectRef was built on.
func TestResolveTicketIDPrefixCaseAmbiguity(t *testing.T) {
	s := newTestStore(t)
	upper := seedProject(t, s, "Glow Upper", "GLOW")
	lower := seedProject(t, s, "Glow Lower", "glow")
	upperTicket := seedTicket(t, s, upper.ID, "Upper first") // GLOW-1
	lowerTicket := seedTicket(t, s, lower.ID, "Lower first") // glow-1

	if id, err := s.ResolveTicketID("glow-1"); err != nil {
		t.Fatalf("ResolveTicketID(glow-1): %v", err)
	} else if id != lowerTicket.ID {
		t.Fatalf("ResolveTicketID(glow-1) = %q, want the exact-case match %q", id, lowerTicket.ID)
	}

	if id, err := s.ResolveTicketID("GLOW-1"); err != nil {
		t.Fatalf("ResolveTicketID(GLOW-1): %v", err)
	} else if id != upperTicket.ID {
		t.Fatalf("ResolveTicketID(GLOW-1) = %q, want the exact-case match %q", id, upperTicket.ID)
	}

	_, err := s.ResolveTicketID("Glow-1")
	if err == nil {
		t.Fatal("expected an error for a prefix matching two projects with no exact case match")
	}
	if !strings.Contains(err.Error(), "more than one project prefix") {
		t.Fatalf("error %q should explain the ambiguity", err)
	}
}

func TestCreateTicketAcceptsProjectPrefix(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: "bill", Title: "Invoice"})
	if err != nil {
		t.Fatalf("CreateTicket with project prefix: %v", err)
	}
	if tk.DisplayKey() != "BILL-1" {
		t.Fatalf("display key = %q, want BILL-1", tk.DisplayKey())
	}
}

func TestCreateTicketWithUnknownProjectIsAClearError(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: "NOPE", Title: "Invoice"})
	if err == nil {
		t.Fatal("expected an error for an unknown project")
	}
	if strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("error %q leaked a raw constraint failure instead of a clear message", err)
	}
	if !strings.Contains(err.Error(), "project not found") {
		t.Fatalf("error %q should say project not found", err)
	}
}

func TestListTicketsFilterByProjectPrefix(t *testing.T) {
	s := newTestStore(t)
	billing := seedProject(t, s, "Billing", "BILL")
	auth := seedProject(t, s, "Auth", "AUTH")
	seedTicket(t, s, billing.ID, "Invoice")
	seedTicket(t, s, auth.ID, "Login")

	tickets, err := s.ListTickets(models.TicketFilter{ProjectID: "bill"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(tickets) != 1 || tickets[0].Title != "Invoice" {
		t.Fatalf("tickets = %+v, want just Invoice", tickets)
	}
}

func TestListTicketsFilterByUnknownProjectIsEmptyNotError(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "Invoice")

	tickets, err := s.ListTickets(models.TicketFilter{ProjectID: "NOPE"})
	if err != nil {
		t.Fatalf("ListTickets with unknown project filter: %v", err)
	}
	if len(tickets) != 0 {
		t.Fatalf("tickets = %+v, want none for an unknown project filter", tickets)
	}
}

func TestGetBoardFilterByProjectPrefixEchoesResolvedID(t *testing.T) {
	s := newTestStore(t)
	billing := seedProject(t, s, "Billing", "BILL")
	auth := seedProject(t, s, "Auth", "AUTH")
	seedTicket(t, s, billing.ID, "Invoice")
	seedTicket(t, s, auth.ID, "Login")

	board, err := s.GetBoard("bill")
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if board.ProjectID != billing.ID {
		t.Fatalf("board.ProjectID = %q, want the resolved id %q, not the raw prefix", board.ProjectID, billing.ID)
	}
	var total int
	for _, col := range board.Columns {
		total += len(col.Tickets)
		for _, tk := range col.Tickets {
			if tk.ProjectID != billing.ID {
				t.Fatalf("board included a ticket from another project: %+v", tk)
			}
		}
	}
	if total != 1 {
		t.Fatalf("board carried %d tickets, want 1 scoped to BILL", total)
	}
}

func TestGetBoardFilterByUnknownProjectIsEmptyNotError(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "Invoice")

	board, err := s.GetBoard("NOPE")
	if err != nil {
		t.Fatalf("GetBoard with unknown project: %v", err)
	}
	if board.ProjectID != "" {
		t.Fatalf("board.ProjectID = %q, want empty for an unresolvable project", board.ProjectID)
	}
	for _, col := range board.Columns {
		if len(col.Tickets) != 0 {
			t.Fatalf("column %+v carried tickets for an unknown project", col)
		}
	}
}

func TestListTicketsCarriesLabelsAndDependenciesButNotBlocks(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker")
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Dependent",
		Labels:    []string{"frontend"},
		DependsOn: []string{blocker.ID},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	list, err := s.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("tickets = %d, want 2", len(list))
	}

	var dependent, blockerRow models.Ticket
	for _, tk := range list {
		switch tk.Title {
		case "Dependent":
			dependent = tk
		case "Blocker":
			blockerRow = tk
		}
	}

	if len(dependent.Labels) != 1 || dependent.Labels[0].Name != "frontend" {
		t.Fatalf("labels on list = %+v, want [frontend]", dependent.Labels)
	}
	if len(dependent.DependsOn) != 1 || dependent.DependsOn[0].Key != "BILL-1" {
		t.Fatalf("dependsOn on list = %+v, want [BILL-1]", dependent.DependsOn)
	}
	if len(blockerRow.Blocks) != 0 {
		t.Fatalf("blocks must not be populated on list results, got %+v", blockerRow.Blocks)
	}
}

func TestListTicketsFilterByLabel(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Tagged", Labels: []string{"bug"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Untagged",
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := s.ListTickets(models.TicketFilter{Label: "BUG"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Tagged" {
		t.Fatalf("label filter returned %d tickets, want 1 (Tagged)", len(got))
	}
}

func TestListTicketsEmptyResultDoesNotQuery(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatalf("ListTickets on an empty database: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("tickets = %d, want 0", len(got))
	}
}

func TestListLabelsIncludesTicketCount(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "One", Labels: []string{"bug", "backend"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Two", Labels: []string{"bug"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}

	counts := map[string]int{}
	for _, l := range labels {
		counts[l.Name] = l.TicketCount
	}
	if counts["bug"] != 2 {
		t.Fatalf("bug count = %d, want 2", counts["bug"])
	}
	if counts["backend"] != 1 {
		t.Fatalf("backend count = %d, want 1", counts["backend"])
	}
}

func TestDeletingLabelDetachesItFromTickets(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Tagged", Labels: []string{"bug"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if _, err := s.DeleteLabel(labels[0].ID); err != nil {
		t.Fatalf("DeleteLabel: %v", err)
	}

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if len(got.Labels) != 0 {
		t.Fatalf("labels = %d, want 0 after the label was deleted", len(got.Labels))
	}
}

// A create that fails resolution must leave nothing behind. Before the fix the
// ticket row and the auto-created label were already committed by the time the
// unresolvable dependency key produced the error the caller sees as a 400.
func TestFailedCreateTicketWritesNothing(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	_, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Half written",
		Labels:    []string{"newthing"},
		DependsOn: []string{"NOPE-9"},
	})
	if err == nil {
		t.Fatal("expected an error for an unresolvable dependency")
	}

	tickets, err := s.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(tickets) != 0 {
		t.Fatalf("tickets = %d, want 0; a failed create must not leave a ticket", len(tickets))
	}

	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("labels = %d, want 0; a failed create must not leave a label", len(labels))
	}
}

// The update equivalent: the scalar fields must not commit when the dependency
// list that came in the same request cannot be resolved.
func TestFailedUpdateTicketWritesNothing(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Original")

	newTitle := "Renamed"
	_, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{
		Title:     &newTitle,
		Labels:    []string{"newthing"},
		DependsOn: []string{"NOPE-9"},
	})
	if err == nil {
		t.Fatal("expected an error for an unresolvable dependency")
	}

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.Title != "Original" {
		t.Fatalf("title = %q, want %q; a failed update must not half-apply", got.Title, "Original")
	}
	if len(got.Labels) != 0 {
		t.Fatalf("labels = %d, want 0 after a failed update", len(got.Labels))
	}
	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("labels = %d, want 0; a failed update must not create one", len(labels))
	}
}

func TestCreateLabelRejectsCaseInsensitiveDuplicate(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	// Auto-created through a ticket, the way an agent would make it.
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Tagged", Labels: []string{"bug"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	if _, err := s.CreateLabel(models.CreateLabelRequest{Name: "Bug", Color: "#FF0000"}); err == nil {
		t.Fatal("expected an error creating Bug while bug exists")
	} else if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput so the HTTP layer returns 400", err)
	}

	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 1 {
		t.Fatalf("labels = %d, want 1; casing must not split one label in two", len(labels))
	}
}

func TestCreateLabelTrimsName(t *testing.T) {
	s := newTestStore(t)

	l, err := s.CreateLabel(models.CreateLabelRequest{Name: "  pad  ", Color: "#FF0000"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if l.Name != "pad" {
		t.Fatalf("name = %q, want %q; the padding must be trimmed before storing", l.Name, "pad")
	}

	// The trimmed name is what a later exact-name lookup and duplicate check see.
	id, err := s.ResolveLabelRef("pad")
	if err != nil {
		t.Fatalf("ResolveLabelRef: %v", err)
	}
	if id != l.ID {
		t.Fatalf("ResolveLabelRef(pad) = %q, want %q", id, l.ID)
	}

	if _, err := s.CreateLabel(models.CreateLabelRequest{Name: "Pad", Color: "#00FF00"}); err == nil {
		t.Fatal("expected an error creating Pad while the trimmed pad label exists")
	} else if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput", err)
	}
}

// A name that is blank after trimming must be rejected, matching UpdateLabel;
// otherwise HTTP POST and CLI `label create "   "` would silently store a
// label named "".
func TestCreateLabelRejectsBlankName(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateLabel(models.CreateLabelRequest{Name: "   ", Color: "#FF0000"}); err == nil {
		t.Fatal("expected an error for a blank name")
	} else if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput", err)
	}

	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("labels = %d, want 0; a rejected create must leave nothing behind", len(labels))
	}
}

func TestUpdateLabelRejectsRenameOntoAnotherName(t *testing.T) {
	s := newTestStore(t)

	bug, err := s.CreateLabel(models.CreateLabelRequest{Name: "bug", Color: "#FF0000"})
	if err != nil {
		t.Fatalf("CreateLabel(bug): %v", err)
	}
	chore, err := s.CreateLabel(models.CreateLabelRequest{Name: "chore", Color: "#00FF00"})
	if err != nil {
		t.Fatalf("CreateLabel(chore): %v", err)
	}

	name := "BUG"
	if _, err := s.UpdateLabel(chore.ID, models.UpdateLabelRequest{Name: &name}); err == nil {
		t.Fatal("expected an error renaming chore onto bug")
	} else if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput so the HTTP layer returns 400", err)
	}

	// Renaming a label onto its own name, differing only in case, still works.
	sameName := "Bug"
	updated, err := s.UpdateLabel(bug.ID, models.UpdateLabelRequest{Name: &sameName})
	if err != nil {
		t.Fatalf("renaming a label onto its own name: %v", err)
	}
	if updated == nil || updated.Name != "Bug" {
		t.Fatalf("updated label = %+v, want name Bug", updated)
	}
}

// A blank (or all-whitespace) name must not be saved: a label needs a
// readable name. This guards both UpdateLabel directly and the MCP/CLI
// callers that pass whatever the caller typed straight through.
func TestUpdateLabelRejectsBlankName(t *testing.T) {
	s := newTestStore(t)
	bug, err := s.CreateLabel(models.CreateLabelRequest{Name: "bug", Color: "#FF0000"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	blank := "   "
	if _, err := s.UpdateLabel(bug.ID, models.UpdateLabelRequest{Name: &blank}); err == nil {
		t.Fatal("expected an error for a blank name")
	} else if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput", err)
	}

	// The label is untouched by the rejected update.
	got, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(got) != 1 || got[0].Name != "bug" {
		t.Fatalf("labels = %+v, want unchanged [bug]", got)
	}

	// A name with surrounding whitespace is still accepted, and stored trimmed.
	padded := "  defect  "
	updated, err := s.UpdateLabel(bug.ID, models.UpdateLabelRequest{Name: &padded})
	if err != nil {
		t.Fatalf("UpdateLabel with padded name: %v", err)
	}
	if updated.Name != "defect" {
		t.Fatalf("name = %q, want trimmed %q", updated.Name, "defect")
	}
}

// A blank color means "not given", leaving the existing color untouched,
// rather than blanking it — distinct from a blank name, which is rejected.
func TestUpdateLabelBlankColorLeavesExistingColorUnchanged(t *testing.T) {
	s := newTestStore(t)
	bug, err := s.CreateLabel(models.CreateLabelRequest{Name: "bug", Color: "#FF0000"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	blank := "   "
	updated, err := s.UpdateLabel(bug.ID, models.UpdateLabelRequest{Color: &blank})
	if err != nil {
		t.Fatalf("UpdateLabel with blank color: %v", err)
	}
	if updated.Color != "#FF0000" {
		t.Fatalf("color = %q, want unchanged %q", updated.Color, "#FF0000")
	}
}

func TestDeleteLabelReportsDetachedTicketCount(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "One", Labels: []string{"bug"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Two", Labels: []string{"bug"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Three", Labels: []string{"chore"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	var bugID string
	for _, l := range labels {
		if l.Name == "bug" {
			bugID = l.ID
		}
	}
	if bugID == "" {
		t.Fatal("bug label not found")
	}

	count, err := s.DeleteLabel(bugID)
	if err != nil {
		t.Fatalf("DeleteLabel: %v", err)
	}
	if count != 2 {
		t.Fatalf("detached count = %d, want 2", count)
	}

	// chore still carries its one ticket.
	choreID, err := s.ResolveLabelRef("chore")
	if err != nil {
		t.Fatalf("ResolveLabelRef(chore): %v", err)
	}
	count, err = s.DeleteLabel(choreID)
	if err != nil {
		t.Fatalf("DeleteLabel(chore): %v", err)
	}
	if count != 1 {
		t.Fatalf("detached count = %d, want 1", count)
	}

	// A label with no tickets at all (and an unknown id) reports zero, not an error.
	count, err = s.DeleteLabel("MISSING")
	if err != nil {
		t.Fatalf("DeleteLabel(missing id): %v", err)
	}
	if count != 0 {
		t.Fatalf("detached count = %d, want 0 for an unknown label id", count)
	}
}

func TestResolveLabelRefByIDOrExactName(t *testing.T) {
	s := newTestStore(t)
	bug, err := s.CreateLabel(models.CreateLabelRequest{Name: "bug", Color: "#FF0000"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	// By id.
	id, err := s.ResolveLabelRef(bug.ID)
	if err != nil {
		t.Fatalf("ResolveLabelRef(id): %v", err)
	}
	if id != bug.ID {
		t.Fatalf("resolved id = %q, want %q", id, bug.ID)
	}

	// By exact name, case-insensitive, matching findLabelIDByName's behavior.
	id, err = s.ResolveLabelRef("BUG")
	if err != nil {
		t.Fatalf("ResolveLabelRef(name): %v", err)
	}
	if id != bug.ID {
		t.Fatalf("resolved id by name = %q, want %q", id, bug.ID)
	}

	// A substring or unrelated string matches nothing.
	id, err = s.ResolveLabelRef("bu")
	if err != nil {
		t.Fatalf("ResolveLabelRef(partial): %v", err)
	}
	if id != "" {
		t.Fatalf("resolved id for partial match = %q, want empty", id)
	}

	id, err = s.ResolveLabelRef("does-not-exist")
	if err != nil {
		t.Fatalf("ResolveLabelRef(missing): %v", err)
	}
	if id != "" {
		t.Fatalf("resolved id for missing ref = %q, want empty", id)
	}
}

func TestUpdateLabelUnknownIDReturnsNilWithoutError(t *testing.T) {
	s := newTestStore(t)

	name := "anything"
	l, err := s.UpdateLabel("MISSING", models.UpdateLabelRequest{Name: &name})
	if err != nil {
		t.Fatalf("UpdateLabel on an unknown id returned %v, want nil so the handler can send 404", err)
	}
	if l != nil {
		t.Fatalf("label = %+v, want nil for an unknown id", l)
	}
}

func TestListTicketsFilterByLabelIsUnicodeCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Tagged", Labels: []string{"Étude"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Untagged",
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	// SQLite's LOWER() leaves É alone, so all three spellings must go through
	// the same Go-side fold that resolveLabelNames uses.
	for _, name := range []string{"Étude", "étude", "ÉTUDE"} {
		got, err := s.ListTickets(models.TicketFilter{Label: name})
		if err != nil {
			t.Fatalf("ListTickets(label=%q): %v", name, err)
		}
		if len(got) != 1 || got[0].Title != "Tagged" {
			t.Fatalf("label filter %q returned %d tickets, want 1 (Tagged)", name, len(got))
		}
	}

	none, err := s.ListTickets(models.TicketFilter{Label: "nosuchlabel"})
	if err != nil {
		t.Fatalf("ListTickets(label=nosuchlabel): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("unknown label returned %d tickets, want 0", len(none))
	}
}

func TestDependencyResolvesKeyWithHyphenatedPrefix(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "My App", "MY-APP")
	blocker := seedTicket(t, s, p.ID, "Blocker") // MY-APP-1

	got, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{"MY-APP-1"},
	})
	if err != nil {
		t.Fatalf("CreateTicket with a hyphenated prefix key: %v", err)
	}
	if len(got.DependsOn) != 1 || got.DependsOn[0].ID != blocker.ID {
		t.Fatalf("dependsOn = %+v, want the MY-APP-1 blocker", got.DependsOn)
	}
}

// GetTicket must surface a failed relation read rather than quietly returning a
// ticket with no labels or no subtasks.
func TestGetTicketPropagatesRelationErrors(t *testing.T) {
	for _, table := range []string{"ticket_labels", "subtasks"} {
		t.Run(table, func(t *testing.T) {
			s := newTestStore(t)
			p := seedProject(t, s, "Billing", "BILL")
			tk := seedTicket(t, s, p.ID, "Solo")

			if _, err := s.db.Exec("DROP TABLE " + table); err != nil {
				t.Fatalf("dropping %s: %v", table, err)
			}
			if _, err := s.GetTicket(tk.ID); err == nil {
				t.Fatalf("GetTicket returned no error after %s became unreadable", table)
			}
		})
	}
}

// SQLite permits one writer at a time, and CreateTicket/UpdateTicket hold a
// transaction across label and dependency resolution. Without an explicit
// busy_timeout the driver installs no busy handler at all, so a second writer
// fails immediately with SQLITE_BUSY rather than waiting. This asserts the
// pragma in the DSN actually reaches the connection.
func TestBusyTimeoutIsSet(t *testing.T) {
	s := newTestStore(t)

	var ms int
	if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&ms); err != nil {
		t.Fatalf("querying busy_timeout: %v", err)
	}
	if ms != 5000 {
		t.Fatalf("busy_timeout = %d ms, want 5000; a blocked writer would fail instantly", ms)
	}
}

// A second connection must WAIT for an open write transaction instead of
// failing immediately. Without busy_timeout this write returns SQLITE_BUSY
// right away; with it, the write succeeds once the transaction commits.
func TestConcurrentWriterWaitsRatherThanFailing(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(
		"INSERT INTO labels (id, name, color) VALUES (?, ?, ?)", newID(), "holder", "#6B7280",
	); err != nil {
		tx.Rollback()
		t.Fatalf("write inside tx: %v", err)
	}

	// Release the lock shortly, from another goroutine, while the write below
	// is already blocked on it.
	done := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		done <- tx.Commit()
	}()

	start := time.Now()
	_, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Waits for the lock"})
	elapsed := time.Since(start)

	if commitErr := <-done; commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}
	if err != nil {
		t.Fatalf("second writer failed instead of waiting after %v: %v", elapsed, err)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("second writer returned in %v, so it never actually contended for the lock", elapsed)
	}
}

func TestMoveTicketToAgentReview(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Bounces to the review agent")

	moved, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusAgentReview})
	if err != nil {
		t.Fatalf("moving to agent_review: %v", err)
	}
	if moved.Status != models.StatusAgentReview {
		t.Fatalf("status after move = %q, want %q", moved.Status, models.StatusAgentReview)
	}

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("reading the moved ticket: %v", err)
	}
	if got.Status != models.StatusAgentReview {
		t.Fatalf("stored status = %q, want %q", got.Status, models.StatusAgentReview)
	}

	listed, err := s.ListTickets(models.TicketFilter{Status: models.StatusAgentReview})
	if err != nil {
		t.Fatalf("listing agent_review tickets: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != tk.ID {
		t.Fatalf("filtering by agent_review returned %d tickets, want just the moved one", len(listed))
	}
}

// An unknown status must reject the whole create rather than storing a
// ticket that no board column has, the same way a malformed due date is
// rejected.
func TestCreateTicketRejectsUnknownStatus(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	_, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Invoice export", Status: "bogus",
	})
	if err == nil {
		t.Fatal("expected an error for an unknown status")
	}
	if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput so the HTTP layer returns 400", err)
	}

	tickets, err := s.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(tickets) != 0 {
		t.Fatalf("tickets = %d, want 0; a rejected create must leave nothing behind", len(tickets))
	}
}

// An omitted status on create still defaults to todo, the behaviour from
// before this validation existed.
func TestCreateTicketWithNoStatusDefaultsToTodo(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Invoice export"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if tk.Status != models.StatusTodo {
		t.Fatalf("status = %q, want %q", tk.Status, models.StatusTodo)
	}
}

// An unknown status must reject the whole update rather than writing it,
// leaving the ticket's previous status in place.
func TestUpdateTicketRejectsUnknownStatus(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice export")

	bad := "bogus"
	_, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Status: &bad})
	if err == nil {
		t.Fatal("expected an error for an unknown status")
	}
	if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput so the HTTP layer returns 400", err)
	}

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.Status != models.StatusTodo {
		t.Fatalf("status = %q, want unchanged at %q", got.Status, models.StatusTodo)
	}
}

// An unknown status must reject the whole move rather than writing it or its
// history row.
func TestMoveTicketRejectsUnknownStatus(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice export")

	_, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: "bogus"})
	if err == nil {
		t.Fatal("expected an error for an unknown status")
	}
	if !errors.As(err, new(*ErrInvalidInput)) {
		t.Fatalf("error %v should be an ErrInvalidInput so the HTTP layer returns 400", err)
	}

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.Status != models.StatusTodo {
		t.Fatalf("status = %q, want unchanged at %q", got.Status, models.StatusTodo)
	}

	history, err := s.ListStatusChanges(tk.ID)
	if err != nil {
		t.Fatalf("ListStatusChanges: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history has %d entries, want just the creation row (a rejected move must write no history)", len(history))
	}
}

func TestGetBoardHasAColumnPerStatusInOrder(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Bounces to the review agent")
	if _, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusAgentReview}); err != nil {
		t.Fatalf("moving to agent_review: %v", err)
	}

	board, err := s.GetBoard(p.ID)
	if err != nil {
		t.Fatalf("reading the board: %v", err)
	}
	if len(board.Columns) != len(models.Statuses) {
		t.Fatalf("board has %d columns, want %d", len(board.Columns), len(models.Statuses))
	}
	for i, status := range models.Statuses {
		if board.Columns[i].Status != status {
			t.Fatalf("column %d is %q, want %q", i, board.Columns[i].Status, status)
		}
	}

	review := board.Columns[2]
	if review.Status != models.StatusAgentReview {
		t.Fatalf("third column is %q, want %q", review.Status, models.StatusAgentReview)
	}
	if len(review.Tickets) != 1 || review.Tickets[0].ID != tk.ID {
		t.Fatalf("agent_review column holds %d tickets, want just the moved one", len(review.Tickets))
	}
}

// SetSubtaskState must tick, untick, and tolerate a repeated call to the same
// state without error, unlike ToggleSubtask which would undo itself.
func TestSetSubtaskStateTicksAndUnticks(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice")
	st, err := s.AddSubtask(tk.ID, models.CreateSubtaskRequest{Title: "Step one"})
	if err != nil {
		t.Fatalf("AddSubtask: %v", err)
	}
	if st.Completed {
		t.Fatalf("new subtask started completed")
	}

	got, err := s.SetSubtaskState(st.ID, true)
	if err != nil {
		t.Fatalf("SetSubtaskState(true): %v", err)
	}
	if !got.Completed {
		t.Fatalf("completed = false after SetSubtaskState(true)")
	}

	// Repeating the same state is not an error.
	got, err = s.SetSubtaskState(st.ID, true)
	if err != nil {
		t.Fatalf("SetSubtaskState(true) repeated: %v", err)
	}
	if !got.Completed {
		t.Fatalf("completed = false after repeating SetSubtaskState(true)")
	}

	got, err = s.SetSubtaskState(st.ID, false)
	if err != nil {
		t.Fatalf("SetSubtaskState(false): %v", err)
	}
	if got.Completed {
		t.Fatalf("completed = true after SetSubtaskState(false)")
	}

	// Repeating false is also not an error.
	got, err = s.SetSubtaskState(st.ID, false)
	if err != nil {
		t.Fatalf("SetSubtaskState(false) repeated: %v", err)
	}
	if got.Completed {
		t.Fatalf("completed = true after repeating SetSubtaskState(false)")
	}
}

// A call that finds the subtask already at the target state must write
// nothing at all: PRAGMA data_version, which the live-refresh watcher polls,
// must not move. A call that actually changes the state must move it.
func TestSetSubtaskStateNoopMakesNoWrite(t *testing.T) {
	s, _, w := openWatched(t)
	ctx := context.Background()
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice")
	st, err := s.AddSubtask(tk.ID, models.CreateSubtaskRequest{Title: "Step one"})
	if err != nil {
		t.Fatalf("AddSubtask: %v", err)
	}

	v0, err := w.dataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Already false: setting it false again must not write.
	if _, err := s.SetSubtaskState(st.ID, false); err != nil {
		t.Fatalf("SetSubtaskState(false) no-op: %v", err)
	}
	if v, _ := w.dataVersion(ctx); v != v0 {
		t.Fatalf("data_version moved on a no-op SetSubtaskState(false): %d -> %d", v0, v)
	}

	// An actual change must write.
	if _, err := s.SetSubtaskState(st.ID, true); err != nil {
		t.Fatalf("SetSubtaskState(true): %v", err)
	}
	v1, err := w.dataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v1 == v0 {
		t.Fatalf("data_version did not move when SetSubtaskState(true) actually changed the row")
	}

	// Already true: setting it true again must not write.
	if _, err := s.SetSubtaskState(st.ID, true); err != nil {
		t.Fatalf("SetSubtaskState(true) no-op: %v", err)
	}
	if v, _ := w.dataVersion(ctx); v != v1 {
		t.Fatalf("data_version moved on a no-op SetSubtaskState(true): %d -> %d", v1, v)
	}
}

// An unknown subtask id is a caller mistake, reported as an ErrInvalidInput
// rather than a bare sql.ErrNoRows, so any caller that checks the error type
// can tell it apart from an unexpected failure. POST /api/subtasks/{id}/toggle
// does not check the error type today (toggleSubtask in server.go answers
// every error with a 500 via writeError, unlike writeStoreError elsewhere),
// so this does not itself change what that route returns.
func TestSetSubtaskStateUnknownIDIsInvalidInput(t *testing.T) {
	s := newTestStore(t)
	_, err := s.SetSubtaskState("nope", true)
	assertInvalidInput(t, err, "")
}

// ToggleSubtask picked up the same clear-error treatment as SetSubtaskState
// when it was changed to share the "look the row up after writing" shape;
// an unknown id must report ErrInvalidInput here too, not a bare
// sql.ErrNoRows.
func TestToggleSubtaskUnknownIDIsInvalidInput(t *testing.T) {
	s := newTestStore(t)
	_, err := s.ToggleSubtask("nope")
	assertInvalidInput(t, err, "")
}
