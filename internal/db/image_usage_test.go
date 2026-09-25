package db

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
)

func seedText(t *testing.T, s *Store, owner DocumentOwner, name, format, content string) *models.Document {
	t.Helper()
	d, err := s.CreateDocument(models.CreateDocumentRequest{
		TicketID: owner.TicketID, EpicID: owner.EpicID, Name: name, Format: format, Content: content,
	})
	if err != nil {
		t.Fatalf("seeding %q: %v", name, err)
	}
	return d
}

func mustDocument(t *testing.T, s *Store, id string) *models.Document {
	t.Helper()
	d, err := s.GetDocument(id)
	if err != nil || d == nil {
		t.Fatalf("GetDocument(%s) = %v, %v", id, d, err)
	}
	return d
}

func mustTicket(t *testing.T, s *Store, id string) *models.Ticket {
	t.Helper()
	tk, err := s.GetTicket(id)
	if err != nil || tk == nil {
		t.Fatalf("GetTicket(%s) = %v, %v", id, tk, err)
	}
	return tk
}

func description() models.ImagePlace { return models.ImagePlace{Kind: models.ImagePlaceDescription} }

func docPlace(d *models.Document) models.ImagePlace {
	return models.ImagePlace{Kind: models.ImagePlaceDocument, DocumentID: d.ID, Name: models.DocumentDisplayName(d.Name, d.Format)}
}

func wantPlaces(t *testing.T, what string, got []models.ImagePlace, want ...models.ImagePlace) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %+v, want %+v", what, got, want)
	}
}

// ticketWithImageText seeds a ticket whose description and documents use
// "Login screen.png" in several forms, and another ticket using the same
// name for its own image.
type imageTextFixture struct {
	ticket                 *models.Ticket
	image, other           *models.Document
	plan, page, unrelated  *models.Document
	elsewhere, elsewherePg *models.Document
}

func seedImageText(t *testing.T, s *Store) imageTextFixture {
	t.Helper()
	p := seedProject(t, s, "Docs", "DOC")
	var f imageTextFixture
	f.ticket = seedTicket(t, s, p.ID, "Has images")
	desc := "Before: ![shot](Login screen.png) and ![other](Other.png)"
	if _, err := s.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	f.ticket = mustTicket(t, s, f.ticket.ID)
	owner := DocumentOwner{TicketID: f.ticket.ID}
	f.plan = seedText(t, s, owner, "Plan", models.DocumentFormatMarkdown,
		"# Plan\n\n![a](<Login screen.png> \"Login\")\n\n![r]\n\n[r]: Login%20screen.png\n")
	f.page = seedText(t, s, owner, "Page", models.DocumentFormatHTML,
		`<img src="./Login screen.png"><div style="background:url('Login%20screen.png')"></div>`)
	f.unrelated = seedText(t, s, owner, "Unrelated", models.DocumentFormatMarkdown, "No images, just Login screen.png as text.")
	f.image = seedImage(t, s, owner, "Login screen", models.DocumentFormatPNG, imagedoctest.PNG(8, 8))
	f.other = seedImage(t, s, owner, "Other", models.DocumentFormatPNG, imagedoctest.PNG(8, 8))

	// Another ticket's text names its own image the same way.
	theirs := DocumentOwner{TicketID: seedTicket(t, s, p.ID, "Theirs").ID}
	seedImage(t, s, theirs, "Login screen", models.DocumentFormatPNG, imagedoctest.PNG(8, 8))
	f.elsewhere = seedText(t, s, theirs, "Plan", models.DocumentFormatMarkdown, "![a](Login screen.png)")
	f.elsewherePg = seedText(t, s, theirs, "Page", models.DocumentFormatHTML, `<img src="Login screen.png">`)
	return f
}

