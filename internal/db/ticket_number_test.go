package db

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func lastTicketNumber(t *testing.T, q dbtx, projectID string) int {
	t.Helper()
	var n int
	if err := q.QueryRow(`SELECT last_ticket_number FROM projects WHERE id = ?`, projectID).Scan(&n); err != nil {
		t.Fatalf("reading last_ticket_number of %s: %v", projectID, err)
	}
	return n
}

// Migration 014 starts each project's counter at the highest number it has
// handed out so far that is still visible: MAX(number), or 0 for a project
// with no tickets. Existing tickets keep their numbers.
func TestMigrationSetsTicketNumberCounterFromMax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	legacy := openLegacyDB(t, path, "014_ticket_number_counter.sql")
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix) VALUES ('p1', 'Billing', 'BILL')`)
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix) VALUES ('p2', 'Search', 'SRCH')`)
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix) VALUES ('p3', 'Empty', 'EMPTY')`)
	// BILL-3 and BILL-4 were deleted: numbering has a gap below the max.
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, number, title) VALUES
		('b1', 'p1', 1, 'One'), ('b2', 'p1', 2, 'Two'), ('b5', 'p1', 5, 'Five'),
		('s7', 'p2', 7, 'Seven')`)
	if err := legacy.Close(); err != nil {
		t.Fatalf("closing legacy database: %v", err)
	}

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating legacy database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	for id, want := range map[string]int{"p1": 5, "p2": 7, "p3": 0} {
		if got := lastTicketNumber(t, database, id); got != want {
			t.Errorf("%s last_ticket_number = %d, want %d", id, got, want)
		}
	}

	for id, want := range map[string]int{"b1": 1, "b2": 2, "b5": 5, "s7": 7} {
		var got int
		if err := database.QueryRow(`SELECT number FROM tickets WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("reading number of %s: %v", id, err)
		}
		if got != want {
			t.Errorf("ticket %s number = %d after migration, want %d", id, got, want)
		}
	}

	// The next ticket in each project continues from the counter. (These
	// hand-written ids are not ULIDs, so the store resolves projects by
	// prefix here.)
	s := NewStore(database)
	for prefix, want := range map[string]int{"BILL": 6, "SRCH": 8, "EMPTY": 1} {
		tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: prefix, Title: "Next"})
		if err != nil {
			t.Fatalf("CreateTicket in %s: %v", prefix, err)
		}
		if tk.Number != want {
			t.Errorf("next ticket in %s = %d, want %d", prefix, tk.Number, want)
		}
	}
}

func TestNewProjectCounterStartsAtZero(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	if got := lastTicketNumber(t, s.db, p.ID); got != 0 {
		t.Fatalf("new project's last_ticket_number = %d, want 0", got)
	}
	if tk := seedTicket(t, s, p.ID, "First"); tk.Number != 1 {
		t.Fatalf("first ticket number = %d, want 1", tk.Number)
	}
}

// Deleting the project's latest ticket must not free its number: a display
// key names one ticket forever, so a stale link or commit reference never
// lands on a different ticket.
func TestDeletedTicketNumberIsNeverReused(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	one := seedTicket(t, s, p.ID, "One")
	two := seedTicket(t, s, p.ID, "Two")
	if two.Number != 2 {
		t.Fatalf("second ticket number = %d, want 2", two.Number)
	}
	if err := s.DeleteTicket(two.ID); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}

	three := seedTicket(t, s, p.ID, "Three")
	if three.Number != 3 {
		t.Fatalf("ticket created after deleting BILL-2 got number %d, want 3", three.Number)
	}
	if id, err := s.ResolveTicketID("BILL-2"); err == nil && id != "" {
		t.Fatalf("BILL-2 resolves to %s after its deletion, want nothing", id)
	}

	// Deleting every ticket still leaves the counter where it was.
	for _, id := range []string{one.ID, three.ID} {
		if err := s.DeleteTicket(id); err != nil {
			t.Fatalf("DeleteTicket: %v", err)
		}
	}
	if four := seedTicket(t, s, p.ID, "Four"); four.Number != 4 {
		t.Fatalf("ticket created in an emptied project got number %d, want 4", four.Number)
	}
}

// Each project counts on its own.
func TestTicketNumbersArePerProject(t *testing.T) {
	s := newTestStore(t)
	a := seedProject(t, s, "Billing", "BILL")
	b := seedProject(t, s, "Search", "SRCH")

	seedTicket(t, s, a.ID, "A1")
	seedTicket(t, s, a.ID, "A2")
	if tk := seedTicket(t, s, b.ID, "B1"); tk.Number != 1 {
		t.Fatalf("first ticket in a second project = %d, want 1", tk.Number)
	}
	if tk := seedTicket(t, s, a.ID, "A3"); tk.Number != 3 {
		t.Fatalf("third ticket in the first project = %d, want 3", tk.Number)
	}
}

// A create that fails after numbering rolls back with its transaction, so it
// burns no number.
func TestFailedCreateTicketDoesNotAdvanceCounter(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "One")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Bad", DependsOn: []string{"BILL-99"},
	}); err == nil {
		t.Fatal("CreateTicket with an unknown dependency succeeded")
	}
	if got := lastTicketNumber(t, s.db, p.ID); got != 1 {
		t.Fatalf("last_ticket_number after a failed create = %d, want 1", got)
	}
	if tk := seedTicket(t, s, p.ID, "Two"); tk.Number != 2 {
		t.Fatalf("ticket after a failed create = %d, want 2", tk.Number)
	}
}

// A ticket written without going through CreateTicket (an older binary still
// running against the migrated file numbers from MAX(number) and never
// touches the counter) must not make the next CreateTicket collide with it.
func TestCounterSkipsPastTicketsNumberedElsewhere(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "One")
	execOrFail(t, s.db, `INSERT INTO tickets (id, project_id, number, title) VALUES ('raw', ?, 2, 'Old binary')`, p.ID)

	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Next"})
	if err != nil {
		t.Fatalf("CreateTicket after an out-of-band ticket: %v", err)
	}
	if tk.Number != 3 {
		t.Fatalf("number = %d, want 3", tk.Number)
	}
}

// Every process opening the file (the web server and each MCP server) has
// its own pool, so creates race across connections, not just goroutines.
// Numbering must still hand out distinct, gapless numbers.
func TestConcurrentCreateTicketsGetDistinctNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")

	const stores = 4
	const perStore = 10
	var ss []*Store
	for i := 0; i < stores; i++ {
		database, err := OpenAt(path)
		if err != nil {
			t.Fatalf("opening store %d: %v", i, err)
		}
		t.Cleanup(func() { database.Close() })
		ss = append(ss, NewStore(database))
	}
	p := seedProject(t, ss[0], "Billing", "BILL")

	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[int]string{}
	errs := make(chan error, stores*perStore)
	start := make(chan struct{})
	for i, s := range ss {
		for j := 0; j < perStore; j++ {
			wg.Add(1)
			go func(s *Store, title string) {
				defer wg.Done()
				<-start
				tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: title})
				if err != nil {
					errs <- fmt.Errorf("%s: %w", title, err)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if prev, dup := seen[tk.Number]; dup {
					errs <- fmt.Errorf("number %d given to both %q and %q", tk.Number, prev, title)
					return
				}
				seen[tk.Number] = title
			}(s, fmt.Sprintf("s%d-t%d", i, j))
		}
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	total := stores * perStore
	if len(seen) != total {
		t.Fatalf("got %d distinct numbers, want %d", len(seen), total)
	}
	for n := 1; n <= total; n++ {
		if _, ok := seen[n]; !ok {
			t.Errorf("number %d was never handed out", n)
		}
	}
	if got := lastTicketNumber(t, ss[0].db, p.ID); got != total {
		t.Fatalf("last_ticket_number = %d, want %d", got, total)
	}
}
