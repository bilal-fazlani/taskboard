package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// The kinds of thing an entry sits on, as the entry routes name them.
const (
	entryOnProject = "project"
	entryOnEpic    = "epic"
	entryOnTicket  = "ticket"
)

// ticketWithEntries is a ticket as GET /api/tickets/{id} answers it: with
// its current entries and its open notes counted (models.TicketEntries).
type ticketWithEntries struct {
	*models.Ticket
	models.TicketEntries
}

// entryOwner resolves the {id} of an entry route on kind: a project by id or
// prefix, an epic by id, a ticket by id or display key. An unknown one is a
// 404; ok is false once an error has been written.
func (s *Server) entryOwner(w http.ResponseWriter, r *http.Request, kind string) (models.EntryOwner, bool) {
	ref := chi.URLParam(r, "id")
	switch kind {
	case entryOnProject:
		id, err := s.store.ResolveProjectRef(ref)
		if err != nil {
			writeLookupError(w, err)
			return models.EntryOwner{}, false
		}
		return models.EntryOwner{ProjectID: id}, true
	case entryOnEpic:
		e, err := s.store.GetEpic(ref)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return models.EntryOwner{}, false
		}
		if e == nil {
			writeError(w, http.StatusNotFound, "epic not found")
			return models.EntryOwner{}, false
		}
		return models.EntryOwner{EpicID: e.ID}, true
	default:
		id, err := s.store.ResolveTicketID(ref)
		if err != nil {
			writeLookupError(w, err)
			return models.EntryOwner{}, false
		}
		return models.EntryOwner{TicketID: id}, true
	}
}

// listEntries answers GET /api/{projects,epics,tickets}/{id}/entries with
// one page of that owner's entries, newest first, as {entries, total,
// hasMore, nextBefore}. Only current entries unless ?includeReplaced=true;
// ?type= keeps entries of that type (repeat it, or comma-separate, for
// several); ?limit= sets the page size (default db.EntryDefaultLimit) and
// ?before= takes the previous page's nextBefore. A bad filter is a 400.
func (s *Server) listEntries(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := s.entryOwner(w, r, kind)
		if !ok {
			return
		}
		q := r.URL.Query()
		var filter models.EntryFilter
		var err error
		if filter.IncludeReplaced, err = parseBoolParam(q.Get("includeReplaced")); err != nil {
			writeError(w, http.StatusBadRequest, "includeReplaced must be true or false")
			return
		}
		for _, v := range q["type"] {
			for _, t := range strings.Split(v, ",") {
				if t = strings.TrimSpace(t); t != "" {
					filter.Types = append(filter.Types, t)
				}
			}
		}
		limit := db.EntryDefaultLimit
		if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
			if limit, err = strconv.Atoi(raw); err != nil {
				writeError(w, http.StatusBadRequest, "limit must be a whole number")
				return
			}
		}
		page, err := s.store.ListEntries(owner, filter, q.Get("before"), limit)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}

// createEntry answers POST /api/{projects,epics,tickets}/{id}/entries, whose
// body is a models.CreateEntryRequest without its owner (the route names
// it), with the new entry. The author is agentId, or authorName for the
// person. Entries are never edited: a new one replaces an old one.
func (s *Server) createEntry(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := s.entryOwner(w, r, kind)
		if !ok {
			return
		}
		var req models.CreateEntryRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		req.EntryOwner = owner
		e, err := s.store.CreateEntry(req)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, e)
	}
}

// markNoteHandled answers POST /api/entries/{id}/handled, whose body is
// {agentId}, the agent that handled the note, with the note. An unknown
// entry is a 404; one that is not an open note, or an unknown agent, a 400.
func (s *Server) markNoteHandled(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	e, err := s.store.GetEntry(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "entry not found")
		return
	}
	var req struct {
		AgentID string `json:"agentId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	note, err := s.store.MarkNoteHandled(e.ID, req.AgentID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, note)
}
