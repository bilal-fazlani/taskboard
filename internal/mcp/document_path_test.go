package mcp

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// writeTempFile writes data to name in a fresh temporary directory and
// returns its absolute path.
func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCreateDocumentToolReadsEveryFormatFromPath(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)

	for _, tc := range []struct {
		file, name string
		data       []byte
		format     string
	}{
		{"plan.md", "Plan", []byte("# Plan\n"), models.DocumentFormatMarkdown},
		{"report.html", "Report", []byte("<h1>Report</h1>"), models.DocumentFormatHTML},
		{"legacy.HTM", "Legacy", []byte("<p>old</p>"), models.DocumentFormatHTML},
		{"notes.txt", "Notes", []byte("plain text"), models.DocumentFormatMarkdown}, // no known extension: markdown
		{"shot.png", "Shot", imagedoctest.PNG(40, 30), models.DocumentFormatPNG},
		{"photo.jpg", "Photo", imagedoctest.JPEGWithGPS(60, 40, 6), models.DocumentFormatJPEG},
		{"photo2.jpeg", "Photo two", imagedoctest.JPEGWithGPS(20, 10, 1), models.DocumentFormatJPEG},
		{"spinner.gif", "Spinner", imagedoctest.AnimatedGIF(), models.DocumentFormatGIF},
		{"mock.webp", "Mock", imagedoctest.WebPWithGPS(1), models.DocumentFormatWebP},
	} {
		path := writeTempFile(t, tc.file, tc.data)
		got, err := s.callTool("create_document", mustJSON(t, map[string]any{
			"full": true, "ticket": "DOC-1", "name": tc.name, "path": path,
		}))
		if err != nil {
			t.Errorf("create_document %s: %v", tc.file, err)
			continue
		}
		d := got.(*models.Document)
		if d.Name != tc.name || d.Format != tc.format {
			t.Errorf("%s: created %s as %s, want %s as %s", tc.file, d.Name, d.Format, tc.name, tc.format)
		}
		if !models.IsImageFormat(tc.format) {
			if d.Content != string(tc.data) {
				t.Errorf("%s: content %q", tc.file, d.Content)
			}
			continue
		}
		f, err := s.store.GetDocumentImage(d.ID)
		if err != nil || f == nil {
			t.Fatalf("%s: image %v %v", tc.file, f, err)
		}
		// The same checks as data: metadata goes, the picture stays.
		if secret, found := imagedoctest.HasSecrets(f.Data); found {
			t.Errorf("%s: stored image holds %s", tc.file, secret)
		}
		if d.Width == 0 || d.Height == 0 {
			t.Errorf("%s: no size %+v", tc.file, d.DocumentMeta)
		}
	}
}

func TestCreateDocumentToolPathAnswersShortly(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)
	got, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"ticket": "DOC-1", "name": "Login screen.png", "path": writeTempFile(t, "anything.png", imagedoctest.PNG(8, 8)),
	}))
	if err != nil {
		t.Fatal(err)
	}
	c, ok := got.(documentConfirmation)
	if !ok || !c.Created || c.Name != "Login screen" || c.Format != models.DocumentFormatPNG ||
		c.URL != "http://board.test/?ticket=DOC-1&doc=Login+screen.png" {
		t.Fatalf("short answer = %#v", got)
	}
}

