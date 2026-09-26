package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// execOrFail runs a statement that seeds or changes rows directly, below the
// store API, so a schema rule is exercised on its own.
func execOrFail(t *testing.T, q dbtx, query string, args ...any) {
	t.Helper()
	if _, err := q.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestMigrationAddsEpicsAndTicketEpic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// A database from before epics keeps its tickets, and they have no epic.
	legacy := openLegacyDB(t, path, "006_epics.sql")
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix) VALUES ('p1', 'Billing', 'BILL')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, number, title) VALUES ('t1', 'p1', 1, 'Old ticket')`)
	if err := legacy.Close(); err != nil {
		t.Fatalf("closing legacy database: %v", err)
	}

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating legacy database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	var epicID sql.NullString
	if err := database.QueryRow(`SELECT epic_id FROM tickets WHERE id = 't1'`).Scan(&epicID); err != nil {
		t.Fatalf("reading tickets.epic_id: %v", err)
	}
	if epicID.Valid {
		t.Fatalf("existing ticket's epic_id = %q, want NULL", epicID.String)
	}

	var n int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_tickets_epic_id'`,
	).Scan(&n); err != nil || n != 1 {
		t.Fatalf("idx_tickets_epic_id: count %d, err %v", n, err)
	}

	execOrFail(t, database, `INSERT INTO projects (id, name, prefix) VALUES ('p2', 'Search', 'SRCH')`)
	execOrFail(t, database, `INSERT INTO epics (id, project_id, name) VALUES ('e1', 'p1', 'Invoices')`)

	// The same name in another project is fine.
	execOrFail(t, database, `INSERT INTO epics (id, project_id, name) VALUES ('e2', 'p2', 'Invoices')`)

	// The same name in the same project, in any case, is not.
	if _, err := database.Exec(`INSERT INTO epics (id, project_id, name) VALUES ('e3', 'p1', 'INVOICES')`); err == nil {
		t.Fatal("a second epic named INVOICES in the same project was accepted")
	}

	// An epic must belong to a project that exists.
	if _, err := database.Exec(`INSERT INTO epics (id, project_id, name) VALUES ('e4', 'nope', 'Orphan')`); err == nil {
		t.Fatal("an epic in a missing project was accepted")
	}

	var desc string
	if err := database.QueryRow(`SELECT description FROM epics WHERE id = 'e1'`).Scan(&desc); err != nil || desc != "" {
		t.Fatalf("default description = %q, err %v; want empty", desc, err)
	}

	execOrFail(t, database, `UPDATE tickets SET epic_id = 'e1' WHERE id = 't1'`)
	if _, err := database.Exec(`UPDATE tickets SET epic_id = 'missing' WHERE id = 't1'`); err == nil {
		t.Fatal("a ticket pointing at a missing epic was accepted")
	}
}

func epicIDOf(t *testing.T, s *Store, ticketID string) sql.NullString {
	t.Helper()
	var id sql.NullString
	if err := s.db.QueryRow(`SELECT epic_id FROM tickets WHERE id = ?`, ticketID).Scan(&id); err != nil {
		t.Fatalf("reading epic_id of %s: %v", ticketID, err)
	}
	return id
}

func countRows(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("counting %q: %v", query, err)
	}
	return n
}

