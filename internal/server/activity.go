package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tcarac/taskboard/internal/db"
)

// listActivity answers GET /api/projects/{id}/activity with one page of the
// project's status changes, newest first. ?epic= narrows it to the tickets in
// the given epics (repeatable: a name, an id, or "none" for no epic), ?limit=
// sets the page size (default db.ActivityDefaultLimit) and ?before= a change
// id to read older changes from, the previous page's nextBefore. {id} is a
// project id or prefix; an unknown one is a 404, a bad limit or before a 400.
func (s *Server) listActivity(w http.ResponseWriter, r *http.Request) {
	projectID, err := s.store.ResolveProjectRef(chi.URLParam(r, "id"))
	if err != nil {
		writeLookupError(w, err)
		return
	}
	q := r.URL.Query()
	limit := db.ActivityDefaultLimit
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be a whole number")
			return
		}
		limit = n
	}
	page, err := s.store.ListActivity(projectID, q["epic"], q.Get("before"), limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
