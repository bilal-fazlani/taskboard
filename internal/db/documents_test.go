package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func wantInvalid(t *testing.T, err error, msg string) {
	t.Helper()
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want ErrInvalidInput %q", err, msg)
	}
	if invalid.Msg != msg {
		t.Fatalf("message = %q, want %q", invalid.Msg, msg)
	}
}

func TestValidateDocumentName(t *testing.T) {
	for _, ok := range []struct{ raw, want string }{
		{"Design spec", "Design spec"},
		{"  padded  ", "padded"},
		{"api-design_v2", "api-design_v2"},
		{"Étude 2", "Étude 2"},
		{"設計", "設計"},
		{strings.Repeat("a", 200), strings.Repeat("a", 200)},
	} {
		got, err := validateDocumentName(ok.raw)
		if err != nil || got != ok.want {
			t.Errorf("validateDocumentName(%q) = %q, %v; want %q", ok.raw, got, err, ok.want)
		}
	}

	_, err := validateDocumentName("")
	wantInvalid(t, err, msgDocNameRequired)
	_, err = validateDocumentName("   ")
	wantInvalid(t, err, msgDocNameRequired)
	for _, bad := range []string{"plan.md", "a/b", "tab\there", "emoji 🙂", "semi;colon"} {
		_, err = validateDocumentName(bad)
		wantInvalid(t, err, msgDocNameChars)
	}
	_, err = validateDocumentName(strings.Repeat("a", 201))
	wantInvalid(t, err, msgDocNameTooLong)
}

func TestDocumentNameFromFilename(t *testing.T) {
	for _, tc := range []struct{ file, name string }{
		{"api-design_v2.md", "api-design_v2"},
		{"notes v1.2.md", "notes v1 2"},
		{"/tmp/x/Design spec.MD", "Design spec"},
		{"  spaced .md", "spaced"},
		{"-.md", "-"},
	} {
		name, format, err := DocumentNameFromFilename(tc.file)
		if err != nil || name != tc.name || format != models.DocumentFormatMarkdown {
			t.Errorf("DocumentNameFromFilename(%q) = %q, %q, %v; want %q, markdown", tc.file, name, format, err, tc.name)
		}
	}

	_, _, err := DocumentNameFromFilename("notes.txt")
	wantInvalid(t, err, msgDocExtension)
	_, _, err = DocumentNameFromFilename("README")
	wantInvalid(t, err, msgDocExtension)
	for _, tc := range []struct{ file, name string }{
		{"report.html", "report"},
		{"Load test.HTM", "Load test"},
		{"/tmp/x/Report.Html", "Report"},
	} {
		name, format, err := DocumentNameFromFilename(tc.file)
		if err != nil || name != tc.name || format != models.DocumentFormatHTML {
			t.Errorf("DocumentNameFromFilename(%q) = %q, %q, %v; want %q, html", tc.file, name, format, err, tc.name)
		}
	}
	if msgDocExtension != "Only .md, .html and .htm files can be attached." {
		t.Errorf("msgDocExtension = %q", msgDocExtension)
	}
	// Nothing left once the extension and symbols go.
	for _, empty := range []string{"%%.md", "...md", ".md"} {
		_, _, err = DocumentNameFromFilename(empty)
		wantInvalid(t, err, msgDocNameRequired)
	}
}

func seedDocument(t *testing.T, s *Store, ticketID, name, content string) *models.Document {
	t.Helper()
	d, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: ticketID, Name: name, Content: content})
	if err != nil {
		t.Fatalf("seeding document %q: %v", name, err)
	}
	return d
}

