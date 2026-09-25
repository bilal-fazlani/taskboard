package server

import (
	_ "embed"
	"strings"
)

// documentGuardJS keeps an HTML document's own navigations out of the
// browser tab's history, so that one Back closes the document window on the
// board rather than stepping back inside the frame:
//
//   - history.pushState replaces the current entry instead of adding one;
//   - navigation.navigate always replaces;
//   - a click on a link that would navigate the page is redone with
//     location.replace. A same-page #anchor still scrolls, matches :target
//     and fires hashchange as it would have.
//
// Links with a target, a download attribute or a modifier key, and links to
// anything but http(s), are left to the browser. So are location.hash,
// location.href, location.assign, meta refresh and nested frames, which a
// page may still use to add entries; × on the board closes in one press
// regardless (web/src/lib/historyTraversal.ts).
//
//go:embed documentguard.js
var documentGuardJS string

// documentGuard is the tag the raw route inserts. The sandbox the page runs
// in allows scripts and sets no script-src, so an inline script runs.
var documentGuard = "<script>" + documentGuardJS + "</script>"

// withDocumentGuard returns an HTML page with the history guard inserted
// where it runs before the page's own scripts and changes nothing else.
func withDocumentGuard(page string) string {
	at := guardOffset(page)
	return page[:at] + documentGuard + page[at:]
}

// byteOrderMark is U+FEFF in UTF-8, which the browser drops from the start of
// the page before it parses anything.
const byteOrderMark = "\xef\xbb\xbf"

// guardOffset is where the guard goes in page: after any leading byte order
// mark, whitespace, comments and the doctype. Anything but a comment or
// whitespace before the doctype would put the page in quirks mode, so the
// guard cannot simply go first. Comments here are what the HTML tokenizer
// takes as one before the doctype: <!-- -->, and the bogus <!…> and <?…>.
func guardOffset(page string) int {
	i := 0
	if strings.HasPrefix(page, byteOrderMark) {
		i = len(byteOrderMark)
	}
	for {
		i += len(page[i:]) - len(strings.TrimLeft(page[i:], "\t\n\f\r "))
		rest := page[i:]
		switch {
		case len(rest) >= len("<!doctype") && strings.EqualFold(rest[:len("<!doctype")], "<!doctype"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return len(page)
			}
			return i + end + 1
		case strings.HasPrefix(rest, "<!--"):
			end := commentEnd(rest)
			if end < 0 {
				return i
			}
			i += end
		case strings.HasPrefix(rest, "<!"), strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return i
			}
			i += end + 1
		default:
			return i
		}
	}
}

// commentEnd is the length of the <!-- comment at the start of s, or -1 when
// it never closes. As the HTML tokenizer reads it, <!--> and <!---> are empty
// comments, and a comment ends at the first --> or --!>.
func commentEnd(s string) int {
	body := s[len("<!--"):]
	switch {
	case strings.HasPrefix(body, ">"):
		return len("<!-->")
	case strings.HasPrefix(body, "->"):
		return len("<!--->")
	}
	end := -1
	for _, marker := range []string{"-->", "--!>"} {
		if at := strings.Index(body, marker); at >= 0 && (end < 0 || at+len(marker) < end) {
			end = at + len(marker)
		}
	}
	if end < 0 {
		return -1
	}
	return len("<!--") + end
}