func TestCreateDocumentToolPathFormat(t *testing.T) {
	s := newTestServer(t)
	seedMCPTicket(t, s)

	// format overrides the extension, as doc add --format does.
	got, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"full": true, "ticket": "DOC-1", "name": "Page", "format": "html", "path": writeTempFile(t, "page.md", []byte("<p>x</p>")),
	}))
	if err != nil || got.(*models.Document).Format != models.DocumentFormatHTML {
		t.Fatalf("format override: %v %v", got, err)
	}

	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"name": "Logo", "path": writeTempFile(t, "logo.svg", []byte("<svg/>"))},
			"SVG images can't be attached. Use PNG, JPEG, GIF or WebP."},
		// An image is still checked for its real format.
		{map[string]any{"name": "Shot", "path": writeTempFile(t, "shot.png", imagedoctest.JPEGWithGPS(8, 8, 0))},
			"This isn't a PNG image: its content is JPEG."},
		{map[string]any{"name": "Shot", "path": writeTempFile(t, "shot2.png", imagedoctest.PNG(8, 8)), "content": "x"},
			"pass one of content, data or path, not several"},
		{map[string]any{"name": "Shot", "path": writeTempFile(t, "shot3.png", imagedoctest.PNG(8, 8)), "data": b64(imagedoctest.PNG(8, 8))},
			"pass one of content, data or path, not several"},
		{map[string]any{"name": "plan.md", "path": writeTempFile(t, "plan.md", []byte("x"))},
			"Use letters, digits, spaces, _ and - only."},
	} {
		tc.args["ticket"] = "DOC-1"
		if _, err := s.callTool("create_document", mustJSON(t, tc.args)); err == nil || err.Error() != tc.want {
			t.Errorf("create_document %v: %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestUpdateDocumentToolReadsPath(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	text, err := s.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Plan", Content: "# v1"})
	if err != nil {
		t.Fatal(err)
	}
	img, err := s.store.CreateImageDocument(models.CreateImageRequest{
		TicketID: tk.ID, Name: "Shot", Format: models.DocumentFormatPNG, Data: imagedoctest.PNG(40, 30),
	})
	if err != nil {
		t.Fatal(err)
	}

	// A text document's content, with the short answer.
	got, err := s.callTool("update_document", mustJSON(t, map[string]any{
		"id": "Plan", "ticket": "DOC-1", "path": writeTempFile(t, "plan.md", []byte("# v2\n")),
	}))
	if err != nil {
		t.Fatalf("update text: %v", err)
	}
	if c := got.(documentConfirmation); c.Changed == nil || strings.Join(*c.Changed, ",") != "content" {
		t.Errorf("text answer = %#v", c)
	}
	if cur, _ := s.store.GetDocument(text.ID); cur.Content != "# v2\n" || cur.Revision != 2 {
		t.Errorf("text after update: %q rev %d", cur.Content, cur.Revision)
	}

	// An image's picture and its name together, metadata stripped.
	got, err = s.callTool("update_document", mustJSON(t, map[string]any{
		"full": true, "id": img.ID, "name": "Final shot", "path": writeTempFile(t, "final.png", imagedoctest.PNG(20, 50)),
	}))
	if err != nil {
		t.Fatalf("update image: %v", err)
	}
	u := got.(*models.Document)
	if u.Width != 20 || u.Height != 50 || u.Name != "Final shot" || u.Revision != 2 {
		t.Errorf("image after update: %+v", u.DocumentMeta)
	}
	got, err = s.callTool("update_document", mustJSON(t, map[string]any{
		"id": img.ID, "path": writeTempFile(t, "again.png", imagedoctest.PNG(20, 50)),
	}))
	if err != nil {
		t.Fatalf("update image again: %v", err)
	}
	if c := got.(documentConfirmation); c.Changed == nil || strings.Join(*c.Changed, ",") != "data" {
		t.Errorf("image answer = %#v", c)
	}

	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"id": img.ID, "path": writeTempFile(t, "shot.gif", imagedoctest.AnimatedGIF())},
			"ends in .gif, but this document is PNG; a document's format never changes"},
		{map[string]any{"id": img.ID, "path": writeTempFile(t, "shot", imagedoctest.AnimatedGIF())},
			"This isn't a PNG image: its content is GIF."},
		{map[string]any{"id": text.ID, "path": writeTempFile(t, "shot.png", imagedoctest.PNG(8, 8))},
			"ends in .png, but this document is markdown; a document's format never changes"},
		{map[string]any{"id": text.ID, "path": writeTempFile(t, "page.html", []byte("<p>x</p>"))},
			"ends in .html, but this document is markdown; a document's format never changes"},
		{map[string]any{"id": text.ID, "path": writeTempFile(t, "x.md", []byte("x")), "content": "y"},
			"pass one of content, data or path, not several"},
		{map[string]any{"id": img.ID, "path": writeTempFile(t, "x.png", imagedoctest.PNG(8, 8)), "data": b64(imagedoctest.PNG(8, 8))},
			"pass one of content, data or path, not several"},
	} {
		_, err := s.callTool("update_document", mustJSON(t, tc.args))
		if err == nil || !strings.HasSuffix(err.Error(), tc.want) {
			t.Errorf("update_document %v: %v, want %q", tc.args, err, tc.want)
		}
	}
	if cur, _ := s.store.GetDocument(text.ID); cur.Content != "# v2\n" {
		t.Errorf("a refused path changed the text: %q", cur.Content)
	}
}

