package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
)

// rawColumn reads a created_at/updated_at column's stored bytes directly.
// "|| ''" turns the result into a plain expression with no declared column
// type, so modernc.org/sqlite returns the stored bytes as they are instead
// of parsing them into a time.Time and reformatting that with Go's
// trailing-zero-trimming RFC3339Nano (what a bare "SELECT col" would do for
// a DATETIME column scanned into a *string): see conn.go's parseTime and
// convertAssign in the driver.
func rawColumn(t *testing.T, database *sql.DB, table, col, id string) string {
	t.Helper()
	var raw string
	if err := database.QueryRow(
		`SELECT `+col+` || '' FROM `+table+` WHERE id = ?`, id,
	).Scan(&raw); err != nil {
		t.Fatalf("reading raw %s.%s: %v", table, col, err)
	}
	return raw
}

// assertSortableFormat checks raw is exactly the fixed-width, sortable UTC
// layout every created_at/updated_at write uses: "2006-01-02T15:04:05.000000000Z",
// 30 bytes, always this length so a plain text ORDER BY/MAX() compares the
// same way the instants do.
func assertSortableFormat(t *testing.T, label, raw string) {
	t.Helper()
	const want = "2006-01-02T15:04:05.000000000Z"
	if len(raw) != len(want) {
		t.Errorf("%s = %q, want %d bytes (fixed width), got %d", label, raw, len(want), len(raw))
		return
	}
	if raw[10] != 'T' || raw[19] != '.' || raw[len(raw)-1] != 'Z' {
		t.Errorf("%s = %q, not the sortable UTC shape", label, raw)
		return
	}
	if _, err := time.Parse(want, raw); err != nil {
		t.Errorf("%s = %q does not parse as the sortable layout: %v", label, raw, err)
	}
}

// assertRawSortable reads and checks the format of one created_at/updated_at
// column in one call.
func assertRawSortable(t *testing.T, database *sql.DB, table, col, id string) string {
	t.Helper()
	raw := rawColumn(t, database, table, col, id)
	assertSortableFormat(t, table+"."+col, raw)
	return raw
}

