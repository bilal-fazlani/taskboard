package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tcarac/taskboard/internal/models"
)

// identifyAgent answers POST /api/agents: a session (vendor, vendor session
// ID, machine, resume command, web link) and a new agent within it (role,
// model, provider). It answers with the new agent. Anything missing or
// unknown (models.ValidProvider) is a 400 with the store's message.
func (s *Server) identifyAgent(w http.ResponseWriter, r *http.Request) {
	var req models.IdentifyAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	a, err := s.store.IdentifyAgent(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

// listAgents answers GET /api/agents with every agent there is, most
// recently seen first, each with its session and the tickets it currently
// holds.
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agents == nil {
		agents = []models.AgentListItem{}
	}
	writeJSON(w, http.StatusOK, agents)
}

// getAgent answers GET /api/agents/{id} with one agent, the same shape
// listAgents uses.
func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAgentView(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// releaseTicket answers POST /api/tickets/{id}/release: an agent giving a
// ticket back (with its hand-off) or finishing it (with its proof). The
// ticket id or display key is resolved first, so an unknown one is a 404;
// everything else ReleaseTicket refuses (a missing hand-off or proof, an
// unknown outcome, an agent that isn't the holder or its session, an open
// request) is a 400 naming what is wrong.
func (s *Server) releaseTicket(w http.ResponseWriter, r *http.Request) {
	id, err := s.store.ResolveTicketID(chi.URLParam(r, "id"))
	if err != nil {
		writeLookupError(w, err)
		return
	}
	var req models.ReleaseTicketRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	t, err := s.store.ReleaseTicket(id, req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}
