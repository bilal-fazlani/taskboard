package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// Server is the HTTP API and web UI. It can be used as an http.Handler directly;
// Serve and ListenAndServe also watch the database and can be called at most
// once per Server (see Serve).
type Server struct {
	store         *db.Store
	router        chi.Router
	events        *broadcaster
	keepAlive     time.Duration
	watchInterval time.Duration
}

// Option configures a Server.
type Option func(*Server)

// WithKeepAlive sets how long an events stream may stay idle before it gets a
// keep-alive comment. The default is DefaultKeepAlive.
func WithKeepAlive(d time.Duration) Option {
	return func(s *Server) { s.keepAlive = d }
}

// WithWatchInterval sets how often Serve polls the database for changes. The
// default is db.DefaultWatchInterval.
func WithWatchInterval(d time.Duration) Option {
	return func(s *Server) { s.watchInterval = d }
}

func New(store *db.Store, webFS fs.FS, opts ...Option) *Server {
	s := &Server{
		store:         store,
		events:        newBroadcaster(),
		keepAlive:     DefaultKeepAlive,
		watchInterval: db.DefaultWatchInterval,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.setupRoutes(webFS)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// shutdownTimeout bounds how long Serve waits for in-flight requests.
const shutdownTimeout = 5 * time.Second

// openWatcher opens the database watcher Serve runs. Tests replace it to
// observe the watcher.
var openWatcher = db.OpenWatcher

// ListenAndServe serves on port until ctx is done. dbPath is the database the
// store uses; it is watched for changes from any process (see Serve).
func (s *Server) ListenAndServe(ctx context.Context, port int, dbPath string) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	return s.serve(ctx, ln, dbPath, func() {
		fmt.Printf("Taskboard running at http://localhost:%d\n", port)
	})
}

// Serve serves HTTP on ln until ctx is done, then shuts down cleanly.
//
// While it runs, a watcher polls the database at dbPath and every commit, from
// this server, the CLI or the MCP server, becomes a "changed" event on
// /api/events. On shutdown the watcher stops, open event streams end and
// in-flight requests get shutdownTimeout to finish. If serving fails instead,
// Serve closes every connection and returns the error. Either way it returns
// only after the watcher has stopped and its connection is closed.
//
// A Server can be served once: when Serve returns, its event streams stay
// closed, so serving it again would accept event clients and end them at once.
func (s *Server) Serve(ctx context.Context, ln net.Listener, dbPath string) error {
	return s.serve(ctx, ln, dbPath, nil)
}

// serve is Serve with a callback that runs once the watcher is open, just
// before requests are accepted.
func (s *Server) serve(ctx context.Context, ln net.Listener, dbPath string, ready func()) error {
	watcher, err := openWatcher(dbPath, s.watchInterval)
	if err != nil {
		ln.Close()
		return err
	}

	// Event streams never finish on their own; closing the broadcaster makes
	// them return so Shutdown does not wait for them.
	defer s.events.close()

	watchCtx, stopWatch := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		watcher.Run(watchCtx, s.events.publish)
	}()
	// One deferred step, so the watcher is always told to stop before Serve
	// waits for it, whichever way Serve returns.
	defer func() {
		stopWatch()
		<-watchDone
		watcher.Close()
	}()

	srv := &http.Server{Handler: s.router}
	srv.RegisterOnShutdown(s.events.close)

	if ready != nil {
		ready()
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		s.events.close()
		srv.Close()
		return err
	case <-ctx.Done():
	}
	stopWatch()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) setupRoutes(webFS fs.FS) {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	// The API is meant to be reached only from the embedded web UI (same
	// origin) or the Vite dev proxy (which forwards requests server-side, so
	// the browser never sees the backend's origin directly). Two separate
	// defenses replace the previous `Access-Control-Allow-Origin: *`, which
	// let any site open in the same browser read and write the whole board:
	//
	//  1. No CORS middleware at all, so no Access-Control-Allow-Origin
	//     header ever goes out. For a cross-origin GET (a "read", including
	//     the /api/events stream) the request still runs, but the browser
	//     refuses to let the requesting page's JavaScript read the
	//     response.
	//  2. rejectCrossOriginWrites, below, actively refuses a cross-origin
	//     POST/PUT/PATCH/DELETE (a "write") with 403 before it reaches a
	//     handler. This is load-bearing on its own: a browser only sends a
	//     CORS preflight for a "non-simple" request, and a POST with
	//     Content-Type text/plain, application/x-www-form-urlencoded or
	//     multipart/form-data counts as simple, so it reaches the server
	//     with no preflight and no CORS check at all — omitting CORS
	//     headers alone does not stop it, since the handlers (see
	//     decodeJSON) parse the body as JSON regardless of the Content-Type
	//     header the browser actually sent.
	//
	// The CLI and MCP server talk to the store directly rather than over
	// HTTP, and non-browser HTTP clients (curl, scripts) never send an
	// Origin header and are unaffected by either defense, since CORS and
	// Fetch Metadata are both enforced by the browser, not the server.
	r.Use(rejectCrossOriginWrites)

	r.Route("/api", func(r chi.Router) {
		r.Route("/projects", func(r chi.Router) {
			r.Get("/", s.listProjects)
			r.Post("/", s.createProject)
			r.Get("/{id}", s.getProject)
			r.Put("/{id}", s.updateProject)
			r.Delete("/{id}", s.deleteProject)
		})

		r.Route("/tickets", func(r chi.Router) {
			r.Get("/", s.listTickets)
			r.Post("/", s.createTicket)
			r.Get("/{id}", s.getTicket)
			r.Put("/{id}", s.updateTicket)
			r.Post("/{id}/move", s.moveTicket)
			r.Get("/{id}/history", s.ticketHistory)
			r.Delete("/{id}", s.deleteTicket)
			r.Post("/{id}/subtasks", s.addSubtask)
			r.Get("/{id}/documents", s.listTicketDocuments)
		})

		r.Route("/subtasks", func(r chi.Router) {
			r.Post("/{id}/toggle", s.toggleSubtask)
			r.Delete("/{id}", s.deleteSubtask)
		})

		r.Route("/labels", func(r chi.Router) {
			r.Get("/", s.listLabels)
			r.Post("/", s.createLabel)
			r.Put("/{id}", s.updateLabel)
			r.Delete("/{id}", s.deleteLabel)
		})

		r.Route("/epics", func(r chi.Router) {
			r.Get("/", s.listEpics)
			r.Post("/", s.createEpic)
			r.Put("/{id}", s.updateEpic)
			r.Delete("/{id}", s.deleteEpic)
		})

		r.Route("/documents", func(r chi.Router) {
			r.Get("/{ref}", s.getDocument)
			r.Put("/{ref}", s.updateDocument)
			r.Delete("/{ref}", s.deleteDocument)
			r.Get("/{ref}/download", s.downloadDocument)
		})

		r.Get("/board", s.getBoard)
		r.Get("/events", s.handleEvents)
	})

	if webFS != nil {
		fileServer := http.FileServer(http.FS(webFS))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			if _, err := fs.Stat(webFS, r.URL.Path[1:]); err != nil {
				r.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	s.router = r
}

// rejectCrossOriginWrites refuses a state-changing request (POST, PUT,
// PATCH, DELETE) whose Origin header names an origin other than this
// server's own, with 403. It exists because a browser only preflights a
// "non-simple" cross-origin request; a POST with Content-Type text/plain,
// application/x-www-form-urlencoded or multipart/form-data is simple, so it
// is sent (and, without this check, executed) with no preflight and no
// Access-Control-* headers involved at all.
//
// A request with no Origin header is treated as same-origin: only a browser
// adds Origin to a request a page's script made, so its absence means
// either a non-browser client (curl, a script) or a request this check does
// not need to cover. The origin comparison is against this request's own
// Host, in both the http and https forms, rather than a fixed origin, so it
// keeps working across ports and across the embedded UI vs. the Vite dev
// proxy without configuration.
//
// Sec-Fetch-Site is checked too, belt and braces: a browser that sends Fetch
// Metadata headers reports "cross-site" or "same-site" (a different
// subdomain of the same registrable domain — not "same-origin") on a
// request the Origin check above would already refuse, so both values are
// refused outright even if Origin were absent or, hypothetically, wrong.
//
// Known limitations: this check is self-referential — it compares Origin
// against the request's own Host rather than against a fixed allowlist — so
// DNS rebinding to 127.0.0.1 (a public DNS name that resolves to the
// loopback address) still passes it, since both Origin and Host would name
// that same rebound host. A fixed Host allowlist would close that gap but
// would also break reaching the board through a tunnel or reverse proxy,
// where the Host the server sees is neither localhost nor known in advance.
// Both the http and https forms of Host are accepted on purpose for the
// same reason: TLS may terminate upstream (a tunnel, a reverse proxy) so the
// browser's Origin can be https even though this server only ever speaks
// plain http.
func rejectCrossOriginWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			switch r.Header.Get("Sec-Fetch-Site") {
			case "cross-site", "same-site":
				writeError(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" &&
				origin != "http://"+r.Host && origin != "https://"+r.Host {
				writeError(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeStoreError maps a caller's bad input to 400 and everything else to 500.
func writeStoreError(w http.ResponseWriter, err error) {
	var invalid *db.ErrInvalidInput
	if errors.As(err, &invalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	projects, err := s.store.ListProjects(status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if projects == nil {
		projects = []models.Project{}
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetProject(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req models.CreateProjectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" || req.Prefix == "" {
		writeError(w, http.StatusBadRequest, "name and prefix are required")
		return
	}
	p, err := s.store.CreateProject(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateProjectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	p, err := s.store.UpdateProject(chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProject(chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listTickets(w http.ResponseWriter, r *http.Request) {
	filter := models.TicketFilter{
		ProjectID: r.URL.Query().Get("projectId"),
		Status:    r.URL.Query().Get("status"),
		Priority:  r.URL.Query().Get("priority"),
		Repo:      r.URL.Query().Get("repo"),
		Label:     r.URL.Query().Get("label"),
		Epic:      r.URL.Query().Get("epic"),
	}
	tickets, err := s.store.ListTickets(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tickets == nil {
		tickets = []models.Ticket{}
	}
	writeJSON(w, http.StatusOK, tickets)
}

func (s *Server) getTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTicket(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) createTicket(w http.ResponseWriter, r *http.Request) {
	var req models.CreateTicketRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.ProjectID == "" || req.Title == "" {
		writeError(w, http.StatusBadRequest, "projectId and title are required")
		return
	}
	t, err := s.store.CreateTicket(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) updateTicket(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateTicketRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	t, err := s.store.UpdateTicket(chi.URLParam(r, "id"), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) moveTicket(w http.ResponseWriter, r *http.Request) {
	var req models.MoveTicketRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}
	// A note is optional here, even out of agent_review: only the MCP tools
	// require one.
	t, err := s.store.MoveTicket(chi.URLParam(r, "id"), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// ticketHistory answers with a ticket's status changes, newest first.
func (s *Server) ticketHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := s.store.GetTicket(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	changes, err := s.store.ListStatusChanges(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, changes)
}

func (s *Server) deleteTicket(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTicket(chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addSubtask(w http.ResponseWriter, r *http.Request) {
	var req models.CreateSubtaskRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	st, err := s.store.AddSubtask(chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) toggleSubtask(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.ToggleSubtask(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) deleteSubtask(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSubtask(chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listLabels(w http.ResponseWriter, r *http.Request) {
	labels, err := s.store.ListLabels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if labels == nil {
		labels = []models.Label{}
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Server) createLabel(w http.ResponseWriter, r *http.Request) {
	var req models.CreateLabelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" || req.Color == "" {
		writeError(w, http.StatusBadRequest, "name and color are required")
		return
	}
	l, err := s.store.CreateLabel(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) updateLabel(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateLabelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	l, err := s.store.UpdateLabel(chi.URLParam(r, "id"), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// UpdateLabel returns (nil, nil) for an unknown id.
	if l == nil {
		writeError(w, http.StatusNotFound, "label not found")
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) deleteLabel(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.DeleteLabel(chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// epicsResponse is what GET /api/epics answers with: a project's epics (each
// carrying its own progress — see models.Epic), plus the same progress for
// the project's tickets that have no epic. The Epics view renders that as one
// more row alongside the real epics, so it comes from the same request.
type epicsResponse struct {
	Epics  []models.Epic        `json:"epics"`
	NoEpic *models.EpicProgress `json:"noEpic"`
}

func (s *Server) listEpics(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectId is required")
		return
	}
	epics, err := s.store.ListEpics(projectID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	noEpic, err := s.store.NoEpicProgress(projectID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, epicsResponse{Epics: epics, NoEpic: noEpic})
}

func (s *Server) createEpic(w http.ResponseWriter, r *http.Request) {
	var req models.CreateEpicRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	e, err := s.store.CreateEpic(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) updateEpic(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateEpicRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	e, err := s.store.UpdateEpic(chi.URLParam(r, "id"), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// UpdateEpic returns (nil, nil) for an unknown id, like UpdateLabel.
	if e == nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) deleteEpic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// DeleteEpic reports (0, nil) for an unknown id rather than an error (like
	// DeleteLabel/DeleteProject), so a 404 needs its own lookup first.
	e, err := s.store.GetEpic(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}
	if _, err := s.store.DeleteEpic(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getBoard(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	board, err := s.store.GetBoard(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, board)
}
