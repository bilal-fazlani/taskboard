package imageref

import (
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

var loginScreen = Image{Name: "Login screen", Format: models.DocumentFormatPNG}

type renameCase struct {
	name      string
	img       Image
	newName   string
	in, want  string
	uses      int
	rewritten int
	left      int
}

func runRenameCases(t *testing.T, format string, cases []renameCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			img := c.img
			if img == (Image{}) {
				img = loginScreen
			}
			newName := c.newName
			if newName == "" {
				newName = "Home page"
			}
			if got := Uses(format, c.in, img); got != c.uses {
				t.Errorf("Uses = %d, want %d", got, c.uses)
			}
			got, res := Rename(format, c.in, img, newName)
			if got != c.want {
				t.Errorf("Rename:\n got %q\nwant %q", got, c.want)
			}
			if res.Rewritten != c.rewritten || res.Left != c.left {
				t.Errorf("Result = %+v, want Rewritten %d, Left %d", res, c.rewritten, c.left)
			}
		})
	}
}

func TestRenameMarkdownForms(t *testing.T) {
	runRenameCases(t, models.DocumentFormatMarkdown, []renameCase{
		{name: "bare with spaces", in: "See ![shot](Login screen.png) here.", want: "See ![shot](Home page.png) here.", uses: 1, rewritten: 1},
		{name: "bare with spaces, spaces inside the parentheses", in: "![shot](  Login screen.png  )", want: "![shot](  Home page.png  )", uses: 1, rewritten: 1},
		{name: "angle-bracketed", in: "![shot](<Login screen.png>)", want: "![shot](<Home page.png>)", uses: 1, rewritten: 1},
		{name: "angle-bracketed with a title", in: `![shot](<Login screen.png> "Login screen.png")`, want: `![shot](<Home page.png> "Login screen.png")`, uses: 1, rewritten: 1},
		{name: "percent-encoded", in: "![shot](Login%20screen.png)", want: "![shot](Home%20page.png)", uses: 1, rewritten: 1},
		{name: "percent-encoded with a title", in: "![shot](Login%20screen.png 'T')", want: "![shot](Home%20page.png 'T')", uses: 1, rewritten: 1},
		{name: "percent-encoded, spaces inside the parentheses", in: "![shot](\n  Login%20screen.png\n)", want: "![shot](\n  Home%20page.png\n)", uses: 1, rewritten: 1},
		{name: "angle-bracketed and percent-encoded", in: "![shot](<Login%20screen.png>)", want: "![shot](<Home%20page.png>)", uses: 1, rewritten: 1},
		{name: "character reference", in: "![shot](Login&#32;screen.png)", want: "![shot](Home&#32;page.png)", uses: 1, rewritten: 1},
		{name: "any case, extension kept as written", in: "![shot](<login SCREEN.PNG>)", want: "![shot](<Home page.PNG>)", uses: 1, rewritten: 1},
		{name: "a name without spaces gains %20", img: Image{Name: "Login", Format: "png"}, in: "![shot](Login.png)", want: "![shot](Home%20page.png)", uses: 1, rewritten: 1},
		{name: "spaced form to a name without spaces", newName: "Home", in: "![shot](Login screen.png)", want: "![shot](Home.png)", uses: 1, rewritten: 1},
		{name: "full reference", in: "![shot][r]\n\n[r]: <Login screen.png>\n", want: "![shot][r]\n\n[r]: <Home page.png>\n", uses: 1, rewritten: 1},
		{name: "collapsed and shortcut references share a definition", in: "![r][] and ![r]\n\n[r]: Login%20screen.png \"T\"\n", want: "![r][] and ![r]\n\n[r]: Home%20page.png \"T\"\n", uses: 2, rewritten: 1},
		{name: "definition on the next line", in: "![shot][r]\n\n[r]:\n  <Login screen.png>\n  'T'\n", want: "![shot][r]\n\n[r]:\n  <Home page.png>\n  'T'\n", uses: 1, rewritten: 1},
		{name: "definition no image uses is still kept in step", in: "Text.\n\n[r]: Login%20screen.png\n", want: "Text.\n\n[r]: Home%20page.png\n", uses: 0, rewritten: 1},
		{name: "in a list, a quote and a heading", in: "- ![a](<Login screen.png>)\n\n> ![b](Login%20screen.png)\n\n# ![c](Login screen.png)\n", want: "- ![a](<Home page.png>)\n\n> ![b](Home%20page.png)\n\n# ![c](Home page.png)\n", uses: 3, rewritten: 3},
		{name: "quoted destination on the next line", in: "> ![b](\n> Login%20screen.png)\n", want: "> ![b](\n> Home%20page.png)\n", uses: 1, rewritten: 1},
		{name: "inside a link", in: "[![a](Login%20screen.png)](https://example.com/Login%20screen.png)", want: "[![a](Home%20page.png)](https://example.com/Login%20screen.png)", uses: 1, rewritten: 1},
		{name: "several in one text", in: "![a](Login screen.png) and ![b](<Login screen.png>) and ![c](Other.png)", want: "![a](Home page.png) and ![b](<Home page.png>) and ![c](Other.png)", uses: 2, rewritten: 2},
		{name: "code span untouched", in: "`![a](Login screen.png)` and `![b](Login%20screen.png)`", want: "`![a](Login screen.png)` and `![b](Login%20screen.png)`", uses: 0},
		{name: "fenced code untouched", in: "```\n![a](Login%20screen.png)\n```\n", want: "```\n![a](Login%20screen.png)\n```\n", uses: 0},
		{name: "escaped untouched", in: `\![a](Login screen.png) and !\[b](Login screen.png)`, want: `\![a](Login screen.png) and !\[b](Login screen.png)`, uses: 0},
		{name: "a link is not an image", in: "[a](Login%20screen.png)", want: "[a](Login%20screen.png)", uses: 0},
		{name: "raw HTML is not shown by markdown", in: `<img src="Login screen.png">`, want: `<img src="Login screen.png">`, uses: 0},
		{name: "./ does not resolve in markdown", in: "![a](./Login%20screen.png)", want: "![a](./Login%20screen.png)", uses: 0},
		{name: "another name untouched", in: "![a](Login screen 2.png)", want: "![a](Login screen 2.png)", uses: 0},
		{name: "a %-escape spelled with a character reference is left", in: "![a](Login&#37;20screen.png)", want: "![a](Login&#37;20screen.png)", uses: 1, left: 1},
		{name: "JPEG answers to .jpeg", img: Image{Name: "Shot", Format: models.DocumentFormatJPEG}, newName: "Home", in: "![a](shot.JPEG) ![b](Shot.jpg)", want: "![a](Home.JPEG) ![b](Home.jpg)", uses: 2, rewritten: 2},
		{name: "non-ASCII letters keep their escaping", img: Image{Name: "Écran", Format: "png"}, newName: "Été 2", in: "![a](%C3%89cran.png) ![b](<Écran.png>)", want: "![a](%C3%89t%C3%A9%202.png) ![b](<Été 2.png>)", uses: 2, rewritten: 2},
		{name: "backslash escape kept", img: Image{Name: "login_screen", Format: "png"}, newName: "home_page", in: `![a](login\_screen.png)`, want: `![a](home\_page.png)`, uses: 1, rewritten: 1},
		{name: "tab-indented list item", in: "-\t![a](Login%20screen.png)\n\n1.  - ![b](<Login screen.png>)\n", want: "-\t![a](Home%20page.png)\n\n1.  - ![b](<Home page.png>)\n", uses: 2, rewritten: 2},
		{name: "spaced form inside emphasis and a link", in: "*![a](Login screen.png)* [see ![b](Login screen.png)](https://example.com)", want: "*![a](Home page.png)* [see ![b](Home page.png)](https://example.com)", uses: 2, rewritten: 2},
		{name: "an escaped backslash does not escape the image", in: `\\![a](Login screen.png)`, want: `\\![a](Home page.png)`, uses: 1, rewritten: 1},
		{name: "bare with spaces across a line break", in: "Look: ![shot](\n  Login screen.png\n) here", want: "Look: ![shot](\n  Home page.png\n) here", uses: 1, rewritten: 1},
		{name: "a leading BOM is skipped, as the web skips it", in: "\uFEFF```\n![a](<Login screen.png>)\n```\n\nShown: ![b](Login%20screen.png)\n", want: "\uFEFF```\n![a](<Login screen.png>)\n```\n\nShown: ![b](Home%20page.png)\n", uses: 1, rewritten: 1},
		{name: "a BOM before an indented code block and a list", in: "\uFEFF    ![a](Login%20screen.png)\n\n- ![b](<Login screen.png>)\n", want: "\uFEFF    ![a](Login%20screen.png)\n\n- ![b](<Home page.png>)\n", uses: 1, rewritten: 1},
		{name: "a BOM before a spaced reference", in: "\uFEFF![a](Login screen.png)", want: "\uFEFF![a](Home page.png)", uses: 1, rewritten: 1},
		{name: "empty text", in: "", want: "", uses: 0},
	})
}