// TestMigrationNormalizesLegacyTimestamps seeds every table that carries
// created_at/updated_at with rows in the shapes seen before this migration:
// Go's time.Time.String() with a local offset and zone name, the same with
// a trailing monotonic-clock reading, a row already stamped in UTC, a bare
// SQLite CURRENT_TIMESTAMP value with no offset at all, and three ISO 8601
// shapes (never written by this app, but not to be corrupted if ever seen:
// see the migration's shape-B branch and its review). After the database is
// opened (running migration 011), every row must read back as the correct
// instant, and the text actually stored must be the fixed-width sortable
// UTC format so a plain ORDER BY/MAX() compares correctly.
func TestMigrationNormalizesLegacyTimestamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	legacy := openLegacyDB(t, path, "011_sortable_timestamps.sql")
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix, created_at, updated_at) VALUES
		('p1', 'Billing', 'BILL',
		 '2024-01-01 12:00:00.123456789 +0100 CET m=+0.000123456',
		 '2024-01-01 12:00:00.123456789 +0100 CET m=+0.000123456')`)
	execOrFail(t, legacy, `INSERT INTO epics (id, project_id, name, created_at, updated_at) VALUES
		('e1', 'p1', 'Invoices',
		 '2024-06-15 23:59:59.5 -0700 PDT m=+12345.6789',
		 '2024-06-15 23:59:59.5 -0700 PDT m=+12345.6789')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, title, created_at, updated_at) VALUES
		('t1', 'p1', 'e1', 1, 'Old ticket',
		 '2024-01-01 12:00:00 +0000 UTC',
		 '2024-01-01 12:00:00 +0000 UTC')`)
	execOrFail(t, legacy, `INSERT INTO documents (id, ticket_id, name, format, content, revision, created_at, updated_at) VALUES
		('d1', 't1', 'notes', 'markdown', 'hello', 1,
		 '2024-01-01 12:00:00.000000001 +0530 IST',
		 '2024-01-01 12:00:00.000000001 +0530 IST')`)
	// Never actually produced by this app (created_at/updated_at are always
	// supplied explicitly), but CURRENT_TIMESTAMP's own bare text is a
	// plausible "other format" a very old row could carry.
	execOrFail(t, legacy, `INSERT INTO ticket_status_changes (id, ticket_id, from_status, to_status, created_at) VALUES
		('c1', 't1', '', 'todo', '2024-01-01 12:00:00')`)
	// ISO 8601 shapes: not written by this app, but a value that must be
	// read correctly, not corrupted, if ever present (see the migration's
	// "shape B" and the review that added these three rows).
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, title, created_at, updated_at) VALUES
		('iso1', 'p1', 'e1', 2, 'ISO with T and a colon offset',
		 '2026-09-23T10:25:50.765352+01:00', '2026-09-23T10:25:50.765352+01:00')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, title, created_at, updated_at) VALUES
		('iso2', 'p1', 'e1', 3, 'ISO with a space and a colon offset',
		 '2026-09-23 10:25:50.765352+01:00', '2026-09-23 10:25:50.765352+01:00')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, title, created_at, updated_at) VALUES
		('iso3', 'p1', 'e1', 4, 'ISO with Z and a one-digit fraction',
		 '2024-01-01T12:00:00.5Z', '2024-01-01T12:00:00.5Z')`)
	if err := legacy.Close(); err != nil {
		t.Fatalf("closing legacy database: %v", err)
	}

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating legacy database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewStore(database)

	assertExact := func(table, col, id, wantRaw string) {
		t.Helper()
		raw := rawColumn(t, database, table, col, id)
		if raw != wantRaw {
			t.Errorf("%s.%s raw text = %q, want %q", table, col, raw, wantRaw)
		}
		assertSortableFormat(t, table+"."+col, raw)
	}

	assertExact("projects", "created_at", "p1", "2024-01-01T11:00:00.123456789Z")
	assertExact("epics", "created_at", "e1", "2024-06-16T06:59:59.500000000Z")
	assertExact("tickets", "created_at", "t1", "2024-01-01T12:00:00.000000000Z")
	assertExact("documents", "created_at", "d1", "2024-01-01T06:30:00.000000001Z")
	assertExact("ticket_status_changes", "created_at", "c1", "2024-01-01T12:00:00.000000000Z")
	assertExact("tickets", "created_at", "iso1", "2026-09-23T09:25:50.765352000Z")
	assertExact("tickets", "created_at", "iso2", "2026-09-23T09:25:50.765352000Z")
	assertExact("tickets", "created_at", "iso3", "2024-01-01T12:00:00.500000000Z")

	// Reads still return correct times, not just correct-looking text.
	p, err := s.GetProject("p1")
	if err != nil || p == nil {
		t.Fatalf("GetProject: %v", err)
	}
	wantP := time.Date(2024, 1, 1, 11, 0, 0, 123456789, time.UTC)
	if !p.CreatedAt.Equal(wantP) {
		t.Errorf("project CreatedAt = %v, want %v", p.CreatedAt, wantP)
	}

	tk, err := s.GetTicket("t1")
	if err != nil || tk == nil {
		t.Fatalf("GetTicket: %v", err)
	}
	wantT := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	if !tk.CreatedAt.Equal(wantT) {
		t.Errorf("ticket CreatedAt = %v, want %v", tk.CreatedAt, wantT)
	}

	changes, err := s.ListStatusChanges("t1")
	if err != nil {
		t.Fatalf("ListStatusChanges: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("ListStatusChanges: got %d rows, want 1", len(changes))
	}
	wantC := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	if !changes[0].CreatedAt.Equal(wantC) {
		t.Errorf("status change CreatedAt = %v, want %v", changes[0].CreatedAt, wantC)
	}

	iso1, err := s.GetTicket("iso1")
	if err != nil || iso1 == nil {
		t.Fatalf("GetTicket iso1: %v", err)
	}
	wantISO1 := time.Date(2026, 9, 23, 9, 25, 50, 765352000, time.UTC)
	if !iso1.CreatedAt.Equal(wantISO1) {
		t.Errorf("iso1 CreatedAt = %v, want %v", iso1.CreatedAt, wantISO1)
	}
	iso3, err := s.GetTicket("iso3")
	if err != nil || iso3 == nil {
		t.Fatalf("GetTicket iso3: %v", err)
	}
	wantISO3 := time.Date(2024, 1, 1, 12, 0, 0, 500000000, time.UTC)
	if !iso3.CreatedAt.Equal(wantISO3) {
		t.Errorf("iso3 CreatedAt = %v, want %v", iso3.CreatedAt, wantISO3)
	}
}

