package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const testIndexHTML = `<!doctype html><html><body><div id="root"></div></body></html>`

// newStaticServer serves a stand-in web build. Static requests never reach the
// store, so there is none.
func newStaticServer(t *testing.T) string {
	t.Helper()
	webFS := fstest.MapFS{
		"index.html":           {Data: []byte(testIndexHTML)},
		"vite.svg":             {Data: []byte("<svg></svg>")},
		"assets/index-abc.js":  {Data: []byte("console.log('app')")},
		"assets/index-abc.css": {Data: []byte("body{}")},
	}
	ts := httptest.NewServer(New(nil, webFS))
	t.Cleanup(ts.Close)
	return ts.URL
}

func getStatic(t *testing.T, url string) (int, string, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

func TestStaticServesFilesThatExist(t *testing.T) {
	base := newStaticServer(t)
	for path, want := range map[string]string{
		"/assets/index-abc.js":  "console.log('app')",
		"/assets/index-abc.css": "body{}",
		"/vite.svg":             "<svg></svg>",
	} {
		status, _, body := getStatic(t, base+path)
		if status != http.StatusOK || body != want {
			t.Errorf("GET %s = %d %q, want 200 %q", path, status, body, want)
		}
	}
}

// A missing asset must be a 404, not index.html: a stale bundle reference
// would otherwise fail as a JavaScript parse error of the HTML page.
func TestStaticMissingFileIsNotFound(t *testing.T) {
	base := newStaticServer(t)
	for _, path := range []string{
		"/assets/nope.js",
		"/assets/nope.css",
		"/assets/nope",
		"/assets/deep/nope.js",
		"/nope.js",
		"/favicon.ico",
		"/kanban/nope.png",
	} {
		status, _, body := getStatic(t, base+path)
		if status != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, status)
		}
		if strings.Contains(body, `<div id="root">`) {
			t.Errorf("GET %s served index.html", path)
		}
	}
}

// Any other path is the web app's to route, so a hard reload works on every
// client route; the app itself shows its not-found page for unknown ones,
// including the removed /board, /tickets and /graph.
func TestStaticClientRoutesFallBackToIndex(t *testing.T) {
	base := newStaticServer(t)
	for _, path := range []string{
		"/",
		"/kanban",
		"/table",
		"/epics",
		"/projects",
		"/labels",
		"/?project=ACP&ticket=ACP-7",
		"/kanban?project=ACP&label=web&ticket=ACP-7&doc=spec",
		"/table/",
		"/board",
		"/tickets",
		"/graph",
		"/no/such/page",
		"/assets-like",
	} {
		status, contentType, body := getStatic(t, base+path)
		if status != http.StatusOK || body != testIndexHTML {
			t.Errorf("GET %s = %d %q, want 200 with index.html", path, status, body)
		}
		if !strings.HasPrefix(contentType, "text/html") {
			t.Errorf("GET %s Content-Type = %q, want text/html", path, contentType)
		}
	}
}

// The API never falls back to the web app.
func TestStaticUnknownAPIPathIsNotFound(t *testing.T) {
	base := newStaticServer(t)
	status, _, body := getStatic(t, base+"/api/nope")
	if status != http.StatusNotFound || strings.Contains(body, `<div id="root">`) {
		t.Errorf("GET /api/nope = %d %q, want a 404 that is not index.html", status, body)
	}
}