func TestDeletingEpicRowClearsItFromTickets(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	a := seedTicket(t, s, p.ID, "In the epic")
	b := seedTicket(t, s, p.ID, "Also in the epic")
	execOrFail(t, s.db, `INSERT INTO epics (id, project_id, name) VALUES ('e1', ?, 'Invoices')`, p.ID)
	execOrFail(t, s.db, `UPDATE tickets SET epic_id = 'e1' WHERE id IN (?, ?)`, a.ID, b.ID)

	execOrFail(t, s.db, `DELETE FROM epics WHERE id = 'e1'`)

	for _, id := range []string{a.ID, b.ID} {
		if got := epicIDOf(t, s, id); got.Valid {
			t.Fatalf("ticket %s still has epic %q after the epic was deleted", id, got.String)
		}
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM tickets WHERE project_id = ?`, p.ID); n != 2 {
		t.Fatalf("tickets = %d after deleting their epic, want 2 (they stay, without an epic)", n)
	}
}

func TestDeletingProjectDeletesItsEpics(t *testing.T) {
	s := newTestStore(t)
	doomed := seedProject(t, s, "Billing", "BILL")
	kept := seedProject(t, s, "Search", "SRCH")
	execOrFail(t, s.db, `INSERT INTO epics (id, project_id, name) VALUES ('e1', ?, 'Invoices'), ('e2', ?, 'Refunds'), ('e3', ?, 'Indexing')`,
		doomed.ID, doomed.ID, kept.ID)
	tk := seedTicket(t, s, doomed.ID, "In the epic")
	execOrFail(t, s.db, `UPDATE tickets SET epic_id = 'e1' WHERE id = ?`, tk.ID)

	if err := s.DeleteProject(doomed.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	if n := countRows(t, s, `SELECT COUNT(*) FROM epics WHERE project_id = ?`, doomed.ID); n != 0 {
		t.Fatalf("epics of the deleted project = %d, want 0", n)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM epics WHERE project_id = ?`, kept.ID); n != 1 {
		t.Fatalf("epics of the other project = %d, want 1", n)
	}
}

func seedEpic(t *testing.T, s *Store, projectRef, name string) *models.Epic {
	t.Helper()
	e, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: projectRef, Name: name})
	if err != nil {
		t.Fatalf("seeding epic %q: %v", name, err)
	}
	return e
}

func strPtr(v string) *string { return &v }

func assertInvalidInput(t *testing.T, err error, wantMsg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an ErrInvalidInput %q, got no error", wantMsg)
	}
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("error %v should be an ErrInvalidInput so the HTTP layer returns 400", err)
	}
	if wantMsg != "" && err.Error() != wantMsg {
		t.Fatalf("message = %q, want %q", err.Error(), wantMsg)
	}
}

func TestCreateAndGetEpic(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	created, err := s.CreateEpic(models.CreateEpicRequest{
		ProjectID: "bill", Name: "  Invoices  ", Description: "Everything about invoices",
	})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	if created.ID == "" || created.ProjectID != p.ID {
		t.Fatalf("created = %+v, want an id and project %s (resolved from the prefix)", created, p.ID)
	}
	if created.Name != "Invoices" {
		t.Fatalf("name = %q, want it stored trimmed", created.Name)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: %+v", created)
	}

	got, err := s.GetEpic(created.ID)
	if err != nil {
		t.Fatalf("GetEpic: %v", err)
	}
	if got == nil || got.Name != "Invoices" || got.Description != "Everything about invoices" || got.ProjectID != p.ID {
		t.Fatalf("GetEpic = %+v", got)
	}
}

func TestCreateEpicUnknownProjectIsInvalidInput(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: "NOPE", Name: "Invoices"})
	assertInvalidInput(t, err, "")
}

func TestGetEpicUnknownIDReturnsNil(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetEpic("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil || got != nil {
		t.Fatalf("GetEpic(unknown) = %+v, %v; want nil, nil", got, err)
	}
}

