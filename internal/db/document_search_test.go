package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// storedText reads the readable text kept for a document, and the revision
// it was worked out from; ok is false when none is kept.
func storedText(t *testing.T, s *Store, id string) (text string, revision int, ok bool) {
	t.Helper()
	err := s.db.QueryRow("SELECT text, revision FROM document_search WHERE document_id = ?", id).Scan(&text, &revision)
	if err == sql.ErrNoRows {
		return "", 0, false
	}
	if err != nil {
		t.Fatalf("reading document text: %v", err)
	}
	return text, revision, true
}

func wantStoredText(t *testing.T, s *Store, id, text string, revision int) {
	t.Helper()
	got, rev, ok := storedText(t, s, id)
	if !ok || got != text || rev != revision {
		t.Fatalf("stored text = %q at revision %d (kept %v), want %q at %d", got, rev, ok, text, revision)
	}
}

func TestDocumentTextFollowsContentSaves(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")

	md := seedDocument(t, s, tk.ID, "Plan", "# Rollout\n\nSee [the runbook](https://example.com/style).")
	wantStoredText(t, s, md.ID, "Rollout\nSee the runbook.", 1)

	page, err := s.CreateDocument(models.CreateDocumentRequest{
		TicketID: tk.ID, Name: "Report", Format: models.DocumentFormatHTML,
		Content: "<style>p { color: red }</style><p>Load <b>te</b>st</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantStoredText(t, s, page.ID, "Load test", 1)

	// A content save replaces the text and moves it to the new revision.
	content := "Second **draft**"
	if _, err := s.UpdateDocument(md.ID, models.UpdateDocumentRequest{Content: &content}); err != nil {
		t.Fatal(err)
	}
	wantStoredText(t, s, md.ID, "Second draft", 2)

	// A rename leaves it alone.
	name := "Renamed"
	if _, err := s.UpdateDocument(md.ID, models.UpdateDocumentRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	wantStoredText(t, s, md.ID, "Second draft", 2)

	// A refused save leaves it alone too.
	stale, other := 1, "Stale"
	var conflict *ErrDocumentConflict
	if _, err := s.UpdateDocument(md.ID, models.UpdateDocumentRequest{Content: &other, ExpectedRevision: &stale}); err == nil || !errors.As(err, &conflict) {
		t.Fatalf("stale save: err = %v, want a conflict", err)
	}
	wantStoredText(t, s, md.ID, "Second draft", 2)

	// Saving to an unknown document is still (nil, nil).
	if d, err := s.UpdateDocument("nope", models.UpdateDocumentRequest{Content: &content}); d != nil || err != nil {
		t.Fatalf("unknown document: %v, %v", d, err)
	}

	// Epic documents keep their text as well.
	e := seedEpic(t, s, p.ID, "Launch")
	ed, err := s.CreateDocument(models.CreateDocumentRequest{EpicID: e.ID, Name: "Brief", Content: "_Epic_ brief"})
	if err != nil {
		t.Fatal(err)
	}
	wantStoredText(t, s, ed.ID, "Epic brief", 1)

	// The text goes with its document.
	if ok, err := s.DeleteDocument(page.ID); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if _, _, ok := storedText(t, s, page.ID); ok {
		t.Fatal("the deleted document's text was kept")
	}
}

// Documents written before the text was kept, or by an older build since,
// get their text the next time the database is opened.
func TestOpeningFillsMissingDocumentText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	database, err := OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(database)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	now := time.Now()

	// As an older build writes: a document with no text row, and a content
	// save that bumps the revision without touching the text.
	if _, err := s.db.Exec(`INSERT INTO documents (id, ticket_id, name, format, content, revision, created_at, updated_at)
		VALUES ('old', ?, 'Old', 'html', '<title>Tab</title><p>Legacy <i>page</i></p>', 3, ?, ?)`, tk.ID, now, now); err != nil {
		t.Fatal(err)
	}
	behind := seedDocument(t, s, tk.ID, "Behind", "first")
	if _, err := s.db.Exec(`UPDATE documents SET content = 'saved **later**', revision = revision + 1 WHERE id = ?`, behind.ID); err != nil {
		t.Fatal(err)
	}
	current := seedDocument(t, s, tk.ID, "Current", "kept")
	if _, err := s.db.Exec(`UPDATE document_search SET text = 'untouched' WHERE document_id = ?`, current.ID); err != nil {
		t.Fatal(err)
	}
	database.Close()

	database, err = OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s = NewStore(database)
	wantStoredText(t, s, "old", "Legacy page", 3)
	wantStoredText(t, s, behind.ID, "saved later", 2)
	// A document whose text is up to date is not parsed again.
	wantStoredText(t, s, current.ID, "untouched", 1)
}

// The open-time check must not read every document's content to find the
// ones without text: it compares revisions through an index.
func TestMissingTextCheckUsesTheIndex(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT d.id FROM documents d
		LEFT JOIN document_search s ON s.document_id = d.id
		WHERE s.revision IS NOT d.revision`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if len(plan) == 0 || plan[0] != "SCAN d USING COVERING INDEX idx_documents_revision" {
		t.Fatalf("query plan = %q", plan)
	}
}
