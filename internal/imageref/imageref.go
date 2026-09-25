// Package imageref finds where a text refers to one of its owner's images by
// name, and rewrites those references when the image is renamed. The texts
// are a ticket's description (markdown) and its or an epic's markdown and
// HTML documents; the reference forms are the ones listed in
// internal/models/image_ref.go (and web/src/lib/imageRefs.ts).
//
// References are found from real parse positions, never by searching the
// text for the name: goldmark's CommonMark parser (the web's react-markdown
// is CommonMark too) gives each image's destination and each link reference
// definition, and golang.org/x/net/html's tokenizer each tag, whose
// attributes are then located in the tag's own bytes. A reference names the
// image when its destination, decoded the way the web or the browser decodes
// it (backslash escapes and character references, then %XX escapes),
// names it (models.IsImageRef).
//
// A rename rewrites only the bytes of the name before its extension, and
// writes the new name the way the old one was written: a space as the old
// reference wrote a space (literally, %20 or &#32;), and, where a literal
// space would end the reference (a bare markdown destination, an unquoted
// HTML attribute or CSS url(), a srcset URL), as %20. The extension, any ./
// in front, angle brackets, quotes and titles stay as they were. A reference
// whose bytes cannot be told apart reliably (a %XX escape spelled with
// character references, say) is left as it is and counted in Result.Left,
// and a text whose rewrite does not parse back to the expected references is
// left untouched.
package imageref

import (
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tcarac/taskboard/internal/models"
)

// Image is the image references are looked for: its name, without an
// extension, and its format.
type Image struct {
	Name   string
	Format string
}

func (img Image) matches(ref string) bool {
	return models.IsImageRef(img.Name, img.Format, ref)
}

// Result says what a rename did to one text.
type Result struct {
	// Rewritten counts the references rewritten to the new name.
	Rewritten int
	// Left counts the references that still show the image under its old
	// name, because their form could not be rewritten reliably. After the
	// rename they show a missing image.
	Left int
}

// Uses counts the references in content, a text of format (markdown or
// html), that show img. Reference-style images count once per image, not
// per definition, and a definition no image uses counts not at all.
func Uses(format, content string, img Image) int {
	return find(format, content, img).uses
}

