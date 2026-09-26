package db

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

func appendEntry(t *testing.T, s *Store, projectRef, author, text string) *models.JournalEntry {
	t.Helper()
	e, err := s.AppendJournalEntry(projectRef, models.AppendJournalEntryRequest{Author: author, Text: text})
	if err != nil {
		t.Fatalf("appending %q: %v", text, err)
	}
	return e
}

func entryTexts(entries []models.JournalEntry) []string {
	texts := make([]string, len(entries))
	for i, e := range entries {
		texts[i] = e.Text
	}
	return texts
}

// An entry is stored with its project, author, text and the time it was
// written, author and text trimmed, and reads back the same.
func TestAppendJournalEntryStoresDateAndAuthor(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")

	before := time.Now().UTC()
	e := appendEntry(t, s, "acp", "  orchestrator  ", "\n Run started: 3 tickets. \n")
	after := time.Now().UTC()

	if e.ID == "" || e.ProjectID != p.ID {
		t.Fatalf("entry id %q project %q, want an id and project %s", e.ID, e.ProjectID, p.ID)
	}
	if e.Author != "orchestrator" || e.Text != "Run started: 3 tickets." {
		t.Fatalf("entry author %q text %q, want both trimmed", e.Author, e.Text)
	}
	if e.CreatedAt.Before(before) || e.CreatedAt.After(after) {
		t.Fatalf("entry createdAt %v, want between %v and %v", e.CreatedAt, before, after)
	}

	page, err := s.ListJournal(p.ID, "", JournalDefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("journal has %d entries, want 1", len(page.Entries))
	}
	got := page.Entries[0]
	if got.ID != e.ID || got.Author != e.Author || got.Text != e.Text || !got.CreatedAt.Equal(e.CreatedAt) {
		t.Fatalf("read back %+v, want %+v", got, *e)
	}
}

// Blank authors and texts, an overlong author and an unknown project are
// the caller's mistakes, and write nothing.
func TestAppendJournalEntryRejectsBadInput(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")

	cases := []struct {
		name, project, author, text, want string
	}{
		{"blank author", "ACP", "   ", "text", "author is required"},
		{"blank text", "ACP", "Bilal", " \n\t ", "text is required"},
		{"long author", "ACP", strings.Repeat("a", JournalAuthorMaxLength+1), "text", "at most 100"},
		{"unknown project", "NOPE", "Bilal", "text", "project not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.AppendJournalEntry(c.project, models.AppendJournalEntryRequest{Author: c.author, Text: c.text})
			var invalid *ErrInvalidInput
			if !errors.As(err, &invalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want an ErrInvalidInput containing %q", err, c.want)
			}
		})
	}

	page, err := s.ListJournal(p.ID, "", JournalDefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("journal has %d entries after rejected appends, want 0", page.Total)
	}
	// An author of exactly the limit, in characters rather than bytes, is fine.
	appendEntry(t, s, "ACP", strings.Repeat("é", JournalAuthorMaxLength), "text")
}

