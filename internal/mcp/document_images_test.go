package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

func b64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

func TestCreateImageDocumentTool(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)

	// A name with its extension sets the format.
	got, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"full":   true,
		"ticket": "doc-1", "name": "Login screen.png", "data": b64(imagedoctest.PNG(40, 30)),
	}))
	if err != nil {
		t.Fatalf("create_document: %v", err)
	}
	d := got.(*models.Document)
	if d.Name != "Login screen" || d.Format != "png" || d.Width != 40 || d.Height != 30 ||
		d.URL != "http://board.test/?ticket=DOC-1&doc=Login+screen.png" {
		t.Errorf("created %+v", d.DocumentMeta)
	}

	// Or format does, with "jpg" for jpeg; a data: URL prefix and line breaks
	// are fine. The metadata goes.
	encoded := b64(imagedoctest.JPEGWithGPS(60, 40, 6))
	got, err = s.callTool("create_document", mustJSON(t, map[string]any{
		"full":   true,
		"ticket": "DOC-1", "name": "Photo", "format": "jpg",
		"data": "data:image/jpeg;base64," + encoded[:40] + "\n" + encoded[40:],
	}))
	if err != nil {
		t.Fatalf("create_document jpeg: %v", err)
	}
	photo := got.(*models.Document)
	if photo.Format != "jpeg" || photo.Width != 40 || photo.Height != 60 {
		t.Errorf("photo %+v", photo.DocumentMeta)
	}
	f, _ := s.store.GetDocumentImage(photo.ID)
	if secret, found := imagedoctest.HasSecrets(f.Data); found {
		t.Errorf("stored photo holds %s", secret)
	}

	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"ticket": "DOC-1", "name": "Shot", "format": "png", "data": b64(imagedoctest.JPEGWithGPS(8, 8, 0))},
			"This isn't a PNG image: its content is JPEG."},
		{map[string]any{"ticket": "DOC-1", "name": "Logo", "format": "svg", "data": b64([]byte("<svg/>"))},
			"SVG images can't be attached. Use PNG, JPEG, GIF or WebP."},
		{map[string]any{"ticket": "DOC-1", "name": "Logo.svg", "data": b64([]byte("<svg/>"))},
			"SVG images can't be attached. Use PNG, JPEG, GIF or WebP."},
		{map[string]any{"ticket": "DOC-1", "name": "Shot.png", "format": "gif", "data": b64(imagedoctest.PNG(8, 8))},
			`the name ends in .png but format is "gif"; pass one or make them agree`},
		{map[string]any{"ticket": "DOC-1", "name": "Shot", "data": b64(imagedoctest.PNG(8, 8))},
			`Format must be "png", "jpeg", "gif" or "webp" for an image.`},
		{map[string]any{"ticket": "DOC-1", "name": "Shot", "format": "png", "data": "not base64!"},
			"data must be the image file's bytes in base64"},
		{map[string]any{"ticket": "DOC-1", "name": "Shot", "format": "png", "data": b64(imagedoctest.PNG(8, 8)), "content": "x"},
			"pass content for a text document or data for an image, not both"},
		{map[string]any{"ticket": "DOC-1", "name": "Shot", "format": "png", "content": "x"},
			"An image is made from its file, not from text."},
		{map[string]any{"ticket": "DOC-1", "name": "login SCREEN.gif", "data": b64(imagedoctest.AnimatedGIF())},
			`This ticket already has a document called "Login screen.png".`},
		{map[string]any{"ticket": "DOC-1", "name": "Big", "format": "png",
			"data": b64(append(imagedoctest.PNG(8, 8), make([]byte, models.MaxDocumentBytes)...))},
			"This image is 8.1 MB. The limit is 8 MB."},
	} {
		if _, err := s.callTool("create_document", mustJSON(t, tc.args)); err == nil || err.Error() != tc.want {
			t.Errorf("create_document %v: %v, want %q", tc.args["name"], err, tc.want)
		}
	}
}

