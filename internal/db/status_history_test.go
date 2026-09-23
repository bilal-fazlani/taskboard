package db

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func TestMigrationCreatesTicketStatusChanges(t *testing.T) {
	s := newTestStore(t)

	cols := map[string]bool{}
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info('ticket_status_changes')`)
	if err != nil {
		t.Fatalf("reading table info: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	rows.Close()
	for _, want := range []string{"id", "ticket_id", "from_status", "to_status", "note", "created_at"} {
		if !cols[want] {
			t.Fatalf("ticket_status_changes has no %s column (has %v)", want, cols)
		}
	}
	if cols["actor"] {
		t.Fatal("ticket_status_changes has an actor column; agent identity adds it later")
	}

	var index int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_index_list('ticket_status_changes') il
		JOIN pragma_index_info(il.name) ii WHERE ii.name = 'ticket_id'`,
	).Scan(&index); err != nil {
		t.Fatalf("reading index list: %v", err)
	}
	if index == 0 {
		t.Fatal("ticket_status_changes has no index on ticket_id")
	}

	var onDelete string
	if err := s.db.QueryRow(
		`SELECT on_delete FROM pragma_foreign_key_list('ticket_status_changes') WHERE "table" = 'tickets'`,
	).Scan(&onDelete); err != nil {
		t.Fatalf("reading foreign keys: %v", err)
	}
	if onDelete != "CASCADE" {
		t.Fatalf("ticket_id's foreign key on delete = %q, want CASCADE", onDelete)
	}
}

func countStatusChanges(t *testing.T, s *Store, ticketID string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ticket_status_changes WHERE ticket_id = ?`, ticketID).Scan(&n); err != nil {
		t.Fatalf("counting status changes: %v", err)
	}
	return n
}

func TestDeletingTicketDeletesItsStatusChanges(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Goes away")
	if _, err := s.db.Exec(
		`INSERT INTO ticket_status_changes (id, ticket_id, from_status, to_status) VALUES (?, ?, 'todo', 'in_progress')`,
		newID(), tk.ID,
	); err != nil {
		t.Fatal(err)
	}
	if n := countStatusChanges(t, s, tk.ID); n == 0 {
		t.Fatal("expected status changes before the delete")
	}
	if err := s.DeleteTicket(tk.ID); err != nil {
		t.Fatal(err)
	}
	if n := countStatusChanges(t, s, tk.ID); n != 0 {
		t.Fatalf("%d status changes left after deleting the ticket, want 0", n)
	}
}

func TestMigrationDoesNotBackfillExistingTickets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openLegacyDB(t, path, "007_ticket_status_changes.sql")
	if _, err := legacy.Exec(`INSERT INTO projects (id, name, prefix) VALUES ('p1', 'Billing', 'BILL')`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO tickets (id, project_id, number, title, status) VALUES ('t1', 'p1', 1, 'Old', 'agent_review')`); err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating legacy database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewStore(database)

	if n := countStatusChanges(t, s, "t1"); n != 0 {
		t.Fatalf("existing ticket got %d status changes, want no backfill", n)
	}
}

func statusPtr(s string) *string { return &s }

// historyPairs renders a ticket's history newest first as "from>to:note".
func historyPairs(t *testing.T, s *Store, ticketID string) []string {
	t.Helper()
	changes, err := s.ListStatusChanges(ticketID)
	if err != nil {
		t.Fatalf("ListStatusChanges: %v", err)
	}
	out := make([]string, len(changes))
	for i, c := range changes {
		if c.TicketID != ticketID {
			t.Fatalf("change %d belongs to %s, want %s", i, c.TicketID, ticketID)
		}
		if c.CreatedAt.IsZero() {
			t.Fatalf("change %d has no time", i)
		}
		out[i] = c.FromStatus + ">" + c.ToStatus + ":" + c.Note
	}
	return out
}

func assertHistory(t *testing.T, s *Store, ticketID string, want ...string) {
	t.Helper()
	got := historyPairs(t, s, ticketID)
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("history (newest first) = %q, want %q", got, want)
	}
}

func TestCreateTicketWritesTheBirthRow(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")

	tk := seedTicket(t, s, p.ID, "Defaults to todo")
	assertHistory(t, s, tk.ID, ">todo:")

	review, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Born in review", Status: models.StatusAgentReview})
	if err != nil {
		t.Fatal(err)
	}
	assertHistory(t, s, review.ID, ">agent_review:")
}

func TestFailedCreateTicketWritesNoHistory(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	if _, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Bad", DependsOn: []string{"ACP-99"}}); err == nil {
		t.Fatal("expected an unresolvable dependency to fail")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ticket_status_changes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d history rows after a failed create, want 0", n)
	}
}

