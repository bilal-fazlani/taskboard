package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tcarac/taskboard/internal/models"
)

// The await long-poll's default and greatest timeout. A caller's ?timeout=
// is honoured up to maxAwaitTimeout; longer than that is capped rather than
// refused, since the wait itself is never an error, only its absence of an
// answer.
const (
	defaultAwaitTimeout = 30 * time.Second
	maxAwaitTimeout     = 60 * time.Second
)

// createRequest answers POST /api/tickets/{id}/requests: an agent asking the
// person for user input (type, prompt, choices). The ticket id or display
// key is resolved first, so an unknown one is a 404; everything CreateRequest
// refuses (an unknown type, a second open request, a ticket no agent holds)
// is a 400. It answers with the new request.
func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	ticketID, err := s.store.ResolveTicketID(chi.URLParam(r, "id"))
	if err != nil {
		writeLookupError(w, err)
		return
	}
	var req models.CreateUserInputRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.TicketID = ticketID
	id, err := s.store.CreateRequest(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	created, err := s.store.GetRequest(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// listTicketRequests answers GET /api/tickets/{id}/requests with the
// ticket's whole history of requests for user input, newest first, answered
// and unanswered alike. An unknown ticket is a 404.
func (s *Server) listTicketRequests(w http.ResponseWriter, r *http.Request) {
	ticketID, err := s.store.ResolveTicketID(chi.URLParam(r, "id"))
	if err != nil {
		writeLookupError(w, err)
		return
	}
	reqs, err := s.store.ListRequests(ticketID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}

// answerRequest answers POST /api/requests/{id}/answer: the person's answer,
// and who gave it. answeredBy left out or blank takes the local person
// (localPerson, entries.go), since an answer is today always the local
// person's. An unknown request is a 404; an already-answered request, a
// blank answer, or an answer that isn't one of the request's choices is a
// 400.
func (s *Server) answerRequest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.store.GetRequest(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	var req struct {
		Answer     string `json:"answer"`
		AnsweredBy string `json:"answeredBy"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	answeredBy := strings.TrimSpace(req.AnsweredBy)
	if answeredBy == "" {
		answeredBy = localPerson()
	}
	answered, err := s.store.AnswerRequest(id, req.Answer, answeredBy)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, answered)
}

// awaitAnswerRequestEntered, when set, is called just before awaitAnswerRequest
// starts waiting on the store, letting a test observe that a request has
// really reached the handler instead of guessing with a sleep. Nil by
// default; tests that set it must restore it.
var awaitAnswerRequestEntered func()

// awaitAnswerRequest answers GET /api/requests/{id}/await?timeout=30s: a
// long-poll on that one request, never on whatever else is open on its
// ticket. The wait itself is never an error: it answers with the request as
// last read, answered or (once the capped timeout passes) still not. An
// unknown request is a 404; a timeout that doesn't parse as a duration is a
// 400.
func (s *Server) awaitAnswerRequest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.store.GetRequest(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	timeout := defaultAwaitTimeout
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			writeError(w, http.StatusBadRequest, "timeout must be a positive duration, such as 30s")
			return
		}
		timeout = d
	}
	if timeout > maxAwaitTimeout {
		timeout = maxAwaitTimeout
	}
	// r.Context() alone is not enough: http.Server.Shutdown never cancels a
	// request's own context, so a pending await would otherwise hold up
	// shutdown until the wait's own timeout, not the shutdown grace period.
	// shutdownAwareContext also ends the wait once the server starts
	// shutting down, the same signal /api/events streams watch.
	ctx, cancel := s.shutdownAwareContext(r.Context())
	defer cancel()
	if awaitAnswerRequestEntered != nil {
		awaitAnswerRequestEntered()
	}
	result, err := s.store.AwaitAnswer(ctx, id, timeout)
	if err != nil {
		if ctx.Err() != nil {
			// The wait's own context ended: AwaitAnswer returns ctx.Err()
			// itself in this case (internal/db/requests.go), never a store
			// error, so this branch is only ever a context ending, not any
			// of AwaitAnswer's other errors below.
			if r.Context().Err() != nil {
				// The client itself went away: there is no one left to
				// answer to, and no connection left to write a response on.
				return
			}
			// The client is still there, so only shutdownAwareContext's own
			// cancel could have ended ctx: the server is shutting down. The
			// request is still unanswered and nothing is wrong with it, so
			// say so plainly rather than silently dropping the connection or
			// answering as if it had simply timed out: a 503 tells the
			// caller to retry, an empty 200 would not.
			writeError(w, http.StatusServiceUnavailable, "the server is shutting down; retry the await")
			return
		}
		// ctx did not end, so this is one of AwaitAnswer's own errors: most
		// often its request vanishing mid-wait (an ErrInvalidInput, the same
		// as an unresolved reference elsewhere), for instance because its
		// ticket was deleted; anything else is a genuine store failure.
		// Both go through writeLookupError, which maps exactly that way.
		writeLookupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
