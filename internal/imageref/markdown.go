package imageref

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdownParser is plain CommonMark, like the web's react-markdown, with
// its link parser wrapped to note where each inline image's destination
// sits in the source.
var markdownParser = newMarkdownParser()

func newMarkdownParser() parser.Parser {
	link := parser.NewLinkParser()
	var inlines []util.PrioritizedValue
	for _, v := range parser.DefaultInlineParsers() {
		if v.Value == link {
			v.Value = positionedLinkParser{link.(parser.InlineParser)}
		}
		inlines = append(inlines, v)
	}
	return parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
		parser.WithInlineParsers(inlines...),
		parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
	)
}

// destinationsKey holds, per parse, where each inline image's destination
// is: map[*ast.Image]destination.
var destinationsKey = parser.NewContextKey()

type destination struct {
	start, end int
	angle      bool
}

// positionedLinkParser is goldmark's link parser, noting for each inline
// image ![alt](dest "title") the bytes of dest. It finds them by stepping
// over "](" and the spaces after it, exactly as the link parser itself did,
// from the position the link parser started at.
type positionedLinkParser struct {
	parser.InlineParser
}

func (p positionedLinkParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) == 0 || line[0] != ']' {
		return p.InlineParser.Parse(parent, block, pc)
	}
	startLine, startPos := block.Position()
	n := p.InlineParser.Parse(parent, block, pc)
	img, ok := n.(*ast.Image)
	if !ok || img.Reference != nil || len(img.Destination) == 0 {
		return n
	}
	endLine, endPos := block.Position()
	block.SetPosition(startLine, startPos)
	block.Advance(2) // "]("
	block.SkipSpaces()
	angle := block.Peek() == '<'
	if angle {
		block.Advance(1)
	}
	_, seg := block.PeekLine()
	source := block.Source()
	start, end := seg.Start, seg.Start+len(img.Destination)
	if seg.Padding == 0 && end <= len(source) && string(source[start:end]) == string(img.Destination) {
		dests, _ := pc.Get(destinationsKey).(map[*ast.Image]destination)
		if dests == nil {
			dests = map[*ast.Image]destination{}
			pc.Set(destinationsKey, dests)
		}
		dests[img] = destination{start, end, angle}
	}
	block.SetPosition(endLine, endPos)
	return n
}

func (p positionedLinkParser) CloseBlock(parent ast.Node, block text.Reader, pc parser.Context) {
	if c, ok := p.InlineParser.(parser.CloseBlocker); ok {
		c.CloseBlock(parent, block, pc)
	}
}

// spacedImageRef is the web's bare form with spaces, which CommonMark leaves
// as text: ![alt](Login screen.png). It must match remarkSpacedImageRefs in
// web/src/lib/imageRefs.ts.
var spacedImageRef = regexp.MustCompile(`(?i)!\[([^\]\n]*)\]\(\s*([^()<>\n]*?\S\.(?:png|jpe?g|gif|webp))\s*\)`)

// bom is the byte order mark CommonMark parsers (the web's micromark among
// them) drop from the start of a text before reading it.
const bom = "\uFEFF"

// findMarkdown reads content as the web does: a leading byte order mark is
// skipped first, as micromark skips it, so both see the same blocks, and the
// positions found are then moved past it.
func findMarkdown(content string, img Image) scan {
	if !strings.HasPrefix(content, bom) {
		return findMarkdownBody(content, img)
	}
	s := findMarkdownBody(content[len(bom):], img)
	for i := range s.targets {
		units := make([]unit, len(s.targets[i].units))
		for j, u := range s.targets[i].units {
			u.start += len(bom)
			u.end += len(bom)
			units[j] = u
		}
		s.targets[i].units = units
	}
	return s
}

func findMarkdownBody(content string, img Image) scan {
	source := []byte(content)
	pc := parser.NewContext()
	doc := markdownParser.Parse(text.NewReader(source), parser.WithContext(pc))
	dests, _ := pc.Get(destinationsKey).(map[*ast.Image]destination)

	var s scan
	var textRun []*ast.Text
	flushText := func() {
		s.findSpaced(content, textRun, img)
		textRun = textRun[:0]
	}
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				if len(textRun) > 0 && !adjacent(content, textRun[len(textRun)-1], c) {
					flushText()
				}
				textRun = append(textRun, c)
				continue
			case *ast.Image:
				flushText()
				s.image(content, c, dests, img)
				continue // its alt text is not text the web looks in
			case *ast.CodeSpan, *ast.RawHTML:
				flushText()
				continue
			case *ast.LinkReferenceDefinition:
				flushText()
				s.definition(source, c, img)
				continue
			}
			flushText()
			walk(c)
		}
		flushText()
	}
	walk(doc)
	return s
}

