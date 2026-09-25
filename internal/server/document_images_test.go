package server

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
)

// sendBytes makes a request with a raw body and headers, and returns the
// response with its body read.
func sendBytes(t *testing.T, method, u string, body []byte, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, u, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

func seedImageTicket(t *testing.T, r *running) *models.Ticket {
	t.Helper()
	p, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := r.srv.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Has images"})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func uploadURL(r *running, owner, filename string) string {
	return r.url + "/api/documents/images?" + owner + "&filename=" + url.QueryEscape(filename)
}

func upload(t *testing.T, r *running, owner, filename string, data []byte) models.Document {
	t.Helper()
	resp, body := sendBytes(t, http.MethodPost, uploadURL(r, owner, filename), data, map[string]string{"Content-Type": "application/octet-stream"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload %s: %d %s", filename, resp.StatusCode, body)
	}
	var d models.Document
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func wantImageHeaders(t *testing.T, resp *http.Response, contentType string) {
	t.Helper()
	h := resp.Header
	if h.Get("Content-Type") != contentType {
		t.Errorf("Content-Type = %q, want %q", h.Get("Content-Type"), contentType)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("no nosniff")
	}
	if !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Errorf("CSP = %q", h.Get("Content-Security-Policy"))
	}
	if h.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Errorf("CORP = %q", h.Get("Cross-Origin-Resource-Policy"))
	}
}

func TestUploadAndServeImages(t *testing.T) {
	r := serve(t)
	tk := seedImageTicket(t, r)

	cases := []struct {
		filename, format, display, contentType string
		data                                   []byte
		width, height                          int
	}{
		{"Login screen.png", "png", "Login screen.png", "image/png", imagedoctest.PNG(40, 30), 40, 30},
		{"IMG_0042.JPEG", "jpeg", "IMG_0042.jpg", "image/jpeg", imagedoctest.JPEGWithGPS(60, 40, 6), 40, 60},
		{"spinner.gif", "gif", "spinner.gif", "image/gif", imagedoctest.AnimatedGIF(), 20, 10},
		{"mock-up.webp", "webp", "mock-up.webp", "image/webp", imagedoctest.WebPWithGPS(1), 64, 48},
	}
	for _, c := range cases {
		d := upload(t, r, "ticket=DOC-1", c.filename, c.data)
		if models.DocumentDisplayName(d.Name, d.Format) != c.display || d.Format != c.format ||
			d.Width != c.width || d.Height != c.height || d.Revision != 1 || d.Content != "" {
			t.Errorf("%s: created %+v", c.filename, d)
		}

		// The image, by id and by name with the owner.
		for _, u := range []string{
			r.url + "/api/documents/" + d.ID + "/image",
			r.url + "/api/documents/" + url.PathEscape(c.display) + "/image?ticket=doc-1",
		} {
			resp, body := sendBytes(t, http.MethodGet, u, nil, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s: %d %s", u, resp.StatusCode, body)
			}
			wantImageHeaders(t, resp, c.contentType)
			if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "inline") {
				t.Errorf("Content-Disposition = %q", resp.Header.Get("Content-Disposition"))
			}
			if len(body) != d.Size {
				t.Errorf("%s: %d bytes, size says %d", c.filename, len(body), d.Size)
			}
			if s, found := imagedoctest.HasSecrets(body); found {
				t.Errorf("%s: served file holds %s", c.filename, s)
			}
			if _, _, err := image.DecodeConfig(bytes.NewReader(body)); err != nil && c.format != "webp" {
				t.Errorf("%s: served file does not decode: %v", c.filename, err)
			}
		}

		// Download: the stripped file, named with the display name.
		resp, body := sendBytes(t, http.MethodGet, r.url+"/api/documents/"+d.ID+"/download", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download: %d", resp.StatusCode)
		}
		wantImageHeaders(t, resp, c.contentType)
		if disp, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err != nil ||
			disp != "attachment" || params["filename"] != c.display {
			t.Errorf("Content-Disposition = %q, want an attachment named %q", resp.Header.Get("Content-Disposition"), c.display)
		}
		if s, found := imagedoctest.HasSecrets(body); found || len(body) != d.Size {
			t.Errorf("%s: download holds %s, %d bytes", c.filename, s, len(body))
		}

		// The thumbnail.
		resp, body = sendBytes(t, http.MethodGet, r.url+"/api/documents/"+d.ID+"/thumbnail", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("thumbnail: %d", resp.StatusCode)
		}
		wantImageHeaders(t, resp, resp.Header.Get("Content-Type"))
		cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || "image/"+format != resp.Header.Get("Content-Type") || cfg.Width != c.width || cfg.Height != c.height {
			t.Errorf("%s: thumbnail %s %d×%d (%s), %v", c.filename, format, cfg.Width, cfg.Height, resp.Header.Get("Content-Type"), err)
		}
	}

	// The document details carry the dimensions and no content.
	docs, status := doRequest[[]models.DocumentMeta](t, http.MethodGet, r.url+"/api/tickets/"+tk.ID+"/documents", "")
	if status != http.StatusOK || len(docs) != 4 || docs[1].Width != 40 || docs[1].Height != 60 {
		t.Errorf("list: %d %+v", status, docs)
	}
}

