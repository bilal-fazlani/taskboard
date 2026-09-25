package doctext

import (
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func TestHTMLKeepsOnlyVisibleText(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain text", `hello world`, "hello world"},
		{"tags and attributes", `<div class="card" title="tooltip"><a href="https://example.com/x">Open</a></div>`, "Open"},
		{"style", `<style>.stylesheet { color: red }</style><p>Body</p>`, "Body"},
		{"script", `<script>const chart = "hidden"</script><p>Shown</p>`, "Shown"},
		{"noscript", `<noscript>Enable scripts</noscript><p>Shown</p>`, "Shown"},
		{"template", `<template><p>Not rendered</p></template><p>Shown</p>`, "Shown"},
		{"comment", `<!-- a note --><p>Shown</p>`, "Shown"},
		{"head and title", `<html><head><title>Tab title</title><meta name="x" content="meta"></head><body><h1>Heading</h1></body></html>`, "Heading"},
		{"iframe fallback", `<iframe>fallback</iframe><p>Shown</p>`, "Shown"},
		{"entities", `<p>Fish &amp; chips &lt;3 caf&eacute; &#233;tude&nbsp;now</p>`, "Fish & chips <3 café étude now"},
		{"inline elements join", `<p><b>sty</b>le and <em>st</em><span>ory</span></p>`, "style and story"},
		{"blocks separate", `<p>a</p><p>b</p><div>c</div><ul><li>d</li><li>e</li></ul>`, "a\nb\nc\nd\ne"},
		{"line break separates", `one<br>two`, "one\ntwo"},
		{"table cells separate", `<table><tr><td>x</td><td>y</td></tr></table>`, "x\ny"},
		{"whitespace collapses", "<p>  lots \n\t of   space  </p>", "lots of space"},
		{"image alt ignored", `<p>Before<img alt="diagram" src="a.png">after</p>`, "Beforeafter"},
		{"empty", ``, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReadableText(models.DocumentFormatHTML, tc.in); got != tc.want {
				t.Errorf("ReadableText(html, %q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The case the ticket is about: "style" appears in the markup of almost any
// HTML page, and must not be searchable unless the page shows the word.
func TestHTMLStyleTagIsNotText(t *testing.T) {
	page := `<!doctype html><html><head><style>body { font: 14px sans-serif }</style></head>
<body><div style="color: red" class="styled"><p>Rollout plan</p></div></body></html>`
	got := ReadableText(models.DocumentFormatHTML, page)
	if strings.Contains(strings.ToLower(got), "style") {
		t.Errorf("readable text %q holds \"style\"", got)
	}
	if got != "Rollout plan" {
		t.Errorf("readable text = %q, want %q", got, "Rollout plan")
	}
}

func TestMarkdownKeepsRenderedText(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"heading and emphasis", "# Title\n\nSome **bold** and _it_alic text.", "Title\nSome bold and _it_alic text."},
		{"emphasis inside a word joins", "sty**le**", "style"},
		{"link keeps text, drops address", "See [the docs](https://example.com/stylesheet \"link title\").", "See the docs."},
		{"image keeps alt, drops address", "![Architecture diagram](img/arch.png \"image title\") below", "Architecture diagram below"},
		{"reference link", "Read [the spec][s].\n\n[s]: https://example.com/spec", "Read the spec."},
		{"inline code kept", "Call `doc write` now", "Call doc write now"},
		{"code block kept", "```go\nfunc main() {}\n```", "func main() {}"},
		{"indented code kept", "    x := 1", "x := 1"},
		{"lists", "- one\n- two\n\n1. three", "one\ntwo\nthree"},
		{"blockquote", "> quoted\n> text", "quoted text"},
		{"table", "| Name | Status |\n| --- | --- |\n| Alpha | done |", "Name\nStatus\nAlpha\ndone"},
		{"entities and escapes", "Fish &amp; chips, caf&eacute;, \\*not emphasis\\*", "Fish & chips, café, *not emphasis*"},
		{"autolink shows its address", "<https://example.com/a>", "https://example.com/a"},
		// react-markdown shows raw HTML as literal text, so it reads as written.
		{"raw html reads as written", "before\n\n<div class=\"x\">\nblock\n</div>\n\nshown <span class=\"y\">inline</span>", "before\n<div class=\"x\"> block </div>\nshown <span class=\"y\">inline</span>"},
		{"html comment reads as written", "a <!-- note --> b", "a <!-- note --> b"},
		{"thematic break", "a\n\n---\n\nb", "a\nb"},
		{"hard break", "one  \ntwo", "one\ntwo"},
		{"soft break", "one\ntwo", "one two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReadableText(models.DocumentFormatMarkdown, tc.in); got != tc.want {
				t.Errorf("ReadableText(markdown, %q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestUnknownFormatIsPlainText(t *testing.T) {
	if got := ReadableText("pdf", "  as   is "); got != "as is" {
		t.Errorf("ReadableText(pdf) = %q", got)
	}
}