func TestListEpicsIsPerProjectAndByName(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	other := seedProject(t, s, "Search", "SRCH")
	seedEpic(t, s, p.ID, "refunds")
	seedEpic(t, s, p.ID, "Invoices")
	seedEpic(t, s, p.ID, "Accounts")
	seedEpic(t, s, other.ID, "Indexing")

	got, err := s.ListEpics("bill")
	if err != nil {
		t.Fatalf("ListEpics: %v", err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	want := []string{"Accounts", "Invoices", "refunds"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v (by name, ignoring case)", names, want)
		}
	}

	empty := seedProject(t, s, "Empty", "EMP")
	none, err := s.ListEpics(empty.ID)
	if err != nil {
		t.Fatalf("ListEpics on a project with no epics: %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Fatalf("ListEpics = %#v, want an empty, non-nil slice", none)
	}

	_, err = s.ListEpics("NOPE")
	assertInvalidInput(t, err, "")
}

func TestUpdateEpic(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	e, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Invoices", Description: "old"})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}

	renamed, err := s.UpdateEpic(e.ID, models.UpdateEpicRequest{Name: strPtr(" Billing runs ")})
	if err != nil {
		t.Fatalf("UpdateEpic name: %v", err)
	}
	if renamed.Name != "Billing runs" || renamed.Description != "old" {
		t.Fatalf("after rename = %+v, want trimmed new name and the description untouched", renamed)
	}
	if !renamed.UpdatedAt.After(e.UpdatedAt) {
		t.Fatalf("updatedAt %v did not move past %v", renamed.UpdatedAt, e.UpdatedAt)
	}

	described, err := s.UpdateEpic(e.ID, models.UpdateEpicRequest{Description: strPtr("")})
	if err != nil {
		t.Fatalf("UpdateEpic description: %v", err)
	}
	if described.Name != "Billing runs" || described.Description != "" {
		t.Fatalf("after clearing the description = %+v", described)
	}

	missing, err := s.UpdateEpic("01ARZ3NDEKTSV4RRFFQ69G5FAV", models.UpdateEpicRequest{Name: strPtr("x")})
	if err != nil || missing != nil {
		t.Fatalf("UpdateEpic(unknown) = %+v, %v; want nil, nil", missing, err)
	}
}

func TestDeleteEpicKeepsTicketsWithoutAnEpic(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	e := seedEpic(t, s, p.ID, "Invoices")
	a := seedTicket(t, s, p.ID, "In the epic")
	b := seedTicket(t, s, p.ID, "Also in the epic")
	c := seedTicket(t, s, p.ID, "No epic")
	execOrFail(t, s.db, `UPDATE tickets SET epic_id = ? WHERE id IN (?, ?)`, e.ID, a.ID, b.ID)

	cleared, err := s.DeleteEpic(e.ID)
	if err != nil {
		t.Fatalf("DeleteEpic: %v", err)
	}
	if cleared != 2 {
		t.Fatalf("cleared = %d, want 2", cleared)
	}
	if got, _ := s.GetEpic(e.ID); got != nil {
		t.Fatalf("epic still there after DeleteEpic: %+v", got)
	}
	for _, id := range []string{a.ID, b.ID, c.ID} {
		if got := epicIDOf(t, s, id); got.Valid {
			t.Fatalf("ticket %s still has epic %q", id, got.String)
		}
	}
}

// DeleteEpic of an unknown id reports ErrInvalidInput rather than silently
// succeeding with a cleared count of 0.
func TestDeleteEpicUnknownIDIsInvalidInput(t *testing.T) {
	s := newTestStore(t)
	_, err := s.DeleteEpic("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	assertInvalidInput(t, err, `epic not found: "01ARZ3NDEKTSV4RRFFQ69G5FAV"`)
}

func TestResolveEpicRefByIDOrName(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	e := seedEpic(t, s, p.ID, "Invoices")

	for _, ref := range []string{e.ID, "invoices", " INVOICES "} {
		got, err := s.ResolveEpicRef("bill", ref)
		if err != nil || got != e.ID {
			t.Fatalf("ResolveEpicRef(%q) = %q, %v; want %s", ref, got, err, e.ID)
		}
	}
	_, err := s.ResolveEpicRef(p.ID, "Refunds")
	assertInvalidInput(t, err, `This project has no epic called "Refunds".`)
	_, err = s.ResolveEpicRef(p.ID, "  ")
	assertInvalidInput(t, err, "Enter an epic name or id.")
}

func TestEpicNameMustNotBeEmpty(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	for _, name := range []string{"", "   ", "\t\n"} {
		_, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: name})
		assertInvalidInput(t, err, "Enter a name")
	}
	if n := countRows(t, s, "SELECT COUNT(*) FROM epics"); n != 0 {
		t.Fatalf("epics = %d after rejected creates, want 0", n)
	}

	e := seedEpic(t, s, p.ID, "Invoices")
	_, err := s.UpdateEpic(e.ID, models.UpdateEpicRequest{Name: strPtr("  ")})
	assertInvalidInput(t, err, "Enter a name")
	if got, _ := s.GetEpic(e.ID); got.Name != "Invoices" {
		t.Fatalf("name = %q after a rejected rename, want Invoices", got.Name)
	}
}

func TestEpicNameIsUniquePerProjectIgnoringCase(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	other := seedProject(t, s, "Search", "SRCH")
	seedEpic(t, s, p.ID, "Invoices")

	// The message names the epic as it is stored, not as it was typed.
	for _, name := range []string{"Invoices", "invoices", " INVOICES "} {
		_, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: name})
		assertInvalidInput(t, err, `This project already has an epic called "Invoices".`)
	}

	// Unicode case folding too, which SQLite's NOCASE would miss.
	seedEpic(t, s, p.ID, "Étude")
	_, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "étude"})
	assertInvalidInput(t, err, `This project already has an epic called "Étude".`)

	// Another project may use the name.
	if _, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: other.ID, Name: "invoices"}); err != nil {
		t.Fatalf("same name in another project: %v", err)
	}

	// Renaming onto another epic's name is rejected.
	refunds := seedEpic(t, s, p.ID, "Refunds")
	_, err = s.UpdateEpic(refunds.ID, models.UpdateEpicRequest{Name: strPtr("INVOICES")})
	assertInvalidInput(t, err, `This project already has an epic called "Invoices".`)

	// Renaming an epic to itself in different capitals is allowed.
	got, err := s.UpdateEpic(refunds.ID, models.UpdateEpicRequest{Name: strPtr("REFUNDS")})
	if err != nil {
		t.Fatalf("recasing an epic's own name: %v", err)
	}
	if got.Name != "REFUNDS" {
		t.Fatalf("name = %q, want REFUNDS", got.Name)
	}
}