func TestImageCachingAndReplace(t *testing.T) {
	r := serve(t)
	seedImageTicket(t, r)
	d := upload(t, r, "ticket=DOC-1", "Shot.png", imagedoctest.PNG(40, 30))
	imageURL := r.url + "/api/documents/" + d.ID + "/image"

	resp, _ := sendBytes(t, http.MethodGet, imageURL, nil, nil)
	etag := resp.Header.Get("ETag")
	if etag == "" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("ETag %q, Cache-Control %q", etag, resp.Header.Get("Cache-Control"))
	}
	resp, _ = sendBytes(t, http.MethodGet, imageURL, nil, map[string]string{"If-None-Match": etag})
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("unchanged image: %d, want 304", resp.StatusCode)
	}

	// Replace by name with the owner: a content save.
	resp, body := sendBytes(t, http.MethodPut, r.url+"/api/documents/shot.png/image?ticket=DOC-1", imagedoctest.PNG(20, 50), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replace: %d %s", resp.StatusCode, body)
	}
	var got models.Document
	json.Unmarshal(body, &got)
	if got.Revision != 2 || got.Width != 20 || got.Height != 50 || got.ID != d.ID {
		t.Errorf("replaced: %+v", got)
	}
	resp, body = sendBytes(t, http.MethodGet, imageURL, nil, map[string]string{"If-None-Match": etag})
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, imagedoctest.PNG(20, 50)) {
		t.Errorf("after replace: %d, new file %v", resp.StatusCode, bytes.Equal(body, imagedoctest.PNG(20, 50)))
	}
	resp, body = sendBytes(t, http.MethodGet, r.url+"/api/documents/"+d.ID+"/thumbnail", nil, nil)
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(body)); err != nil || cfg.Width != 20 || cfg.Height != 50 {
		t.Errorf("thumbnail after replace: %d×%d, %v", cfg.Width, cfg.Height, err)
	}

	// A replace keeps the format.
	resp, body = sendBytes(t, http.MethodPut, imageURL, imagedoctest.JPEGWithGPS(8, 8, 0), nil)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "This isn't a PNG image: its content is JPEG.") {
		t.Errorf("replace with a JPEG: %d %s", resp.StatusCode, body)
	}
	// Rename through the JSON route; text content is refused.
	body2, status := doRequest[models.Document](t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"name":"Final shot"}`)
	if status != http.StatusOK || body2.Name != "Final shot" || body2.Revision != 2 {
		t.Errorf("rename: %d %+v", status, body2)
	}
	e, status := errorBody(t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"content":"text"}`)
	if status != http.StatusBadRequest || e.Error != "An image's content is replaced with a new image file, not text." {
		t.Errorf("text content on an image: %d %q", status, e.Error)
	}
	resp, _ = sendBytes(t, http.MethodGet, r.url+"/api/documents/"+d.ID+"/download", nil, nil)
	if resp.Header.Get("Content-Disposition") != `attachment; filename="Final shot.png"` {
		t.Errorf("download after rename: %q", resp.Header.Get("Content-Disposition"))
	}
}

