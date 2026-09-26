package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// listJournal answers GET /api/projects/{id}/journal with one page of the
// project's journal, newest first. ?limit= sets the page size (default
// db.JournalDefaultLimit) and ?before= an entry id to read older entries
// from, the previous page's nextBefore. {id} is a project id or prefix; an
// unknown one is a 404, a bad limit or before a 400.
func (s *Server) listJournal(w http.ResponseWriter, r *http.Request) {
	projectID, err := s.store.ResolveProjectRef(chi.URLParam(r, "id"))
	if err != nil {
		writeLookupError(w, err)
		return
	}
	q := r.URL.Query()
	limit := db.JournalDefaultLimit
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be a whole number")
			return
		}
		limit = n
	}
	page, err := s.store.ListJournal(projectID, q.Get("before"), limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// appendJournalEntry answers POST /api/projects/{id}/journal, whose body is
// {author, text}, with the new entry. Entries are never changed or deleted
// one by one, so the journal has no other write routes.
func (s *Server) appendJournalEntry(w http.ResponseWriter, r *http.Request) {
	projectID, err := s.store.ResolveProjectRef(chi.URLParam(r, "id"))
	if err != nil {
		writeLookupError(w, err)
		return
	}
	var req models.AppendJournalEntryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	e, err := s.store.AppendJournalEntry(projectID, req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}