// adjacent reports whether text node b follows a in the source with nothing
// but a line break (and indentation) between them, as one text node on the
// web does.
func adjacent(content string, a, b *ast.Text) bool {
	if a.Segment.Stop == b.Segment.Start {
		return true
	}
	if !a.SoftLineBreak() || a.Segment.Stop > b.Segment.Start {
		return false
	}
	return strings.TrimSpace(content[a.Segment.Stop:b.Segment.Start]) == ""
}

// image notes an image node: a use when its destination names img, and a
// target when it is inline (a reference-style image is rewritten through
// its definition).
func (s *scan) image(content string, n *ast.Image, dests map[*ast.Image]destination, img Image) {
	raw := string(n.Destination)
	name := decodedUnits(raw, 0, len(raw), true)
	if _, ok := nameTarget(name, img, false, false, true); !ok {
		return
	}
	s.uses++
	if n.Reference != nil {
		return
	}
	d, known := dests[n]
	if !known {
		s.targets = append(s.targets, target{})
		return
	}
	t, _ := nameTarget(decodedUnits(content, d.start, d.end, true), img, d.angle, false, true)
	s.targets = append(s.targets, t)
}

// definition notes a link reference definition [r]: dest "title" naming
// img as a target, whether or not an image uses it. Its destination is found
// by reading the definition again as goldmark did: past the label and the
// colon, and the spaces (or line break) after it.
func (s *scan) definition(source []byte, n *ast.LinkReferenceDefinition, img Image) {
	raw := string(n.Destination)
	if _, ok := nameTarget(decodedUnits(raw, 0, len(raw), true), img, false, false, true); !ok {
		return
	}
	block := text.NewBlockReader(source, n.Lines())
	t := target{}
	if block.Peek() == '[' {
		block.Advance(1)
		_, found := block.FindClosure('[', ']', text.FindClosureOptions{Newline: true, Advance: true})
		if found && block.Peek() == ':' {
			block.Advance(1)
			block.SkipSpaces()
			angle := block.Peek() == '<'
			if angle {
				block.Advance(1)
			}
			_, seg := block.PeekLine()
			start, end := seg.Start, seg.Start+len(n.Destination)
			if seg.Padding == 0 && end <= len(source) && string(source[start:end]) == raw {
				t, _ = nameTarget(decodedUnits(string(source), start, end, true), img, angle, false, true)
			}
		}
	}
	s.targets = append(s.targets, t)
}

func allLiteral(units []unit) bool {
	for _, u := range units {
		if u.kind != literal {
			return false
		}
	}
	return true
}

// findSpaced looks for the spaced form in a run of adjacent text nodes, as
// the web's plugin looks in a text node, skipping one whose "!" the author
// escaped.
func (s *scan) findSpaced(content string, run []*ast.Text, img Image) {
	if len(run) == 0 {
		return
	}
	start, end := run[0].Segment.Start, run[len(run)-1].Segment.Stop
	written := content[start:end]
	for _, m := range spacedImageRef.FindAllStringSubmatchIndex(written, -1) {
		at := start + m[0]
		slashes := 0
		for i := at - 1; i >= 0 && content[i] == '\\'; i-- {
			slashes++
		}
		if slashes%2 == 1 {
			continue
		}
		// The web matches the decoded text and then looks for it, as
		// written, in the source: a match spelled with a backslash escape or
		// a character reference (![a&amp;b](Login screen.png)) is not found
		// there, so the web shows it as text, and so it is text here.
		if !allLiteral(decodedUnits(content, at, start+m[1], true)) {
			continue
		}
		t, ok := nameTarget(decodedUnits(content, start+m[4], start+m[5], true), img, true, false, true)
		if !ok {
			continue
		}
		s.uses++
		s.targets = append(s.targets, t)
	}
}