func TestCreateAndGetDocument(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")

	d := seedDocument(t, s, tk.ID, "  Design spec ", "# Hello\n")
	if d.Name != "Design spec" || d.Format != models.DocumentFormatMarkdown || d.TicketID != tk.ID {
		t.Fatalf("created = %+v", d.DocumentMeta)
	}
	if d.Size != len("# Hello\n") || d.Revision != 1 || d.Content != "# Hello\n" {
		t.Fatalf("size/revision/content = %d/%d/%q", d.Size, d.Revision, d.Content)
	}

	got, err := s.GetDocument(d.ID)
	if err != nil || got == nil || got.Content != "# Hello\n" {
		t.Fatalf("GetDocument = %+v, %v", got, err)
	}
	missing, err := s.GetDocument("nope")
	if err != nil || missing != nil {
		t.Fatalf("GetDocument(unknown) = %+v, %v; want nil, nil", missing, err)
	}
}

func TestCreateDocumentRules(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	other := seedTicket(t, s, p.ID, "Other")
	seedDocument(t, s, tk.ID, "Étude", "")

	_, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "étude"})
	wantInvalid(t, err, `This ticket already has a document called "Étude.md".`)
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "plan.md"})
	wantInvalid(t, err, msgDocNameChars)
	html, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Report", Format: models.DocumentFormatHTML, Content: "<h1>x</h1>"})
	if err != nil || html.Format != models.DocumentFormatHTML || html.Content != "<h1>x</h1>" {
		t.Fatalf("html create = %+v, %v", html, err)
	}
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Deck", Format: "pdf"})
	wantInvalid(t, err, `Format must be "markdown" or "html".`)
	// A name is taken whatever the format: Report.md cannot join Report.html.
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "report"})
	wantInvalid(t, err, `This ticket already has a document called "Report.html".`)
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Big", Content: strings.Repeat("x", models.MaxDocumentBytes+1)})
	wantInvalid(t, err, "This document is 8.1 MB. The limit is 8 MB.")
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: "nope", Name: "Plan"})
	wantInvalid(t, err, "ticket not found")

	// Exactly at the limit is fine, and another ticket may reuse a name.
	seedDocument(t, s, tk.ID, "Max", strings.Repeat("x", models.MaxDocumentBytes))
	seedDocument(t, s, other.ID, "Étude", "")
}

func TestListDocumentsInOrderAdded(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	seedDocument(t, s, tk.ID, "Zeta", "")
	seedDocument(t, s, tk.ID, "Alpha", "")

	docs, err := s.ListDocuments(DocumentOwner{TicketID: tk.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Name != "Zeta" || docs[1].Name != "Alpha" {
		t.Fatalf("order = %+v", docs)
	}

	empty, err := s.ListDocuments(DocumentOwner{TicketID: seedTicket(t, s, p.ID, "Bare").ID})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty list = %#v, %v; want [], nil", empty, err)
	}
	_, err = s.ListDocuments(DocumentOwner{TicketID: "nope"})
	wantInvalid(t, err, "ticket not found")
}

func TestResolveDocumentRef(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	d := seedDocument(t, s, tk.ID, "Design spec", "")
	owner := DocumentOwner{TicketID: tk.ID}

	for _, ref := range []string{d.ID, "Design spec", "design SPEC", "Design spec.md", " design spec.MD "} {
		id, err := s.ResolveDocumentRef(owner, ref)
		if err != nil || id != d.ID {
			t.Errorf("ResolveDocumentRef(%q) = %q, %v", ref, id, err)
		}
	}
	_, err := s.ResolveDocumentRef(owner, "Design spec.html")
	wantInvalid(t, err, `This ticket has no document called "Design spec.html".`)
	_, err = s.ResolveDocumentRef(owner, " ")
	wantInvalid(t, err, msgDocRefRequired)
}

func TestUpdateDocument(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	d := seedDocument(t, s, tk.ID, "Plan", "v1")
	seedDocument(t, s, tk.ID, "Notes", "")

	content := "v2"
	updated, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &content})
	if err != nil || updated.Content != "v2" || updated.Revision != 2 {
		t.Fatalf("content update = %+v, %v", updated, err)
	}
	if !updated.UpdatedAt.After(d.UpdatedAt) && !updated.UpdatedAt.Equal(d.UpdatedAt) {
		t.Fatalf("updatedAt went backwards")
	}

	// A rename keeps the revision; a new case of its own name is allowed.
	name := "PLAN"
	renamed, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Name: &name})
	if err != nil || renamed.Name != "PLAN" || renamed.Revision != 2 {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}

	taken := "notes"
	_, err = s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Name: &taken})
	wantInvalid(t, err, `This ticket already has a document called "Notes.md".`)
	big := strings.Repeat("x", models.MaxDocumentBytes+1)
	_, err = s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &big})
	wantInvalid(t, err, "This document is 8.1 MB. The limit is 8 MB.")
	_, err = s.UpdateDocument(d.ID, models.UpdateDocumentRequest{})
	wantInvalid(t, err, msgDocNothingToUpdate)

	missing, err := s.UpdateDocument("nope", models.UpdateDocumentRequest{Content: &content})
	if err != nil || missing != nil {
		t.Fatalf("update unknown = %+v, %v; want nil, nil", missing, err)
	}
}

