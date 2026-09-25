package imageref

import (
	"html"
	"regexp"
	"strings"

	nethtml "golang.org/x/net/html"
)

// charRef matches a character reference at the start of a string: decimal,
// hexadecimal or named, with its semicolon.
var charRefPattern = regexp.MustCompile(`^&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{0,31});`)

// charRefAt returns the length of the character reference s starts with and
// what it stands for, or 0 when s starts with none (an unknown name is none).
func charRefAt(s string) (int, string) {
	ref := charRefPattern.FindString(s)
	if ref == "" {
		return 0, ""
	}
	decoded := html.UnescapeString(ref)
	if decoded == ref {
		return 0, ""
	}
	return len(ref), decoded
}

// findHTML reads an HTML page as the browser's tokenizer does and looks at
// the URLs the page's images come from: src attributes, each URL of a srcset
// attribute, and url() in a style attribute or a <style> element. The
// tokenizer gives each tag's raw bytes and decoded attributes; the
// attributes' positions are found by reading the tag's bytes again, and
// used only when that reading agrees with the tokenizer's.
func findHTML(content string, img Image) scan {
	var s scan
	z := nethtml.NewTokenizer(strings.NewReader(content))
	offset := 0
	inStyle := false
	var found []target
	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			break
		}
		raw := string(z.Raw())
		switch tt {
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			name, more := z.TagName()
			var attrs []nethtml.Attribute
			for more {
				var key, val []byte
				key, val, more = z.TagAttr()
				attrs = append(attrs, nethtml.Attribute{Key: string(key), Val: string(val)})
			}
			found = append(found, tagTargets(content, offset, raw, attrs, img)...)
			inStyle = tt == nethtml.StartTagToken && string(name) == "style"
		case nethtml.TextToken:
			if inStyle {
				found = append(found, cssTargets(literalUnits(content, offset, offset+len(raw)), img, true)...)
			}
			inStyle = false
		default:
			inStyle = false
		}
		offset += len(raw)
	}
	for _, t := range found {
		if offset != len(content) {
			t.ok = false // the tokens do not cover the page: no position is trusted
		}
		s.uses++
		s.targets = append(s.targets, t)
	}
	return s
}

// tagTargets finds the references in one tag's attributes. offset is where
// the tag's raw bytes start in the page.
func tagTargets(content string, offset int, raw string, attrs []nethtml.Attribute, img Image) []target {
	located := locateAttrs(raw)
	agree := len(located) == len(attrs)
	var values [][]unit
	for i := 0; agree && i < len(attrs); i++ {
		a := located[i]
		units := decodedUnits(content, offset+a.start, offset+a.end, false)
		if strings.ToLower(a.key) != attrs[i].Key || unitsText(units) != attrs[i].Val {
			agree = false
			break
		}
		values = append(values, units)
	}
	var out []target
	seen := map[string]bool{}
	for i, a := range attrs {
		// A browser keeps the first of a repeated attribute and ignores the
		// rest, so only the first is a reference.
		if seen[a.Key] {
			continue
		}
		seen[a.Key] = true
		var units []unit
		quoted := false
		if agree {
			units, quoted = values[i], located[i].quoted
		} else {
			// Positions unknown: decode the value alone, to find the
			// references, which then cannot be rewritten.
			units = literalUnits(a.Val, 0, len(a.Val))
		}
		switch a.Key {
		case "src":
			if t, ok := nameTarget(units, img, quoted, true, agree); ok {
				out = append(out, t)
			}
		case "srcset":
			out = append(out, srcsetTargets(units, img, agree)...)
		case "style":
			out = append(out, cssTargets(units, img, agree)...)
		}
	}
	return out
}

// A located attribute: its name as written, and where its value's bytes
// are within the tag.
type locatedAttr struct {
	key        string
	start, end int
	quoted     bool
}