func TestEpicNameNoneIsReserved(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	for _, name := range []string{"none", "None", " NONE "} {
		_, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: name})
		assertInvalidInput(t, err, `"none" is reserved for tickets without an epic.`)
	}
	e := seedEpic(t, s, p.ID, "Invoices")
	_, err := s.UpdateEpic(e.ID, models.UpdateEpicRequest{Name: strPtr("nOnE")})
	assertInvalidInput(t, err, `"none" is reserved for tickets without an epic.`)

	// A longer name that contains it is fine.
	if _, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "None yet"}); err != nil {
		t.Fatalf(`"None yet": %v`, err)
	}
}

func TestTicketEpicMustBeInTheTicketsProject(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	other := seedProject(t, s, "Search", "SRCH")
	elsewhere := seedEpic(t, s, other.ID, "Indexing")

	// By id: the epic exists, but in another project.
	_, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "x", Epic: &elsewhere.ID})
	assertInvalidInput(t, err, `Epic "Indexing" belongs to another project.`)

	// By name: names resolve within the ticket's project only.
	_, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "x", Epic: strPtr("Indexing")})
	assertInvalidInput(t, err, `This project has no epic called "Indexing".`)

	if n := countRows(t, s, "SELECT COUNT(*) FROM tickets"); n != 0 {
		t.Fatalf("tickets = %d after rejected creates, want 0", n)
	}

	tk := seedTicket(t, s, p.ID, "Invoice export")
	_, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Epic: &elsewhere.ID})
	assertInvalidInput(t, err, `Epic "Indexing" belongs to another project.`)
	_, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Epic: strPtr("Indexing")})
	assertInvalidInput(t, err, `This project has no epic called "Indexing".`)
	if got := epicIDOf(t, s, tk.ID); got.Valid {
		t.Fatalf("ticket got epic %q from a rejected update", got.String)
	}
}

func moveTo(t *testing.T, s *Store, ticketID, status string) *models.Ticket {
	t.Helper()
	tk, err := s.MoveTicket(ticketID, models.MoveTicketRequest{Status: status})
	if err != nil {
		t.Fatalf("MoveTicket %s to %s: %v", ticketID, status, err)
	}
	return tk
}

