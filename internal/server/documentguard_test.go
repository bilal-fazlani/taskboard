package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func TestDocumentGuardPlacement(t *testing.T) {
	const g = "GUARD"
	for _, tc := range []struct {
		name, page, want string
	}{
		{"doctype", "<!DOCTYPE html><html><p>x", "<!DOCTYPE html>" + g + "<html><p>x"},
		{"lower-case doctype", "<!doctype html>\n<p>x", "<!doctype html>" + g + "\n<p>x"},
		{"legacy doctype", `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd"><p>x`,
			`<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">` + g + "<p>x"},
		{"no doctype", "<h1>Report</h1>", g + "<h1>Report</h1>"},
		{"no doctype, leading whitespace", "\n  <h1>Report</h1>", "\n  " + g + "<h1>Report</h1>"},
		{"empty", "", g},
		{"byte order mark", byteOrderMark + "<!DOCTYPE html><p>x", byteOrderMark + "<!DOCTYPE html>" + g + "<p>x"},
		{"byte order mark, no doctype", byteOrderMark + "<p>x", byteOrderMark + g + "<p>x"},
		{"leading comment", "<!-- generated -->\n<!DOCTYPE html><p>x", "<!-- generated -->\n<!DOCTYPE html>" + g + "<p>x"},
		{"everything before the doctype", byteOrderMark + " \t<!-- a -->\r\n<!-- b --!><?xml version=\"1.0\"?><!DOCTYPE html><p>x",
			byteOrderMark + " \t<!-- a -->\r\n<!-- b --!><?xml version=\"1.0\"?><!DOCTYPE html>" + g + "<p>x"},
		{"comment holding a doctype", "<!-- <!DOCTYPE html> --><p>x", "<!-- <!DOCTYPE html> -->" + g + "<p>x"},
		{"empty comments", "<!--><!---><!DOCTYPE html><p>x", "<!--><!---><!DOCTYPE html>" + g + "<p>x"},
		{"comment, no doctype", "<!-- note --><p>x", "<!-- note -->" + g + "<p>x"},
		{"unclosed comment", "<!-- never closed <p>x", g + "<!-- never closed <p>x"},
		{"unclosed doctype", "<!DOCTYPE html", "<!DOCTYPE html" + g},
		{"doctype after content stays put", "<p>x</p><!DOCTYPE html>", g + "<p>x</p><!DOCTYPE html>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := guardOffset(tc.page)
			if got := tc.page[:at] + g + tc.page[at:]; got != tc.want {
				t.Errorf("guard at %d:\n got %q\nwant %q", at, got, tc.want)
			}
		})
	}
}

// The guard is inlined in a <script>, so its source must not close it early.
func TestDocumentGuardStaysInItsScript(t *testing.T) {
	if strings.Contains(strings.ToLower(documentGuardJS), "</script") {
		t.Fatal("documentguard.js contains </script")
	}
	if !strings.HasPrefix(documentGuard, "<script>") || !strings.HasSuffix(documentGuard, "</script>") {
		t.Fatalf("guard tag = %.40q…", documentGuard)
	}
}

// The raw route serves an HTML page with the guard after its doctype, and
// the download route serves the stored bytes untouched.
func TestRawHTMLDocumentIsGuardedAndDownloadIsNot(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)
	content := byteOrderMark + "<!-- made by hand -->\n<!DOCTYPE html>\n<html><body><a href=\"#s2\">Two</a><h2 id=\"s2\">Two</h2></body></html>\n"
	page, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{
		TicketID: tk.ID, Name: "Toc", Format: models.DocumentFormatHTML, Content: content,
	})
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) string {
		t.Helper()
		resp, err := http.Get(r.url + "/api/documents/" + page.ID + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %q", path, resp.StatusCode, body)
		}
		return string(body)
	}

	want := byteOrderMark + "<!-- made by hand -->\n<!DOCTYPE html>" + documentGuard +
		"\n<html><body><a href=\"#s2\">Two</a><h2 id=\"s2\">Two</h2></body></html>\n"
	if got := get("/raw"); got != want {
		t.Errorf("raw:\n got %q\nwant %q", got, want)
	}
	if got := get("/download"); got != content {
		t.Errorf("download:\n got %q\nwant the stored content %q", got, content)
	}
}
