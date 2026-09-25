package server

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
)

// htmlPage adds an HTML document to an owner ("ticket=DOC-1" style) and
// returns it.
func htmlPage(t *testing.T, r *running, owner models.CreateDocumentRequest, name, content string) *models.Document {
	t.Helper()
	owner.Name, owner.Format, owner.Content = name, models.DocumentFormatHTML, content
	d, err := r.srv.store.CreateDocument(owner)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// fromPage resolves src the way a browser does inside the page served at
// the web's raw URL for page (api.documents.rawUrl), and fetches it.
func fromPage(t *testing.T, r *running, page *models.Document, src string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	base, err := url.Parse(r.url + "/api/documents/" + page.ID + "/raw?rev=1")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := url.Parse(strings.ReplaceAll(src, " ", "%20"))
	if err != nil {
		t.Fatal(err)
	}
	return sendBytes(t, http.MethodGet, base.ResolveReference(ref).String(), nil, headers)
}

// The headers a sandboxed page's image request carries: an opaque origin
// is cross-site to everything.
var sandboxedImageRequest = map[string]string{
	"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "image", "Origin": "null",
}

func TestHTMLDocumentShowsItsOwnersImages(t *testing.T) {
	r := serve(t)
	tk := seedImageTicket(t, r)
	page := htmlPage(t, r, models.CreateDocumentRequest{TicketID: tk.ID}, "Report", `<img src="Login screen.png">`)
	login := upload(t, r, "ticket=DOC-1", "Login screen.png", imagedoctest.PNG(40, 30))
	upload(t, r, "ticket=DOC-1", "Photo.jpeg", imagedoctest.JPEGWithGPS(20, 10, 0))
	stored, err := r.srv.store.GetDocumentImage(login.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ src, contentType string }{
		{"Login screen.png", "image/png"},
		{"./Login screen.png", "image/png"},
		{"login%20SCREEN.PNG", "image/png"},
		{"Photo.jpg", "image/jpeg"},
		{"photo.JPEG", "image/jpeg"},
	} {
		resp, body := fromPage(t, r, page, c.src, sandboxedImageRequest)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", c.src, resp.StatusCode, body)
		}
		h := resp.Header
		if h.Get("Content-Type") != c.contentType || h.Get("X-Content-Type-Options") != "nosniff" ||
			h.Get("Content-Security-Policy") != imageCSP || h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: headers %v", c.src, h)
		}
		// Loadable from the page's opaque origin, but never readable by it.
		if got := h.Get("Cross-Origin-Resource-Policy"); got != "cross-origin" {
			t.Errorf("%s: CORP = %q, want cross-origin", c.src, got)
		}
		if got := h.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s: Access-Control-Allow-Origin = %q, want none", c.src, got)
		}
		if c.contentType == "image/png" && !bytes.Equal(body, stored.Data) {
			t.Errorf("%s: not the stored image", c.src)
		}
	}

	// An epic's HTML document shows the epic's image of that name, not a
	// ticket's.
	p, err := r.srv.store.GetProject(tk.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.srv.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	epicPage := htmlPage(t, r, models.CreateDocumentRequest{EpicID: e.ID}, "Board", `<img src="Login screen.gif">`)
	upload(t, r, "epic="+e.ID, "Login screen.gif", imagedoctest.AnimatedGIF())
	if resp, body := fromPage(t, r, epicPage, "Login screen.gif", sandboxedImageRequest); resp.StatusCode != http.StatusOK ||
		resp.Header.Get("Content-Type") != "image/gif" {
		t.Errorf("epic image: %d %s", resp.StatusCode, body)
	}
	if resp, _ := fromPage(t, r, epicPage, "Login screen.png", sandboxedImageRequest); resp.StatusCode != http.StatusNotFound {
		t.Errorf("the ticket's image from the epic's page: %d, want 404", resp.StatusCode)
	}

	// The page itself is served as before: sandboxed to scripts only.
	resp, _ := sendBytes(t, http.MethodGet, r.url+"/api/documents/"+page.ID+"/raw?rev=1", nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Security-Policy") != "sandbox allow-scripts" {
		t.Errorf("raw page: %d CSP %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}
	// /image keeps its same-origin policy for the web's own use.
	resp, _ = sendBytes(t, http.MethodGet, r.url+"/api/documents/"+login.ID+"/image", nil, nil)
	if resp.Header.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Errorf("/image CORP = %q", resp.Header.Get("Cross-Origin-Resource-Policy"))
	}
}

func TestRawRefusesAnImage(t *testing.T) {
	r := serve(t)
	seedImageTicket(t, r)
	d := upload(t, r, "ticket=DOC-1", "Shot.png", imagedoctest.PNG(8, 8))
	for _, path := range []string{"/api/documents/" + d.ID + "/raw", "/api/documents/Shot.png/raw?ticket=DOC-1"} {
		msg, status := errorBody(t, http.MethodGet, r.url+path, "")
		if status != http.StatusNotFound || msg.Error != "an image has no raw page; use /image" {
			t.Errorf("%s: %d %q", path, status, msg)
		}
	}
}

// TestReferencedImageRouteExposesOnlyTheOwnersImages: the route an HTML
// page's relative image names land on answers with nothing but images of
// the owner of the HTML document whose id it is given (whoever asks: the
// accepted boundary, see getReferencedImage), never their bytes to a reader
// on another origin, and gives the sandboxed page no way to read or write
// anything else on the board.
func TestReferencedImageRouteExposesOnlyTheOwnersImages(t *testing.T) {
	r := serve(t)
	tk := seedImageTicket(t, r)
	other, err := r.srv.store.CreateTicket(models.CreateTicketRequest{ProjectID: tk.ProjectID, Title: "Someone else's"})
	if err != nil {
		t.Fatal(err)
	}
	page := htmlPage(t, r, models.CreateDocumentRequest{TicketID: tk.ID}, "Report", "<p>x</p>")
	notes, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Notes", Content: "secret notes"})
	if err != nil {
		t.Fatal(err)
	}
	login := upload(t, r, "ticket=DOC-1", "Login screen.png", imagedoctest.PNG(8, 8))
	theirs := upload(t, r, "ticket=DOC-2", "Their shot.png", imagedoctest.PNG(8, 8))
	upload(t, r, "ticket=DOC-2", "Shared name.png", imagedoctest.PNG(8, 8))
	otherPage := htmlPage(t, r, models.CreateDocumentRequest{TicketID: other.ID}, "Theirs", "<p>y</p>")

	base := r.url + "/api/documents/"
	for _, path := range []string{
		// Another owner's image, by name, by id, or with an owner query the
		// route ignores.
		base + page.ID + "/Their%20shot.png",
		base + page.ID + "/Shared%20name.png",
		base + page.ID + "/" + theirs.ID,
		base + page.ID + "/Their%20shot.png?ticket=DOC-2",
		// The owner's documents that are not images, and the page itself.
		base + page.ID + "/Notes.md",
		base + page.ID + "/Notes",
		base + page.ID + "/Report.html",
		// An image needs its extension; an id is not a name.
		base + page.ID + "/Login%20screen",
		base + page.ID + "/" + login.ID,
		// Only an HTML document's id: not a markdown document, an image, a
		// name with an owner, a ticket or nothing.
		base + notes.ID + "/Login%20screen.png",
		base + login.ID + "/Login%20screen.png",
		base + "Report.html/Login%20screen.png?ticket=DOC-1",
		base + tk.ID + "/Login%20screen.png",
		base + "nope/Login%20screen.png",
		// A name is decoded once, as the browser encoded it, never twice.
		base + page.ID + "/Login%2520screen.png",
		// Escapes that would step out of the name.
		base + page.ID + "/..%2F" + login.ID + "%2Fimage",
		base + page.ID + "/%2e%2e",
	} {
		resp, body := sendBytes(t, http.MethodGet, path, nil, sandboxedImageRequest)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
		if strings.Contains(string(body), "secret notes") || strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
			t.Errorf("GET %s answered with %q %q", path, resp.Header.Get("Content-Type"), body)
		}
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("GET %s: Access-Control-Allow-Origin = %q", path, got)
		}
	}
	// The other ticket's page reaches its own images and not this one's.
	if resp, _ := fromPage(t, r, otherPage, "Their shot.png", sandboxedImageRequest); resp.StatusCode != http.StatusOK {
		t.Errorf("their page, their image: %d", resp.StatusCode)
	}
	if resp, _ := fromPage(t, r, otherPage, "Login screen.png", sandboxedImageRequest); resp.StatusCode != http.StatusNotFound {
		t.Errorf("their page, this ticket's image: %d, want 404", resp.StatusCode)
	}

	// The accepted boundary: the route is keyed by the HTML document's id,
	// not by the page asking. Knowing another owner's HTML document id lets
	// this page (../<id>/name) or any site display that owner's images, and
	// learn their size, but not read them: cross-origin CORP, no CORS.
	theirImage, err := r.srv.store.GetDocumentImage(theirs.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, get := range []func() (*http.Response, []byte){
		func() (*http.Response, []byte) {
			return sendBytes(t, http.MethodGet, base+otherPage.ID+"/Their%20shot.png", nil,
				map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "image", "Origin": "https://evil.example"})
		},
		func() (*http.Response, []byte) {
			return fromPage(t, r, page, "../"+otherPage.ID+"/Their shot.png", sandboxedImageRequest)
		},
	} {
		resp, body := get()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, theirImage.Data) {
			t.Errorf("their image by their page's id: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got != "cross-origin" {
			t.Errorf("their image by their page's id: CORP = %q, want cross-origin", got)
		}
		for _, h := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials"} {
			if got := resp.Header.Get(h); got != "" {
				t.Errorf("their image by their page's id: %s = %q, want none", h, got)
			}
		}
	}
	// Still nothing but images: their page's id reaches none of their other
	// documents, and never this ticket's.
	for _, name := range []string{"Theirs.html", "Login%20screen.png"} {
		if resp, _ := sendBytes(t, http.MethodGet, base+otherPage.ID+"/"+name, nil, sandboxedImageRequest); resp.StatusCode != http.StatusNotFound {
			t.Errorf("their page's id, %s: %d, want 404", name, resp.StatusCode)
		}
	}

	// The route only reads: other methods, from the page or anywhere, change
	// nothing.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, headers := range []map[string]string{{"Origin": "null", "Content-Type": "text/plain"}, nil} {
			resp, _ := sendBytes(t, method, base+page.ID+"/Login%20screen.png", imagedoctest.PNG(9, 9), headers)
			if resp.StatusCode < 400 {
				t.Errorf("%s with %v = %d, want a refusal", method, headers, resp.StatusCode)
			}
		}
	}
	if got, _ := r.srv.store.GetDocument(login.ID); got == nil || got.Revision != 1 {
		t.Errorf("the image changed: %+v", got)
	}

	// From the page's opaque origin, the rest of the API stays unreadable (no
	// CORS header, so the browser keeps the answer from the page) and
	// unwritable (Origin: null is refused).
	for _, path := range []string{
		"/api/tickets", "/api/tickets/" + tk.ID, "/api/projects", "/api/board",
		"/api/tickets/" + tk.ID + "/documents", "/api/documents/" + notes.ID,
		"/api/documents/" + notes.ID + "/raw", "/api/documents/" + login.ID + "/image",
		"/api/documents/" + login.ID + "/download",
	} {
		resp, _ := sendBytes(t, http.MethodGet, r.url+path, nil, map[string]string{"Origin": "null", "Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "cors"})
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("GET %s from Origin: null: Access-Control-Allow-Origin = %q", path, got)
		}
		if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "" {
			t.Errorf("GET %s from Origin: null: Access-Control-Allow-Credentials = %q", path, got)
		}
	}
	// The image and download routes still refuse to be embedded elsewhere.
	for _, path := range []string{"/api/documents/" + login.ID + "/image", "/api/documents/" + login.ID + "/thumbnail", "/api/documents/" + login.ID + "/download"} {
		resp, _ := sendBytes(t, http.MethodGet, r.url+path, nil, sandboxedImageRequest)
		if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
			t.Errorf("GET %s: CORP = %q, want same-origin", path, got)
		}
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/documents/" + notes.ID, `{"content":"pwned"}`},
		{http.MethodPut, "/api/documents/" + login.ID, `{"name":"pwned"}`},
		{http.MethodDelete, "/api/documents/" + login.ID, ""},
		{http.MethodPut, "/api/tickets/" + tk.ID, `{"title":"pwned"}`},
		{http.MethodPost, "/api/documents", `{"ticketId":"` + tk.ID + `","name":"pwned","content":"x"}`},
	} {
		resp, _ := sendBytes(t, tc.method, r.url+tc.path, []byte(tc.body), map[string]string{"Origin": "null", "Content-Type": "text/plain"})
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s from Origin: null = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
	docs, err := r.srv.store.ListDocuments(db.DocumentOwner{TicketID: tk.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 3 {
		t.Errorf("documents = %d, want the 3 seeded", len(docs))
	}
	if got, _ := r.srv.store.GetTicket(tk.ID); got == nil || got.Title != "Has images" {
		t.Errorf("the ticket changed: %+v", got)
	}
}