func seedTicketInEpic(t *testing.T, s *Store, projectID, epic, title string) *models.Ticket {
	t.Helper()
	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: projectID, Title: title, Epic: &epic})
	if err != nil {
		t.Fatalf("seeding ticket %q in epic %q: %v", title, epic, err)
	}
	return tk
}

func assertProgress(t *testing.T, context string, got models.EpicProgress, counts map[string]int, complete bool, last *time.Time) {
	t.Helper()
	total := 0
	for _, status := range models.Statuses {
		c, ok := got.Counts[status]
		if !ok {
			t.Fatalf("%s: counts %v have no %q entry", context, got.Counts, status)
		}
		if c != counts[status] {
			t.Fatalf("%s: counts = %v, want %v", context, got.Counts, counts)
		}
		total += counts[status]
	}
	if got.Total != total {
		t.Fatalf("%s: total = %d, want %d", context, got.Total, total)
	}
	if got.Complete != complete {
		t.Fatalf("%s: complete = %v, want %v", context, got.Complete, complete)
	}
	switch {
	case last == nil && got.LastActivityAt != nil:
		t.Fatalf("%s: lastActivityAt = %v, want nil", context, got.LastActivityAt)
	case last != nil && (got.LastActivityAt == nil || !got.LastActivityAt.Equal(*last)):
		t.Fatalf("%s: lastActivityAt = %v, want %v", context, got.LastActivityAt, *last)
	}
}