func TestDeleteDocumentAndCascade(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	d := seedDocument(t, s, tk.ID, "Plan", "")
	kept := seedDocument(t, s, tk.ID, "Kept", "")

	deleted, err := s.DeleteDocument(d.ID)
	if err != nil || !deleted {
		t.Fatalf("DeleteDocument = %v, %v", deleted, err)
	}
	again, err := s.DeleteDocument(d.ID)
	if err != nil || again {
		t.Fatalf("second delete = %v, %v; want false, nil", again, err)
	}

	if err := s.DeleteTicket(tk.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetDocument(kept.ID); got != nil {
		t.Fatal("deleting the ticket should delete its documents")
	}
}

func TestTicketsCarryDocuments(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	bare := seedTicket(t, s, p.ID, "Bare")
	seedDocument(t, s, tk.ID, "One", "1")
	seedDocument(t, s, tk.ID, "Two", "22")

	list, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, x := range list {
		counts[x.ID] = x.DocumentCount
		if x.Documents != nil {
			t.Fatal("list payloads must not carry the document list")
		}
	}
	if counts[tk.ID] != 2 || counts[bare.ID] != 0 {
		t.Fatalf("counts = %v", counts)
	}

	full, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.DocumentCount != 2 || len(full.Documents) != 2 || full.Documents[1].Size != 2 {
		t.Fatalf("full ticket documents = %d %+v", full.DocumentCount, full.Documents)
	}

	if err := s.ClearData(); err != nil {
		t.Fatalf("ClearData with documents: %v", err)
	}
}

func TestUpdateDocumentRefusesAStaleRevision(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	d := seedDocument(t, s, tk.ID, "Plan", "v1")

	theirs := "theirs"
	if _, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &theirs}); err != nil {
		t.Fatal(err)
	}

	mine := "mine"
	stale := 1
	_, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &mine, ExpectedRevision: &stale})
	var conflict *ErrDocumentConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ErrDocumentConflict", err)
	}
	if conflict.Current.Content != "theirs" || conflict.Current.Revision != 2 {
		t.Fatalf("current = %+v", conflict.Current)
	}
	if got, _ := s.GetDocument(d.ID); got.Content != "theirs" {
		t.Fatalf("a refused save changed the content to %q", got.Content)
	}

	current := 2
	saved, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &mine, ExpectedRevision: &current})
	if err != nil || saved.Content != "mine" || saved.Revision != 3 {
		t.Fatalf("save at the current revision = %+v, %v", saved, err)
	}

	// A rename never conflicts: it does not change the content.
	name := "Plan v2"
	renamed, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Name: &name, ExpectedRevision: &stale})
	if err != nil || renamed.Name != "Plan v2" || renamed.Revision != 3 {
		t.Fatalf("rename with a stale revision = %+v, %v", renamed, err)
	}

	// A stale save to a document deleted meanwhile is still not found.
	if _, err := s.DeleteDocument(d.ID); err != nil {
		t.Fatal(err)
	}
	missing, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &mine, ExpectedRevision: &stale})
	if err != nil || missing != nil {
		t.Fatalf("stale save to a deleted document = %+v, %v; want nil, nil", missing, err)
	}
}
