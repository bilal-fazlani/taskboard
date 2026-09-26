package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
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
	if err := s.DeleteDocument(page.ID); err != nil {
		t.Fatalf("delete: %v", err)
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

func TestSearchDocumentTickets(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	other := seedProject(t, s, "Other", "OTH")
	named := seedTicket(t, s, p.ID, "Named")
	inText := seedTicket(t, s, p.ID, "In text")
	styled := seedTicket(t, s, p.ID, "Styled page")
	linked := seedTicket(t, s, p.ID, "Linked")
	seedTicket(t, s, p.ID, "No documents")
	elsewhere := seedTicket(t, s, other.ID, "Elsewhere")
	e := seedEpic(t, s, p.ID, "Launch")

	seedDocument(t, s, named.ID, "Storage plan", "nothing here")
	seedDocument(t, s, inText.ID, "Notes", "We compared the ÉTUDE results.")
	if _, err := s.CreateDocument(models.CreateDocumentRequest{
		TicketID: styled.ID, Name: "Report", Format: models.DocumentFormatHTML,
		Content: `<style>.a { color: red }</style><div class="wrapper"><p>Latency <b>bud</b>get</p><p>one</p><p>two</p></div>`,
	}); err != nil {
		t.Fatal(err)
	}
	seedDocument(t, s, linked.ID, "Links", "Read [the runbook](https://example.com/zebra) and ![a diagram](arch.png).")
	seedDocument(t, s, elsewhere.ID, "Storage", "")
	if _, err := s.CreateDocument(models.CreateDocumentRequest{EpicID: e.ID, Name: "Epic storage", Content: "étude giraffe"}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		q, project string
		want       []string
	}{
		// Display names, in any case, anywhere in a word.
		{"storage", "DOC", []string{named.ID}},
		{"  STORAGE ", "", sortedIDs(named.ID, elsewhere.ID)},
		{"plan.md", "DOC", []string{named.ID}},
		{"report.html", "doc", []string{styled.ID}},
		// Readable text, with a Unicode case fold.
		{"nothing here", "DOC", []string{named.ID}},
		{"étude", "doc", []string{inText.ID}},
		{"ÉTUDE", "DOC", []string{inText.ID}},
		// HTML: visible text, joined across inline tags, never markup.
		{"budget", "DOC", []string{styled.ID}},
		{"style", "DOC", []string{}},
		{"wrapper", "DOC", []string{}},
		{"color", "DOC", []string{}},
		{"onetwo", "DOC", []string{}},
		// Markdown: link and alt text, not their addresses.
		{"runbook", "DOC", []string{linked.ID}},
		{"diagram", "DOC", []string{linked.ID}},
		{"zebra", "DOC", []string{}},
		{"arch.png", "DOC", []string{}},
		// Epic documents are never searched.
		{"giraffe", "DOC", []string{}},
		{"epic storage", "", []string{}},
		{"absent", "DOC", []string{}},
		{"   ", "DOC", []string{}},
		{"storage", "NOPE", []string{}},
	} {
		got, err := s.SearchDocumentTickets(tc.q, tc.project)
		if err != nil {
			t.Fatalf("search %q: %v", tc.q, err)
		}
		if got == nil || !slices.Equal(got, tc.want) {
			t.Errorf("search %q in %q = %#v, want %#v", tc.q, tc.project, got, tc.want)
		}
	}

	// A ticket with several matching documents is listed once.
	seedDocument(t, s, named.ID, "Storage notes", "")
	got, err := s.SearchDocumentTickets("storage", "DOC")
	if err != nil || !slices.Equal(got, []string{named.ID}) {
		t.Fatalf("two matching documents: %#v, %v", got, err)
	}

	// The search follows a content save, and not the old text.
	content := "now about zebras"
	doc, err := s.ResolveDocumentRef(DocumentOwner{TicketID: inText.ID}, "Notes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateDocument(doc, models.UpdateDocumentRequest{Content: &content}); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string][]string{"étude": {}, "zebra": {inText.ID}} {
		got, err := s.SearchDocumentTickets(q, "DOC")
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("after the save, search %q = %#v, %v; want %#v", q, got, err, want)
		}
	}
}

func TestFoldCase(t *testing.T) {
	for _, pair := range [][2]string{
		{"ΟΔΟΣ", "οδος"}, {"ΟΔΟΣ", "οδοσ"}, {"οδος", "οδοσ"},
		{"ÉTUDE", "étude"}, {"Kelvin \u212a", "kELVIN k"}, {"ſtyle", "STYLE"},
	} {
		if foldCase(pair[0]) != foldCase(pair[1]) {
			t.Errorf("foldCase(%q) = %q, foldCase(%q) = %q; want equal", pair[0], foldCase(pair[0]), pair[1], foldCase(pair[1]))
		}
	}
	if foldCase("style") == foldCase("stylf") {
		t.Error("foldCase merged different letters")
	}
}

// Words ending in sigma match in every one of its forms, capital, medial and
// final, in document text and names alike, as they do in the browser.
func TestSearchDocumentTicketsFoldsSigma(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	upper := seedTicket(t, s, p.ID, "Upper")
	medial := seedTicket(t, s, p.ID, "Medial")
	final := seedTicket(t, s, p.ID, "Final")
	named := seedTicket(t, s, p.ID, "Named")
	accented := seedTicket(t, s, p.ID, "Accented")
	seedDocument(t, s, upper.ID, "Upper", "Η ΟΔΟΣ ΤΟΥ ΣΧΕΔΙΟΥ")
	seedDocument(t, s, medial.ID, "Medial", "η οδοσ του σχεδίου")
	seedDocument(t, s, final.ID, "Final", "η οδος του σχεδίου")
	seedDocument(t, s, named.ID, "Χάρτης ΟΔΟΣ", "")
	seedDocument(t, s, accented.ID, "Notes", "We compared the étude results.")

	all := sortedIDs(upper.ID, medial.ID, final.ID, named.ID)
	for _, q := range []string{"ΟΔΟΣ", "οδοσ", "οδος", "Οδος", "ΟΔΟς"} {
		got, err := s.SearchDocumentTickets(q, "DOC")
		if err != nil || !slices.Equal(got, all) {
			t.Errorf("search %q = %#v, %v; want %#v", q, got, err, all)
		}
	}
	for _, q := range []string{"ÉTUDE", "étude", "Étude"} {
		got, err := s.SearchDocumentTickets(q, "DOC")
		if err != nil || !slices.Equal(got, []string{accented.ID}) {
			t.Errorf("search %q = %#v, %v; want %#v", q, got, err, []string{accented.ID})
		}
	}
}

func sortedIDs(ids ...string) []string {
	slices.Sort(ids)
	return ids
}
