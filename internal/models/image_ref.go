package models

import "strings"

// Text refers to its owner's images by name: a ticket's description and its
// markdown and HTML documents, or an epic's documents, name one of that
// owner's images by its display name with the extension ("Login screen.png"),
// ignoring case. A JPEG shows as .jpg and answers to .jpeg too. The web
// resolves markdown references (web/src/lib/imageRefs.ts); an HTML
// document's relative URLs land on the server's referenced-image route.
// Renames keep exactly these forms in step (the same list as imageRefs.ts;
// internal/imageref finds and rewrites them from parse positions):
//
// Markdown:
//   - ![alt](Login screen.png), bare with spaces, no title
//   - ![alt](<Login screen.png>) and ![alt](Login%20screen.png) (any %XX
//     escape of the name's characters), with or without a title, and both
//     at once: ![alt](<Login%20screen.png>)
//   - reference style, ![alt][r], ![r][] or ![r], whose definition
//     [r]: <Login screen.png> or [r]: Login%20screen.png (bare when the name
//     has no spaces) gives the name, with or without a title
//   - in any of these, whitespace or a line break inside the parentheses,
//     character references (Login&#32;screen.png) and backslash escapes
//     (login\_screen.png), except in the bare form with spaces, which the
//     web shows as text when it is spelled with either
//   - after a leading byte order mark, which is skipped as the web skips it
//
// HTML: src attributes, each URL in a srcset attribute, and CSS url() in a
// style attribute or <style> element, quoted or not, where the URL is the
// name bare, with ./ in front, or percent-encoded, and may use character
// references in an attribute. Other forms that reach the route
// (../<id>/name, /api/documents/<id>/name, absolute URLs, hrefs, URLs a
// script builds, CSS escapes such as url("Login\20 screen.png")) are not kept
// in step.
//
// A rename writes the new name the way the old one was written (a space as
// the reference wrote one: " ", %20 or &#32;; %20 where a literal space would
// end the reference) and keeps the extension, ./, brackets and quotes. A
// reference that resolves but whose bytes can't be told apart reliably (a
// %XX escape spelled with a character reference, &#37;20) is left as it is,
// and the rename says where.

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

// Image places are where an owner's text uses one of its images: a ticket's
// description, or one of the owner's markdown or HTML documents.
const (
	ImagePlaceDescription = "description"
	ImagePlaceDocument    = "document"
)

// ImagePlace is one place an image is used. A document is named by its id
// and its display name ("Plan.md").
type ImagePlace struct {
	Kind       string `json:"kind"`
	DocumentID string `json:"documentId,omitempty"`
	Name       string `json:"name,omitempty"`
}

// Label is how a place is named to people: "the description" or
// "Plan.md".
func (p ImagePlace) Label() string {
	if p.Kind == ImagePlaceDescription {
		return "the description"
	}
	return p.Name
}

// JoinImagePlaces names places in a sentence: "the description",
// "the description and Plan.md", "the description, Plan.md and Page.html".
func JoinImagePlaces(places []ImagePlace) string {
	labels := make([]string, len(places))
	for i, p := range places {
		labels[i] = p.Label()
	}
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	}
	return strings.Join(labels[:len(labels)-1], ", ") + " and " + labels[len(labels)-1]
}
