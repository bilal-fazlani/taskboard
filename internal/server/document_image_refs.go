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
// (models.IsImageRef) that belongs to the same owner as the HTML document,
// and a 404 for anything else: an id that is not an HTML document's (a name
// with ?ticket= or ?epic= is not accepted here, as the page's relative URLs
// never carry one), a name that is not one of that owner's images, or
// another owner's image.
//
// The page runs sandboxed in an opaque origin, so to the browser every
// request it makes is cross-origin, and the Cross-Origin-Resource-Policy:
// same-origin that /image sends would block this one. This route sends
// cross-origin instead, and nothing else changes: no CORS header ever goes
// out, so the page (or any other site) can display the image but never read
// its bytes back (a canvas it is drawn on is tainted, and fetch() gets an
// opaque response); it is still an image type with nosniff and a sandboxing
// CSP; and it is a GET that reads the one image and writes nothing. The
// images reachable are only those the page's own owner holds, which its
// author could already show. Writes from the page are still refused by
// rejectCrossOriginWrites (Origin: null), and the rest of the API still
// sends no CORS headers, so the page reads nothing else.
func (s *Server) getReferencedImage(w http.ResponseWriter, r *http.Request) {
	id, name := chi.URLParam(r, "ref"), chi.URLParam(r, "name")
	// chi hands back the escaped segment when the path was sent with an
	// unusual escaping; names and ids never hold a %, so unescaping is safe.
	if unescaped, err := url.PathUnescape(id); err == nil {
		id = unescaped
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
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