// TestDocumentPathErrors covers every refusal of the file itself, on both
// tools: each names the path, and none reads an oversized file.
func TestDocumentPathErrors(t *testing.T) {
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	if _, err := s.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Plan", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	missing := filepath.Join(dir, "missing.png")

	oversized := filepath.Join(dir, "huge.md")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	// Sparse: 9 MB by its size, nothing on disk.
	if err := f.Truncate(9 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()

	fifo := filepath.Join(dir, "pipe.md")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		label, path, want string
	}{
		{"relative", "notes/plan.md", `path must be absolute: "notes/plan.md" is not`},
		{"empty", "", `path must be absolute: "" is not`},
		{"missing", missing, "no file at " + missing},
		{"directory", dir, dir + " is a directory, not a file"},
		{"oversized", oversized, oversized + " is 9.0 MB. The limit is 8 MB."},
		// A pipe would block reading, and is refused without being opened.
		{"pipe", fifo, fifo + " is not a regular file"},
	}
	if os.Geteuid() != 0 { // root reads anything
		unreadable := writeTempFile(t, "secret.md", []byte("x"))
		if err := os.Chmod(unreadable, 0); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, struct{ label, path, want string }{
			"unreadable", unreadable, "cannot read " + unreadable + ": permission denied"})
	}

	for _, tc := range cases {
		for _, call := range []struct {
			tool string
			args map[string]any
		}{
			{"create_document", map[string]any{"ticket": "DOC-1", "name": "New", "path": tc.path}},
			{"update_document", map[string]any{"ticket": "DOC-1", "id": "Plan", "path": tc.path}},
		} {
			_, err := s.callTool(call.tool, mustJSON(t, call.args))
			if err == nil || err.Error() != tc.want {
				t.Errorf("%s %s: %v, want %q", call.tool, tc.label, err, tc.want)
			}
		}
	}
	if docs, _ := s.store.ListDocuments(db.DocumentOwner{TicketID: tk.ID}); len(docs) != 1 {
		t.Errorf("a refused path created a document: %d documents", len(docs))
	}
}

// endless is a file that never ends, counting what is read from it.
type endless struct{ read int }

func (e *endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	e.read += len(p)
	return len(p), nil
}

// TestReadAtMostDocumentStopsAtTheLimit: a file that grows past the limit
// after it was looked at is refused after one byte past the limit, never
// read in full.
func TestReadAtMostDocumentStopsAtTheLimit(t *testing.T) {
	r := &endless{}
	_, err := readAtMostDocument("/tmp/growing.md", r)
	if err == nil || err.Error() != "/tmp/growing.md is over 8 MB. The limit is 8 MB." {
		t.Fatalf("err = %v", err)
	}
	if r.read != models.MaxDocumentBytes+1 {
		t.Fatalf("read %d bytes, want %d", r.read, models.MaxDocumentBytes+1)
	}

	data, err := readAtMostDocument("/tmp/ok.md", io.LimitReader(&endless{}, models.MaxDocumentBytes))
	if err != nil || len(data) != models.MaxDocumentBytes {
		t.Fatalf("exactly the limit: %d bytes, %v", len(data), err)
	}
}

