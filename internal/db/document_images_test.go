package db

import (
	"bytes"
	"database/sql"
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
)

// migrateThrough applies the migrations up to and including last, as an
// older build would have left the database.
func migrateThrough(t *testing.T, database *sql.DB, last string) {
	t.Helper()
	if _, err := database.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationsFS.ReadDir(migrations)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() > last {
			break
		}
		content, _ := migrationsFS.ReadFile(filepath.Join(migrations, e.Name()))
		if _, err := database.Exec(string(content)); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		if _, err := database.Exec("INSERT INTO schema_migrations (version) VALUES (?)", e.Name()); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMigrationAddsImageDocuments opens a database as migration 009 left it,
// with text documents and their search text, and checks that 010 keeps them
// all as they were and adds what images need.
func TestMigrationAddsImageDocuments(t *testing.T) {
	database, err := sql.Open("sqlite", dsn(filepath.Join(t.TempDir(), "old.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	migrateThrough(t, database, "009_document_search.sql")

	created := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for _, q := range []string{
		`INSERT INTO projects (id, name, prefix) VALUES ('p1', 'Docs', 'DOC')`,
		`INSERT INTO tickets (id, project_id, number, title) VALUES ('t1', 'p1', 1, 'Has docs')`,
		`INSERT INTO epics (id, project_id, name) VALUES ('e1', 'p1', 'Launch')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(id string, ticket, epic any, name, format, content string, revision int) {
		t.Helper()
		if _, err := database.Exec(`INSERT INTO documents (id, ticket_id, epic_id, name, format, content, revision, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, ticket, epic, name, format, content, revision, created, created); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`INSERT INTO document_search (document_id, revision, text) VALUES (?, ?, ?)`,
			id, revision, "text of "+name); err != nil {
			t.Fatal(err)
		}
	}
	insert("d1", "t1", nil, "Plan", "markdown", "# Plan\n\nÉtape une", 3)
	insert("d2", nil, "e1", "Report", "html", "<p>Report</p>", 1)

	if err := runMigrations(database); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	s := NewStore(database)

	d, err := s.GetDocument("d1")
	if err != nil || d == nil {
		t.Fatalf("GetDocument after migrating: %v, %v", d, err)
	}
	if d.Content != "# Plan\n\nÉtape une" || d.Revision != 3 || d.Size != len("# Plan\n\nÉtape une") ||
		d.Width != 0 || d.Height != 0 || !d.CreatedAt.Equal(created) || d.TicketID != "t1" {
		t.Errorf("document changed by the migration: %+v", d)
	}
	wantStoredText(t, s, "d1", "text of Plan", 3)
	wantStoredText(t, s, "d2", "text of Report", 1)
	if ids, err := s.SearchDocumentTickets("text of plan", ""); err != nil || len(ids) != 1 {
		t.Errorf("search after migrating = %v, %v", ids, err)
	}

	// The rebuilt table keeps its rules: one owner, unique names, the
	// search text and images deleted with their document.
	if _, err := database.Exec(`INSERT INTO documents (id, ticket_id, name, format, created_at, updated_at)
		VALUES ('d3', 't1', 'PLAN', 'markdown', ?, ?)`, created, created); err == nil {
		t.Error("a second name differing only in case was accepted")
	}
	if _, err := database.Exec(`INSERT INTO documents (id, name, format, created_at, updated_at)
		VALUES ('d4', 'Orphan', 'markdown', ?, ?)`, created, created); err == nil {
		t.Error("a document with no owner was accepted")
	}
	// An image needs its size and dimensions and has no text content; a
	// text document has none of them.
	if _, err := database.Exec(`INSERT INTO documents (id, ticket_id, name, format, created_at, updated_at)
		VALUES ('d5', 't1', 'Shot', 'png', ?, ?)`, created, created); err == nil {
		t.Error("an image without size and dimensions was accepted")
	}
	if _, err := database.Exec(`INSERT INTO documents (id, ticket_id, name, format, size, width, height, created_at, updated_at)
		VALUES ('d6', 't1', 'Notes', 'markdown', 1, 1, 1, ?, ?)`, created, created); err == nil {
		t.Error("a text document with image dimensions was accepted")
	}
	if _, err := database.Exec(`INSERT INTO documents (id, ticket_id, name, format, size, width, height, created_at, updated_at)
		VALUES ('d7', 't1', 'Vector', 'svg', 1, 1, 1, ?, ?)`, created, created); err == nil {
		t.Error("an svg format was accepted")
	}
	if _, err := database.Exec(`DELETE FROM documents WHERE id = 'd1'`); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := storedText(t, s, "d1"); ok {
		t.Error("the search text outlived its document")
	}
}

func seedImage(t *testing.T, s *Store, owner DocumentOwner, name, format string, data []byte) *models.Document {
	t.Helper()
	d, err := s.CreateImageDocument(models.CreateImageRequest{
		TicketID: owner.TicketID, EpicID: owner.EpicID, Name: name, Format: format, Data: data,
	})
	if err != nil {
		t.Fatalf("seeding image %q: %v", name, err)
	}
	return d
}

func TestCreateImageDocument(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has images")
	owner := DocumentOwner{TicketID: tk.ID}

	orig := imagedoctest.JPEGWithGPS(120, 80, 6)
	d := seedImage(t, s, owner, "  Login screen ", models.DocumentFormatJPEG, orig)
	if d.Name != "Login screen" || d.Format != "jpeg" || d.Revision != 1 || d.Content != "" {
		t.Errorf("created %+v", d)
	}
	// Turned right by its EXIF orientation: shown 80 wide, 120 high.
	if d.Width != 80 || d.Height != 120 {
		t.Errorf("dimensions %d×%d, want 80×120", d.Width, d.Height)
	}
	if models.DocumentDisplayName(d.Name, d.Format) != "Login screen.jpg" {
		t.Errorf("display name %q", models.DocumentDisplayName(d.Name, d.Format))
	}

	f, err := s.GetDocumentImage(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.ContentType != "image/jpeg" || f.Document.ID != d.ID || len(f.Data) != d.Size {
		t.Errorf("image file: type %s, %d bytes, size %d", f.ContentType, len(f.Data), d.Size)
	}
	if s, found := imagedoctest.HasSecrets(f.Data); found {
		t.Errorf("stored file still holds %s", s)
	}
	if len(f.Data) >= len(orig) {
		t.Errorf("stored %d bytes of a %d-byte upload; the metadata was not removed", len(f.Data), len(orig))
	}
	thumb, err := s.GetDocumentThumbnail(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(thumb.Data))
	if err != nil || thumb.ContentType != "image/jpeg" || cfg.Width != 80 || cfg.Height != 120 {
		t.Errorf("thumbnail %s %dx%d, %v", thumb.ContentType, cfg.Width, cfg.Height, err)
	}

	// Found by name, with or without the extension, .jpeg as well as .jpg.
	for _, ref := range []string{"login SCREEN", "Login screen.jpg", "login screen.JPEG"} {
		if id, err := s.ResolveDocumentRef(owner, ref); err != nil || id != d.ID {
			t.Errorf("ResolveDocumentRef(%q) = %q, %v", ref, id, err)
		}
	}
	if _, err := s.ResolveDocumentRef(owner, "Login screen.png"); err == nil {
		t.Error("a name with the wrong extension found the image")
	}

	// Names are shared with text documents, ignoring case.
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "login screen", Content: "x"})
	wantInvalid(t, err, `This ticket already has a document called "Login screen.jpg".`)
	_, err = s.CreateImageDocument(models.CreateImageRequest{TicketID: tk.ID, Name: "Bad/name", Format: "png", Data: imagedoctest.PNG(4, 4)})
	wantInvalid(t, err, msgDocNameChars)

	// Epics take images too.
	e, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	gifDoc := seedImage(t, s, DocumentOwner{EpicID: e.ID}, "Spinner", models.DocumentFormatGIF, imagedoctest.AnimatedGIF())
	if gifDoc.EpicID != e.ID || gifDoc.Width != 20 || gifDoc.Height != 10 {
		t.Errorf("epic image %+v", gifDoc)
	}
	webpDoc := seedImage(t, s, owner, "Mock", models.DocumentFormatWebP, imagedoctest.WebPWithGPS(1))
	if f, _ := s.GetDocumentImage(webpDoc.ID); f == nil || bytes.Contains(f.Data, []byte("SECRET")) {
		t.Error("the WebP kept its metadata")
	}
	seedImage(t, s, owner, "Diagram", models.DocumentFormatPNG, imagedoctest.PNG(30, 20))

	// Lists and counts include images alongside text documents.
	seedDocument(t, s, tk.ID, "Plan", "# Plan")
	docs, err := s.ListDocuments(owner)
	if err != nil || len(docs) != 4 {
		t.Fatalf("ListDocuments = %d documents, %v", len(docs), err)
	}
	if docs[0].Width != 80 || docs[3].Width != 0 || docs[2].Size == 0 {
		t.Errorf("list details: %+v", docs)
	}
	got, err := s.GetTicket(tk.ID)
	if err != nil || got.DocumentCount != 4 || len(got.Documents) != 4 {
		t.Errorf("GetTicket documents = %d (%d listed), %v", got.DocumentCount, len(got.Documents), err)
	}
	list, err := s.ListTickets(models.TicketFilter{})
	if err != nil || len(list) != 1 || list[0].DocumentCount != 4 {
		t.Errorf("ListTickets document count = %+v, %v", list, err)
	}
	epics, err := s.ListEpics(p.ID)
	if err != nil || len(epics) != 1 || epics[0].DocumentCount != 1 {
		t.Errorf("ListEpics document count = %+v, %v", epics, err)
	}
}

func TestImageRules(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has images")
	create := func(format string, data []byte) error {
		_, err := s.CreateImageDocument(models.CreateImageRequest{TicketID: tk.ID, Name: "Shot", Format: format, Data: data})
		return err
	}

	// The content decides, not the name.
	wantInvalid(t, create("png", imagedoctest.JPEGWithGPS(8, 8, 0)), "This isn't a PNG image: its content is JPEG.")
	wantInvalid(t, create("gif", imagedoctest.PNG(8, 8)), "This isn't a GIF image: its content is PNG.")
	wantInvalid(t, create("webp", []byte("hello")), "This isn't a WebP image: its content is not PNG, JPEG, GIF or WebP.")
	wantInvalid(t, create("png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)),
		"This isn't a PNG image: it's an SVG, and SVG images can't be attached.")
	wantInvalid(t, create("svg", []byte(`<svg/>`)), msgDocSVG)
	wantInvalid(t, create("markdown", imagedoctest.PNG(8, 8)), msgDocImageFormat)
	wantInvalid(t, create("", imagedoctest.PNG(8, 8)), msgDocImageFormat)

	// 8 MB at most, refused with the size, never shrunk.
	big := append(imagedoctest.PNG(8, 8), make([]byte, models.MaxDocumentBytes)...)
	wantInvalid(t, create("png", big), "This image is 8.1 MB. The limit is 8 MB.")

	// Text routes refuse images and image routes refuse text.
	_, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Shot", Format: "png", Content: "x"})
	wantInvalid(t, err, msgDocImageFromFile)
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Shot", Format: "svg", Content: "<svg/>"})
	wantInvalid(t, err, msgDocSVG)

	img := seedImage(t, s, DocumentOwner{TicketID: tk.ID}, "Shot", "png", imagedoctest.PNG(8, 8))
	text := "hello"
	_, err = s.UpdateDocument(img.ID, models.UpdateDocumentRequest{Content: &text})
	wantInvalid(t, err, msgDocImageText)
	doc := seedDocument(t, s, tk.ID, "Plan", "# Plan")
	_, err = s.ReplaceDocumentImage(doc.ID, imagedoctest.PNG(8, 8), nil)
	wantInvalid(t, err, msgDocNotImage)
	_, err = s.GetDocumentImage(doc.ID)
	wantInvalid(t, err, msgDocNotImage)
	if f, err := s.GetDocumentImage("nope"); f != nil || err != nil {
		t.Errorf("unknown id: %v, %v", f, err)
	}
	if d, err := s.ReplaceDocumentImage("nope", imagedoctest.PNG(8, 8), nil); d != nil || err != nil {
		t.Errorf("replacing an unknown id: %v, %v", d, err)
	}

	// Filenames: images and SVG.
	for file, want := range map[string]string{"shot.PNG": "png", "photo.jpeg": "jpeg", "photo.JPG": "jpeg", "a.gif": "gif", "b.webp": "webp"} {
		if _, format, err := DocumentNameFromFilename(file); err != nil || format != want {
			t.Errorf("DocumentNameFromFilename(%q) format %q, %v", file, format, err)
		}
	}
	_, _, err = DocumentNameFromFilename("logo.svg")
	wantInvalid(t, err, msgDocSVG)
}

func TestReplaceDocumentImage(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has images")
	d := seedImage(t, s, DocumentOwner{TicketID: tk.ID}, "Shot", "png", imagedoctest.PNG(40, 20))
	oldThumb, _ := s.GetDocumentThumbnail(d.ID)

	time.Sleep(5 * time.Millisecond)
	got, err := s.ReplaceDocumentImage(d.ID, imagedoctest.PNG(20, 50), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || got.Width != 20 || got.Height != 50 || got.Name != "Shot" || !got.UpdatedAt.After(d.UpdatedAt) {
		t.Errorf("after replace: %+v", got)
	}
	f, _ := s.GetDocumentImage(d.ID)
	if !bytes.Equal(f.Data, imagedoctest.PNG(20, 50)) || f.Document.Size != len(f.Data) {
		t.Error("the stored file is not the new one")
	}
	thumb, _ := s.GetDocumentThumbnail(d.ID)
	if bytes.Equal(thumb.Data, oldThumb.Data) {
		t.Error("the thumbnail was not made anew")
	}

	// The format is fixed: a PNG is replaced by a PNG.
	_, err = s.ReplaceDocumentImage(d.ID, imagedoctest.AnimatedGIF(), nil)
	wantInvalid(t, err, "This isn't a PNG image: its content is GIF.")
	big := append(imagedoctest.PNG(8, 8), make([]byte, models.MaxDocumentBytes)...)
	_, err = s.ReplaceDocumentImage(d.ID, big, nil)
	wantInvalid(t, err, "This image is 8.1 MB. The limit is 8 MB.")

	// A rename keeps the file and the revision.
	name := "Final shot"
	renamed, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Name: &name})
	if err != nil || renamed.Revision != 2 || renamed.Width != 20 || renamed.Size != f.Document.Size {
		t.Errorf("rename: %+v, %v", renamed, err)
	}

	// A picture and a name together: all or nothing.
	seedDocument(t, s, tk.ID, "Plan", "# Plan")
	for _, bad := range []string{"Bad/name", "plan"} {
		_, err = s.ReplaceDocumentImage(d.ID, imagedoctest.PNG(33, 33), &bad)
		if err == nil {
			t.Fatalf("rename to %q was taken", bad)
		}
		if cur, _ := s.GetDocumentImage(d.ID); cur.Document.Width != 20 || cur.Document.Revision != 2 || cur.Document.Name != "Final shot" {
			t.Errorf("a refused name %q still changed the image: %+v", bad, cur.Document)
		}
	}
	good := "Home page"
	both, err := s.ReplaceDocumentImage(d.ID, imagedoctest.PNG(33, 33), &good)
	if err != nil || both.Name != "Home page" || both.Width != 33 || both.Revision != 3 {
		t.Errorf("replace and rename: %+v, %v", both, err)
	}
}

func TestImagesGoWithTheirOwner(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has images")
	e, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	a := seedImage(t, s, DocumentOwner{TicketID: tk.ID}, "A", "png", imagedoctest.PNG(4, 4))
	b := seedImage(t, s, DocumentOwner{EpicID: e.ID}, "B", "png", imagedoctest.PNG(4, 4))
	c := seedImage(t, s, DocumentOwner{TicketID: tk.ID}, "C", "png", imagedoctest.PNG(4, 4))

	imageRows := func() int {
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM document_images").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := imageRows(); n != 3 {
		t.Fatalf("%d image rows, want 3", n)
	}
	if ok, err := s.DeleteDocument(c.ID); !ok || err != nil {
		t.Fatal(err)
	}
	if n := imageRows(); n != 2 {
		t.Errorf("after deleting an image, %d rows, want 2", n)
	}
	if err := s.DeleteTicket(tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteEpic(e.ID); err != nil {
		t.Fatal(err)
	}
	if n := imageRows(); n != 0 {
		t.Errorf("after deleting the owners, %d image rows, want 0", n)
	}
	for _, id := range []string{a.ID, b.ID} {
		if d, _ := s.GetDocument(id); d != nil {
			t.Errorf("document %s outlived its owner", id)
		}
	}
}

// TestImageBytesStayUnread hides the image table, so any query that reads
// image bytes fails, then runs everything that lists, counts or searches
// documents, and the open-time search check.
func TestImageBytesStayUnread(t *testing.T) {
	s := newTestStore(t)
	database := s.db
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has images")
	e, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	img := seedImage(t, s, DocumentOwner{TicketID: tk.ID}, "Login screen", "png", imagedoctest.PNG(8, 8))
	seedImage(t, s, DocumentOwner{EpicID: e.ID}, "Mock", "png", imagedoctest.PNG(8, 8))
	seedDocument(t, s, tk.ID, "Plan", "# Plan")

	// Images have no search text and the open-time check leaves them be.
	if _, _, ok := storedText(t, s, img.ID); ok {
		t.Error("an image has search text")
	}
	if _, err := database.Exec("ALTER TABLE document_images RENAME TO hidden_images"); err != nil {
		t.Fatal(err)
	}
	if err := fillDocumentSearch(database); err != nil {
		t.Fatalf("open-time check: %v", err)
	}
	if _, _, ok := storedText(t, s, img.ID); ok {
		t.Error("the open-time check wrote search text for an image")
	}

	if docs, err := s.ListDocuments(DocumentOwner{TicketID: tk.ID}); err != nil || len(docs) != 2 {
		t.Errorf("ListDocuments: %d, %v", len(docs), err)
	}
	if d, err := s.GetDocument(img.ID); err != nil || d.Size == 0 {
		t.Errorf("GetDocument: %+v, %v", d, err)
	}
	if got, err := s.GetTicket(tk.ID); err != nil || got.DocumentCount != 2 {
		t.Errorf("GetTicket: %v", err)
	}
	if _, err := s.ListTickets(models.TicketFilter{}); err != nil {
		t.Errorf("ListTickets: %v", err)
	}
	if got, err := s.GetEpic(e.ID); err != nil || got.DocumentCount != 1 {
		t.Errorf("GetEpic: %v", err)
	}
	if _, err := s.ListEpics(p.ID); err != nil {
		t.Errorf("ListEpics: %v", err)
	}
	// Images match by name only.
	if ids, err := s.SearchDocumentTickets("login screen.png", ""); err != nil || len(ids) != 1 {
		t.Errorf("search by image name = %v, %v", ids, err)
	}
	if ids, err := s.SearchDocumentTickets("IHDR", ""); err != nil || len(ids) != 0 {
		t.Errorf("search matched inside an image: %v, %v", ids, err)
	}
	name := "Renamed"
	if _, err := s.UpdateDocument(img.ID, models.UpdateDocumentRequest{Name: &name}); err != nil {
		t.Errorf("rename: %v", err)
	}
	// Reading the image itself is the one thing that needs the table.
	if _, err := s.GetDocumentImage(img.ID); err == nil {
		t.Error("the image was read with its table hidden")
	}
}