func TestMoveTicketWritesHistoryWithNote(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Moves")

	if _, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusInProgress}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusAgentReview, Note: "  ready for review  "}); err != nil {
		t.Fatal(err)
	}
	assertHistory(t, s, tk.ID,
		"in_progress>agent_review:ready for review",
		"todo>in_progress:",
		">todo:",
	)
}

func TestMoveTicketWithinAColumnWritesNoHistory(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Reordered")

	pos := 5.0
	moved, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusTodo, Position: &pos, Note: "just reordering"})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Position != pos {
		t.Fatalf("position = %v, want %v", moved.Position, pos)
	}
	assertHistory(t, s, tk.ID, ">todo:")
}

func TestMoveTicketUnknownIDReturnsNil(t *testing.T) {
	s := newTestStore(t)
	got, err := s.MoveTicket("01ARZ3NDEKTSV4RRFFQ69G5FAV", models.MoveTicketRequest{Status: models.StatusDone})
	if err != nil || got != nil {
		t.Fatalf("MoveTicket(unknown) = %v, %v; want nil, nil", got, err)
	}
}

func TestUpdateTicketWritesHistoryWithNote(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Updated")

	if _, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Status: statusPtr(models.StatusDone), Note: "shipped"}); err != nil {
		t.Fatal(err)
	}
	assertHistory(t, s, tk.ID, "todo>done:shipped", ">todo:")
}

func TestUpdateTicketWithoutStatusChangeWritesNoHistory(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Retitled")

	title := "Retitled again"
	if _, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title, Note: "ignored"}); err != nil {
		t.Fatal(err)
	}
	// Setting the status it already has is not a change either.
	if _, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Status: statusPtr(models.StatusTodo), Note: "still todo"}); err != nil {
		t.Fatal(err)
	}
	assertHistory(t, s, tk.ID, ">todo:")
}

// An update that does not set the status must not write back the status it
// read before its transaction began: a move made in between would be undone
// with no history row to show for it.
func TestUpdateTicketKeepsTheStatusItFindsInItsTransaction(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Moved underneath")
	// Simulate a move committed after UpdateTicket read the ticket by
	// changing the row directly, as the other writer would have.
	if _, err := s.db.Exec(`UPDATE tickets SET status = 'in_progress' WHERE id = ?`, tk.ID); err != nil {
		t.Fatal(err)
	}
	title := "New title"
	got, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusInProgress {
		t.Fatalf("status = %q, want in_progress", got.Status)
	}
	assertHistory(t, s, tk.ID, ">todo:")
}

func TestFailedUpdateTicketWritesNoHistory(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Rejected")
	bad := "not-a-date"
	if _, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Status: statusPtr(models.StatusDone), DueDate: &bad}); err == nil {
		t.Fatal("expected a malformed due date to fail")
	}
	assertHistory(t, s, tk.ID, ">todo:")
}

func TestListStatusChangesIsEmptyNotNil(t *testing.T) {
	s := newTestStore(t)
	changes, err := s.ListStatusChanges("nope")
	if err != nil {
		t.Fatal(err)
	}
	if changes == nil || len(changes) != 0 {
		t.Fatalf("changes = %#v, want an empty list", changes)
	}
}

// seedInReview returns a ticket that has just been moved into agent_review.
func seedInReview(t *testing.T, s *Store) *models.Ticket {
	t.Helper()
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Under review")
	if _, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusAgentReview}); err != nil {
		t.Fatal(err)
	}
	return tk
}

func assertNoteRequired(t *testing.T, err error) {
	t.Helper()
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v (%T), want an ErrInvalidInput", err, err)
	}
	if invalid.Msg != NoteRequiredLeavingReviewMsg {
		t.Fatalf("error = %q, want %q", invalid.Msg, NoteRequiredLeavingReviewMsg)
	}
}

func TestRequireNoteLeavingReviewOnMove(t *testing.T) {
	for _, to := range []string{models.StatusInProgress, models.StatusDone, models.StatusTodo} {
		for _, note := range []string{"", "   \n\t"} {
			s := newTestStore(t)
			tk := seedInReview(t, s)

			_, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: to, Note: note}, RequireNoteLeavingReview())
			assertNoteRequired(t, err)

			got, _ := s.GetTicket(tk.ID)
			if got.Status != models.StatusAgentReview {
				t.Fatalf("rejected move to %s left status %q, want agent_review", to, got.Status)
			}
			assertHistory(t, s, tk.ID, "todo>agent_review:", ">todo:")
		}
	}
}

func TestRequireNoteLeavingReviewOnMoveAcceptsANote(t *testing.T) {
	s := newTestStore(t)
	tk := seedInReview(t, s)
	got, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusInProgress, Note: "bounced: missing tests"}, RequireNoteLeavingReview())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusInProgress {
		t.Fatalf("status = %q, want in_progress", got.Status)
	}
	assertHistory(t, s, tk.ID, "agent_review>in_progress:bounced: missing tests", "todo>agent_review:", ">todo:")
}