func TestGetDocumentToolReturnsThePicture(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)
	if _, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"ticket": "DOC-1", "name": "Mock.webp", "data": b64(imagedoctest.WebPWithGPS(1)),
	})); err != nil {
		t.Fatal(err)
	}

	// Through the JSON-RPC layer, as a client sees it.
	resp := s.handleRequest(jsonrpcRequest{JSONRPC: "2.0", ID: 7, Method: "tools/call",
		Params: mustJSON(t, map[string]any{"name": "get_document", "arguments": map[string]any{"id": "mock.webp", "ticket": "DOC-1"}})})
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Data     string `json:"data"`
				MimeType string `json:"mimeType"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	c := out.Result.Content
	if out.Result.IsError || len(c) != 2 || c[0].Type != "text" || c[1].Type != "image" || c[1].MimeType != "image/webp" {
		t.Fatalf("get_document result: %s", raw)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(c[0].Text), &meta); err != nil {
		t.Fatalf("details are not JSON: %q", c[0].Text)
	}
	if meta["name"] != "Mock" || meta["format"] != "webp" || meta["width"] != float64(64) || meta["height"] != float64(48) ||
		meta["url"] != "http://board.test/?ticket=DOC-1&doc=Mock.webp" || meta["size"] == nil {
		t.Errorf("details %v", meta)
	}
	if _, has := meta["content"]; has {
		t.Error("the details carry an empty content field")
	}
	if _, has := meta["preview"]; has {
		t.Error("a small image came as a preview")
	}
	if meta["downloadUrl"] != "http://board.test/api/documents/"+meta["id"].(string)+"/download" {
		t.Errorf("downloadUrl = %v", meta["downloadUrl"])
	}
	picture, err := base64.StdEncoding.DecodeString(c[1].Data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(picture, []byte("RIFF")) || float64(len(picture)) != meta["size"] {
		t.Errorf("picture is %d bytes, size %v", len(picture), meta["size"])
	}
	if secret, found := imagedoctest.HasSecrets(picture); found {
		t.Errorf("picture holds %s", secret)
	}

	// Text documents still come back as JSON text.
	if _, err := s.callTool("create_document", mustJSON(t, map[string]any{"ticket": "DOC-1", "name": "Plan", "content": "# Plan"})); err != nil {
		t.Fatal(err)
	}
	got, err := s.callTool("get_document", mustJSON(t, map[string]any{"id": "Plan", "ticket": "DOC-1"}))
	if err != nil || got.(*models.Document).Content != "# Plan" {
		t.Errorf("text get_document: %+v, %v", got, err)
	}
	listed, err := s.callTool("list_documents", mustJSON(t, map[string]any{"ticket": "DOC-1"}))
	if docs := listed.([]models.DocumentMeta); err != nil || len(docs) != 2 || docs[0].Width != 64 || docs[0].URL == "" {
		t.Errorf("list_documents: %+v, %v", listed, err)
	}
}

func TestUpdateImageDocumentTool(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)
	got, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"full":   true,
		"ticket": "DOC-1", "name": "Shot.png", "data": b64(imagedoctest.PNG(40, 30)),
	}))
	if err != nil {
		t.Fatal(err)
	}
	d := got.(*models.Document)

	got, err = s.callTool("update_document", mustJSON(t, map[string]any{
		"full": true,
		"id":   "shot.png", "ticket": "DOC-1", "data": b64(imagedoctest.PNG(20, 50)), "name": "Final shot",
	}))
	if err != nil {
		t.Fatalf("update_document: %v", err)
	}
	u := got.(*models.Document)
	if u.ID != d.ID || u.Revision != 2 || u.Width != 20 || u.Height != 50 || u.Name != "Final shot" ||
		u.URL != "http://board.test/?ticket=DOC-1&doc=Final+shot.png" {
		t.Errorf("updated %+v", u.DocumentMeta)
	}

	text, _ := s.callTool("create_document", mustJSON(t, map[string]any{"full": true, "ticket": "DOC-1", "name": "Plan", "content": "# Plan"}))
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"id": d.ID, "data": b64(imagedoctest.AnimatedGIF())}, "This isn't a PNG image: its content is GIF."},
		{map[string]any{"id": d.ID, "content": "text"}, "An image's content is replaced with a new image file, not text."},
		{map[string]any{"id": text.(*models.Document).ID, "data": b64(imagedoctest.PNG(8, 8))}, "This document isn't an image."},
		{map[string]any{"id": d.ID, "data": b64(imagedoctest.PNG(8, 8)), "content": "x"}, "pass content for a text document or data for an image, not both"},
	} {
		if _, err := s.callTool("update_document", mustJSON(t, tc.args)); err == nil || err.Error() != tc.want {
			t.Errorf("update_document %v: %v, want %q", tc.args, err, tc.want)
		}
	}
	// A refused name leaves the picture alone.
	if _, err := s.callTool("update_document", mustJSON(t, map[string]any{
		"id": d.ID, "data": b64(imagedoctest.PNG(33, 33)), "name": "Bad/name",
	})); err == nil || err.Error() != "Use letters, digits, spaces, _ and - only." {
		t.Errorf("bad name with data: %v", err)
	}
	if cur, _ := s.store.GetDocument(d.ID); cur.Width != 20 || cur.Revision != 2 {
		t.Errorf("a refused name still replaced the picture: %+v", cur.DocumentMeta)
	}
	// A refused picture leaves the name alone too.
	if _, err := s.callTool("update_document", mustJSON(t, map[string]any{"id": d.ID, "data": "bad!", "name": "Other"})); err == nil {
		t.Error("bad data was taken")
	}
	if cur, _ := s.store.GetDocument(d.ID); cur.Name != "Final shot" || cur.Revision != 2 {
		t.Errorf("after refusals: %+v", cur.DocumentMeta)
	}
}

func TestImageToolsOnEpics(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	if _, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: tk.ProjectID, Name: "Launch"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"full": true,
		"epic": "launch", "project": "DOC", "name": "Flow", "format": "gif", "data": b64(imagedoctest.AnimatedGIF()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if d := got.(*models.Document); d.EpicID == "" || !strings.Contains(d.URL, "doc=Flow.gif") {
		t.Errorf("epic image %+v", d.DocumentMeta)
	}
	result, err := s.callTool("get_document", mustJSON(t, map[string]any{"id": "Flow.gif", "epic": "Launch", "project": "DOC"}))
	if c, ok := result.(contentResult); err != nil || !ok || c[1].(imageContent).MimeType != "image/gif" {
		t.Errorf("get_document on an epic image: %#v, %v", result, err)
	}
}

// callGetDocument calls get_document through the JSON-RPC layer and returns
// the details and the picture.
func callGetDocument(t *testing.T, s *MCPServer, args map[string]any) (map[string]any, []byte, string) {
	t.Helper()
	resp := s.handleRequest(jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: mustJSON(t, map[string]any{"name": "get_document", "arguments": args})})
	raw, _ := json.Marshal(resp)
	var out struct {
		Result struct {
			Content []struct {
				Type, Text, Data, MimeType string
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Result.Content) != 2 {
		t.Fatalf("get_document: %s", raw)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(out.Result.Content[0].Text), &meta); err != nil {
		t.Fatal(err)
	}
	picture, err := base64.StdEncoding.DecodeString(out.Result.Content[1].Data)
	if err != nil {
		t.Fatal(err)
	}
	return meta, picture, out.Result.Content[1].MimeType
}

func TestGetDocumentToolScalesLargeImages(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)
	stored := imagedoctest.PNG(3000, 1000)
	got, err := s.callTool("create_document", mustJSON(t, map[string]any{"full": true, "ticket": "DOC-1", "name": "Wide.png", "data": b64(stored)}))
	if err != nil {
		t.Fatal(err)
	}
	d := got.(*models.Document)

	meta, picture, mimeType := callGetDocument(t, s, map[string]any{"id": d.ID})
	cfg, format, err := image.DecodeConfig(bytes.NewReader(picture))
	if err != nil || format != "jpeg" || mimeType != "image/jpeg" || cfg.Width != 1568 || cfg.Height != 523 {
		t.Fatalf("picture %s %d×%d (%s), %v; want a 1568×523 JPEG", format, cfg.Width, cfg.Height, mimeType, err)
	}
	if meta["width"] != float64(3000) || meta["height"] != float64(1000) || meta["size"] != float64(len(stored)) {
		t.Errorf("details should describe the stored image: %v", meta)
	}
	preview, ok := meta["preview"].(map[string]any)
	if !ok || preview["width"] != float64(1568) || preview["height"] != float64(523) || preview["size"] != float64(len(picture)) ||
		preview["mimeType"] != "image/jpeg" || !strings.Contains(preview["note"].(string), "scaled-down copy") ||
		!strings.Contains(preview["note"].(string), "3000×1000") {
		t.Errorf("preview details %v", meta["preview"])
	}
	if meta["downloadUrl"] != "http://board.test/api/documents/"+d.ID+"/download" {
		t.Errorf("downloadUrl %v", meta["downloadUrl"])
	}
	// The stored file is untouched.
	if f, _ := s.store.GetDocumentImage(d.ID); !bytes.Equal(f.Data, stored) {
		t.Error("making the preview changed the stored file")
	}
}

func TestImageRenameAndDeleteKeepTextInStep(t *testing.T) {
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	desc := "![shot](Login screen.png)"
	if _, err := s.store.UpdateTicket(tk.ID, models.UpdateTicketRequest{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	page, err := s.store.CreateDocument(models.CreateDocumentRequest{
		TicketID: tk.ID, Name: "Page", Format: models.DocumentFormatHTML, Content: `<img src="Login%20screen.png">`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"ticket": "DOC-1", "name": "Login screen.png", "data": b64(imagedoctest.PNG(8, 8)),
	})); err != nil {
		t.Fatal(err)
	}

	// Renaming, with or without a new picture, rewrites the references and
	// says where.
	got, err := s.callTool("update_document", mustJSON(t, map[string]any{"full": true, "ticket": "DOC-1", "id": "Login screen.png", "name": "Home page"}))
	if err != nil {
		t.Fatalf("update_document: %v", err)
	}
	d := got.(*models.Document)
	want := []models.ImagePlace{{Kind: "description"}, {Kind: "document", DocumentID: page.ID, Name: "Page.html"}}
	if len(d.ReferencesUpdated) != 2 || d.ReferencesUpdated[0] != want[0] || d.ReferencesUpdated[1] != want[1] {
		t.Errorf("referencesUpdated = %+v", d.ReferencesUpdated)
	}
	got, err = s.callTool("update_document", mustJSON(t, map[string]any{
		"full":   true,
		"ticket": "DOC-1", "id": "Home page.png", "name": "Start", "data": b64(imagedoctest.PNG(9, 9)),
	}))
	if err != nil {
		t.Fatalf("update_document with data: %v", err)
	}
	if d := got.(*models.Document); len(d.ReferencesUpdated) != 2 {
		t.Errorf("referencesUpdated with data = %+v", d.ReferencesUpdated)
	}
	if tk, _ := s.store.GetTicket(tk.ID); tk.Description != "![shot](Start.png)" {
		t.Errorf("description = %q", tk.Description)
	}
	if p, _ := s.store.GetDocument(page.ID); p.Content != `<img src="Start.png">` || p.Revision != 3 {
		t.Errorf("Page.html = %q rev %d", p.Content, p.Revision)
	}

	// Deleting it is not refused; the result says where it was used.
	got, err = s.callTool("delete_document", mustJSON(t, map[string]any{"ticket": "DOC-1", "id": "start.png"}))
	if err != nil {
		t.Fatalf("delete_document: %v", err)
	}
	m := got.(map[string]any)
	used, _ := m["usedIn"].([]models.ImagePlace)
	if m["deleted"] != true || len(used) != 2 || used[0] != want[0] || used[1] != want[1] {
		t.Errorf("delete_document = %+v", m)
	}
	// A document nothing uses reports no places.
	got, err = s.callTool("delete_document", mustJSON(t, map[string]any{"ticket": "DOC-1", "id": "Page"}))
	if err != nil {
		t.Fatal(err)
	}
	if m := got.(map[string]any); m["deleted"] != true || m["usedIn"] != nil {
		t.Errorf("delete_document of a page = %+v", m)
	}

	for _, def := range s.toolDefinitions() {
		switch def.Name {
		case "update_document":
			if !strings.Contains(def.Description, "rewrites every reference to it by name") {
				t.Errorf("update_document description doesn't say a rename rewrites references: %s", def.Description)
			}
		case "delete_document":
			if !strings.Contains(def.Description, "is not refused") || !strings.Contains(def.Description, "usedIn") {
				t.Errorf("delete_document description doesn't say what deleting a used image does: %s", def.Description)
			}
		}
	}
}

func TestImageRefsHelpIsInToolDescriptions(t *testing.T) {
	s := newTestServer(t)
	forms := []string{"![](Login screen.png)", "![](<Login screen.png>)", "![](Login%20screen.png)",
		"with its extension", "shows as a missing image"}
	want := map[string][]string{
		"create_ticket":   {"Only that ticket's own images show, not its epic's"},
		"update_ticket":   {"Only that ticket's own images show, not its epic's"},
		"create_document": {`<img src="Login screen.png">`, "Only the ticket's or epic's own images resolve"},
		"update_document": {`<img src="Login screen.png">`, "Only the ticket's or epic's own images resolve"},
		"get_document":    {`<img src="Login screen.png">`, "Only the ticket's or epic's own images resolve"},
	}
	seen := 0
	for _, def := range s.toolDefinitions() {
		extra, ok := want[def.Name]
		if !ok {
			continue
		}
		seen++
		for _, w := range append(append([]string{}, forms...), extra...) {
			if !strings.Contains(def.Description, w) {
				t.Errorf("%s description lacks %q", def.Name, w)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("checked %d of the %d tools", seen, len(want))
	}
}