func TestRenameHTMLForms(t *testing.T) {
	runRenameCases(t, models.DocumentFormatHTML, []renameCase{
		{name: "src bare", in: `<img src="Login screen.png" alt="Login screen.png">`, want: `<img src="Home page.png" alt="Login screen.png">`, uses: 1, rewritten: 1},
		{name: "src with ./, single quotes", in: `<img src='./Login screen.png'>`, want: `<img src='./Home page.png'>`, uses: 1, rewritten: 1},
		{name: "src percent-encoded", in: `<img src="Login%20screen.png">`, want: `<img src="Home%20page.png">`, uses: 1, rewritten: 1},
		{name: "src unquoted", in: `<img src=Login%20screen.png>`, want: `<img src=Home%20page.png>`, uses: 1, rewritten: 1},
		{name: "src unquoted gains %20", img: Image{Name: "Login", Format: "png"}, in: `<img src=./Login.png alt=x>`, want: `<img src=./Home%20page.png alt=x>`, uses: 1, rewritten: 1},
		{name: "src with a character reference", in: `<img src="Login&#32;screen.png">`, want: `<img src="Home&#32;page.png">`, uses: 1, rewritten: 1},
		{name: "src on any element, any case", in: `<picture><SOURCE SRC="login screen.PNG"><img
  class="x"
  src = "Login screen.png"></picture>`, want: `<picture><SOURCE SRC="Home page.PNG"><img
  class="x"
  src = "Home page.png"></picture>`, uses: 2, rewritten: 2},
		{name: "srcset, each URL", in: `<img srcset="Login%20screen.png 1x, Other.png 2x,./Login%20screen.png 3x">`, want: `<img srcset="Home%20page.png 1x, Other.png 2x,./Home%20page.png 3x">`, uses: 2, rewritten: 2},
		{name: "srcset URL gains %20", img: Image{Name: "Login", Format: "png"}, in: `<img srcset="Login.png">`, want: `<img srcset="Home%20page.png">`, uses: 1, rewritten: 1},
		{name: "style attribute url(), quoted and not", in: `<div style="background: url('Login screen.png'); border-image: URL(./Login%20screen.png)"></div>`, want: `<div style="background: url('Home page.png'); border-image: URL(./Home%20page.png)"></div>`, uses: 2, rewritten: 2},
		{name: "style attribute with character references", in: `<div style="background:url(&quot;Login screen.png&quot;)"></div>`, want: `<div style="background:url(&quot;Home page.png&quot;)"></div>`, uses: 1, rewritten: 1},
		{name: "style element, comments skipped", in: "<style>\n.a { background: url(\"Login screen.png\") }\n/* url(Login%20screen.png) */\n.b::after { content: \"url(Login%20screen.png)\" }\n</style>", want: "<style>\n.a { background: url(\"Home page.png\") }\n/* url(Login%20screen.png) */\n.b::after { content: \"url(Login%20screen.png)\" }\n</style>", uses: 1, rewritten: 1},
		{name: "several attributes and duplicates", in: `<img src="Login screen.png" src="Login screen.png" data-src="Login screen.png">`, want: `<img src="Home page.png" src="Home page.png" data-src="Login screen.png">`, uses: 2, rewritten: 2},
		{name: "comments, scripts, textareas and hrefs untouched", in: `<!-- <img src="Login screen.png"> --><script>var s = "<img src='Login screen.png'>"</script><textarea><img src="Login screen.png"></textarea><a href="Login screen.png">x</a>`, want: `<!-- <img src="Login screen.png"> --><script>var s = "<img src='Login screen.png'>"</script><textarea><img src="Login screen.png"></textarea><a href="Login screen.png">x</a>`, uses: 0},
		{name: "other paths untouched", in: `<img src="../01ABC/Login screen.png"><img src="/api/documents/01ABC/Login screen.png"><img src="https://example.com/Login screen.png">`, want: `<img src="../01ABC/Login screen.png"><img src="/api/documents/01ABC/Login screen.png"><img src="https://example.com/Login screen.png">`, uses: 0},
		{name: "CSS escapes untouched", in: `<div style="background:url('Login\20 screen.png')"></div>`, want: `<div style="background:url('Login\20 screen.png')"></div>`, uses: 0},
		{name: "unquoted url() with a space is not valid CSS", in: `<div style="background:url(Login screen.png)"></div>`, want: `<div style="background:url(Login screen.png)"></div>`, uses: 0},
		{name: "whole page", in: "<!doctype html>\n<html><head><title>Login screen.png</title></head><body>\n<p>Login screen.png</p><img src=\"Login screen.png\">\n</body></html>\n", want: "<!doctype html>\n<html><head><title>Login screen.png</title></head><body>\n<p>Login screen.png</p><img src=\"Home page.png\">\n</body></html>\n", uses: 1, rewritten: 1},
	})
}

func TestRenameLeavesUnknownFormatsAlone(t *testing.T) {
	got, res := Rename("png", "![a](Login screen.png)", loginScreen, "Home page")
	if got != "![a](Login screen.png)" || res != (Result{}) {
		t.Fatalf("Rename = %q, %+v", got, res)
	}
}

func TestLocateAttrsAgreesWithTheTokenizer(t *testing.T) {
	raw := `<img  a=1 b = "two" c='3'/d e>`
	got := locateAttrs(raw)
	want := []struct{ key, val string }{{"a", "1"}, {"b", "two"}, {"c", "3"}, {"d", ""}, {"e", ""}}
	if len(got) != len(want) {
		t.Fatalf("got %d attributes, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].key != w.key || raw[got[i].start:got[i].end] != w.val {
			t.Errorf("attribute %d = %q=%q, want %q=%q", i, got[i].key, raw[got[i].start:got[i].end], w.key, w.val)
		}
	}
}
