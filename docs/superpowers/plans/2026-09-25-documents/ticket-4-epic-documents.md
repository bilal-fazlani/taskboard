# Ticket 4: Documents on epics — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Epics get documents with every rule and action tickets have, reached from a paperclip on the Epics page row or the row menu's Edit, in an epic modal that replaces today's Edit epic dialog and has its own URL.

**Architecture:** `DocumentOwner` gains `EpicID`; the table already allows it. Epics carry a document count (list) and document list (single epic). HTTP, MCP and CLI accept an epic (id, or name with its project) wherever they accept a ticket for documents. The web UI adds `useEpicParam` (the `epic` query parameter, through `useOverlayHistory`) and an `EpicModal` holding the existing epic form plus the shared Documents section and document modal.

**Tech Stack:** Go, React 19, React Router 7, Vitest + jsdom.

**Spec:** `docs/superpowers/specs/2026-09-25-documents-design.md`, section "Ticket 4".

**Depends on:** tickets 0–3.

## Global constraints

See [README.md](README.md#global-constraints). Messages name the owner: "This epic already has a document called …", "Couldn't find … on this epic." The epic modal keeps the Edit epic dialog's labels and texts ("Edit epic", "Name", "Description (optional)", "Save", "Cancel") so its behaviour stays familiar. Like today's dialog, the epic's name and description fields do not ask before unsaved typing is dropped; document edits inside it still do (ticket 2).

## Review Focus

- A document request that names both a ticket and an epic is refused, not resolved to one of them. Pinned in Tasks 1 and 3.
- Deleting an epic deletes its documents but not its tickets' documents. Pinned in Task 1.
- Renaming the open epic from the modal keeps the modal open: the URL follows the new name once the list has reloaded. Pinned in Task 5.
- `?epic=` naming an epic that is not in the shown project closes nothing and opens nothing; the parameter is dropped. Pinned in Task 5.
- Escape inside an epic document closes the document, not the epic modal. Pinned in Task 6.

---

### Task 1: Epics own documents in the store

**Files:**
- Modify: `internal/models/document.go` (`EpicID` on meta and create request)
- Modify: `internal/models/epic.go` (`DocumentCount`, `Documents`)
- Modify: `internal/db/documents.go` (owner, scan, create, update)
- Modify: `internal/db/epics.go` (`GetEpic`, `ListEpics`)
- Test: `internal/db/documents_test.go`

**Interfaces:**
- Produces:
  ```go
  type DocumentOwner struct{ TicketID, EpicID string }   // exactly one set
  // models.DocumentMeta.EpicID string `json:"epicId,omitempty"`
  // models.CreateDocumentRequest.TicketID `json:"ticketId,omitempty"`, EpicID `json:"epicId,omitempty"`
  // models.Epic.DocumentCount int `json:"documentCount"`; Documents []DocumentMeta `json:"documents,omitempty"` (GetEpic only)
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestEpicDocuments(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	e := seedEpic(t, s, p.ID, "Launch")
	tk := seedTicket(t, s, p.ID, "Has docs")
	ticketDoc := seedDocument(t, s, tk.ID, "Plan", "")

	d, err := s.CreateDocument(models.CreateDocumentRequest{EpicID: e.ID, Name: "Plan", Content: "# Rollout"})
	if err != nil || d.EpicID != e.ID || d.TicketID != "" {
		t.Fatalf("epic document = %+v, %v", d, err)
	}
	_, err = s.CreateDocument(models.CreateDocumentRequest{EpicID: e.ID, Name: "plan"})
	wantInvalid(t, err, `This epic already has a document called "Plan.md".`)
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, EpicID: e.ID, Name: "Both"})
	wantInvalid(t, err, msgDocOwner)
	_, err = s.CreateDocument(models.CreateDocumentRequest{Name: "Neither"})
	wantInvalid(t, err, msgDocOwner)
	_, err = s.CreateDocument(models.CreateDocumentRequest{EpicID: "nope", Name: "X"})
	wantInvalid(t, err, "epic not found")

	id, err := s.ResolveDocumentRef(DocumentOwner{EpicID: e.ID}, "plan.md")
	if err != nil || id != d.ID {
		t.Fatalf("resolve on epic = %q, %v", id, err)
	}
	_, err = s.ResolveDocumentRef(DocumentOwner{EpicID: e.ID}, "Missing")
	wantInvalid(t, err, `This epic has no document called "Missing".`)

	name := "Rollout"
	renamed, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Name: &name})
	if err != nil || renamed.Name != "Rollout" || renamed.EpicID != e.ID {
		t.Fatalf("rename epic document = %+v, %v", renamed, err)
	}

	full, err := s.GetEpic(e.ID)
	if err != nil || full.DocumentCount != 1 || len(full.Documents) != 1 {
		t.Fatalf("GetEpic documents = %+v, %v", full, err)
	}
	list, err := s.ListEpics(p.ID)
	if err != nil || len(list) != 1 || list[0].DocumentCount != 1 || list[0].Documents != nil {
		t.Fatalf("ListEpics = %+v, %v", list, err)
	}

	if _, err := s.DeleteEpic(e.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetDocument(d.ID); got != nil {
		t.Fatal("deleting the epic should delete its documents")
	}
	if got, _ := s.GetDocument(ticketDoc.ID); got == nil {
		t.Fatal("deleting the epic must not touch its tickets' documents")
	}
}
```

(`seedEpic` lives in `epics_test.go`; its second argument is a project ref, so `p.ID` works.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/db -run TestEpicDocuments -v`
Expected: FAIL, `unknown field EpicID`.

- [ ] **Step 3: Models**

`internal/models/document.go`: add `EpicID string `json:"epicId,omitempty"`` to `DocumentMeta` after `TicketID`; in `CreateDocumentRequest` make `TicketID` `json:"ticketId,omitempty"` and add `EpicID string `json:"epicId,omitempty"``, with the comment "Exactly one of TicketID and EpicID is set."

`internal/models/epic.go`, in `Epic` after `UpdatedAt`:

```go
	// DocumentCount is how many documents the epic has. Documents lists them,
	// without content, on a single epic only (GetEpic).
	DocumentCount int            `json:"documentCount"`
	Documents     []DocumentMeta `json:"documents,omitempty"`
```

- [ ] **Step 4: Store**

In `internal/db/documents.go`:

```go
	msgDocOwner = "A document belongs to one ticket or one epic."
```

Replace `DocumentOwner` and its helpers:

```go
// DocumentOwner names what a document belongs to: exactly one of a ticket or
// an epic, by id. Callers resolve display keys and epic names first.
type DocumentOwner struct {
	TicketID string
	EpicID   string
}

func (o DocumentOwner) check() error {
	if (o.TicketID == "") == (o.EpicID == "") {
		return invalidInput(msgDocOwner)
	}
	return nil
}

func (o DocumentOwner) noun() string {
	if o.EpicID != "" {
		return "epic"
	}
	return "ticket"
}

func (o DocumentOwner) where() (string, any) {
	if o.EpicID != "" {
		return "epic_id = ?", o.EpicID
	}
	return "ticket_id = ?", o.TicketID
}

func checkOwnerExists(q dbtx, owner DocumentOwner) error {
	if err := owner.check(); err != nil {
		return err
	}
	table, id := "tickets", owner.TicketID
	if owner.EpicID != "" {
		table, id = "epics", owner.EpicID
	}
	var one int
	err := q.QueryRow("SELECT 1 FROM "+table+" WHERE id = ?", id).Scan(&one)
	if err == sql.ErrNoRows {
		return invalidInput("%s not found", owner.noun())
	}
	return err
}

// nullable stores "" as NULL, which the one-owner CHECK and the foreign keys need.
func nullable(id string) any {
	if id == "" {
		return nil
	}
	return id
}
```

Update the scan to read the epic too:

```go
const documentMetaColumns = `id, COALESCE(ticket_id, ''), COALESCE(epic_id, ''), name, format,
	LENGTH(CAST(content AS BLOB)), revision, created_at, updated_at`

func scanDocumentMeta(row interface{ Scan(...any) error }, extra ...any) (models.DocumentMeta, error) {
	var d models.DocumentMeta
	dest := append([]any{&d.ID, &d.TicketID, &d.EpicID, &d.Name, &d.Format, &d.Size, &d.Revision, &d.CreatedAt, &d.UpdatedAt}, extra...)
	err := row.Scan(dest...)
	return d, err
}
```

In `CreateDocument`: `owner := DocumentOwner{TicketID: req.TicketID, EpicID: req.EpicID}`, and insert both columns:

```go
		`INSERT INTO documents (id, ticket_id, epic_id, name, format, content, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		id, nullable(owner.TicketID), nullable(owner.EpicID), name, format, req.Content, now, now,
```

In `UpdateDocument`, read both owners:

```go
	err = tx.QueryRow("SELECT COALESCE(ticket_id, ''), COALESCE(epic_id, ''), name, revision FROM documents WHERE id = ?", id).
		Scan(&owner.TicketID, &owner.EpicID, &name, &revision)
```

In `internal/db/epics.go`:

- `GetEpic`, before `return &e, nil`:

```go
	if e.Documents, err = loadOwnerDocuments(s.db, DocumentOwner{EpicID: e.ID}); err != nil {
		return nil, err
	}
	e.DocumentCount = len(e.Documents)
```

- `ListEpics`, after the progress loop:

```go
	counts, err := loadEpicDocumentCounts(s.db, projectID)
	if err != nil {
		return nil, err
	}
	for i := range epics {
		epics[i].DocumentCount = counts[epics[i].ID]
	}
```

- and add:

```go
// loadEpicDocumentCounts counts the documents of every epic in a project in
// one grouped query.
func loadEpicDocumentCounts(q dbtx, projectID string) (map[string]int, error) {
	rows, err := q.Query(`SELECT d.epic_id, COUNT(*) FROM documents d
		JOIN epics e ON e.id = d.epic_id WHERE e.project_id = ? GROUP BY d.epic_id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("counting epic documents: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}
```

Update `DeleteEpic`'s comment: "Its tickets stay and lose their epic; its documents are deleted with it."

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/db -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/models/document.go internal/models/epic.go internal/db/documents.go internal/db/epics.go internal/db/documents_test.go
git commit -m "feat: let epics own documents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Epic links, and HTTP routes for epic documents

**Files:**
- Modify: `internal/weburl/weburl.go` (+ test)
- Modify: `internal/server/documents.go`, `internal/server/server.go`
- Test: `internal/server/documents_test.go`

**Interfaces:**
- Produces:
  ```go
  func weburl.Epic(base, projectPrefix, epicName string) string          // base + "/epics?project=P&epic=N"
  func weburl.EpicDocument(base, projectPrefix, epicName, displayName string) string
  ```
  - `GET /api/epics/{id}` → `200 Epic` with `documents`; unknown `404`.
  - `GET /api/epics/{id}/documents` → `200 []DocumentMeta`; unknown `404`.
  - Document routes accept `?epic=<id>` or `?epic=<name>&project=<id or prefix>` in place of `?ticket=`; both at once → `400`.
  - `POST /api/documents` accepts `epicId`.

- [ ] **Step 1: Write the failing tests**

`internal/weburl/weburl_test.go`:

```go
func TestEpicDocumentURL(t *testing.T) {
	got := EpicDocument("http://localhost:3011", "ACP", "M4: Documents", "Rollout plan.md")
	want := "http://localhost:3011/epics?project=ACP&epic=M4%3A+Documents&doc=Rollout+plan.md"
	if got != want {
		t.Fatalf("EpicDocument = %q, want %q", got, want)
	}
}
```

`internal/server/documents_test.go`:

```go
func TestEpicDocumentsOverHTTP(t *testing.T) {
	r := serve(t)
	p, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.srv.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}

	created, status := doRequest[models.Document](t, http.MethodPost, r.url+"/api/documents",
		`{"epicId":"`+e.ID+`","name":"Rollout","format":"markdown","content":"# r"}`)
	if status != http.StatusCreated || created.EpicID != e.ID {
		t.Fatalf("create: %d %+v", status, created)
	}

	docs, status := doRequest[[]models.DocumentMeta](t, http.MethodGet, r.url+"/api/epics/"+e.ID+"/documents", "")
	if status != http.StatusOK || len(docs) != 1 {
		t.Fatalf("list: %d %+v", status, docs)
	}
	epic, status := doRequest[models.Epic](t, http.MethodGet, r.url+"/api/epics/"+e.ID, "")
	if status != http.StatusOK || epic.DocumentCount != 1 || len(epic.Documents) != 1 {
		t.Fatalf("get epic: %d %+v", status, epic)
	}
	_, status = errorBody(t, http.MethodGet, r.url+"/api/epics/nope", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown epic status = %d", status)
	}

	byName, status := doRequest[models.Document](t, http.MethodGet, r.url+"/api/documents/rollout.md?epic=launch&project=doc", "")
	if status != http.StatusOK || byName.ID != created.ID {
		t.Fatalf("by epic name: %d %+v", status, byName)
	}
	byEpicID, status := doRequest[models.Document](t, http.MethodGet, r.url+"/api/documents/Rollout?epic="+e.ID, "")
	if status != http.StatusOK || byEpicID.ID != created.ID {
		t.Fatalf("by epic id: %d %+v", status, byEpicID)
	}
	_, status = errorBody(t, http.MethodGet, r.url+"/api/documents/Rollout?epic="+e.ID+"&ticket=DOC-1", "")
	if status != http.StatusBadRequest {
		t.Fatalf("both owners status = %d, want 400", status)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/weburl ./internal/server -run 'Epic' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`internal/weburl/weburl.go`:

```go
// Epic is the URL that opens an epic's modal on the Epics view.
func Epic(base, projectPrefix, epicName string) string {
	return base + "/epics?project=" + url.QueryEscape(projectPrefix) + "&epic=" + url.QueryEscape(epicName)
}

// EpicDocument is the URL that opens an epic with one of its documents on top.
func EpicDocument(base, projectPrefix, epicName, displayName string) string {
	return Epic(base, projectPrefix, epicName) + "&doc=" + url.QueryEscape(displayName)
}
```

`internal/server/documents.go`: replace `documentID` with an owner-aware version, and add the epic handlers:

```go
// documentOwner reads the owner a document route names by query: ?ticket=
// (id or key), or ?epic= (id, or name with ?project=). ok is false once an
// error has been written; a zero owner means none was named.
func (s *Server) documentOwner(w http.ResponseWriter, r *http.Request) (db.DocumentOwner, bool) {
	q := r.URL.Query()
	ticket := strings.TrimSpace(q.Get("ticket"))
	epic := strings.TrimSpace(q.Get("epic"))
	switch {
	case ticket != "" && epic != "":
		writeError(w, http.StatusBadRequest, "pass ticket or epic, not both")
		return db.DocumentOwner{}, false
	case ticket != "":
		id, err := s.store.ResolveTicketID(ticket)
		if err != nil {
			writeLookupError(w, err)
			return db.DocumentOwner{}, false
		}
		return db.DocumentOwner{TicketID: id}, true
	case epic != "":
		id, err := s.resolveEpic(epic, q.Get("project"))
		if err != nil {
			writeLookupError(w, err)
			return db.DocumentOwner{}, false
		}
		return db.DocumentOwner{EpicID: id}, true
	}
	return db.DocumentOwner{}, true
}

// resolveEpic takes an epic id, or a name within a project.
func (s *Server) resolveEpic(ref, project string) (string, error) {
	if strings.TrimSpace(project) != "" {
		return s.store.ResolveEpicRef(project, ref)
	}
	e, err := s.store.GetEpic(ref)
	if err != nil {
		return "", err
	}
	if e == nil {
		return "", &db.ErrInvalidInput{Msg: "epic not found; pass project when naming an epic"}
	}
	return e.ID, nil
}

func (s *Server) documentID(w http.ResponseWriter, r *http.Request) (string, bool) {
	ref := chi.URLParam(r, "ref")
	owner, ok := s.documentOwner(w, r)
	if !ok {
		return "", false
	}
	if owner == (db.DocumentOwner{}) {
		return ref, true
	}
	id, err := s.store.ResolveDocumentRef(owner, ref)
	if err != nil {
		writeLookupError(w, err)
		return "", false
	}
	return id, true
}

func (s *Server) getEpic(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.GetEpic(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) listEpicDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := s.store.ListDocuments(db.DocumentOwner{EpicID: chi.URLParam(r, "id")})
	if err != nil {
		writeLookupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}
```

`setupRoutes`: in `/epics` add `r.Get("/{id}", s.getEpic)` and `r.Get("/{id}/documents", s.listEpicDocuments)`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/weburl ./internal/server -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/weburl internal/server/documents.go internal/server/server.go internal/server/documents_test.go
git commit -m "feat: serve epic documents over HTTP

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: MCP and CLI address epic documents

**Files:**
- Modify: `internal/mcp/documents.go` (+ test)
- Modify: `internal/cli/document.go` (+ test)

**Interfaces:**
- Produces:
  - MCP: `get_document`, `update_document`, `delete_document` take `epic` + `project` as an alternative to `ticket`; `create_document` takes `ticket` or `epic` (+ `project`); new `list_documents {ticket? | epic?, project?}` → documents with URLs. Epic documents' `url` is `weburl.EpicDocument`.
  - CLI: every `doc` command takes `--epic NAME|ID` and `--project P` as an alternative to the ticket; `doc list` and `doc add` take the ticket as an optional positional argument.

- [ ] **Step 1: Write the failing tests**

`internal/mcp/documents_test.go`:

```go
func TestEpicDocumentTools(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	p, _ := s.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if _, err := s.store.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Launch"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"epic": "launch", "project": "DOC", "name": "Rollout", "content": "# r",
	}))
	if err != nil {
		t.Fatalf("create_document on epic: %v", err)
	}
	d := got.(*models.Document)
	if d.URL != "http://board.test/epics?project=DOC&epic=Launch&doc=Rollout.md" {
		t.Fatalf("url = %q", d.URL)
	}

	listed, err := s.callTool("list_documents", mustJSON(t, map[string]any{"epic": "Launch", "project": "doc"}))
	if err != nil {
		t.Fatal(err)
	}
	docs := listed.([]models.DocumentMeta)
	if len(docs) != 1 || docs[0].URL == "" {
		t.Fatalf("list_documents = %+v", docs)
	}

	if _, err := s.callTool("get_document", mustJSON(t, map[string]any{"id": "rollout.md", "epic": "Launch", "project": "DOC"})); err != nil {
		t.Fatalf("get_document by epic name: %v", err)
	}
	_, err = s.callTool("get_document", mustJSON(t, map[string]any{"id": "Rollout", "epic": "Launch", "project": "DOC", "ticket": "DOC-1"}))
	if err == nil || err.Error() != "pass ticket or epic, not both" {
		t.Fatalf("both owners error = %v", err)
	}
}
```

`internal/cli/document_test.go`:

```go
func TestDocCommandsOnEpics(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	for _, args := range [][]string{
		{"project", "create", "Docs", "--prefix", "DOC"},
		{"epic", "create", "DOC", "Launch"},
	} {
		if _, err := runCLI(t, append([]string{"--db", path}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	added := captureStdout(t, func() {
		if _, err := runCLIWithInput(t, "# r", "--db", path, "doc", "add", "--epic", "Launch", "--project", "DOC", "--name", "Rollout", "--file", "-"); err != nil {
			t.Fatalf("doc add on epic: %v", err)
		}
	})
	if !strings.Contains(added, "Added document Rollout.md") || !strings.Contains(added, "/epics?project=DOC&epic=Launch&doc=Rollout.md") {
		t.Fatalf("doc add printed %q", added)
	}
	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "list", "--epic", "launch", "--project", "doc"); err != nil {
			t.Fatalf("doc list on epic: %v", err)
		}
	})
	if !strings.Contains(listed, "Rollout.md") {
		t.Fatalf("doc list printed %q", listed)
	}
	shown := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "show", "Rollout", "--epic", "Launch", "--project", "DOC"); err != nil {
			t.Fatalf("doc show on epic: %v", err)
		}
	})
	if shown != "# r" {
		t.Fatalf("doc show printed %q", shown)
	}
	if _, err := runCLI(t, "--db", path, "doc", "list"); err == nil || !strings.Contains(err.Error(), "--epic") {
		t.Fatalf("doc list with no owner: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcp ./internal/cli -run 'Epic' -v`
Expected: FAIL.

- [ ] **Step 3: MCP**

In `internal/mcp/documents.go`:

1. Add the owner resolver and use it everywhere a document is resolved:

```go
// resolveDocumentOwner reads the owner arguments: ticket (id or key), or
// epic (id, or name with project). A zero owner means none was given.
func (s *MCPServer) resolveDocumentOwner(ticket, epic, project string) (db.DocumentOwner, error) {
	ticket, epic = strings.TrimSpace(ticket), strings.TrimSpace(epic)
	switch {
	case ticket != "" && epic != "":
		return db.DocumentOwner{}, fmt.Errorf("pass ticket or epic, not both")
	case ticket != "":
		id, err := s.store.ResolveTicketID(ticket)
		return db.DocumentOwner{TicketID: id}, err
	case epic != "":
		id, err := s.resolveEpicRefOrError(epic, project)
		return db.DocumentOwner{EpicID: id}, err
	}
	return db.DocumentOwner{}, nil
}

func (s *MCPServer) resolveDocumentRefOrError(ref, ticket, epic, project string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("id is required")
	}
	owner, err := s.resolveDocumentOwner(ticket, epic, project)
	if err != nil {
		return "", err
	}
	if owner != (db.DocumentOwner{}) {
		return s.store.ResolveDocumentRef(owner, ref)
	}
	d, err := s.store.GetDocument(ref)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("no document matches %q; pass ticket, or epic and project, when addressing by name", ref)
	}
	return d.ID, nil
}
```

2. Every argument struct gains `Epic string `json:"epic"`` and `Project string `json:"project"``; call `resolveDocumentRefOrError(a.ID, a.Ticket, a.Epic, a.Project)`. In `create_document`, replace the ticket check with:

```go
		owner, err := s.resolveDocumentOwner(a.Ticket, a.Epic, a.Project)
		if err != nil {
			return nil, true, err
		}
		if owner == (db.DocumentOwner{}) {
			return nil, true, fmt.Errorf("ticket or epic is required")
		}
		d, err := s.store.CreateDocument(models.CreateDocumentRequest{
			TicketID: owner.TicketID, EpicID: owner.EpicID, Name: a.Name, Format: a.Format, Content: a.Content,
		})
```

   and change the ticket 1 test expectation `"ticket is required"` to `"ticket or epic is required"`.

3. Add `list_documents`:

```go
	case "list_documents":
		var a struct {
			Ticket  string `json:"ticket"`
			Epic    string `json:"epic"`
			Project string `json:"project"`
		}
		json.Unmarshal(args, &a)
		owner, err := s.resolveDocumentOwner(a.Ticket, a.Epic, a.Project)
		if err != nil {
			return nil, true, err
		}
		if owner == (db.DocumentOwner{}) {
			return nil, true, fmt.Errorf("ticket or epic is required")
		}
		docs, err := s.store.ListDocuments(owner)
		if err != nil {
			return nil, true, err
		}
		for i := range docs {
			docs[i].URL = s.documentURL(&docs[i])
		}
		return docs, true, nil
```

4. Replace `withDocumentURL` with a URL builder both use:

```go
// documentURL is the link that opens a document in the web UI: on its
// ticket, or on its epic's modal in the Epics view.
func (s *MCPServer) documentURL(d *models.DocumentMeta) string {
	display := models.DocumentDisplayName(d.Name, d.Format)
	if d.EpicID != "" {
		e, err := s.store.GetEpic(d.EpicID)
		if err != nil || e == nil {
			return ""
		}
		p, err := s.store.GetProject(e.ProjectID)
		if err != nil || p == nil {
			return ""
		}
		return weburl.EpicDocument(weburl.Base(), p.Prefix, e.Name, display)
	}
	t, err := s.store.GetTicket(d.TicketID)
	if err != nil || t == nil {
		return ""
	}
	return weburl.TicketDocument(weburl.Base(), weburl.Ref(*t), display)
}

func (s *MCPServer) withDocumentURL(d *models.Document) *models.Document {
	d.URL = s.documentURL(&d.DocumentMeta)
	return d
}
```

5. Definitions: add `"epic": {Type: "string", Description: "Epic ID, or its name together with project; instead of ticket"}` and `"project": {Type: "string", Description: "Project ID or prefix (case-insensitive); required when epic is a name"}` to the four existing tools (for `create_document`, drop `"ticket"` from `Required`, leaving `name` and `content`, and say "Attach … to a ticket or an epic"). Add:

```go
		{
			Name:        "list_documents",
			Description: "List the documents attached to a ticket or an epic, without content, each with a link.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"ticket":  {Type: "string", Description: "Ticket ID or display key (e.g. BILL-2), case-insensitive"},
					"epic":    {Type: "string", Description: "Epic ID, or its name together with project; instead of ticket"},
					"project": {Type: "string", Description: "Project ID or prefix (case-insensitive); required when epic is a name"},
				},
			},
		},
```

- [ ] **Step 4: CLI**

In `internal/cli/document.go`:

1. A resolver used by every command:

```go
// documentOwnerArg resolves where a document lives: a ticket (the positional
// argument or --ticket), or --epic with --project. A zero owner means none.
func documentOwnerArg(store *db.Store, ticket, epic, project string) (db.DocumentOwner, error) {
	switch {
	case ticket != "" && epic != "":
		return db.DocumentOwner{}, fmt.Errorf("pass a ticket or --epic, not both")
	case ticket != "":
		id, err := store.ResolveTicketID(ticket)
		return db.DocumentOwner{TicketID: id}, err
	case epic != "":
		id, err := resolveEpicArg(store, epic, project)
		return db.DocumentOwner{EpicID: id}, err
	}
	return db.DocumentOwner{}, nil
}
```

2. `resolveDocumentArg(store, ref, ticket, epic, project string)`: use `documentOwnerArg`; with a zero owner fall back to the id lookup, with the error `document not found: %q (to use a name, pass --ticket, or --epic with --project)`. Update `loadDocumentArg` the same way.
3. Each of `show`, `write`, `rename`, `delete` gains `--epic` and `--project` string flags passed through.
4. `list` and `add` change `Args` to `cobra.MaximumNArgs(1)`, gain `--epic`/`--project`, and resolve with `documentOwnerArg(store, firstArg(args), epic, project)`; a zero owner is the error `provide a ticket, or --epic with --project`. `add` passes `TicketID`/`EpicID` from the owner into `CreateDocumentRequest`.
5. `documentURL(store, d)` builds the epic link when `d.EpicID != ""` (same logic as the MCP `documentURL`, using `weburl.EpicDocument`).

```go
func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/documents.go internal/mcp/documents_test.go internal/cli/document.go internal/cli/document_test.go
git commit -m "feat: address epic documents from MCP and the CLI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Web client and owner-generic document hooks

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/lib/documents.ts` (+ test)
- Modify: `web/src/hooks/useOwnerDocuments.ts`
- Create: `web/src/components/DiscardChangesConfirm.tsx` (moved out of `DocumentModal.tsx`)
- Modify: `web/src/components/DocumentModal.tsx`

**Interfaces:**
- Produces:
  ```ts
  export type DocumentOwnerRef = { ticketId: string } | { epicId: string };
  // DocumentMeta.epicId?: string; Epic.documentCount?: number; Epic.documents?: DocumentMeta[]
  api.documents.list(owner) // /api/tickets/{id}/documents or /api/epics/{id}/documents
  api.documents.create(data: DocumentOwnerRef & { name; format; content })
  ownerKey(owner) // "ticket:<id>" | "epic:<id>"
  <DiscardChangesConfirm what="this document" onCancel onDiscard />
  ```

- [ ] **Step 1: Write the failing test**

In `web/src/lib/documents.test.ts`, extend `describe("ownerKey")`:

```ts
  it("tells tickets and epics apart", () => {
    expect(ownerKey({ epicId: "e1" })).toBe("epic:e1");
  });
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/documents.test.ts -t ownerKey`
Expected: FAIL (type error or `ticket:undefined`).

- [ ] **Step 3: Implement**

`client.ts`:

```ts
/** What a document belongs to: one ticket or one epic. */
export type DocumentOwnerRef = { ticketId: string } | { epicId: string };
```

Add `epicId?: string;` to `DocumentMeta`; add to `Epic`:

```ts
  /** How many documents the epic has. */
  documentCount?: number;
  /** Its documents, without content; only a single epic carries them. */
  documents?: DocumentMeta[];
```

In `api.documents`:

```ts
    list: (owner: DocumentOwnerRef) =>
      request<DocumentMeta[]>(
        "ticketId" in owner ? `/api/tickets/${owner.ticketId}/documents` : `/api/epics/${owner.epicId}/documents`,
      ),
    create: (data: DocumentOwnerRef & { name: string; format: DocumentFormat; content: string }) =>
      request<DocumentWithContent>("/api/documents", { method: "POST", body: JSON.stringify(data) }),
```

`lib/documents.ts`:

```ts
export function ownerKey(owner: DocumentOwnerRef): string {
  return "ticketId" in owner ? `ticket:${owner.ticketId}` : `epic:${owner.epicId}`;
}
```

`useOwnerDocuments.ts`: replace `const { ticketId } = owner;` with

```ts
  const ticketId = "ticketId" in owner ? owner.ticketId : undefined;
  const epicId = "epicId" in owner ? owner.epicId : undefined;
```

and in `reload` call `api.documents.list(ticketId !== undefined ? { ticketId } : { epicId: epicId! })`, with `[key, ticketId, epicId]` as dependencies.

Move `DiscardDocumentConfirm` from `DocumentModal.tsx` into `web/src/components/DiscardChangesConfirm.tsx` as a default export named `DiscardChangesConfirm`, with a `what: string` prop used in the body: `Your edits to {what} have not been saved.` In `DocumentModal.tsx` import it and render `<DiscardChangesConfirm what="this document" onCancel={cancelDiscard} onDiscard={acceptDiscard} />`.

- [ ] **Step 4: Run the web suite and types**

Run: `cd web && npm test && npx tsc -b`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/client.ts web/src/lib/documents.ts web/src/lib/documents.test.ts web/src/hooks/useOwnerDocuments.ts web/src/components/DiscardChangesConfirm.tsx web/src/components/DocumentModal.tsx
git commit -m "refactor: make the web document helpers work for any owner

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: The `epic` parameter

**Files:**
- Create: `web/src/lib/epicParam.ts` (+ `epicParam.test.ts`)
- Create: `web/src/hooks/useEpicParam.ts` (+ `useEpicParam.dom.test.tsx`)

**Interfaces:**
- Produces:
  ```ts
  const EPIC_PARAM = "epic";
  function findEpic<T extends { id: string; name: string }>(epics: readonly T[], ref: string): T | undefined // id, or name ignoring case
  function withEpic(params: URLSearchParams, epic: { name: string }): URLSearchParams // sets epic, drops doc
  function withoutEpic(params: URLSearchParams): URLSearchParams
  interface EpicParamState<T> { selected: T | null; open(epic: T): void; close(): void; renamed(epic: T): void }
  function useEpicParam<T extends { id: string; name: string }>(epics: readonly T[] | null): EpicParamState<T>
  ```

- [ ] **Step 1: Write the failing tests**

```ts
// web/src/lib/epicParam.test.ts
import { describe, expect, it } from "vitest";
import { findEpic, withEpic, withoutEpic } from "./epicParam";

const epics = [{ id: "e1", name: "M4: Documents" }, { id: "e2", name: "Launch" }];

describe("epic parameter", () => {
  it("finds an epic by id or by name ignoring case", () => {
    expect(findEpic(epics, "e2")?.name).toBe("Launch");
    expect(findEpic(epics, " m4: documents ")?.id).toBe("e1");
    expect(findEpic(epics, "Nope")).toBeUndefined();
    expect(findEpic(epics, "")).toBeUndefined();
  });

  it("sets the epic and drops any open document, and removes it", () => {
    const next = withEpic(new URLSearchParams("project=ACP&doc=Plan.md"), epics[0]);
    expect(next.get("epic")).toBe("M4: Documents");
    expect(next.has("doc")).toBe(false);
    expect(withoutEpic(new URLSearchParams("project=ACP&epic=Launch&doc=x.md")).toString()).toBe("project=ACP");
  });
});
```

```tsx
// web/src/hooks/useEpicParam.dom.test.tsx
// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { useEpicParam, type EpicParamState } from "./useEpicParam";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

type E = { id: string; name: string };
let root: Root;
let container: HTMLDivElement;
let state: EpicParamState<E>;
const launch: E = { id: "e1", name: "Launch" };

function Harness({ epics }: { epics: E[] | null }) {
  const s = useEpicParam(epics);
  useEffect(() => {
    state = s;
  });
  return null;
}
async function settle() {
  for (let i = 0; i < 30; i++) await new Promise((resolve) => setTimeout(resolve, 5));
}
async function render(epics: E[] | null) {
  await act(async () => {
    root.render(<BrowserRouter><Harness epics={epics} /></BrowserRouter>);
  });
}
async function mount(url: string, epics: E[] | null) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await render(epics);
}
const url = () => window.location.pathname + decodeURIComponent(window.location.search.replace(/\+/g, " "));

beforeEach(() => window.history.replaceState(null, "", "/"));
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe("useEpicParam", () => {
  it("opens an epic as a new entry, Back closes it, and close pops it", async () => {
    await mount("/epics?project=ACP", [launch]);
    await act(async () => state.open(launch));
    expect(url()).toBe("/epics?project=ACP&epic=Launch");
    expect(state.selected?.id).toBe("e1");
    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(state.selected).toBeNull();

    await act(async () => {
      window.history.forward();
      await settle();
    });
    expect(state.selected?.id).toBe("e1");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/epics?project=ACP");
  });

  it("follows a rename once the list has the new name", async () => {
    await mount("/epics?project=ACP&epic=launch", [launch]);
    const renamed = { ...launch, name: "Go live" };
    await render([renamed]);
    await act(async () => state.renamed(renamed));
    await act(settle);
    expect(url()).toBe("/epics?project=ACP&epic=Go live");
    expect(state.selected?.name).toBe("Go live");
  });

  it("drops an epic the list does not have, once it has loaded", async () => {
    await mount("/epics?project=ACP&epic=Nope", null);
    expect(url()).toBe("/epics?project=ACP&epic=Nope");
    await render([launch]);
    await act(settle);
    expect(url()).toBe("/epics?project=ACP");
    expect(state.selected).toBeNull();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/epicParam.test.ts src/hooks/useEpicParam.dom.test.tsx`
Expected: FAIL, cannot resolve modules.

- [ ] **Step 3: Implement**

```ts
// web/src/lib/epicParam.ts
// The open epic modal, held in the Epics view's URL as `epic=<name>`. Epic
// names are unique within a project, and the view shows one project, so the
// name is enough and reads better than an id; an id is accepted too.
import { DOC_PARAM } from "./documents";

export const EPIC_PARAM = "epic";

export function findEpic<T extends { id: string; name: string }>(epics: readonly T[], ref: string): T | undefined {
  const wanted = ref.trim();
  if (!wanted) return undefined;
  const lower = wanted.toLowerCase();
  return epics.find((e) => e.id === wanted) ?? epics.find((e) => e.name.trim().toLowerCase() === lower);
}

export function withEpic(params: URLSearchParams, epic: { name: string }): URLSearchParams {
  const next = new URLSearchParams(params);
  next.set(EPIC_PARAM, epic.name);
  next.delete(DOC_PARAM);
  return next;
}

export function withoutEpic(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(EPIC_PARAM);
  next.delete(DOC_PARAM);
  return next;
}
```

```ts
// web/src/hooks/useEpicParam.ts
import { useCallback, useEffect, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { EPIC_PARAM, findEpic, withEpic, withoutEpic } from "../lib/epicParam";
import { latestSearchParams } from "../lib/latestSearch";
import { useOverlayHistory } from "./useOverlayHistory";

export interface EpicParamState<T> {
  selected: T | null;
  open: (epic: T) => void;
  close: () => void;
  /** Point the URL at an epic's new name, once the list carries it. */
  renamed: (epic: T) => void;
}

/**
 * The epic modal, named in the URL. Opening pushes a history entry, closing
 * goes back past every entry it pushed (documents opened inside included).
 * An epic the loaded list lacks (deleted, or a bad link) drops the parameter.
 */
export function useEpicParam<T extends { id: string; name: string }>(epics: readonly T[] | null): EpicParamState<T> {
  const [params] = useSearchParams();
  const history = useOverlayHistory();
  const ref = params.get(EPIC_PARAM) ?? "";
  const selected = useMemo(() => (epics && ref ? (findEpic(epics, ref) ?? null) : null), [epics, ref]);

  const missing = ref !== "" && epics !== null && selected === null;
  useEffect(() => {
    if (missing) history.replace(withoutEpic(latestSearchParams(params)));
  }, [missing, params, history]);

  const open = useCallback((epic: T) => history.push(withEpic(latestSearchParams(params), epic)), [params, history]);
  const close = useCallback(() => history.closeAll(), [history]);
  const renamed = useCallback(
    (epic: T) => history.replace(withEpic(latestSearchParams(params), epic)),
    [params, history],
  );
  return { selected, open, close, renamed };
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && npx vitest run src/lib/epicParam.test.ts src/hooks/useEpicParam.dom.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/epicParam.ts web/src/lib/epicParam.test.ts web/src/hooks/useEpicParam.ts web/src/hooks/useEpicParam.dom.test.tsx
git commit -m "feat: keep the open epic in the URL

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Epic modal on the Epics page

**Files:**
- Create: `web/src/components/EpicForm.tsx` (the form extracted from `EpicDialog` in `Epics.tsx`)
- Create: `web/src/components/EpicModal.tsx`
- Modify: `web/src/pages/Epics.tsx`
- Test: `web/src/components/EpicModal.test.tsx`, `web/src/pages/Epics.dom.test.tsx`

**Interfaces:**
- Consumes: Tasks 4 and 5, ticket 1–3 components.
- Produces:
  ```tsx
  <EpicForm epic?={Epic} epics onCancel onSave={(data: { name: string; description: string }) => Promise<void>} />
  <EpicModal epic epics projectPrefix onClose onSaved={(epic: Epic) => void | Promise<void>} />
  ```

- [ ] **Step 1: Write the failing tests**

```tsx
// web/src/components/EpicModal.test.tsx
// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { DocumentMeta, Epic } from "../api/client";

const mockApi = vi.hoisted(() => ({
  epics: { update: vi.fn() },
  documents: {
    list: vi.fn(), get: vi.fn(), update: vi.fn(), delete: vi.fn(), create: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
    rawUrl: (id: string, rev: number) => `/api/documents/${id}/raw?rev=${rev}`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import EpicModal from "./EpicModal";

const launch = {
  id: "e1", projectId: "p1", name: "Launch", description: "Go live", createdAt: "", updatedAt: "",
  counts: {}, total: 0, complete: false, lastActivityAt: null, documentCount: 1,
} as Epic;
const plan: DocumentMeta = { id: "d1", epicId: "e1", name: "Rollout plan", format: "markdown", size: 6, revision: 1, createdAt: "", updatedAt: "" };

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function setup() {
  const onClose = vi.fn();
  const onSaved = vi.fn();
  render(<EpicModal epic={launch} epics={[launch]} projectPrefix="ACP" onClose={onClose} onSaved={onSaved} />, { wrapper: MemoryRouter });
  return { onClose, onSaved };
}

describe("EpicModal", () => {
  it("edits the name and description and lists the epic's documents", async () => {
    mockApi.documents.list.mockResolvedValue([plan]);
    mockApi.epics.update.mockResolvedValue({ ...launch, name: "Go live" });
    const { onSaved } = setup();
    expect(screen.getByRole("dialog", { name: "Edit epic" })).toBeTruthy();
    expect(await screen.findByRole("button", { name: "Rollout plan.md" })).toBeTruthy();
    expect(mockApi.documents.list).toHaveBeenCalledWith({ epicId: "e1" });

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Go live" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(expect.objectContaining({ name: "Go live" })));
  });

  it("opens an epic document over the modal, and Escape closes only the document", async () => {
    mockApi.documents.list.mockResolvedValue([plan]);
    mockApi.documents.get.mockResolvedValue({ ...plan, content: "# Steps" });
    const { onClose } = setup();
    fireEvent.click(await screen.findByRole("button", { name: "Rollout plan.md" }));
    expect(await screen.findByRole("heading", { name: "Steps" })).toBeTruthy();
    expect(screen.getByText("Launch", { selector: "span" })).toBeTruthy();

    fireEvent.keyDown(document.body, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("heading", { name: "Steps" })).toBeNull());
    expect(onClose).not.toHaveBeenCalled();
  });

  it("names the epic in document messages", async () => {
    mockApi.documents.list.mockResolvedValue([plan]);
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "New document" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "rollout PLAN" } });
    fireEvent.submit(screen.getByRole("textbox", { name: "Name" }));
    expect(screen.getByRole("alert").textContent).toBe('This epic already has a document called "Rollout plan.md".');
  });
});
```

(If `getByLabelText("Name")` is ambiguous once the New document dialog exists, scope it with `within(screen.getByRole("dialog", { name: "Edit epic" }))`.)

In `web/src/pages/Epics.dom.test.tsx`:

1. Add `get: vi.fn()` to `mockApi.epics` and a `documents` mock like the one above, with `list` resolving `[]` in `beforeEach`.
2. Update the `openEdit` helper to click the row menu's Edit and wait for `screen.findByRole("dialog", { name: "Edit epic" })`; the rename, duplicate-name and server-error tests then run unchanged against the epic modal.
3. Add:

```tsx
  it("opens the epic modal from the row's paperclip, with the epic in the URL", async () => {
    serves({ ...LIST, epics: LIST.epics.map((e) => (e.name === "Graph" ? { ...e, documentCount: 2 } : e)) });
    await mount();
    const paperclip = screen.getByRole("button", { name: "Documents of Graph (2)" });
    fireEvent.click(paperclip);
    expect(await screen.findByRole("dialog", { name: "Edit epic" })).toBeTruthy();
    expect(decodeURIComponent(window.location.search)).toContain("epic=Graph");

    await act(async () => {
      window.history.back();
      for (let i = 0; i < 30; i++) await new Promise((r) => setTimeout(r, 5));
    });
    expect(screen.queryByRole("dialog", { name: "Edit epic" })).toBeNull();
  });

  it("opens the epic named in the URL on load", async () => {
    serves(LIST);
    await mount("/epics?project=ACP&epic=graph");
    expect(await screen.findByRole("dialog", { name: "Edit epic" })).toBeTruthy();
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Graph");
  });
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/components/EpicModal.test.tsx src/pages/Epics.dom.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Extract `EpicForm`**

Move the `<form>` and its state out of `EpicDialog` in `web/src/pages/Epics.tsx` into `web/src/components/EpicForm.tsx`, unchanged in markup and behaviour (the name/description state, `touched`, `serverError`, `saving`, `nameError`, the submit, the two fields, Cancel and Save/Create), with the props `epic?`, `epics`, `onCancel`, `onSave`. Move the `FIELD` class constant with it. `EpicDialog` becomes:

```tsx
function EpicDialog({ epic, epics, onClose, onSave }: { epic?: Epic; epics: readonly Epic[]; onClose: () => void; onSave: (data: { name: string; description: string }) => Promise<void> }) {
  return (
    <Dialog title={epic ? "Edit epic" : "New epic"} onClose={onClose}>
      <EpicForm epic={epic} epics={epics} onCancel={onClose} onSave={onSave} />
    </Dialog>
  );
}
```

Run `cd web && npx vitest run src/pages/Epics.dom.test.tsx` before going on: every existing test must still pass.

- [ ] **Step 4: Write the modal**

```tsx
// web/src/components/EpicModal.tsx
import { useEffect, useId, useRef, useState } from "react";
import { X } from "lucide-react";
import type { Epic } from "../api/client";
import { api } from "../api/client";
import { useDocParam } from "../hooks/useDocParam";
import { useOwnerDocuments } from "../hooks/useOwnerDocuments";
import { useEscape } from "../lib/escapeStack";
import DocumentModal from "./DocumentModal";
import DocumentsSection from "./DocumentsSection";
import EpicForm from "./EpicForm";

// An epic: its name and description (saved with Save, which closes it, as
// the Edit epic dialog did) and its documents, which act at once, as
// subtasks do in the ticket editor. A document opens over it.
export default function EpicModal({
  epic,
  epics,
  projectPrefix,
  onClose,
  onSaved,
}: {
  epic: Epic;
  epics: readonly Epic[];
  projectPrefix: string;
  onClose: () => void;
  onSaved: (epic: Epic) => void | Promise<void>;
}) {
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const owner = { epicId: epic.id };
  const { documents, failed, reload } = useOwnerDocuments(owner);
  const docParam = useDocParam(documents, "epic");
  const [editOnOpen, setEditOnOpen] = useState<string | null>(null);
  useEscape(onClose);

  useEffect(() => {
    dialogRef.current?.focus();
  }, []);

  return (
    <div className="fixed inset-0 z-50 flex p-3 sm:p-6 lg:p-10">
      <div aria-hidden="true" className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        inert={docParam.selected !== null}
        className="relative mx-auto my-auto flex max-h-full w-full max-w-3xl flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl focus:outline-none"
      >
        <header className="flex shrink-0 items-center gap-3 border-b border-slate-800 px-5 py-3">
          <h2 id={titleId} className="flex-1 text-base font-semibold text-white">Edit epic</h2>
          <span className="font-mono text-xs text-slate-500">{projectPrefix}</span>
          <button type="button" aria-label="Close" onClick={onClose} className="text-slate-500 hover:text-slate-300">
            <X className="h-5 w-5" />
          </button>
        </header>
        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto p-5">
          <EpicForm
            epic={epic}
            epics={epics}
            onCancel={onClose}
            onSave={async (data) => {
              await onSaved(await api.epics.update(epic.id, data));
            }}
          />
          <DocumentsSection
            owner={owner}
            ownerNoun="epic"
            documents={documents}
            failed={failed}
            notice={docParam.notice}
            onDismissNotice={docParam.dismissNotice}
            onOpen={docParam.open}
            onChanged={reload}
            onCreated={async (doc, edit) => {
              await reload();
              if (edit) {
                setEditOnOpen(doc.id);
                docParam.open(doc);
              }
            }}
          />
        </div>
      </div>
      {docParam.selected && (
        <DocumentModal
          key={docParam.selected.id}
          doc={docParam.selected}
          documents={documents ?? []}
          owner={owner}
          ownerLabel={epic.name}
          ownerNoun="epic"
          startEditing={editOnOpen === docParam.selected.id}
          deleted={docParam.deleted}
          closeRequested={docParam.closeRequested}
          onCloseCancelled={docParam.cancelClose}
          onDirtyChange={docParam.onDirtyChange}
          onClose={() => {
            setEditOnOpen(null);
            docParam.close();
          }}
          onRenamed={(doc) => {
            docParam.renamed(doc);
            reload();
          }}
          onDeleted={() => {
            docParam.close();
            reload();
          }}
          onRecreated={async (doc) => {
            await reload();
            docParam.renamed(doc);
          }}
        />
      )}
    </div>
  );
}
```

The document modal's header shows `ownerLabel` in a `span`, which the test reads.

- [ ] **Step 5: Wire the page**

In `web/src/pages/Epics.tsx`:

1. `type Editing = { kind: "new" } | { kind: "delete"; epic: Epic } | null;` (the `"edit"` kind goes).
2. Make `loadEpics` return its promise (`return api.epics.list(project).then(…).catch(…)`), typed `(project: string) => Promise<void>`.
3. Add, after `epics` is computed:

```tsx
  // The open epic modal comes from the URL (`epic=<name>`), so it survives a
  // reload and Back closes it. Only the shown project's loaded list can name
  // one, so a list for another project never opens or drops anything.
  const epicParam = useEpicParam(current?.list ? epics : null);
  // Focus goes back to what opened the modal once it has closed.
  const modalOpenerRef = useRef<HTMLElement | null>(null);
  const modalWasOpen = useRef(false);
  useEffect(() => {
    if (epicParam.selected) {
      modalWasOpen.current = true;
      return;
    }
    if (!modalWasOpen.current) return;
    modalWasOpen.current = false;
    if (modalOpenerRef.current?.isConnected) modalOpenerRef.current.focus();
  }, [epicParam.selected]);
  const openEpic = (epic: Epic, opener: HTMLElement | null) => {
    modalOpenerRef.current = opener;
    epicParam.open(epic);
  };
```

4. `Row` gains a `documents?: React.ReactNode` slot rendered just before the menu slot (outside the `Link`). In `epicRow` pass:

```tsx
      documents={
        <button
          type="button"
          aria-label={`Documents of ${epic.name} (${epic.documentCount ?? 0})`}
          title="Documents"
          onClick={(e) => openEpic(epic, e.currentTarget)}
          className={`inline-flex shrink-0 items-center gap-1 rounded-md border border-slate-800 px-1.5 py-0.5 text-xs hover:bg-slate-800 ${
            (epic.documentCount ?? 0) > 0 ? "text-slate-300" : "text-slate-600"
          }`}
        >
          <Paperclip aria-hidden="true" className="h-3 w-3" />
          {epic.documentCount ?? 0}
        </button>
      }
```

   and change the menu's `onEdit` to `(opener) => openEpic(epic, opener)`. Add `Paperclip` to the lucide import.
5. Remove the `editing?.kind === "edit"` block and render instead:

```tsx
      {epicParam.selected && shownProject && (
        <EpicModal
          key={epicParam.selected.id}
          epic={epicParam.selected}
          epics={epics}
          projectPrefix={shownProject}
          onClose={epicParam.close}
          onSaved={async (updated) => {
            await loadEpics(shownProject);
            epicParam.renamed(updated);
            epicParam.close();
          }}
        />
      )}
```

   (Save closes the modal, as the Edit epic dialog did; renaming first means the entry being closed names the new name.)
6. Add the `Row` prop type `documents?: React.ReactNode` and render `{documents}` before `<div className="w-6 shrink-0">{menu}</div>`.

- [ ] **Step 6: Run the web suite, lint and types**

Run: `cd web && npm test && npm run lint && npx tsc -b`
Expected: PASS. The old focus test "gives focus back to the row's ⋯ when Edit closes" passes through `modalOpenerRef`.

- [ ] **Step 7: Manual check on a throwaway board**

```bash
mkdir -p .tmp && rm -f .tmp/t4.db
go run ./cmd/taskboard --db ./.tmp/t4.db project create Demo --prefix DEMO
go run ./cmd/taskboard --db ./.tmp/t4.db epic create DEMO "M4: Documents"
printf '# Rollout\n\n1. Ship\n' | go run ./cmd/taskboard --db ./.tmp/t4.db doc add --epic "M4: Documents" --project DEMO --name "Rollout plan" --file -
make frontend && make dev DEV_DB=./.tmp/t4.db DEV_PORT=3011
```

On `http://localhost:3011/epics?project=DEMO`: the row shows a paperclip with 1; clicking it opens "Edit epic" with `epic=M4: Documents` in the URL; the document opens over it with `&doc=Rollout plan.md`; Back closes the document, Back closes the epic; pasting the printed link opens both; New creates an epic document; renaming the epic and saving closes the modal and the row shows the new name. Stop the server with `kill $(lsof -t -- ./.tmp/t4.db)`.

- [ ] **Step 8: Commit**

```bash
git add web/src/components/EpicForm.tsx web/src/components/EpicModal.tsx web/src/components/EpicModal.test.tsx web/src/pages/Epics.tsx web/src/pages/Epics.dom.test.tsx
git commit -m "feat: add the epic modal with documents and its own URL

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