func TestDocumentToolsDescribePath(t *testing.T) {
	s := newTestServer(t)
	seen := 0
	for _, def := range s.toolDefinitions() {
		if def.Name != "create_document" && def.Name != "update_document" {
			continue
		}
		seen++
		path, ok := def.InputSchema.Properties["path"]
		if !ok {
			t.Fatalf("%s takes no path", def.Name)
		}
		for _, want := range []string{"absolute path", "read by this MCP server's own process", "Prefer it to content and data"} {
			if !strings.Contains(path.Description, want) {
				t.Errorf("%s path description lacks %q: %s", def.Name, want, path.Description)
			}
		}
		if !strings.Contains(def.Description, "prefer it to content and data") {
			t.Errorf("%s description does not prefer path: %s", def.Name, def.Description)
		}
		for _, arg := range []string{"content", "data"} {
			if !strings.Contains(def.InputSchema.Properties[arg].Description, "pass path instead") {
				t.Errorf("%s %s description does not point to path", def.Name, arg)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("saw %d of the two tools", seen)
	}
}

// TestDocumentPathRefusesBinaryAsText: a file read into a markdown or HTML
// document must be text. An extensionless screenshot is refused rather than
// saved as a markdown document full of binary bytes.
func TestDocumentPathRefusesBinaryAsText(t *testing.T) {
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	plan, err := s.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Plan", Content: "# v1"})
	if err != nil {
		t.Fatal(err)
	}
	shot := writeTempFile(t, "shot", imagedoctest.PNG(8, 8))
	pngFile := writeTempFile(t, "shot.png", imagedoctest.PNG(8, 8))
	notText := func(path string) string {
		return path + " is not text (it isn't valid UTF-8), so it can't be a markdown or HTML document; " +
			"for an image, give the file its extension or pass format"
	}

	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"create_document", map[string]any{"ticket": "DOC-1", "name": "Shot", "path": shot}, notText(shot)},
		{"create_document", map[string]any{"ticket": "DOC-1", "name": "Shot", "format": "markdown", "path": pngFile}, notText(pngFile)},
		{"create_document", map[string]any{"ticket": "DOC-1", "name": "Shot", "format": "html", "path": shot}, notText(shot)},
		{"update_document", map[string]any{"id": plan.ID, "path": shot}, notText(shot)},
	} {
		if _, err := s.callTool(tc.tool, mustJSON(t, tc.args)); err == nil || err.Error() != tc.want {
			t.Errorf("%s %v: %v, want %q", tc.tool, tc.args, err, tc.want)
		}
	}
	if docs, _ := s.store.ListDocuments(db.DocumentOwner{TicketID: tk.ID}); len(docs) != 1 {
		t.Errorf("binary created a document: %d documents", len(docs))
	}
	if cur, _ := s.store.GetDocument(plan.ID); cur.Content != "# v1" || cur.Revision != 1 {
		t.Errorf("binary replaced the text: %q rev %d", cur.Content, cur.Revision)
	}

	// The same extensionless file is fine as an image when format says so,
	// and UTF-8 text with no extension is fine as markdown.
	if _, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"ticket": "DOC-1", "name": "Shot", "format": "png", "path": shot,
	})); err != nil {
		t.Errorf("extensionless png with format: %v", err)
	}
	if _, err := s.callTool("update_document", mustJSON(t, map[string]any{
		"id": plan.ID, "path": writeTempFile(t, "NOTES", []byte("# Überblick ✓\n")),
	})); err != nil {
		t.Errorf("extensionless UTF-8 text: %v", err)
	}
}
