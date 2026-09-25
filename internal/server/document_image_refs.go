package server

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

// getReferencedImage answers GET /api/documents/{id}/{name}, where an HTML
// document's relative image names land: the page is served from
// /api/documents/{id}/raw, so <img src="Login screen.png"> in it asks for
// /api/documents/{id}/Login%20screen.png. It sends the image of that name
// (models.IsImageRef) that belongs to the same owner as the HTML document
// {id}, and a 404 for anything else: an id that is not an HTML document's (a
// name with ?ticket= or ?epic= is not accepted here, as the page's relative
// URLs never carry one), or a name that is not one of that owner's images.
//
// The page runs sandboxed in an opaque origin, so to the browser every
// request it makes is cross-origin, and the Cross-Origin-Resource-Policy:
// same-origin that /image sends would block this one. This route sends
// cross-origin instead, and nothing else changes: no CORS header ever goes
// out, it is still an image type with nosniff and a sandboxing CSP, and it
// is a GET that reads the one image and writes nothing.
//
// The boundary this sets (accepted by Bilal 2026-09-25): the route cannot
// tell which page asks (a sandboxed page sends no Origin or Referer worth
// checking), so it is keyed by the HTML document's id alone. Anyone who
// knows an HTML document's id, whether another board page
// (<img src="../<id>/Their shot.png">) or any other site, can display that
// document's owner's images and learn their size (and whether a name
// exists). They can never read the image's bytes (a canvas it is drawn on is
// tainted, and fetch() gets an opaque response or a CORS failure), and they
// reach no other data: nothing but images, and only that one owner's. Ids
// are ULIDs, which the board does not hand to its pages. Writes from a
// sandboxed page are still refused by rejectCrossOriginWrites (Origin:
// null), and the rest of the API still sends no CORS headers, so the page
// reads nothing else.
func (s *Server) getReferencedImage(w http.ResponseWriter, r *http.Request) {
	id, name := chi.URLParam(r, "ref"), chi.URLParam(r, "name")
	// chi routes on the escaped path only when the request's escaping was
	// unusual (RawPath set); otherwise the segments are already decoded, and
	// decoding them again would let Login%2520screen.png name "Login
	// screen.png".
	if r.URL.RawPath != "" {
		if unescaped, err := url.PathUnescape(id); err == nil {
			id = unescaped
		}
		if unescaped, err := url.PathUnescape(name); err == nil {
			name = unescaped
		}
	}
	f, err := s.store.ReferencedImage(id, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if f == nil {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}
	writeImageFileWithPolicy(w, r, f, false, "inline", "cross-origin")
}