func epicNamed(t *testing.T, epics []models.Epic, name string) models.Epic {
	t.Helper()
	for _, e := range epics {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no epic %q in %+v", name, epics)
	return models.Epic{}
}

func TestEpicProgressCountsCompleteAndLastActivity(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	other := seedProject(t, s, "Search", "SRCH")
	seedEpic(t, s, p.ID, "Mixed")
	seedEpic(t, s, p.ID, "Finished")
	seedEpic(t, s, p.ID, "Empty")
	seedEpic(t, s, other.ID, "Mixed")

	seedTicketInEpic(t, s, p.ID, "Mixed", "m1")
	m2 := seedTicketInEpic(t, s, p.ID, "Mixed", "m2")
	m3 := seedTicketInEpic(t, s, p.ID, "Mixed", "m3")
	m4 := seedTicketInEpic(t, s, p.ID, "Mixed", "m4")
	f1 := seedTicketInEpic(t, s, p.ID, "Finished", "f1")
	f2 := seedTicketInEpic(t, s, p.ID, "Finished", "f2")
	seedTicket(t, s, p.ID, "loose todo")
	loose := seedTicket(t, s, p.ID, "loose done")
	// Another project's tickets never count here.
	seedTicketInEpic(t, s, other.ID, "Mixed", "elsewhere")

	moveTo(t, s, f1.ID, models.StatusDone)
	finishedLast := moveTo(t, s, f2.ID, models.StatusDone).UpdatedAt
	moveTo(t, s, m3.ID, models.StatusAgentReview)
	moveTo(t, s, m4.ID, models.StatusDone)
	// The latest activity in Mixed is in its in_progress group, touched after
	// its done ticket, so the latest must be taken across the status groups.
	mixedLast := moveTo(t, s, m2.ID, models.StatusInProgress).UpdatedAt
	looseLast := moveTo(t, s, loose.ID, models.StatusDone).UpdatedAt

	epics, err := s.ListEpics(p.ID)
	if err != nil {
		t.Fatalf("ListEpics: %v", err)
	}
	if len(epics) != 3 {
		t.Fatalf("epics = %d, want 3", len(epics))
	}
	assertProgress(t, "Mixed", epicNamed(t, epics, "Mixed").EpicProgress,
		map[string]int{models.StatusTodo: 1, models.StatusInProgress: 1, models.StatusAgentReview: 1, models.StatusDone: 1},
		false, &mixedLast)
	assertProgress(t, "Finished", epicNamed(t, epics, "Finished").EpicProgress,
		map[string]int{models.StatusDone: 2}, true, &finishedLast)
	assertProgress(t, "Empty", epicNamed(t, epics, "Empty").EpicProgress,
		map[string]int{}, false, nil)

	// GetEpic carries the same progress.
	finished, err := s.GetEpic(epicNamed(t, epics, "Finished").ID)
	if err != nil {
		t.Fatalf("GetEpic: %v", err)
	}
	assertProgress(t, "GetEpic Finished", finished.EpicProgress,
		map[string]int{models.StatusDone: 2}, true, &finishedLast)

	noEpic, err := s.NoEpicProgress("bill")
	if err != nil {
		t.Fatalf("NoEpicProgress: %v", err)
	}
	assertProgress(t, "No epic", *noEpic,
		map[string]int{models.StatusTodo: 1, models.StatusDone: 1}, false, &looseLast)

	// Reopening a ticket makes a complete epic incomplete again.
	moveTo(t, s, f1.ID, models.StatusTodo)
	finished, _ = s.GetEpic(finished.ID)
	if finished.Complete {
		t.Fatal("Finished is still complete after one of its tickets was reopened")
	}
}

func TestNoEpicProgressOfAProjectWithoutTickets(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	got, err := s.NoEpicProgress(p.ID)
	if err != nil {
		t.Fatalf("NoEpicProgress: %v", err)
	}
	assertProgress(t, "No epic, no tickets", *got, map[string]int{}, false, nil)

	_, err = s.NoEpicProgress("NOPE")
	assertInvalidInput(t, err, "")
}

// Rows written before epics, or by SQL defaults, carry CURRENT_TIMESTAMP text
// rather than the driver's format; the progress query must still read them.
func TestEpicProgressReadsDefaultTimestamps(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	e := seedEpic(t, s, p.ID, "Invoices")
	execOrFail(t, s.db, `INSERT INTO tickets (id, project_id, number, title, epic_id) VALUES ('t1', ?, 1, 'Raw', ?)`, p.ID, e.ID)

	got, err := s.GetEpic(e.ID)
	if err != nil {
		t.Fatalf("GetEpic: %v", err)
	}
	if got.Total != 1 || got.LastActivityAt == nil || got.LastActivityAt.IsZero() {
		t.Fatalf("progress = %+v, want one ticket with a last activity", got.EpicProgress)
	}
}

func assertTicketEpic(t *testing.T, context string, tk *models.Ticket, want *models.Epic) {
	t.Helper()
	switch {
	case want == nil && tk.Epic != nil:
		t.Fatalf("%s: epic = %+v, want none", context, *tk.Epic)
	case want != nil && tk.Epic == nil:
		t.Fatalf("%s: no epic, want %q", context, want.Name)
	case want != nil && (tk.Epic.ID != want.ID || tk.Epic.Name != want.Name):
		t.Fatalf("%s: epic = %+v, want {%s %s}", context, *tk.Epic, want.ID, want.Name)
	}
}

func TestTicketsCarryTheirEpic(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	e := seedEpic(t, s, p.ID, "Invoices")

	// By name, in any case, or by id.
	byName := seedTicketInEpic(t, s, p.ID, "iNvOiCeS", "by name")
	assertTicketEpic(t, "CreateTicket by name", byName, e)
	byID := seedTicketInEpic(t, s, "BILL", e.ID, "by id")
	assertTicketEpic(t, "CreateTicket by id", byID, e)

	without := seedTicket(t, s, p.ID, "no epic")
	assertTicketEpic(t, "CreateTicket without epic", without, nil)
	empty, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "empty epic", Epic: strPtr("")})
	if err != nil {
		t.Fatalf("CreateTicket with epic \"\": %v", err)
	}
	assertTicketEpic(t, `CreateTicket with epic ""`, empty, nil)

	got, err := s.GetTicket(byName.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	assertTicketEpic(t, "GetTicket", got, e)

	list, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	for _, tk := range list {
		switch tk.ID {
		case byName.ID, byID.ID:
			assertTicketEpic(t, "ListTickets "+tk.Title, &tk, e)
		default:
			assertTicketEpic(t, "ListTickets "+tk.Title, &tk, nil)
		}
	}

	// A rename shows on the ticket at once: the name is read, not copied.
	if _, err := s.UpdateEpic(e.ID, models.UpdateEpicRequest{Name: strPtr("Billing runs")}); err != nil {
		t.Fatalf("UpdateEpic: %v", err)
	}
	got, _ = s.GetTicket(byName.ID)
	if got.Epic == nil || got.Epic.Name != "Billing runs" {
		t.Fatalf("epic after rename = %+v, want Billing runs", got.Epic)
	}

	// Deleting the epic through the store leaves the ticket without one.
	if _, err := s.DeleteEpic(e.ID); err != nil {
		t.Fatalf("DeleteEpic: %v", err)
	}
	got, _ = s.GetTicket(byName.ID)
	assertTicketEpic(t, "after DeleteEpic", got, nil)
}