func TestRequireNoteLeavingReviewOnlyAppliesLeavingReview(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Not in review")

	// Into review, and between other statuses, needs no note.
	for _, to := range []string{models.StatusInProgress, models.StatusAgentReview} {
		if _, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: to}, RequireNoteLeavingReview()); err != nil {
			t.Fatalf("move to %s: %v", to, err)
		}
	}
	// Staying in review (a reorder) is not leaving it.
	pos := 1.0
	if _, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusAgentReview, Position: &pos}, RequireNoteLeavingReview()); err != nil {
		t.Fatalf("reorder within agent_review: %v", err)
	}
	title := "Retitled in review"
	if _, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title}, RequireNoteLeavingReview()); err != nil {
		t.Fatalf("update without a status change: %v", err)
	}
}

func TestLeavingReviewWithoutTheOptionNeedsNoNote(t *testing.T) {
	s := newTestStore(t)
	tk := seedInReview(t, s)
	if _, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusDone}); err != nil {
		t.Fatalf("move without the option: %v", err)
	}
	assertHistory(t, s, tk.ID, "agent_review>done:", "todo>agent_review:", ">todo:")

	s2 := newTestStore(t)
	tk2 := seedInReview(t, s2)
	if _, err := s2.UpdateTicket(tk2.ID, models.UpdateTicketRequest{Status: statusPtr(models.StatusInProgress)}); err != nil {
		t.Fatalf("update without the option: %v", err)
	}
	assertHistory(t, s2, tk2.ID, "agent_review>in_progress:", "todo>agent_review:", ">todo:")
}

func TestRequireNoteLeavingReviewOnUpdate(t *testing.T) {
	s := newTestStore(t)
	tk := seedInReview(t, s)

	title := "Also retitled"
	_, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title, Status: statusPtr(models.StatusDone), Note: "  "}, RequireNoteLeavingReview())
	assertNoteRequired(t, err)
	got, _ := s.GetTicket(tk.ID)
	if got.Status != models.StatusAgentReview || got.Title != tk.Title {
		t.Fatalf("rejected update applied something: status %q, title %q", got.Status, got.Title)
	}
	assertHistory(t, s, tk.ID, "todo>agent_review:", ">todo:")

	got, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Status: statusPtr(models.StatusDone), Note: "approved and landed"}, RequireNoteLeavingReview())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusDone {
		t.Fatalf("status = %q, want done", got.Status)
	}
	assertHistory(t, s, tk.ID, "agent_review>done:approved and landed", "todo>agent_review:", ">todo:")
}

// The rule compares against the status the ticket has inside the write's
// transaction, not one read earlier: a ticket moved into review by someone
// else just before the update still needs the note to leave it.
func TestRequireNoteLeavingReviewSeesTheCurrentStatus(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Moved underneath")
	if _, err := s.db.Exec(`UPDATE tickets SET status = 'agent_review' WHERE id = ?`, tk.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Status: statusPtr(models.StatusDone)}, RequireNoteLeavingReview())
	assertNoteRequired(t, err)
}

func TestReviewRoundsCountEntriesIntoAgentReview(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	never := seedTicket(t, s, p.ID, "Never reviewed")
	twice := seedTicket(t, s, p.ID, "Reviewed twice")
	born, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Born in review", Status: models.StatusAgentReview})
	if err != nil {
		t.Fatal(err)
	}

	for _, to := range []string{"in_progress", "agent_review", "in_progress", "agent_review", "done"} {
		if _, err := s.MoveTicket(twice.ID, models.MoveTicketRequest{Status: to}); err != nil {
			t.Fatal(err)
		}
	}
	// A reorder inside agent_review is not another round.
	pos := 3.0
	if _, err := s.MoveTicket(born.ID, models.MoveTicketRequest{Status: models.StatusAgentReview, Position: &pos}); err != nil {
		t.Fatal(err)
	}

	want := map[string]int{never.ID: 0, twice.ID: 2, born.ID: 1}

	for id, n := range want {
		got, err := s.GetTicket(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.ReviewRounds != n {
			t.Fatalf("GetTicket(%s).ReviewRounds = %d, want %d", got.Title, got.ReviewRounds, n)
		}
	}

	listed, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Fatalf("listed %d tickets, want 3", len(listed))
	}
	for _, tk := range listed {
		if tk.ReviewRounds != want[tk.ID] {
			t.Fatalf("ListTickets: %s has ReviewRounds %d, want %d", tk.Title, tk.ReviewRounds, want[tk.ID])
		}
	}

	board, err := s.GetBoard(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, col := range board.Columns {
		for _, tk := range col.Tickets {
			if tk.ReviewRounds != want[tk.ID] {
				t.Fatalf("GetBoard: %s has ReviewRounds %d, want %d", tk.Title, tk.ReviewRounds, want[tk.ID])
			}
		}
	}
}