// locateAttrs reads a start tag's attributes the way the HTML tokenizer
// does (the "before attribute name" to "after attribute value" states),
// returning each one's value position within raw.
func locateAttrs(raw string) []locatedAttr {
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }
	i := 1 // past "<"
	for i < len(raw) && !isSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++
	}
	var attrs []locatedAttr
	for i < len(raw) {
		for i < len(raw) && (isSpace(raw[i]) || raw[i] == '/') {
			i++
		}
		if i >= len(raw) || raw[i] == '>' {
			break
		}
		nameStart := i
		i++ // the first character may be "="
		for i < len(raw) && !isSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' && raw[i] != '=' {
			i++
		}
		a := locatedAttr{key: raw[nameStart:i]}
		j := i
		for j < len(raw) && isSpace(raw[j]) {
			j++
		}
		if j < len(raw) && raw[j] == '=' {
			j++
			for j < len(raw) && isSpace(raw[j]) {
				j++
			}
			switch {
			case j < len(raw) && (raw[j] == '"' || raw[j] == '\''):
				q := raw[j]
				end := strings.IndexByte(raw[j+1:], q)
				if end < 0 {
					end = len(raw) - j - 1
				}
				a.start, a.end, a.quoted = j+1, j+1+end, true
				i = j + 1 + end + 1
			default:
				k := j
				for k < len(raw) && !isSpace(raw[k]) && raw[k] != '>' {
					k++
				}
				a.start, a.end = j, k
				i = k
			}
		} else {
			a.start, a.end = i, i
		}
		attrs = append(attrs, a)
	}
	return attrs
}

// srcsetTargets splits a srcset attribute's value into its image candidate
// strings, as the HTML standard does, and returns the URLs naming img. A
// literal space never stands in a srcset URL.
func srcsetTargets(units []unit, img Image, ok bool) []target {
	var out []target
	isSpace := func(u unit) bool { return len(u.text) == 1 && strings.IndexByte(" \t\n\f\r", u.text[0]) >= 0 }
	for i := 0; i < len(units); {
		for i < len(units) && (isSpace(units[i]) || units[i].text == ",") {
			i++
		}
		if i >= len(units) {
			break
		}
		start := i
		for i < len(units) && !isSpace(units[i]) {
			i++
		}
		url := units[start:i]
		trailingCommas := false
		for len(url) > 0 && url[len(url)-1].text == "," {
			url = url[:len(url)-1]
			trailingCommas = true
		}
		if t, found := nameTarget(url, img, false, true, ok); found {
			out = append(out, t)
		}
		if trailingCommas {
			continue
		}
		// The descriptors, up to the next comma outside parentheses.
		depth := 0
		for i < len(units) {
			t := units[i].text
			i++
			if t == "(" {
				depth++
			} else if t == ")" && depth > 0 {
				depth--
			} else if t == "," && depth == 0 {
				break
			}
		}
	}
	return out
}

// cssTargets finds url() references in CSS, skipping comments and strings
// that are not a url()'s. A quoted URL may hold a literal space; an
// unquoted one may not (it would not be valid CSS), and neither may hold a
// CSS escape: those are not kept in step.
func cssTargets(units []unit, img Image, ok bool) []target {
	var out []target
	text := func(i int) string {
		if i < len(units) {
			return units[i].text
		}
		return ""
	}
	isWS := func(i int) bool { t := text(i); return len(t) == 1 && strings.IndexByte(" \t\n\f\r", t[0]) >= 0 }
	isIdent := func(i int) bool {
		t := text(i)
		if t == "" {
			return false
		}
		c := t[0]
		return c == '-' || c == '_' || c >= 0x80 || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	skipString := func(i int) int { // i at the opening quote; returns past the closing one
		q := text(i)
		i++
		for i < len(units) && text(i) != q {
			if text(i) == "\\" {
				i++
			}
			i++
		}
		return i + 1
	}
	for i := 0; i < len(units); {
		switch {
		case text(i) == "/" && text(i+1) == "*":
			i += 2
			for i < len(units) && !(text(i) == "*" && text(i+1) == "/") {
				i++
			}
			i += 2
		case text(i) == "\"" || text(i) == "'":
			i = skipString(i)
		case strings.EqualFold(text(i)+text(i+1)+text(i+2), "url") && text(i+3) == "(" && (i == 0 || !isIdent(i-1)):
			i += 4
			for isWS(i) {
				i++
			}
			if text(i) == "\"" || text(i) == "'" {
				q := text(i)
				start := i + 1
				end := start
				escapes := false
				for end < len(units) && text(end) != q {
					if text(end) == "\\" {
						escapes = true
						end++
					}
					end++
				}
				if end > len(units) {
					end = len(units)
				}
				if !escapes {
					if t, found := nameTarget(units[start:end], img, true, true, ok); found {
						out = append(out, t)
					}
				}
				i = end + 1
				continue
			}
			start := i
			for i < len(units) && text(i) != ")" {
				i++
			}
			url := trimSpaceUnits(units[start:i])
			valid := true
			for _, u := range url {
				if isSpace(u.text) || u.text == "\"" || u.text == "'" || u.text == "(" || u.text == "\\" {
					valid = false
				}
			}
			if valid {
				if t, found := nameTarget(url, img, false, true, ok); found {
					out = append(out, t)
				}
			}
		default:
			i++
		}
	}
	return out
}