// Rename rewrites every reference to img in content, a text of format
// (markdown or html), to name the image newName instead, keeping each
// reference's form. It returns content unchanged when there is nothing to
// rewrite or the rewrite could not be checked.
func Rename(format, content string, img Image, newName string) (string, Result) {
	before := find(format, content, img)
	if len(before.targets) == 0 {
		return content, Result{Left: before.uses}
	}
	renamed := Image{Name: newName, Format: img.Format}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for _, t := range before.targets {
		if !t.ok {
			continue
		}
		base, ok := t.baseUnits()
		if !ok {
			continue
		}
		edits = append(edits, edit{base[0].start, base[len(base)-1].end, encodeName(content, newName, base, t.spaceOK)})
	}
	if len(edits) == 0 {
		return content, Result{Left: before.uses}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := content
	for i, e := range edits {
		if i > 0 && e.end > edits[i-1].start {
			return content, Result{Left: before.uses} // overlapping: never expected
		}
		out = out[:e.start] + e.text + out[e.end:]
	}

	// Check the rewrite by parsing it again: every rewritten reference now
	// names the new name, the others are where they were, and no image
	// reference appeared or disappeared on the way.
	newBefore := find(format, content, renamed)
	oldAfter := find(format, out, img)
	newAfter := find(format, out, renamed)
	if oldAfter.uses+newAfter.uses != before.uses+newBefore.uses ||
		len(newAfter.targets) != len(newBefore.targets)+len(edits) ||
		len(oldAfter.targets) != len(before.targets)-len(edits) {
		return content, Result{Left: before.uses}
	}
	return out, Result{Rewritten: len(edits), Left: oldAfter.uses}
}

// scan is what find saw: how many references show the image, and the
// destinations to rewrite for a rename (inline images, the markdown
// spaced form, reference definitions, and HTML's URLs).
type scan struct {
	uses    int
	targets []target
}

func find(format, content string, img Image) scan {
	switch format {
	case models.DocumentFormatMarkdown:
		return findMarkdown(content, img)
	case models.DocumentFormatHTML:
		return findHTML(content, img)
	}
	return scan{}
}

// unitKind is how a unit of a reference was written.
type unitKind int

const (
	literal unitKind = iota // as it is
	escaped                 // a markdown backslash escape: \_
	charRef                 // a character reference: &#32; &amp;
	percent                 // one or more %XX escapes making one character
)

// A unit is one character of a reference as decoded, with the bytes of the
// source that spell it.
type unit struct {
	start, end int
	text       string
	kind       unitKind
}

func unitsText(units []unit) string {
	var b strings.Builder
	for _, u := range units {
		b.WriteString(u.text)
	}
	return b.String()
}

// A target is a reference to rewrite: the units of its name (extension
// included, without surrounding whitespace or ./), whether a literal space
// may stand in it, and whether its bytes are known reliably.
type target struct {
	units   []unit
	spaceOK bool
	ok      bool
}

// baseUnits are the units of the name before its extension.
func (t target) baseUnits() ([]unit, bool) {
	name := unitsText(t.units)
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 {
		return nil, false
	}
	n := 0
	for i, u := range t.units {
		n += len(u.text)
		if n == dot {
			return t.units[:i+1], true
		}
		if n > dot {
			return nil, false
		}
	}
	return nil, false
}

// literalUnits spells source[start:end] one character per unit, as written.
func literalUnits(source string, start, end int) []unit {
	var units []unit
	for i := start; i < end; {
		_, size := utf8.DecodeRuneInString(source[i:end])
		units = append(units, unit{i, i + size, source[i : i+size], literal})
		i += size
	}
	return units
}

// decodedUnits spells source[start:end] with its character references
// decoded, and with markdown's backslash escapes when markdown is set.
func decodedUnits(source string, start, end int, markdown bool) []unit {
	var units []unit
	for i := start; i < end; {
		c := source[i]
		if markdown && c == '\\' && i+1 < end && isASCIIPunct(source[i+1]) {
			units = append(units, unit{i, i + 2, source[i+1 : i+2], escaped})
			i += 2
			continue
		}
		if c == '&' {
			if n, text := charRefAt(source[i:end]); n > 0 {
				units = append(units, unit{i, i + n, text, charRef})
				i += n
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(source[i:end])
		units = append(units, unit{i, i + size, source[i : i+size], literal})
		i += size
	}
	return units
}

func isASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// isSpace is whitespace as trimming a reference counts it.
func isSpace(text string) bool {
	return text != "" && strings.TrimSpace(text) == ""
}

func trimSpaceUnits(units []unit) []unit {
	for len(units) > 0 && isSpace(units[0].text) {
		units = units[1:]
	}
	for len(units) > 0 && isSpace(units[len(units)-1].text) {
		units = units[:len(units)-1]
	}
	return units
}

// percentDecoded decodes the %XX escapes of a reference's units, as the web
// (decodeURIComponent) and the server's image route (one unescape) do: all
// of them, or, if any is malformed, none. reliable is false when an escape
// is spelled with anything but literal characters, so its bytes cannot be
// rewritten with confidence.
func percentDecoded(units []unit) (out []unit, reliable bool) {
	text := unitsText(units)
	if !strings.Contains(text, "%") {
		return units, true
	}
	decoded, err := url.PathUnescape(text)
	if err != nil || !utf8.ValidString(decoded) {
		return units, true // left as written, which names no image
	}
	reliable = true
	for i := 0; i < len(units); {
		u := units[i]
		if !strings.Contains(u.text, "%") {
			out = append(out, u)
			i++
			continue
		}
		// A run of %XX escapes, each three literal units, making whole
		// characters.
		var raw []byte
		start, j := u.start, i
		for j+2 < len(units) && units[j].text == "%" && units[j].kind == literal &&
			isHexUnit(units[j+1]) && isHexUnit(units[j+2]) {
			raw = append(raw, unhex(units[j+1].text[0])<<4|unhex(units[j+2].text[0]))
			j += 3
		}
		if len(raw) == 0 {
			return units, false
		}
		k, used := i, 0
		for len(raw) > 0 {
			r, size := utf8.DecodeRune(raw)
			if r == utf8.RuneError && size <= 1 {
				return units, false
			}
			used += size
			end := units[i+used*3-1].end
			out = append(out, unit{start, end, string(r), percent})
			start = end
			raw = raw[size:]
			k = i + used*3
		}
		i = k
	}
	if unitsText(out) != decoded {
		return units, false
	}
	return out, reliable
}

func isHexUnit(u unit) bool {
	return u.kind == literal && len(u.text) == 1 && strings.IndexByte("0123456789abcdefABCDEF", u.text[0]) >= 0
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// nameTarget works out whether units, a URL or destination as decoded by
// its syntax, name img, and returns it as a target if so. dotSlash allows a
// ./ in front (HTML only). ok says whether the units' bytes are reliable.
func nameTarget(units []unit, img Image, spaceOK, dotSlash, ok bool) (target, bool) {
	units = trimSpaceUnits(units)
	if dotSlash && len(units) >= 2 && units[0].text == "." && units[1].text == "/" {
		units = units[2:]
	}
	decoded, reliable := percentDecoded(units)
	if !reliable {
		// Found by its text, but its bytes can't be told apart.
		text := unitsText(units)
		if unescaped, err := url.PathUnescape(text); err == nil && utf8.ValidString(unescaped) {
			text = unescaped
		}
		return target{}, img.matches(strings.TrimSpace(text))
	}
	decoded = trimSpaceUnits(decoded)
	if !img.matches(unitsText(decoded)) {
		return target{}, false
	}
	return target{units: decoded, spaceOK: spaceOK, ok: ok}, true
}

// encodeName writes name in place of the base units of a reference, the
// way they wrote it: each character the old name had is written as it was
// written there (a space as " ", "%20" or "&#32;"); a new space is %20
// where a literal one would end the reference; a new non-ASCII letter is
// %-escaped if the old name %-escaped one; anything else is written as it
// is. Names hold only letters, digits, spaces, _ and -, none of which end a
// reference in any of the forms kept in step.
func encodeName(source, name string, base []unit, spaceOK bool) string {
	styles := map[rune]string{}
	escapeNonASCII := false
	for _, u := range base {
		r, size := utf8.DecodeRuneInString(u.text)
		if size != len(u.text) {
			continue
		}
		if _, seen := styles[r]; !seen {
			styles[r] = source[u.start:u.end]
		}
		if u.kind == percent && r >= utf8.RuneSelf {
			escapeNonASCII = true
		}
	}
	var b strings.Builder
	for _, r := range name {
		if written, ok := styles[r]; ok {
			b.WriteString(written)
			continue
		}
		switch {
		case unicode.IsSpace(r) && !spaceOK:
			b.WriteString(url.PathEscape(string(r)))
		case r >= utf8.RuneSelf && escapeNonASCII:
			b.WriteString(url.PathEscape(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