// TestOrderingAcrossUTCLocalAndDSTBoundary reproduces the two ways the old
// text format sorted wrong: a row stamped in UTC compared against one
// stamped with a local offset (ACP-82's "older rows stamped in UTC" case),
// and two rows straddling a DST transition, where the same wall-clock hour
// occurs twice at different instants. Both pairs, and the lastActivityAt
// pair below, are chosen so the *old* raw text sorts or picks them
// backwards: run against a plain checkout of 65a6c31 (this project's
// state before this migration existed), every assertion in this test
// fails, which is how the DST pair below was corrected in review — the
// first version happened to still sort right as text. It seeds both
// pairs, and a same-status pair for an epic's lastActivityAt, as they
// would have been written before this migration, migrates, and checks
// that a plain SQL ORDER BY over created_at/updated_at, and the epic
// lastActivityAt aggregate (MAX(t.updated_at)), both agree with the real
// instants rather than the old text.
func TestOrderingAcrossUTCLocalAndDSTBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	legacy := openLegacyDB(t, path, "011_sortable_timestamps.sql")
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix) VALUES ('p1', 'Billing', 'BILL')`)
	execOrFail(t, legacy, `INSERT INTO epics (id, project_id, name) VALUES ('e1', 'p1', 'Invoices')`)

	// UTC/local boundary: A is genuinely earlier (23:00 UTC) than B
	// (2024-01-02 00:05 UTC), but B's local wall-clock hour (19) is less
	// than A's (23), so comparing the old raw text the wrong way round put
	// B first.
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, title, created_at, updated_at) VALUES
		('a', 'p1', 'e1', 1, 'A: UTC-stamped, earlier',
		 '2024-01-01 23:00:00 +0000 UTC', '2024-01-01 23:00:00 +0000 UTC')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, title, created_at, updated_at) VALUES
		('b', 'p1', 'e1', 2, 'B: local-stamped, later',
		 '2024-01-01 19:05:00 -0500 EST', '2024-01-01 19:05:00 -0500 EST')`)

	// DST boundary: the US fall-back on 2024-11-03 turns 01:59:59 PDT into
	// 01:00:00 PST, so both 01:50 PDT and 01:10 PST occur that morning, PDT
	// first. c (01:50 PDT, -0700 => 08:50 UTC) is genuinely 20 minutes
	// before d (01:10 PST, -0800 => 09:10 UTC), but as raw text
	// "01:10:00 -0800 PST" < "01:50:00 -0700 PDT" (the wall-clock digits
	// alone decide it), so the old text sorts d first — backwards.
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, title, created_at, updated_at) VALUES
		('c', 'p1', 'e1', 3, 'C: 01:50 PDT, earlier',
		 '2024-11-03 01:50:00 -0700 PDT', '2024-11-03 01:50:00 -0700 PDT')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, title, created_at, updated_at) VALUES
		('d', 'p1', 'e1', 4, 'D: 01:10 PST, later',
		 '2024-11-03 01:10:00 -0800 PST', '2024-11-03 01:10:00 -0800 PST')`)

	// A second epic, isolated from a/b/c/d, with two same-status tickets
	// (loadProgress's MAX(t.updated_at) groups by epic_id AND status, so the
	// bug only shows up within one such group): x is UTC-stamped and
	// genuinely earlier, y is local-stamped and genuinely later, but old
	// text comparison ranks x's "23:00:00" above y's "19:05:00" and would
	// report x, not y, as the epic's latest activity.
	execOrFail(t, legacy, `INSERT INTO epics (id, project_id, name) VALUES ('e2', 'p1', 'Renewals')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, status, title, created_at, updated_at) VALUES
		('x', 'p1', 'e2', 5, 'todo', 'X: UTC-stamped, earlier',
		 '2024-01-01 23:00:00 +0000 UTC', '2024-01-01 23:00:00 +0000 UTC')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, epic_id, number, status, title, created_at, updated_at) VALUES
		('y', 'p1', 'e2', 6, 'todo', 'Y: local-stamped, later',
		 '2024-01-01 19:05:00 -0500 EST', '2024-01-01 19:05:00 -0500 EST')`)
	if err := legacy.Close(); err != nil {
		t.Fatalf("closing legacy database: %v", err)
	}

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating legacy database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewStore(database)

	assertOrder := func(col string, wantOrder []string) {
		t.Helper()
		rows, err := database.Query(`SELECT id FROM tickets WHERE id IN (?, ?) ORDER BY `+col+` ASC`,
			wantOrder[0], wantOrder[1])
		if err != nil {
			t.Fatalf("ORDER BY %s: %v", col, err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scanning id: %v", err)
			}
			got = append(got, id)
		}
		if len(got) != 2 || got[0] != wantOrder[0] || got[1] != wantOrder[1] {
			t.Errorf("ORDER BY %s ASC = %v, want %v", col, got, wantOrder)
		}
	}
	assertOrder("created_at", []string{"a", "b"})
	assertOrder("updated_at", []string{"a", "b"})
	assertOrder("created_at", []string{"c", "d"})
	assertOrder("updated_at", []string{"c", "d"})

	// The epic's lastActivityAt (ACP-73) must be the latest ticket's real
	// instant: y (19:05 EST = 2024-01-02 00:05 UTC), not x, which the old
	// text MAX would have picked.
	epic, err := s.GetEpic("e2")
	if err != nil || epic == nil {
		t.Fatalf("GetEpic: %v", err)
	}
	if epic.LastActivityAt == nil {
		t.Fatal("epic LastActivityAt is nil, want the latest ticket's updated_at")
	}
	wantLatest := time.Date(2024, 1, 2, 0, 5, 0, 0, time.UTC)
	if !epic.LastActivityAt.Equal(wantLatest) {
		t.Errorf("epic LastActivityAt = %v, want %v", epic.LastActivityAt, wantLatest)
	}
}

// TestWritesUseSortableUTCFormat performs one write of every kind that
// touches a created_at/updated_at column — project, ticket and epic create
// and update, a status-changing move, a document create and update, an
// image create and replace, and the image-rename rewrite of a ticket's
// description and a document's content — and checks after each one that the
// column's raw stored text is the fixed-width sortable UTC format, not
// whatever the sqlite driver or Go's default time formatting would produce
// on their own.
func TestWritesUseSortableUTCFormat(t *testing.T) {
	s := newTestStore(t)
	database := s.db

	p := seedProject(t, s, "Billing", "BILL")
	assertRawSortable(t, database, "projects", "created_at", p.ID)
	assertRawSortable(t, database, "projects", "updated_at", p.ID)

	newName := "Billing Team"
	if _, err := s.UpdateProject(p.ID, models.UpdateProjectRequest{Name: &newName}); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	assertRawSortable(t, database, "projects", "updated_at", p.ID)

	tk := seedTicket(t, s, p.ID, "Invoice export")
	assertRawSortable(t, database, "tickets", "created_at", tk.ID)
	assertRawSortable(t, database, "tickets", "updated_at", tk.ID)
	// CreateTicket's birth row in ticket_status_changes.
	var birthID string
	if err := database.QueryRow(
		`SELECT id FROM ticket_status_changes WHERE ticket_id = ? ORDER BY rowid ASC LIMIT 1`, tk.ID,
	).Scan(&birthID); err != nil {
		t.Fatalf("reading birth status change: %v", err)
	}
	assertRawSortable(t, database, "ticket_status_changes", "created_at", birthID)

	newTitle := "Invoice export v2"
	if _, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &newTitle}); err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	assertRawSortable(t, database, "tickets", "updated_at", tk.ID)

	if _, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusInProgress}); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	assertRawSortable(t, database, "tickets", "updated_at", tk.ID)
	var moveID string
	if err := database.QueryRow(
		`SELECT id FROM ticket_status_changes WHERE ticket_id = ? AND to_status = ?`, tk.ID, models.StatusInProgress,
	).Scan(&moveID); err != nil {
		t.Fatalf("reading move status change: %v", err)
	}
	assertRawSortable(t, database, "ticket_status_changes", "created_at", moveID)

	epic, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	assertRawSortable(t, database, "epics", "created_at", epic.ID)
	assertRawSortable(t, database, "epics", "updated_at", epic.ID)

	newEpicName := "Launch v2"
	if _, err := s.UpdateEpic(epic.ID, models.UpdateEpicRequest{Name: &newEpicName}); err != nil {
		t.Fatalf("UpdateEpic: %v", err)
	}
	assertRawSortable(t, database, "epics", "updated_at", epic.ID)

	doc, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Notes", Format: "markdown", Content: "hello"})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	assertRawSortable(t, database, "documents", "created_at", doc.ID)
	assertRawSortable(t, database, "documents", "updated_at", doc.ID)

	newContent := "hello, updated"
	if _, err := s.UpdateDocument(doc.ID, models.UpdateDocumentRequest{Content: &newContent}); err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	assertRawSortable(t, database, "documents", "updated_at", doc.ID)

	img, err := s.CreateImageDocument(models.CreateImageRequest{
		TicketID: tk.ID, Name: "Screenshot", Format: "png", Data: imagedoctest.PNG(8, 8),
	})
	if err != nil {
		t.Fatalf("CreateImageDocument: %v", err)
	}
	assertRawSortable(t, database, "documents", "created_at", img.ID)
	assertRawSortable(t, database, "documents", "updated_at", img.ID)

	if _, err := s.ReplaceDocumentImage(img.ID, imagedoctest.PNG(20, 20), nil); err != nil {
		t.Fatalf("ReplaceDocumentImage: %v", err)
	}
	assertRawSortable(t, database, "documents", "updated_at", img.ID)

	// The image-rename rewrite: renaming an image whose name is referenced
	// from a ticket's description and from a markdown document rewrites
	// both, bumping their updated_at (internal/db/image_usage.go).
	f := seedImageText(t, s)
	renamed := "Login screen renamed"
	if _, err := s.UpdateDocument(f.image.ID, models.UpdateDocumentRequest{Name: &renamed}); err != nil {
		t.Fatalf("renaming image: %v", err)
	}
	assertRawSortable(t, database, "documents", "updated_at", f.image.ID)
	assertRawSortable(t, database, "tickets", "updated_at", f.ticket.ID)
	assertRawSortable(t, database, "documents", "updated_at", f.plan.ID)
}
