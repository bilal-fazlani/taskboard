package models

import "strings"

// Text refers to its owner's images by name: a ticket's description and its
// markdown and HTML documents, or an epic's documents, name one of that
// owner's images by its display name with the extension ("Login screen.png"),
// ignoring case. A JPEG shows as .jpg and answers to .jpeg too. The web
// resolves markdown references (web/src/lib/imageRefs.ts); an HTML
// document's relative URLs land on the server's referenced-image route.
// Renames keep exactly these forms in step (the same list as imageRefs.ts):
//
// Markdown:
//   - ![alt](Login screen.png), bare with spaces, no title
//   - ![alt](<Login screen.png>) and ![alt](Login%20screen.png) (any %XX
//     escape of the name's characters), with or without a title
//   - reference style, ![alt][r], ![r][] or ![r], whose definition
//     [r]: <Login screen.png> or [r]: Login%20screen.png (bare when the name
//     has no spaces) gives the name, with or without a title
//
// HTML: src attributes, each URL in a srcset attribute, and CSS url() in a
// style attribute or <style> element, quoted or not, where the URL is the
// name bare, with ./ in front, or percent-encoded. Other forms that reach
// the route (../<id>/name, /api/documents/<id>/name, absolute URLs, hrefs,
// URLs a script builds) are not kept in step.

// IsImageRef reports whether ref names the image called name, of format, as
// text refers to it. A document that is not an image never matches, and
// neither does a name without its extension.
func IsImageRef(name, format, ref string) bool {
	if !IsImageFormat(format) {
		return false
	}
	ref = strings.TrimSpace(ref)
	return strings.EqualFold(DocumentDisplayName(name, format), ref) ||
		(format == DocumentFormatJPEG && strings.EqualFold(name+".jpeg", ref))
}
