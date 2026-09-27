package server

import (
	"net/http"
	"time"
)

// getNow answers the web UI's Now page in one request: every ticket in
// progress or in review and the tickets that landed in the last day (see
// Store.Now). ?projectId=, an id or prefix, narrows it to one project; an
// unknown one gives empty lists.
func (s *Server) getNow(w http.ResponseWriter, r *http.Request) {
	now, err := s.store.Now(r.URL.Query().Get("projectId"), time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, now)
}
