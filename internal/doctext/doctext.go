// Package doctext works out the readable text of a document: the words a
// person sees when the document is shown, which is what the search matches.
// HTML keeps only its visible text; markdown keeps its rendered text, with
// link text, image alt text and code, but no markup and no addresses.
package doctext

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// markdown renders CommonMark with GFM tables. Raw HTML inside markdown is
// left out (goldmark's safe default), as the web's react-markdown leaves it
// out too.
var markdown = goldmark.New(goldmark.WithExtensions(extension.Table))

// ReadableText returns content's readable text for its format. Block
// elements (paragraphs, headings, list items, table cells, line breaks) end
// a line; inline elements run on, so "<b>sty</b>le" reads "style". Runs of
// whitespace within a line become one space, and empty lines are dropped.
func ReadableText(format, content string) string {
	switch format {
	case models.DocumentFormatHTML:
		return htmlText(content, false)
	case models.DocumentFormatMarkdown:
		var rendered bytes.Buffer
		if err := markdown.Convert([]byte(content), &rendered); err != nil {
			// goldmark only fails on a failing writer, which a buffer is not.
			return plainText(content)
		}
		return htmlText(rendered.String(), true)
	default:
		return plainText(content)
	}
}

func plainText(content string) string {
	var w textWriter
	w.text(content)
	return w.String()
}

// hidden are the elements whose contents never show on the page.
var hidden = map[atom.Atom]bool{
	atom.Head:     true,
	atom.Title:    true,
	atom.Script:   true,
	atom.Style:    true,
	atom.Noscript: true,
	atom.Template: true,
	atom.Iframe:   true,
}

// blocks are the elements a browser lays out on their own line (or cell),
// so the words either side of them never run together.
var blocks = map[atom.Atom]bool{
	atom.Address: true, atom.Article: true, atom.Aside: true, atom.Blockquote: true,
	atom.Body: true, atom.Br: true, atom.Caption: true, atom.Center: true,
	atom.Dd: true, atom.Details: true, atom.Dialog: true, atom.Dir: true,
	atom.Div: true, atom.Dl: true, atom.Dt: true, atom.Fieldset: true,
	atom.Figcaption: true, atom.Figure: true, atom.Footer: true, atom.Form: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Header: true, atom.Hgroup: true, atom.Hr: true, atom.Html: true,
	atom.Legend: true, atom.Li: true, atom.Listing: true, atom.Main: true,
	atom.Menu: true, atom.Nav: true, atom.Ol: true, atom.Optgroup: true,
	atom.Option: true, atom.P: true, atom.Plaintext: true, atom.Pre: true,
	atom.Search: true, atom.Section: true, atom.Select: true, atom.Summary: true,
	atom.Table: true, atom.Tbody: true, atom.Td: true, atom.Textarea: true,
	atom.Tfoot: true, atom.Th: true, atom.Thead: true, atom.Tr: true,
	atom.Ul: true, atom.Xmp: true,
}

// htmlText walks the parsed page and keeps its text nodes, which the parser
// has already entity-decoded. Attributes are never read, except an image's
// alt text when withAlt is set (markdown, where alt text is written as text).
func htmlText(page string, withAlt bool) string {
	root, err := html.Parse(strings.NewReader(page))
	if err != nil {
		// html.Parse only fails on a failing reader, which a string is not.
		return plainText(page)
	}
	var w textWriter
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			w.text(n.Data)
			return
		case html.ElementNode:
			if hidden[n.DataAtom] {
				return
			}
			if withAlt && n.DataAtom == atom.Img {
				for _, a := range n.Attr {
					if a.Namespace == "" && a.Key == "alt" {
						w.space()
						w.text(a.Val)
						w.space()
					}
				}
				return
			}
		case html.DocumentNode:
		default:
			// Comments and the doctype.
			return
		}
		block := n.Type == html.ElementNode && blocks[n.DataAtom]
		if block {
			w.lineBreak()
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if block {
			w.lineBreak()
		}
	}
	walk(root)
	return w.String()
}

// textWriter collects text, collapsing whitespace to single spaces and
// block boundaries to single line breaks, with nothing at either end.
type textWriter struct {
	b            strings.Builder
	pendingSpace bool
	pendingBreak bool
}

func (w *textWriter) space()     { w.pendingSpace = true }
func (w *textWriter) lineBreak() { w.pendingBreak = true }

func (w *textWriter) text(s string) {
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		if unicode.IsSpace(r) {
			w.pendingSpace = true
			continue
		}
		if w.b.Len() > 0 {
			if w.pendingBreak {
				w.b.WriteByte('\n')
			} else if w.pendingSpace {
				w.b.WriteByte(' ')
			}
		}
		w.pendingBreak, w.pendingSpace = false, false
		w.b.WriteRune(r)
	}
}

func (w *textWriter) String() string { return w.b.String() }