func TestTicketEpicFollowsTheDueDateContract(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	invoices := seedEpic(t, s, p.ID, "Invoices")
	refunds := seedEpic(t, s, p.ID, "Refunds")
	tk := seedTicketInEpic(t, s, p.ID, "Invoices", "Invoice export")

	// nil: an update that does not mention the epic leaves it alone.
	updated, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: strPtr("Renamed")})
	if err != nil {
		t.Fatalf("UpdateTicket without epic: %v", err)
	}
	assertTicketEpic(t, "nil epic", updated, invoices)

	// A name (any case) or an id moves it.
	updated, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Epic: strPtr(" REFUNDS ")})
	if err != nil {
		t.Fatalf("UpdateTicket epic by name: %v", err)
	}
	assertTicketEpic(t, "epic by name", updated, refunds)
	updated, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Epic: &invoices.ID})
	if err != nil {
		t.Fatalf("UpdateTicket epic by id: %v", err)
	}
	assertTicketEpic(t, "epic by id", updated, invoices)

	// Anything that matches no epic is rejected and applies nothing else.
	before := updated
	_, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: strPtr("Should not stick"), Epic: strPtr("Nope")})
	assertInvalidInput(t, err, `This project has no epic called "Nope".`)
	// Only spaces is not a clear: it is rejected, like a due date of spaces.
	_, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: strPtr("Should not stick"), Epic: strPtr("   ")})
	assertInvalidInput(t, err, "Enter an epic name or id.")
	got, _ := s.GetTicket(tk.ID)
	if got.Title != before.Title {
		t.Fatalf("title = %q after a rejected update, want %q", got.Title, before.Title)
	}
	assertTicketEpic(t, "after rejected updates", got, invoices)

	// "" clears it.
	updated, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Epic: strPtr("")})
	if err != nil {
		t.Fatalf("UpdateTicket epic \"\": %v", err)
	}
	assertTicketEpic(t, `epic ""`, updated, nil)
	if !updated.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("updatedAt %v did not move past %v", updated.UpdatedAt, before.UpdatedAt)
	}

	// "none", in any case, clears it too: it means no epic everywhere.
	if _, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Epic: &refunds.ID}); err != nil {
		t.Fatalf("UpdateTicket back into an epic: %v", err)
	}
	updated, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Epic: strPtr(" NONE ")})
	if err != nil {
		t.Fatalf("UpdateTicket epic NONE: %v", err)
	}
	assertTicketEpic(t, "epic NONE", updated, nil)

	// Unknown or only spaces on create is rejected too, and leaves no ticket.
	_, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "x", Epic: strPtr("Nope")})
	assertInvalidInput(t, err, `This project has no epic called "Nope".`)
	_, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "x", Epic: strPtr(" \t ")})
	assertInvalidInput(t, err, "Enter an epic name or id.")
	if n := countRows(t, s, "SELECT COUNT(*) FROM tickets"); n != 1 {
		t.Fatalf("tickets = %d, want 1 after rejected creates", n)
	}

	// "none" on create means no epic.
	created, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "none", Epic: strPtr("none")})
	if err != nil {
		t.Fatalf("CreateTicket with epic none: %v", err)
	}
	assertTicketEpic(t, "create with epic none", created, nil)
}

