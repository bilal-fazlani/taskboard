package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// imageCSP is sent with every image and thumbnail. An image is never served
// as HTML, and nosniff stops a browser guessing otherwise, but should the
// response ever be shown as a page (the URL opened in a tab) it runs
// sandboxed, may load nothing, and keeps only the inline style a browser's
// own image view uses.
const imageCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

// readImageBody reads an upload's body: the image file's bytes, sent as they
// are (any Content-Type; the content decides the format). The store enforces
// the 8 MB limit on what is read; a body too large to read at all is
// refused here with the store's words. ok is false once an error has been
// written.
func readImageBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDocumentRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			size := int(r.ContentLength)
			if size <= maxDocumentRequestBytes {
				size = maxDocumentRequestBytes + 1
			}
			writeError(w, http.StatusBadRequest, db.ImageTooLargeMessage(size))
			return nil, false
		}
		writeError(w, http.StatusBadRequest, "could not read the image: "+err.Error())
		return nil, false
	}
	return data, true
}

// createImageDocument answers POST /api/documents/images?ticket=|epic=
// &filename=[&name=] with the image file as the body. The format comes from
// filename's extension, and the name from filename too unless name is
// given. Rule failures are 400s with the store's message.
func (s *Server) createImageDocument(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.documentOwner(w, r)
	if !ok {
		return
	}
	if owner == (db.DocumentOwner{}) {
		writeError(w, http.StatusBadRequest, "pass ticket or epic")
		return
	}
	q := r.URL.Query()
	filename := strings.TrimSpace(q.Get("filename"))
	if filename == "" {
		writeError(w, http.StatusBadRequest, `pass filename, the image's file name (e.g. "Login screen.png")`)
		return
	}
	// DocumentNameFromFilename refuses an unknown extension (or .svg) in the
	// store's words; a typed name replaces the one it works out.
	name, format, err := db.DocumentNameFromFilename(filename)
	if typed := q.Get("name"); strings.TrimSpace(typed) != "" {
		if f, known := db.DocumentFormatFromFilename(filename); known {
			name, format, err = typed, f, nil
		}
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	data, ok := readImageBody(w, r)
	if !ok {
		return
	}
	d, err := s.store.CreateImageDocument(models.CreateImageRequest{
		TicketID: owner.TicketID, EpicID: owner.EpicID, Name: name, Format: format, Data: data,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// replaceDocumentImage answers PUT /api/documents/{ref}/image with the new
// file as the body: a content save of an image, which must keep its format.
func (s *Server) replaceDocumentImage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	data, ok := readImageBody(w, r)
	if !ok {
		return
	}
	d, err := s.store.ReplaceDocumentImage(id, data)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// getDocumentImage answers GET /api/documents/{ref}/image with the image
// file, shown inline.
func (s *Server) getDocumentImage(w http.ResponseWriter, r *http.Request) {
	s.serveImage(w, r, false, "inline")
}

// getDocumentThumbnail answers GET /api/documents/{ref}/thumbnail with the
// image's thumbnail.
func (s *Server) getDocumentThumbnail(w http.ResponseWriter, r *http.Request) {
	s.serveImage(w, r, true, "inline")
}

// serveImage sends an image's file or thumbnail. A document that is not an
// image is a 404.
func (s *Server) serveImage(w http.ResponseWriter, r *http.Request, thumbnail bool, disposition string) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	var f *db.ImageFile
	var err error
	if thumbnail {
		f, err = s.store.GetDocumentThumbnail(id)
	} else {
		f, err = s.store.GetDocumentImage(id)
	}
	if err != nil {
		writeLookupError(w, err)
		return
	}
	if f == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	writeImageFile(w, r, f, thumbnail, disposition)
}

// writeImageFile sends f with its own image Content-Type, never HTML, and
// headers that keep it an image: nosniff, a sandboxing CSP, and a resource
// policy that stops other sites embedding it. It may be cached but is
// checked each time; the ETag moves with every content save and rename.
func writeImageFile(w http.ResponseWriter, r *http.Request, f *db.ImageFile, thumbnail bool, disposition string) {
	contentType := f.ContentType
	if !strings.HasPrefix(contentType, "image/") || contentType == "image/svg+xml" {
		// Belt and braces: only the four image types are ever stored.
		contentType = "application/octet-stream"
	}
	kind := "image"
	if thumbnail {
		kind = "thumbnail"
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", imageCSP)
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-cache")
	h.Set("ETag", fmt.Sprintf(`"%s-%s-%d-%d"`, f.Document.ID, kind, f.Document.Revision, f.Document.UpdatedAt.UnixNano()))
	filename := models.DocumentDisplayName(f.Document.Name, f.Document.Format)
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filename}))
	http.ServeContent(w, r, "", f.Document.UpdatedAt, bytes.NewReader(f.Data))
}
