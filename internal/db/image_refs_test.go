package db

import (
	"testing"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
)

func TestReferencedImageStaysWithTheOwner(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	mine := DocumentOwner{TicketID: seedTicket(t, s, p.ID, "Mine").ID}
	theirs := DocumentOwner{TicketID: seedTicket(t, s, p.ID, "Theirs").ID}
	e, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	epic := DocumentOwner{EpicID: e.ID}

	html := func(owner DocumentOwner, name string) *models.Document {
		d, err := s.CreateDocument(models.CreateDocumentRequest{
			TicketID: owner.TicketID, EpicID: owner.EpicID, Name: name, Format: models.DocumentFormatHTML, Content: "<p>x</p>",
		})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	page := html(mine, "Report")
	epicPage := html(epic, "Board")
	notes, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: mine.TicketID, Name: "Notes", Content: "![](Login screen.png)"})
	if err != nil {
		t.Fatal(err)
	}
	login := seedImage(t, s, mine, "Login screen", models.DocumentFormatPNG, imagedoctest.PNG(8, 8))
	photo := seedImage(t, s, mine, "Photo", models.DocumentFormatJPEG, imagedoctest.JPEGWithGPS(8, 8, 0))
	seedImage(t, s, theirs, "Their shot", models.DocumentFormatPNG, imagedoctest.PNG(8, 8))
	epicFlow := seedImage(t, s, epic, "Login screen", models.DocumentFormatGIF, imagedoctest.AnimatedGIF())

	for _, c := range []struct {
		from, ref, want string
	}{
		{page.ID, "Login screen.png", login.ID},
		{page.ID, "login SCREEN.PNG", login.ID},
		{page.ID, "Photo.jpg", photo.ID},
		{page.ID, "photo.jpeg", photo.ID},
		{epicPage.ID, "Login screen.gif", epicFlow.ID},
		// Nothing but the same owner's images, by display name.
		{page.ID, "Login screen", ""},
		{page.ID, "Login screen.gif", ""},
		{page.ID, "Their shot.png", ""},
		{page.ID, "Notes.md", ""},
		{page.ID, "Report.html", ""},
		{page.ID, login.ID, ""},
		{epicPage.ID, "Login screen.png", ""},
		// Only from an HTML document.
		{notes.ID, "Login screen.png", ""},
		{login.ID, "Login screen.png", ""},
		{"nope", "Login screen.png", ""},
	} {
		f, err := s.ReferencedImage(c.from, c.ref)
		if err != nil {
			t.Fatalf("ReferencedImage(%q, %q): %v", c.from, c.ref, err)
		}
		got := ""
		if f != nil {
			got = f.Document.ID
			if len(f.Data) == 0 || f.ContentType == "" {
				t.Errorf("ReferencedImage(%q, %q) has no file", c.from, c.ref)
			}
		}
		if got != c.want {
			t.Errorf("ReferencedImage(%q, %q) = %q, want %q", c.from, c.ref, got, c.want)
		}
	}
}