func ticketTitles(t *testing.T, s *Store, filter models.TicketFilter) map[string]bool {
	t.Helper()
	list, err := s.ListTickets(filter)
	if err != nil {
		t.Fatalf("ListTickets(%+v): %v", filter, err)
	}
	titles := map[string]bool{}
	for _, tk := range list {
		if titles[tk.Title] {
			t.Fatalf("ListTickets(%+v) returned %q twice", filter, tk.Title)
		}
		titles[tk.Title] = true
	}
	return titles
}

func assertTitles(t *testing.T, context string, got map[string]bool, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: tickets %v, want %v", context, got, want)
	}
	for _, w := range want {
		if !got[w] {
			t.Fatalf("%s: tickets %v, want %v", context, got, want)
		}
	}
}

func TestListTicketsFilterByEpic(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	other := seedProject(t, s, "Search", "SRCH")
	invoices := seedEpic(t, s, p.ID, "Invoices")
	seedEpic(t, s, p.ID, "Refunds")
	seedEpic(t, s, other.ID, "invoices")
	seedEpic(t, s, p.ID, "Étude")

	seedTicketInEpic(t, s, p.ID, "Invoices", "bill invoice")
	seedTicketInEpic(t, s, p.ID, "Refunds", "bill refund")
	seedTicketInEpic(t, s, p.ID, "Étude", "bill etude")
	seedTicket(t, s, p.ID, "bill loose")
	seedTicketInEpic(t, s, other.ID, "invoices", "search invoice")
	seedTicket(t, s, other.ID, "search loose")

	// A name, in any case, within a project.
	assertTitles(t, "BILL + INVOICES", ticketTitles(t, s, models.TicketFilter{ProjectID: "BILL", Epic: "INVOICES"}), "bill invoice")
	assertTitles(t, "BILL + ÉTUDE", ticketTitles(t, s, models.TicketFilter{ProjectID: p.ID, Epic: "ÉTUDE"}), "bill etude")
	// An id works too.
	assertTitles(t, "BILL + id", ticketTitles(t, s, models.TicketFilter{ProjectID: p.ID, Epic: invoices.ID}), "bill invoice")

	// "none", in any case, is the tickets without an epic.
	assertTitles(t, "BILL + none", ticketTitles(t, s, models.TicketFilter{ProjectID: p.ID, Epic: "none"}), "bill loose")
	assertTitles(t, "BILL + NONE", ticketTitles(t, s, models.TicketFilter{ProjectID: p.ID, Epic: " NONE "}), "bill loose")

	// Without a project, a name matches that epic in every project.
	assertTitles(t, "any project + invoices", ticketTitles(t, s, models.TicketFilter{Epic: "invoices"}), "bill invoice", "search invoice")
	assertTitles(t, "any project + none", ticketTitles(t, s, models.TicketFilter{Epic: "none"}), "bill loose", "search loose")

	// It combines with the other filters.
	assertTitles(t, "none + done", ticketTitles(t, s, models.TicketFilter{ProjectID: p.ID, Epic: "none", Statuses: []string{models.StatusDone}}))

	// An epic that matches nothing matches no tickets, rather than failing.
	assertTitles(t, "unknown epic", ticketTitles(t, s, models.TicketFilter{ProjectID: p.ID, Epic: "Nope"}))
	assertTitles(t, "other project's epic", ticketTitles(t, s, models.TicketFilter{ProjectID: other.ID, Epic: "Refunds"}))
}

func TestClearDataRemovesEpics(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedEpic(t, s, p.ID, "Invoices")
	seedTicketInEpic(t, s, p.ID, "Invoices", "in the epic")

	if err := s.ClearData(); err != nil {
		t.Fatalf("ClearData: %v", err)
	}
	if n := countRows(t, s, "SELECT COUNT(*) FROM epics"); n != 0 {
		t.Fatalf("epics = %d after ClearData, want 0", n)
	}
}
