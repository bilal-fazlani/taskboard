package models

import "strings"

// Text refers to its owner's images by name: a ticket's description and its
// markdown and HTML documents, or an epic's documents, name one of that
// owner's images by its display name with the extension ("Login screen.png"),
// ignoring case. A JPEG shows as .jpg and answers to .jpeg too. The web
// resolves markdown references (web/src/lib/imageRefs.ts, which lists the
// forms a reference takes); an HTML document's relative <img src> lands on
// the server's referenced-image route.

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