func TestImageUploadRefusals(t *testing.T) {
	r := serve(t)
	tk := seedImageTicket(t, r)
	text, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Plan", Content: "# Plan"})
	if err != nil {
		t.Fatal(err)
	}

	refused := func(u string, data []byte, want string) {
		t.Helper()
		resp, body := sendBytes(t, http.MethodPost, u, data, nil)
		var e apiError
		json.Unmarshal(body, &e)
		if resp.StatusCode != http.StatusBadRequest || e.Error != want {
			t.Errorf("%s: %d %q, want 400 %q", u, resp.StatusCode, e.Error, want)
		}
	}
	refused(uploadURL(r, "ticket=DOC-1", "photo.png"), imagedoctest.JPEGWithGPS(8, 8, 0), "This isn't a PNG image: its content is JPEG.")
	refused(uploadURL(r, "ticket=DOC-1", "logo.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"SVG images can't be attached. Use PNG, JPEG, GIF or WebP.")
	refused(uploadURL(r, "ticket=DOC-1", "logo.png"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"This isn't a PNG image: it's an SVG, and SVG images can't be attached.")
	refused(uploadURL(r, "ticket=DOC-1", "notes.txt"), []byte("hello"),
		"Only .md, .html, .htm, .png, .jpg, .jpeg, .gif and .webp files can be attached.")
	refused(uploadURL(r, "ticket=DOC-1", "notes.md"), []byte("# hello"), `Format must be "png", "jpeg", "gif" or "webp" for an image.`)
	big := append(imagedoctest.PNG(8, 8), make([]byte, models.MaxDocumentBytes)...)
	refused(uploadURL(r, "ticket=DOC-1", "big.png"), big, "This image is 8.1 MB. The limit is 8 MB.")
	refused(r.url+"/api/documents/images?ticket=DOC-1", imagedoctest.PNG(8, 8), `pass filename, the image's file name (e.g. "Login screen.png")`)
	refused(r.url+"/api/documents/images?filename=a.png", imagedoctest.PNG(8, 8), "pass ticket or epic")

	// A typed name wins over the file's, the extension still sets the format.
	d := upload(t, r, "ticket=DOC-1&name="+url.QueryEscape("Home page"), "IMG_1.png", imagedoctest.PNG(8, 8))
	if d.Name != "Home page" || d.Format != "png" {
		t.Errorf("typed name: %+v", d)
	}
	refused(uploadURL(r, "ticket=DOC-1", "home PAGE.png"), imagedoctest.PNG(8, 8), `This ticket already has a document called "Home page.png".`)

	// The JSON route takes text only.
	e, status := errorBody(t, http.MethodPost, r.url+"/api/documents",
		`{"ticketId":"`+tk.ID+`","name":"Shot","format":"png","content":"x"}`)
	if status != http.StatusBadRequest || e.Error != "An image is made from its file, not from text." {
		t.Errorf("JSON image create: %d %q", status, e.Error)
	}

	// A text document has no image or thumbnail.
	for _, route := range []string{"image", "thumbnail"} {
		e, status := errorBody(t, http.MethodGet, r.url+"/api/documents/"+text.ID+"/"+route, "")
		if status != http.StatusNotFound || e.Error != "This document isn't an image." {
			t.Errorf("%s of a text document: %d %q", route, status, e.Error)
		}
	}
	resp, body := sendBytes(t, http.MethodPut, r.url+"/api/documents/"+text.ID+"/image", imagedoctest.PNG(8, 8), nil)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "This document isn't an image.") {
		t.Errorf("replacing a text document's image: %d %s", resp.StatusCode, body)
	}
	for _, u := range []string{"/api/documents/nope/image", "/api/documents/nope/thumbnail"} {
		if _, status := errorBody(t, http.MethodGet, r.url+u, ""); status != http.StatusNotFound {
			t.Errorf("%s: %d", u, status)
		}
	}
	if resp, _ := sendBytes(t, http.MethodPut, r.url+"/api/documents/nope/image", imagedoctest.PNG(8, 8), nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("replacing an unknown image: %d", resp.StatusCode)
	}
}

// TestImageWritesRefuseOtherOrigins: uploads and replaces go through the
// cross-origin write guard like every other write, including from a
// sandboxed HTML document (Origin: null).
func TestImageWritesRefuseOtherOrigins(t *testing.T) {
	r := serve(t)
	seedImageTicket(t, r)
	d := upload(t, r, "ticket=DOC-1", "Shot.png", imagedoctest.PNG(8, 8))

	for _, headers := range []map[string]string{
		{"Origin": "https://evil.example", "Content-Type": "text/plain"},
		{"Origin": "null", "Content-Type": "text/plain"},
		{"Sec-Fetch-Site": "cross-site"},
	} {
		resp, _ := sendBytes(t, http.MethodPost, uploadURL(r, "ticket=DOC-1", "Other.png"), imagedoctest.PNG(8, 8), headers)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("upload with %v: %d, want 403", headers, resp.StatusCode)
		}
		resp, _ = sendBytes(t, http.MethodPut, r.url+"/api/documents/"+d.ID+"/image", imagedoctest.PNG(9, 9), headers)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("replace with %v: %d, want 403", headers, resp.StatusCode)
		}
	}
	docs, _ := r.srv.store.ListDocuments(db.DocumentOwner{TicketID: d.TicketID})
	if len(docs) != 1 || docs[0].Revision != 1 {
		t.Errorf("the writes went through: %+v", docs)
	}
	// Same origin is fine.
	resp, _ := sendBytes(t, http.MethodPut, r.url+"/api/documents/"+d.ID+"/image", imagedoctest.PNG(9, 9),
		map[string]string{"Origin": r.url, "Sec-Fetch-Site": "same-origin"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("same-origin replace: %d", resp.StatusCode)
	}
}

func TestEpicImagesOverHTTP(t *testing.T) {
	r := serve(t)
	p, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.srv.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch plan"})
	if err != nil {
		t.Fatal(err)
	}
	d := upload(t, r, "epic="+url.QueryEscape("launch plan")+"&project=DOC", "Flow.gif", imagedoctest.AnimatedGIF())
	if d.EpicID != e.ID {
		t.Fatalf("epic image %+v", d)
	}
	resp, _ := sendBytes(t, http.MethodGet, r.url+"/api/documents/Flow.gif/image?epic="+e.ID, nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/gif" {
		t.Errorf("epic image by name: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	got, status := doRequest[models.Epic](t, http.MethodGet, r.url+"/api/epics/"+e.ID, "")
	if status != http.StatusOK || got.DocumentCount != 1 || got.Documents[0].Width != 20 {
		t.Errorf("epic details: %d %+v", status, got)
	}
}
