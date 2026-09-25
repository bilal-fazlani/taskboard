package server

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// maxDocumentRequestBytes bounds a document write's request body. JSON
// escaping can make content several times its stored size, so this sits well
// above models.MaxDocumentBytes; the store enforces the real limit on the
// decoded content.
const maxDocumentRequestBytes = 64 << 20

// writeLookupError answers a reference that names nothing with 404 and the
// store's message, and anything else with 500.
func writeLookupError(w http.ResponseWriter, err error) {
	var invalid *db.ErrInvalidInput
	if errors.As(err, &invalid) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// documentID resolves a document route's {ref}: a document id, or, with
// ?ticket=<id or key>, the name of one of that ticket's documents.
func (s *Server) documentID(w http.ResponseWriter, r *http.Request) (string, bool) {
	// chi hands back the escaped segment when the path was sent with an
	// unusual escaping; names and ids never hold a %, so unescaping is safe.
	ref := chi.URLParam(r, "ref")
	if unescaped, err := url.PathUnescape(ref); err == nil {
		ref = unescaped
	}
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ticket == "" {
		return ref, true
	}
	ticketID, err := s.store.ResolveTicketID(ticket)
	if err != nil {
		writeLookupError(w, err)
		return "", false
	}
	id, err := s.store.ResolveDocumentRef(db.DocumentOwner{TicketID: ticketID}, ref)
	if err != nil {
		writeLookupError(w, err)
		return "", false
	}
	return id, true
}

func (s *Server) listTicketDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := s.store.ListDocuments(db.DocumentOwner{TicketID: chi.URLParam(r, "id")})
	if err != nil {
		writeLookupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *Server) getDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	d, err := s.store.GetDocument(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// createDocument adds a document to a ticket (the web's New and Upload).
// Rule failures and an unknown ticket are 400s with the store's message.
func (s *Server) createDocument(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentRequestBytes)
	var req models.CreateDocumentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	d, err := s.store.CreateDocument(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) updateDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentRequestBytes)
	var req models.UpdateDocumentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	d, err := s.store.UpdateDocument(id, req)
	if err != nil {
		// A save from a revision the document has moved past: the web shows
		// the current document in its conflict notice.
		var conflict *db.ErrDocumentConflict
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "current": conflict.Current})
			return
		}
		writeStoreError(w, err)
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	deleted, err := s.store.DeleteDocument(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// documentMediaType is the Content-Type a document is served with.
func documentMediaType(format string) string {
	if format == models.DocumentFormatHTML {
		return "text/html; charset=utf-8"
	}
	return "text/markdown; charset=utf-8"
}

// downloadDocument sends the content as a file named with the display name.
// mime.FormatMediaType quotes the name and switches to the RFC 2231 form for
// non-ASCII letters, which names may hold.
func (s *Server) downloadDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	d, err := s.store.GetDocument(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	filename := models.DocumentDisplayName(d.Name, d.Format)
	w.Header().Set("Content-Type", documentMediaType(d.Format))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, d.Content)
}

// documentSandbox is the sandbox an HTML document runs in, framed or opened
// on its own. Scripts run, and may load from the internet, but nothing else
// is granted: with no allow-same-origin the page has an opaque origin, so it
// cannot read the board's storage and its writes arrive with Origin: null,
// which rejectCrossOriginWrites refuses; with no allow-top-navigation or
// allow-popups it cannot navigate the board away or escape into a new
// window; and with no allow-forms, allow-modals or allow-downloads its forms
// don't submit, alert/confirm/prompt/print are blocked and it cannot start a
// download. It must match the iframe's sandbox attribute (DOCUMENT_SANDBOX
// in web/src/lib/documents.ts).
const documentSandbox = "sandbox allow-scripts"

// rawDocument serves a document's content inline, for the web UI's HTML
// frame. The sandbox header applies even when the URL is opened directly in
// a tab, where no iframe attribute is there to confine it.
func (s *Server) rawDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	d, err := s.store.GetDocument(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	contentType := "text/plain; charset=utf-8"
	if d.Format == models.DocumentFormatHTML {
		contentType = "text/html; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", documentSandbox)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, d.Content)
}