func TestRenamingAnImageRewritesItsOwnersText(t *testing.T) {
	s := newTestStore(t)
	f := seedImageText(t, s)
	before := f.ticket.UpdatedAt
	time.Sleep(5 * time.Millisecond)

	name := "Home page"
	d, err := s.UpdateDocument(f.image.ID, models.UpdateDocumentRequest{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Home page" || d.Revision != 1 {
		t.Errorf("renamed image = %+v; a rename alone keeps the revision", d.DocumentMeta)
	}
	wantPlaces(t, "ReferencesUpdated", d.ReferencesUpdated, description(), docPlace(f.plan), docPlace(f.page))
	wantPlaces(t, "ReferencesLeft", d.ReferencesLeft)

	tk := mustTicket(t, s, f.ticket.ID)
	if want := "Before: ![shot](Home page.png) and ![other](Other.png)"; tk.Description != want {
		t.Errorf("description = %q, want %q", tk.Description, want)
	}
	if !tk.UpdatedAt.After(before) {
		t.Errorf("a rewritten description is a ticket update: updatedAt %v, was %v", tk.UpdatedAt, before)
	}

	plan := mustDocument(t, s, f.plan.ID)
	if want := "# Plan\n\n![a](<Home page.png> \"Login\")\n\n![r]\n\n[r]: Home%20page.png\n"; plan.Content != want || plan.Revision != 2 {
		t.Errorf("Plan.md = %q rev %d, want %q rev 2", plan.Content, plan.Revision, want)
	}
	if _, rev, _ := storedText(t, s, f.plan.ID); rev != 2 {
		t.Errorf("Plan.md search text at revision %d, want 2", rev)
	}
	page := mustDocument(t, s, f.page.ID)
	if want := `<img src="./Home page.png"><div style="background:url('Home%20page.png')"></div>`; page.Content != want || page.Revision != 2 {
		t.Errorf("Page.html = %q rev %d, want %q rev 2", page.Content, page.Revision, want)
	}
	for _, kept := range []*models.Document{f.unrelated, f.elsewhere, f.elsewherePg} {
		if got := mustDocument(t, s, kept.ID); got.Content != kept.Content || got.Revision != kept.Revision {
			t.Errorf("%s changed: %q rev %d", kept.Name, got.Content, got.Revision)
		}
	}
}

func TestRenamingAnEpicsImageRewritesItsDocuments(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	e := seedEpic(t, s, p.ID, "Launch")
	owner := DocumentOwner{EpicID: e.ID}
	notes := seedText(t, s, owner, "Notes", models.DocumentFormatMarkdown, "![flow](Flow chart.gif)")
	page := seedText(t, s, owner, "Board", models.DocumentFormatHTML, `<img srcset="Flow%20chart.gif 1x, ./Flow%20chart.gif 2x">`)
	img := seedImage(t, s, owner, "Flow chart", models.DocumentFormatGIF, imagedoctest.AnimatedGIF())

	// Replacing the picture and renaming it together rewrites too.
	name := "Rollout"
	d, err := s.ReplaceDocumentImage(img.ID, imagedoctest.AnimatedGIF(), &name)
	if err != nil {
		t.Fatal(err)
	}
	wantPlaces(t, "ReferencesUpdated", d.ReferencesUpdated, docPlace(notes), docPlace(page))
	if got := mustDocument(t, s, notes.ID); got.Content != "![flow](Rollout.gif)" || got.Revision != 2 {
		t.Errorf("Notes.md = %q rev %d", got.Content, got.Revision)
	}
	if got := mustDocument(t, s, page.ID); got.Content != `<img srcset="Rollout.gif 1x, ./Rollout.gif 2x">` || got.Revision != 2 {
		t.Errorf("Board.html = %q rev %d", got.Content, got.Revision)
	}
}

func TestARefusedRenameRewritesNothing(t *testing.T) {
	s := newTestStore(t)
	f := seedImageText(t, s)

	taken := "other"
	_, err := s.UpdateDocument(f.image.ID, models.UpdateDocumentRequest{Name: &taken})
	wantInvalid(t, err, `This ticket already has a document called "Other.png".`)
	_, err = s.ReplaceDocumentImage(f.image.ID, imagedoctest.PNG(8, 8), &taken)
	wantInvalid(t, err, `This ticket already has a document called "Other.png".`)

	// A write that fails partway through the rewrite undoes the rename and
	// every rewrite before it.
	if _, err := s.db.Exec(`CREATE TRIGGER refuse_page BEFORE UPDATE OF content ON documents
		WHEN NEW.id = '` + f.page.ID + `' BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatal(err)
	}
	name := "Home page"
	if _, err := s.UpdateDocument(f.image.ID, models.UpdateDocumentRequest{Name: &name}); err == nil {
		t.Fatal("the rename went through although a rewrite failed")
	}

	if got := mustDocument(t, s, f.image.ID); got.Name != "Login screen" {
		t.Errorf("image renamed to %q", got.Name)
	}
	if got := mustTicket(t, s, f.ticket.ID); got.Description != f.ticket.Description || !got.UpdatedAt.Equal(f.ticket.UpdatedAt) {
		t.Errorf("description changed: %q at %v", got.Description, got.UpdatedAt)
	}
	for _, kept := range []*models.Document{f.plan, f.page} {
		if got := mustDocument(t, s, kept.ID); got.Content != kept.Content || got.Revision != kept.Revision {
			t.Errorf("%s changed: %q rev %d", kept.Name, got.Content, got.Revision)
		}
	}
}

func TestRenameEdgeCases(t *testing.T) {
	s := newTestStore(t)
	f := seedImageText(t, s)

	// A change of case only: references ignore case, so nothing is rewritten.
	lower := "login screen"
	d, err := s.UpdateDocument(f.image.ID, models.UpdateDocumentRequest{Name: &lower})
	if err != nil || len(d.ReferencesUpdated) != 0 {
		t.Fatalf("case-only rename = %+v, %v", d, err)
	}
	if got := mustDocument(t, s, f.plan.ID); got.Revision != 1 {
		t.Errorf("a case-only rename saved Plan.md (rev %d)", got.Revision)
	}

	// A form that can't be rewritten reliably is left, and said so.
	odd := "Odd: ![a](login&#37;20screen.png)"
	if _, err := s.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Description: &odd}); err != nil {
		t.Fatal(err)
	}
	name := "Home page"
	d, err = s.UpdateDocument(f.image.ID, models.UpdateDocumentRequest{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	wantPlaces(t, "ReferencesUpdated", d.ReferencesUpdated, docPlace(f.plan), docPlace(f.page))
	wantPlaces(t, "ReferencesLeft", d.ReferencesLeft, description())
	if got := mustTicket(t, s, f.ticket.ID); got.Description != odd {
		t.Errorf("description = %q", got.Description)
	}

	// Renaming a document that isn't an image touches no text.
	plan := "Plan B"
	d, err = s.UpdateDocument(f.unrelated.ID, models.UpdateDocumentRequest{Name: &plan})
	if err != nil || d.ReferencesUpdated != nil {
		t.Fatalf("renaming a text document = %+v, %v", d, err)
	}
}

func TestAnEditorOpenDuringTheRewriteIsToldOfIt(t *testing.T) {
	s := newTestStore(t)
	f := seedImageText(t, s)
	editingFrom := f.plan.Revision

	name := "Home page"
	if _, err := s.UpdateDocument(f.image.ID, models.UpdateDocumentRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	// The person's save from the revision before the rewrite is a
	// conflict, carrying the rewritten text, not a silent overwrite.
	mine := "# Plan\n\n![a](<Login screen.png>)\n\nmy edit"
	_, err := s.UpdateDocument(f.plan.ID, models.UpdateDocumentRequest{Content: &mine, ExpectedRevision: &editingFrom})
	var conflict *ErrDocumentConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("save from before the rewrite = %v, want a conflict", err)
	}
	if conflict.Current.Revision != 2 || conflict.Current.Content == f.plan.Content {
		t.Errorf("conflict carries %q rev %d", conflict.Current.Content, conflict.Current.Revision)
	}
}

func TestImageUsage(t *testing.T) {
	s := newTestStore(t)
	f := seedImageText(t, s)

	places, found, err := s.ImageUsage(f.image.ID)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	wantPlaces(t, "Login screen.png used in", places, description(), docPlace(f.plan), docPlace(f.page))

	places, _, err = s.ImageUsage(f.other.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantPlaces(t, "Other.png used in", places, description())

	// A definition no image uses is not a use.
	unused := seedImage(t, s, DocumentOwner{TicketID: f.ticket.ID}, "Unused", models.DocumentFormatPNG, imagedoctest.PNG(8, 8))
	seedText(t, s, DocumentOwner{TicketID: f.ticket.ID}, "Defs", models.DocumentFormatMarkdown, "[u]: Unused.png\n\n`![x](Unused.png)`")
	places, _, err = s.ImageUsage(unused.ID)
	if err != nil || places == nil || len(places) != 0 {
		t.Errorf("unused image places = %+v, %v; want an empty list", places, err)
	}

	_, _, err = s.ImageUsage(f.plan.ID)
	wantInvalid(t, err, msgDocNotImage)
	if _, found, err := s.ImageUsage("nope"); found || err != nil {
		t.Errorf("unknown id = %v, %v", found, err)
	}
}

func TestDeleteDocumentReportingUse(t *testing.T) {
	s := newTestStore(t)
	f := seedImageText(t, s)

	deleted, usedIn, err := s.DeleteDocumentReportingUse(f.image.ID)
	if err != nil || !deleted {
		t.Fatal(deleted, err)
	}
	wantPlaces(t, "usedIn", usedIn, description(), docPlace(f.plan), docPlace(f.page))
	if got, _ := s.GetDocument(f.image.ID); got != nil {
		t.Error("the image is still there")
	}
	// The text stays as it was: those places now show a missing image.
	if got := mustDocument(t, s, f.plan.ID); got.Content != f.plan.Content || got.Revision != 1 {
		t.Errorf("Plan.md changed on delete: %q", got.Content)
	}

	deleted, usedIn, err = s.DeleteDocumentReportingUse(f.plan.ID)
	if err != nil || !deleted || usedIn != nil {
		t.Errorf("deleting a text document = %v, %+v, %v", deleted, usedIn, err)
	}
	if deleted, _, err := s.DeleteDocumentReportingUse("nope"); deleted || err != nil {
		t.Errorf("unknown id = %v, %v", deleted, err)
	}
}