// Entries cannot be changed once written, whatever writes to the file; they
// go only with their project.
func TestJournalEntriesAreAppendOnly(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	other := seedProject(t, s, "Search", "SRCH")
	e := appendEntry(t, s, p.ID, "Bilal", "original")
	appendEntry(t, s, other.ID, "Bilal", "elsewhere")

	_, err := s.db.Exec(`UPDATE project_journal_entries SET text = 'rewritten' WHERE id = ?`, e.ID)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("UPDATE of an entry: error = %v, want it refused as append-only", err)
	}
	page, err := s.ListJournal(p.ID, "", JournalDefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	if got := entryTexts(page.Entries); len(got) != 1 || got[0] != "original" {
		t.Fatalf("entries after a refused UPDATE = %v, want [original]", got)
	}

	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM project_journal_entries WHERE project_id = ?`, p.ID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d entries left after deleting their project, want 0", left)
	}
	if page, err := s.ListJournal(other.ID, "", JournalDefaultLimit); err != nil || page.Total != 1 {
		t.Fatalf("other project's journal: total %d, err %v, want its 1 entry kept", page.Total, err)
	}

	if err := s.ClearData(); err != nil {
		t.Fatalf("ClearData with journal entries: %v", err)
	}
}

// A journal reads newest first, one project's entries only; entries written
// within the same instant come back later-written first.
func TestListJournalOrdersNewestFirst(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	other := seedProject(t, s, "Search", "SRCH")

	appendEntry(t, s, p.ID, "a", "first")
	appendEntry(t, s, other.ID, "a", "elsewhere")
	appendEntry(t, s, p.ID, "b", "second")
	// The store stamps "now", so pin two rows to one instant directly, later
	// than the rest, rather than trying to land two appends in one nanosecond.
	same := stamp(time.Now().Add(time.Second))
	for _, text := range []string{"tied, written first", "tied, written second"} {
		if _, err := s.db.Exec(
			`INSERT INTO project_journal_entries (id, project_id, author, text, created_at) VALUES (?, ?, 'c', ?, ?)`,
			newID(), p.ID, text, same,
		); err != nil {
			t.Fatal(err)
		}
	}

	page, err := s.ListJournal("acp", "", JournalDefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tied, written second", "tied, written first", "second", "first"}
	if got := entryTexts(page.Entries); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("journal = %v, want %v", got, want)
	}
	if page.Total != 4 || page.HasMore || page.NextBefore != "" {
		t.Fatalf("total %d hasMore %v nextBefore %q, want 4, false, empty", page.Total, page.HasMore, page.NextBefore)
	}
}

// Pages read one after another through nextBefore are the whole journal in
// order, each entry once, with the total on every page, even when entries
// are appended between reads, and ties are split across pages correctly.
func TestListJournalPages(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	for i := 0; i < 5; i++ {
		appendEntry(t, s, p.ID, "a", fmt.Sprintf("e%d", i))
	}
	// Two entries in one instant, so a page boundary falls between them.
	same := stamp(time.Now().Add(time.Second))
	for _, text := range []string{"e5", "e6"} {
		if _, err := s.db.Exec(
			`INSERT INTO project_journal_entries (id, project_id, author, text, created_at) VALUES (?, ?, 'a', ?, ?)`,
			newID(), p.ID, text, same,
		); err != nil {
			t.Fatal(err)
		}
	}

	first, err := s.ListJournal(p.ID, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := entryTexts(first.Entries); fmt.Sprint(got) != "[e6]" || !first.HasMore || first.NextBefore != first.Entries[0].ID {
		t.Fatalf("first page %v hasMore %v nextBefore %q, want [e6], true, its id", got, first.HasMore, first.NextBefore)
	}
	// Written after the first page was read, in a later instant: it belongs
	// at the top, and must not shift or repeat what the later pages hold.
	later := stamp(time.Now().Add(2 * time.Second))
	if _, err := s.db.Exec(
		`INSERT INTO project_journal_entries (id, project_id, author, text, created_at) VALUES (?, ?, 'a', 'e7', ?)`,
		newID(), p.ID, later,
	); err != nil {
		t.Fatal(err)
	}

	got := entryTexts(first.Entries)
	before := first.NextBefore
	var sizes []int
	for before != "" {
		page, err := s.ListJournal(p.ID, before, 3)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 8 {
			t.Fatalf("page before %s: total %d, want 8", before, page.Total)
		}
		if page.HasMore != (page.NextBefore != "") {
			t.Fatalf("page before %s: hasMore %v but nextBefore %q", before, page.HasMore, page.NextBefore)
		}
		sizes = append(sizes, len(page.Entries))
		got = append(got, entryTexts(page.Entries)...)
		before = page.NextBefore
	}
	if want := "[e6 e5 e4 e3 e2 e1 e0]"; fmt.Sprint(got) != want {
		t.Fatalf("paged journal = %v, want %s", got, want)
	}
	if fmt.Sprint(sizes) != "[3 3]" {
		t.Fatalf("page sizes %v, want [3 3]", sizes)
	}

	// A page that ends exactly at the oldest entry says there is no more.
	page, err := s.ListJournal(p.ID, "", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 8 || page.HasMore || page.NextBefore != "" {
		t.Fatalf("whole journal in one page: %d entries, hasMore %v, nextBefore %q", len(page.Entries), page.HasMore, page.NextBefore)
	}
	// Before the oldest entry there is nothing, and that is not an error.
	oldest := page.Entries[7].ID
	if page, err := s.ListJournal(p.ID, oldest, 3); err != nil || len(page.Entries) != 0 || page.HasMore {
		t.Fatalf("page before the oldest entry: %d entries, hasMore %v, err %v; want empty", len(page.Entries), page.HasMore, err)
	}
}

// A project with no journal has an empty page, not a null one; a bad limit,
// unknown before, another project's entry as before, or unknown project is
// the caller's mistake.
func TestListJournalEmptyAndBadInput(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	other := seedProject(t, s, "Search", "SRCH")
	elsewhere := appendEntry(t, s, other.ID, "a", "elsewhere")

	page, err := s.ListJournal(p.ID, "", JournalDefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	if page.Entries == nil || len(page.Entries) != 0 || page.Total != 0 || page.HasMore {
		t.Fatalf("empty journal = %+v, want an empty, non-nil page", page)
	}

	cases := []struct {
		name, project, before string
		limit                 int
		want                  string
	}{
		{"zero limit", p.ID, "", 0, "limit must be between 1 and 100"},
		{"limit too large", p.ID, "", JournalMaxLimit + 1, "limit must be between 1 and 100"},
		{"unknown before", p.ID, "01ZZZZZZZZZZZZZZZZZZZZZZZZ", 5, "is not an entry in this project's journal"},
		{"other project's entry", p.ID, elsewhere.ID, 5, "is not an entry in this project's journal"},
		{"unknown project", "NOPE", "", 5, "project not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.ListJournal(c.project, c.before, c.limit)
			var invalid *ErrInvalidInput
			if !errors.As(err, &invalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want an ErrInvalidInput containing %q", err, c.want)
			}
		})
	}
}

// A fresh database runs every migration in order, the journal's (016) after
// the ticket delivery one (015), and ends up with the journal table and its
// append-only trigger. A database already migrated through 015 gets the
// journal the next time it opens.
func TestMigrationsCreateJournalAfterTicketDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	database, err := OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`SELECT version FROM schema_migrations ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	var applied []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		applied = append(applied, v)
	}
	rows.Close()
	delivery, journal := -1, -1
	for i, v := range applied {
		switch v {
		case "015_ticket_delivery.sql":
			delivery = i
		case "016_project_journal.sql":
			journal = i
		}
	}
	if delivery < 0 || journal != delivery+1 {
		t.Fatalf("migrations applied %v: want 016_project_journal.sql right after 015_ticket_delivery.sql", applied)
	}
	assertJournalSchema(t, database)

	// Roll the file back to how 015 left it, then open it again.
	for _, stmt := range []string{
		`DROP TABLE project_journal_entries`,
		`DELETE FROM schema_migrations WHERE version = '016_project_journal.sql'`,
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	database.Close()
	reopened, err := OpenAt(path)
	if err != nil {
		t.Fatalf("reopening a database migrated through 015: %v", err)
	}
	defer reopened.Close()
	assertJournalSchema(t, reopened)
}

func assertJournalSchema(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, obj := range []struct{ kind, name string }{
		{"table", "project_journal_entries"},
		{"index", "idx_project_journal_entries_project"},
		{"trigger", "project_journal_entries_append_only"},
	} {
		var n int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, obj.kind, obj.name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s %s missing after migrating", obj.kind, obj.name)
		}
	}
}
