# Ticket 2: Users edit documents — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Users create (New, Upload) and edit markdown documents in the web UI, with Write/Preview, Save/Cancel, a discard question before unsaved text is lost, and a conflict notice when someone else saves or deletes the document mid-edit.

**Architecture:** Content saves from the web send the revision they started from; the store refuses a stale one with a conflict error that carries the current document, which HTTP turns into `409`. The modal gains an edit mode and reports unsaved edits to `useDocParam`, which holds the document on screen when Back drops it or it is deleted, the same way `useTicketParam` holds a dirty ticket.

**Tech Stack:** Go, React 19, React Router 7, react-markdown, Vitest + jsdom.

**Spec:** `docs/superpowers/specs/2026-09-25-documents-design.md`, section "Ticket 2" and "Live updates".

**Depends on:** tickets 0 and 1.

## Global constraints

See [README.md](README.md#global-constraints). Only markdown documents are editable; upload accepts `.md` in this ticket. MCP and CLI saves stay unconditional (no revision check): agents overwrite.

## Review Focus

- A save that arrives after someone else's save must never overwrite silently: the server refuses a stale revision with 409, and the notice appears. Pinned in Tasks 1 and 5.
- Back with unsaved text asks first and, if the user keeps editing, the document comes back as a history entry. Pinned in Task 3.
- A document deleted mid-edit keeps the text on screen and offers "Save as a new document"; the new document takes the old name. Pinned in Tasks 3 and 5.
- An upload over 8 MB is refused before it is read, with the size in the message. Pinned in Task 4.
- A new document's name is checked before it is created, and a name taken meanwhile shows the server's message. Pinned in Task 4.

---

### Task 1: Store refuses stale content saves

**Files:**
- Modify: `internal/models/document.go` (`UpdateDocumentRequest.ExpectedRevision`)
- Modify: `internal/db/documents.go` (`ErrDocumentConflict`, `UpdateDocument`)
- Test: `internal/db/documents_test.go`

**Interfaces:**
- Produces:
  ```go
  // models
  type UpdateDocumentRequest struct { Name, Content *string; ExpectedRevision *int `json:"expectedRevision,omitempty"` }
  // db
  type ErrDocumentConflict struct{ Current *models.Document }
  func (e *ErrDocumentConflict) Error() string // "This document changed since you started editing."
  ```

- [ ] **Step 1: Write the failing test**

```go
func TestUpdateDocumentRefusesAStaleRevision(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	d := seedDocument(t, s, tk.ID, "Plan", "v1")

	theirs := "theirs"
	if _, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &theirs}); err != nil {
		t.Fatal(err)
	}

	mine := "mine"
	stale := 1
	_, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &mine, ExpectedRevision: &stale})
	var conflict *ErrDocumentConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ErrDocumentConflict", err)
	}
	if conflict.Current.Content != "theirs" || conflict.Current.Revision != 2 {
		t.Fatalf("current = %+v", conflict.Current)
	}

	current := 2
	saved, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &mine, ExpectedRevision: &current})
	if err != nil || saved.Content != "mine" || saved.Revision != 3 {
		t.Fatalf("save at the current revision = %+v, %v", saved, err)
	}

	// A rename never conflicts: it does not change the content.
	name := "Plan v2"
	if _, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Name: &name, ExpectedRevision: &stale}); err != nil {
		t.Fatalf("rename with a stale revision: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/db -run TestUpdateDocumentRefusesAStaleRevision -v`
Expected: FAIL, `unknown field ExpectedRevision`.

- [ ] **Step 3: Implement**

In `internal/models/document.go`, add to `UpdateDocumentRequest`:

```go
	// ExpectedRevision, when set with Content, is the revision the caller
	// started editing from. If the document has moved past it, the save is
	// refused rather than overwriting someone else's. The web UI sends it;
	// agents do not.
	ExpectedRevision *int `json:"expectedRevision,omitempty"`
```

In `internal/db/documents.go`, add:

```go
// ErrDocumentConflict refuses a content save made from a revision the
// document has moved past. Current is the document as it is now, so the
// caller can show it.
type ErrDocumentConflict struct {
	Current *models.Document
}

func (e *ErrDocumentConflict) Error() string {
	return "This document changed since you started editing."
}
```

In `UpdateDocument`, read the revision with the owner and name:

```go
	var owner DocumentOwner
	var name string
	var revision int
	err = tx.QueryRow("SELECT COALESCE(ticket_id, ''), name, revision FROM documents WHERE id = ?", id).
		Scan(&owner.TicketID, &name, &revision)
```

and right after the `err != nil` checks add:

```go
	if req.Content != nil && req.ExpectedRevision != nil && *req.ExpectedRevision != revision {
		tx.Rollback()
		current, err := s.GetDocument(id)
		if err != nil {
			return nil, err
		}
		return nil, &ErrDocumentConflict{Current: current}
	}
```

(The explicit `tx.Rollback()` releases the write lock before `GetDocument` reads on another connection; the deferred rollback is then a no-op.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/db -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/models/document.go internal/db/documents.go internal/db/documents_test.go
git commit -m "feat: refuse a document save made from a stale revision

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: HTTP create, and 409 on conflict

**Files:**
- Modify: `internal/server/documents.go`
- Modify: `internal/server/server.go` (one route)
- Test: `internal/server/documents_test.go`

**Interfaces:**
- Produces:
  - `POST /api/documents` body `CreateDocumentRequest` → `201 Document`; rule failures and unknown ticket → `400 {"error"}`.
  - `PUT /api/documents/{ref}` with a stale `expectedRevision` → `409 {"error": "...", "current": Document}`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCreateDocumentOverHTTP(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)

	created, status := doRequest[models.Document](t, http.MethodPost, r.url+"/api/documents",
		`{"ticketId":"`+tk.ID+`","name":"Notes","format":"markdown","content":""}`)
	if status != http.StatusCreated || created.Name != "Notes" || created.Revision != 1 {
		t.Fatalf("create: %d %+v", status, created)
	}
	body, status := errorBody(t, http.MethodPost, r.url+"/api/documents",
		`{"ticketId":"`+tk.ID+`","name":"design SPEC","content":""}`)
	if status != http.StatusBadRequest || body.Error != `This ticket already has a document called "Design spec.md".` {
		t.Fatalf("taken name: %d %q", status, body.Error)
	}
	_, status = errorBody(t, http.MethodPost, r.url+"/api/documents", `{"ticketId":"nope","name":"X"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("unknown ticket status = %d", status)
	}
}

func TestStaleSaveIsAConflict(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	_, status := doRequest[models.Document](t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"content":"theirs"}`)
	if status != http.StatusOK {
		t.Fatalf("agent save status = %d", status)
	}
	type conflict struct {
		Error   string          `json:"error"`
		Current models.Document `json:"current"`
	}
	got, status := doRequest[conflict](t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"content":"mine","expectedRevision":1}`)
	if status != http.StatusConflict || got.Current.Content != "theirs" || got.Current.Revision != 2 || got.Error == "" {
		t.Fatalf("stale save: %d %+v", status, got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/server -run 'TestCreateDocumentOverHTTP|TestStaleSaveIsAConflict' -v`
Expected: FAIL (405 for POST; 200 instead of 409).

- [ ] **Step 3: Implement**

In `internal/server/documents.go` add:

```go
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
```

In `updateDocument`, replace the `if err != nil { writeStoreError(w, err); return }` after `UpdateDocument` with:

```go
	if err != nil {
		var conflict *db.ErrDocumentConflict
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "current": conflict.Current})
			return
		}
		writeStoreError(w, err)
		return
	}
```

In `setupRoutes`, inside `r.Route("/documents", …)` add `r.Post("/", s.createDocument)`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/server -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/documents.go internal/server/server.go internal/server/documents_test.go
git commit -m "feat: create documents over HTTP and answer stale saves with 409

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Client calls, filename conversion, and `useDocParam` holds unsaved edits

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/lib/documents.ts` (+ `documents.test.ts`)
- Modify: `web/src/hooks/useOwnerDocuments.ts` (`reload` returns a promise)
- Modify: `web/src/hooks/useDocParam.ts` (+ `useDocParam.dom.test.tsx`)

**Interfaces:**
- Produces:
  ```ts
  api.documents.create(data: { ticketId: string; name: string; format: DocumentFormat; content: string }): Promise<DocumentWithContent>
  api.documents.update(id, data: { name?: string; content?: string; expectedRevision?: number }): Promise<DocumentWithContent>
  nameFromFilename(filename: string): { name: string; format: DocumentFormat } | { error: string }
  conflictDocument(error: unknown): DocumentWithContent | null   // the `current` of a 409
  isNotFound(error: unknown): boolean                             // a 404
  contentTooLarge(content: string): string | null                 // the size message, or null
  useOwnerDocuments(...).reload: () => Promise<void>
  // DocParamState gains:
  deleted: boolean; closeRequested: boolean; cancelClose: () => void; onDirtyChange: (dirty: boolean) => void;
  // and `renamed` also clears the dirty flag (used after "Save as a new document").
  ```

- [ ] **Step 1: Write the failing tests**

Append to `web/src/lib/documents.test.ts` (extend the import list):

```ts
describe("nameFromFilename", () => {
  it("drops the extension and turns other symbols into spaces", () => {
    expect(nameFromFilename("api-design_v2.md")).toEqual({ name: "api-design_v2", format: "markdown" });
    expect(nameFromFilename("notes v1.2.MD")).toEqual({ name: "notes v1 2", format: "markdown" });
  });

  it("refuses other types and names that clean to nothing", () => {
    expect(nameFromFilename("notes.txt")).toEqual({ error: "Only .md files can be attached." });
    expect(nameFromFilename("README")).toEqual({ error: "Only .md files can be attached." });
    expect(nameFromFilename("%%.md")).toEqual({ error: "Enter a name" });
  });
});

describe("server errors", () => {
  const current = { id: "d1", name: "Plan", format: "markdown", size: 6, revision: 2, createdAt: "", updatedAt: "", content: "theirs" };

  it("reads the current document out of a 409", () => {
    const err = new Error(`API error 409: ${JSON.stringify({ error: "changed", current })}`);
    expect(conflictDocument(err)).toEqual(current);
    expect(conflictDocument(new Error('API error 400: {"error":"x"}'))).toBeNull();
    expect(conflictDocument("nope")).toBeNull();
  });

  it("recognises a 404", () => {
    expect(isNotFound(new Error('API error 404: {"error":"document not found"}'))).toBe(true);
    expect(isNotFound(new Error("API error 500: x"))).toBe(false);
  });
});

describe("contentTooLarge", () => {
  it("measures UTF-8 bytes against the limit", () => {
    expect(contentTooLarge("x".repeat(8 * 1024 * 1024))).toBeNull();
    expect(contentTooLarge("x".repeat(8 * 1024 * 1024 + 1))).toBe("This document is 8.1 MB. The limit is 8 MB.");
  });
});
```

Append to `web/src/hooks/useDocParam.dom.test.tsx` (the Harness gains a way to report dirty; `state.onDirtyChange` is on the returned state):

```tsx
  it("keeps a document with unsaved edits on screen when Back drops it, and asks", async () => {
    await mount("/?ticket=ACP-7", [spec]);
    await act(async () => state.open(spec));
    await act(async () => state.onDirtyChange(true));
    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/?ticket=ACP-7");
    expect(state.selected?.id).toBe("d1");
    expect(state.closeRequested).toBe(true);

    await act(async () => state.cancelClose());
    expect(url()).toBe("/?ticket=ACP-7&doc=Design spec.md");
    expect(state.closeRequested).toBe(false);

    await act(async () => {
      window.history.back();
      await settle();
    });
    await act(async () => state.close());
    expect(state.selected).toBeNull();
    expect(url()).toBe("/?ticket=ACP-7");
  });

  it("keeps a document with unsaved edits on screen when it is deleted, and says so", async () => {
    await mount("/?ticket=ACP-7", [spec, notes]);
    await act(async () => state.open(spec));
    await act(async () => state.onDirtyChange(true));
    await render([notes]);
    await act(settle);
    expect(state.selected?.id).toBe("d1");
    expect(state.deleted).toBe(true);
    expect(state.notice).toBeNull();
    expect(url()).toBe("/?ticket=ACP-7&doc=Design spec.md");

    // "Save as a new document": the editor reloads the list first, and the
    // new one, under the same name, is what the URL now names.
    const recreated = { ...spec, id: "d3" };
    await render([notes, recreated]);
    await act(async () => state.renamed(recreated));
    await act(settle);
    expect(state.selected?.id).toBe("d3");
    expect(state.deleted).toBe(false);
    expect(state.notice).toBeNull();
  });
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/documents.test.ts src/hooks/useDocParam.dom.test.tsx`
Expected: FAIL (missing exports and state fields).

- [ ] **Step 3: Client calls**

In `web/src/api/client.ts`, change `api.documents.update`'s data type to `{ name?: string; content?: string; expectedRevision?: number }` and add:

```ts
    create: (data: { ticketId: string; name: string; format: DocumentFormat; content: string }) =>
      request<DocumentWithContent>("/api/documents", { method: "POST", body: JSON.stringify(data) }),
```

- [ ] **Step 4: Helpers**

Append to `web/src/lib/documents.ts`:

```ts
const FORMAT_BY_EXTENSION: Record<string, DocumentFormat> = { ".md": "markdown" };
const EXTENSION_MESSAGE = "Only .md files can be attached.";

/**
 * A document name and format from a file's name, as the server makes them
 * (DocumentNameFromFilename): the extension picks the format and goes, and
 * every character the name rules refuse becomes a space.
 */
export function nameFromFilename(filename: string): { name: string; format: DocumentFormat } | { error: string } {
  const base = filename.split(/[\\/]/).pop() ?? "";
  const dot = base.lastIndexOf(".");
  const format = dot > 0 ? FORMAT_BY_EXTENSION[base.slice(dot).toLowerCase()] : undefined;
  if (!format) return { error: EXTENSION_MESSAGE };
  const name = [...base.slice(0, dot)].map((c) => (NAME_CHAR.test(c) ? c : " ")).join("").trim();
  const error = documentNameError(name);
  return error ? { error } : { name, format };
}

const API_ERROR = /^API error (\d{3}): ?([\s\S]*)$/;

function apiError(error: unknown): { status: string; body: string } | null {
  const raw = error instanceof Error ? error.message : "";
  const match = API_ERROR.exec(raw);
  return match ? { status: match[1], body: match[2] } : null;
}

/** The document as it is now, from a 409 refusing a stale save. */
export function conflictDocument(error: unknown): DocumentWithContent | null {
  const api = apiError(error);
  if (api?.status !== "409") return null;
  try {
    const current = (JSON.parse(api.body) as { current?: DocumentWithContent }).current;
    return current && typeof current.id === "string" ? current : null;
  } catch {
    return null;
  }
}

export function isNotFound(error: unknown): boolean {
  return apiError(error)?.status === "404";
}

/** The server's size message for content over the limit, or null. */
export function contentTooLarge(content: string): string | null {
  const bytes = new TextEncoder().encode(content).length;
  return bytes > MAX_DOCUMENT_BYTES ? `This document is ${formatSize(bytes)}. The limit is 8 MB.` : null;
}
```

and add `DocumentWithContent` to the file's type import.

- [ ] **Step 5: `reload` returns a promise**

In `web/src/hooks/useOwnerDocuments.ts`, make `reload` return the chain so a caller can wait for the list to include a document it just created:

```ts
  const reload = useCallback((): Promise<void> => {
    const n = ++seq.current;
    return Promise.resolve()
      .then(() => api.documents.list({ ticketId }))
      .then((docs) => {
        if (n !== seq.current) return;
        setLoaded({ key, documents: Array.isArray(docs) ? docs : [] });
        setFailedKey(null);
      })
      .catch(() => {
        if (n === seq.current) setFailedKey(key);
      });
  }, [key, ticketId]);
```

- [ ] **Step 6: `useDocParam` holds unsaved edits**

Replace the body of `useDocParam` in `web/src/hooks/useDocParam.ts` (keep the imports; add `useCallback`/`useState` if missing) and extend the interface:

```ts
export interface DocParamState {
  selected: DocumentMeta | null;
  /** The open document left the list while it had unsaved edits: it was deleted. */
  deleted: boolean;
  /** The URL stopped naming the open document while it has unsaved edits. */
  closeRequested: boolean;
  notice: string | null;
  dismissNotice: () => void;
  open: (doc: DocumentMeta) => void;
  close: () => void;
  /** Put the document back after a Back that the user cancelled. */
  cancelClose: () => void;
  /** Point the URL at a document (after a rename, or at a recreated copy). */
  renamed: (doc: DocumentMeta) => void;
  /** Told by the modal whether it holds unsaved edits. */
  onDirtyChange: (dirty: boolean) => void;
}

export function useDocParam(documents: readonly DocumentMeta[] | null, ownerNoun = "ticket"): DocParamState {
  const [params] = useSearchParams();
  const history = useOverlayHistory();
  const ref = params.get(DOC_PARAM) ?? "";
  const found = documents && ref ? (findDocument(documents, ref) ?? null) : null;
  const [dirty, setDirty] = useState(false);

  // The document last open, by id: followed through a rename elsewhere, and
  // told apart from a link that never named anything.
  const [shownId, setShownId] = useState<string | null>(null);
  if (found && shownId !== found.id) setShownId(found.id);
  if (!ref && shownId !== null && !dirty) setShownId(null);
  const byId = shownId && documents ? (documents.find((d) => d.id === shownId) ?? null) : null;
  const followed = !found && ref ? byId : null;
  const live = found ?? followed;

  // The copy shown while there are unsaved edits and the URL has dropped the
  // document (Back) or the document has left the list (deleted).
  const [held, setHeld] = useState<DocumentMeta | null>(null);
  if (live && held !== live) setHeld(live);
  const selected = live ?? (dirty ? held : null);
  const deleted =
    !live && dirty && held !== null && documents !== null && !documents.some((d) => d.id === held.id);
  const closeRequested = !ref && dirty && held !== null;

  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (followed) history.replace(withDoc(latestSearchParams(params), followed));
  }, [followed, params, history]);

  const missing = ref !== "" && documents !== null && !found && !followed && !dirty;
  useEffect(() => {
    if (!missing) return;
    setNotice(shownId ? `${ref} was deleted.` : `Couldn't find ${ref} on this ${ownerNoun}.`);
    setShownId(null);
    history.replace(withoutDoc(latestSearchParams(params)));
  }, [missing, shownId, ref, ownerNoun, params, history]);

  const open = useCallback(
    (doc: DocumentMeta) => {
      setNotice(null);
      setDirty(false);
      history.push(withDoc(latestSearchParams(params), doc));
    },
    [params, history],
  );

  const close = useCallback(() => {
    setDirty(false);
    const latest = latestSearchParams(params);
    // A Back already dropped the parameter: nothing to undo.
    if (!latest.get(DOC_PARAM)) {
      setHeld(null);
      return;
    }
    history.closeOne(withoutDoc(latest));
  }, [params, history]);

  const cancelClose = useCallback(() => {
    if (held) history.push(withDoc(latestSearchParams(params), held));
  }, [held, params, history]);

  const renamed = useCallback(
    (doc: DocumentMeta) => {
      setDirty(false);
      setShownId(doc.id);
      history.replace(withDoc(latestSearchParams(params), doc));
    },
    [params, history],
  );

  const dismissNotice = useCallback(() => setNotice(null), []);

  return {
    selected,
    deleted,
    closeRequested,
    notice,
    dismissNotice,
    open,
    close,
    cancelClose,
    renamed,
    onDirtyChange: setDirty,
  };
}
```

- [ ] **Step 7: Run the tests**

Run: `cd web && npx vitest run src/lib/documents.test.ts src/hooks/useDocParam.dom.test.tsx`
Expected: PASS, including every ticket 1 case.

- [ ] **Step 8: Commit**

```bash
git add web/src/api/client.ts web/src/lib/documents.ts web/src/lib/documents.test.ts web/src/hooks/useOwnerDocuments.ts web/src/hooks/useDocParam.ts web/src/hooks/useDocParam.dom.test.tsx
git commit -m "feat: hold a document with unsaved edits through Back and deletion

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: New and Upload in the Documents section

**Files:**
- Create: `web/src/components/NewDocumentDialog.tsx`
- Modify: `web/src/components/DocumentsSection.tsx`
- Test: `web/src/components/DocumentsSection.test.tsx`

**Interfaces:**
- Consumes: Task 3 (`api.documents.create`, `nameFromFilename`, `MAX_DOCUMENT_BYTES`, `formatSize`).
- Produces: `DocumentsSection` gains props `owner: DocumentOwnerRef` and `onCreated: (doc: DocumentMeta, edit: boolean) => void`. New creates an empty document and reports `edit = true`; Upload reports `edit = false`.

- [ ] **Step 1: Write the failing tests**

Add `create: vi.fn()` to `mockApi.documents` in `DocumentsSection.test.tsx`, pass `owner={{ ticketId: "t1" }}` and `onCreated={onCreated}` from `setup` (return `onCreated`), then add:

```tsx
  it("creates a new document by name and asks to open it for editing", async () => {
    const created = { ...spec, id: "d9", name: "Rollout plan", content: "" };
    mockApi.documents.create.mockResolvedValue(created);
    const { onCreated } = setup();
    fireEvent.click(screen.getByRole("button", { name: "New document" }));
    const input = screen.getByRole("textbox", { name: "Name" });
    fireEvent.change(input, { target: { value: "Design spec" } });
    fireEvent.submit(input);
    expect(screen.getByRole("alert").textContent).toBe('This ticket already has a document called "Design spec.md".');

    fireEvent.change(input, { target: { value: " Rollout plan " } });
    fireEvent.submit(input);
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(created, true));
    expect(mockApi.documents.create).toHaveBeenCalledWith({ ticketId: "t1", name: "Rollout plan", format: "markdown", content: "" });
  });

  it("uploads a .md file under a name made from its filename", async () => {
    const created = { ...spec, id: "d9", name: "api-design_v1 2", content: "# x" };
    mockApi.documents.create.mockResolvedValue(created);
    const { onCreated } = setup();
    const file = new File(["# x"], "api-design_v1.2.md", { type: "text/markdown" });
    fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [file] } });
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(created, false));
    expect(mockApi.documents.create).toHaveBeenCalledWith({ ticketId: "t1", name: "api-design_v1 2", format: "markdown", content: "# x" });
  });

  it("refuses an upload of another type, or over 8 MB, without asking the server", async () => {
    setup();
    fireEvent.change(screen.getByLabelText("Upload a document"), {
      target: { files: [new File(["x"], "notes.txt")] },
    });
    expect(await screen.findByText("Only .md files can be attached.")).toBeTruthy();

    const big = new File(["x"], "big.md");
    Object.defineProperty(big, "size", { value: 8 * 1024 * 1024 + 1 });
    fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [big] } });
    expect(await screen.findByText("This document is 8.1 MB. The limit is 8 MB.")).toBeTruthy();
    expect(mockApi.documents.create).not.toHaveBeenCalled();
  });

  it("shows the server's reason when an upload is refused", async () => {
    mockApi.documents.create.mockRejectedValue(new Error('API error 400: {"error":"This ticket already has a document called \\"Notes.md\\"."}'));
    setup();
    fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [new File(["x"], "notes.md")] } });
    expect(await screen.findByText('This ticket already has a document called "Notes.md".')).toBeTruthy();
  });
```

Update the empty-state test's expected text to `"No documents yet."` (users can now add one; the line no longer says only agents can).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/components/DocumentsSection.test.tsx`
Expected: FAIL, no "New document" button.

- [ ] **Step 3: Write the new-document dialog**

```tsx
// web/src/components/NewDocumentDialog.tsx
import { useId, useState } from "react";
import { api, type DocumentMeta, type DocumentOwnerRef } from "../api/client";
import { documentNameError } from "../lib/documents";
import { serverMessage } from "../lib/epics";
import { useEscape } from "../lib/escapeStack";

// Asks for a new markdown document's name and creates it empty. The rules
// are checked here first, in the server's words.
export default function NewDocumentDialog({
  owner,
  documents,
  ownerNoun = "ticket",
  onCancel,
  onCreated,
}: {
  owner: DocumentOwnerRef;
  documents: readonly DocumentMeta[];
  ownerNoun?: string;
  onCancel: () => void;
  onCreated: (doc: DocumentMeta) => void;
}) {
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const ids = useId();
  useEscape(onCancel);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const local = documentNameError(name, documents, undefined, ownerNoun);
    if (local) {
      setError(local);
      return;
    }
    setSaving(true);
    try {
      onCreated(await api.documents.create({ ...owner, name: name.trim(), format: "markdown", content: "" }));
    } catch (err) {
      setError(serverMessage(err, "The document was not created."));
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center p-4">
      <div aria-hidden="true" className="absolute inset-0 bg-slate-950/60" onClick={onCancel} />
      <form
        role="dialog"
        aria-modal="true"
        aria-labelledby={`${ids}-title`}
        onSubmit={submit}
        noValidate
        className="relative w-full max-w-sm space-y-4 rounded-xl border border-slate-700 bg-slate-900 p-5 shadow-2xl"
      >
        <h3 id={`${ids}-title`} className="text-base font-semibold text-white">New document</h3>
        <div>
          <label htmlFor={`${ids}-name`} className="mb-1.5 block text-xs font-medium text-slate-400">Name</label>
          <div className="flex items-center gap-2">
            <input
              id={`${ids}-name`}
              autoFocus
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setError(null);
              }}
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? `${ids}-error` : undefined}
              placeholder="Rollout plan"
              className="min-w-0 flex-1 rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
            <span className="font-mono text-xs text-slate-500">.md</span>
          </div>
          {error && (
            <p id={`${ids}-error`} role="alert" className="mt-1.5 text-xs text-red-400">{error}</p>
          )}
        </div>
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onCancel} className="px-3 py-1.5 text-sm text-slate-400 hover:text-white">Cancel</button>
          <button type="submit" disabled={saving} className="rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-60">Create</button>
        </div>
      </form>
    </div>
  );
}
```

- [ ] **Step 4: Add New and Upload to the section**

In `web/src/components/DocumentsSection.tsx`:

1. Imports: add `useRef` from react; `Plus`, `Upload` from lucide; `type DocumentOwnerRef` from the client; `MAX_DOCUMENT_BYTES`, `nameFromFilename` from `../lib/documents`; `serverMessage` from `../lib/epics`; `NewDocumentDialog` from `./NewDocumentDialog`.
2. Props: add `owner: DocumentOwnerRef` and `onCreated: (doc: DocumentMeta, edit: boolean) => void`.
3. State and the upload handler, at the top of the component:

```tsx
  const [creating, setCreating] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const upload = async (file: File) => {
    setUploadError(null);
    const parsed = nameFromFilename(file.name);
    if ("error" in parsed) {
      setUploadError(parsed.error);
      return;
    }
    if (file.size > MAX_DOCUMENT_BYTES) {
      setUploadError(`This document is ${formatSize(file.size)}. The limit is 8 MB.`);
      return;
    }
    try {
      const content = await file.text();
      onCreated(await api.documents.create({ ...owner, name: parsed.name, format: parsed.format, content }), false);
    } catch (err) {
      setUploadError(serverMessage(err, "The document was not uploaded."));
    }
  };
```

4. Replace the heading with a header row:

```tsx
      <div className="mb-2.5 flex items-center gap-2">
        <h3 className={`${SECTION_HEADING} mb-0 flex-1`}>Documents</h3>
        <button type="button" onClick={() => setCreating(true)} aria-label="New document"
          className="inline-flex items-center gap-1 rounded-md border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800">
          <Plus className="h-3 w-3" /> New
        </button>
        <button type="button" onClick={() => fileRef.current?.click()}
          className="inline-flex items-center gap-1 rounded-md border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800">
          <Upload className="h-3 w-3" /> Upload
        </button>
        <input
          ref={fileRef}
          type="file"
          accept=".md"
          aria-label="Upload a document"
          className="sr-only"
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            if (file) void upload(file);
          }}
        />
      </div>
      {uploadError && <p role="alert" className="mb-2 text-xs text-red-400">{uploadError}</p>}
```

5. Change the empty-state text to `No documents yet.`
6. After the delete confirm, add:

```tsx
      {creating && (
        <NewDocumentDialog
          owner={owner}
          documents={documents ?? []}
          ownerNoun={ownerNoun}
          onCancel={() => setCreating(false)}
          onCreated={(doc) => {
            setCreating(false);
            onCreated(doc, true);
          }}
        />
      )}
```

- [ ] **Step 5: Run the tests**

Run: `cd web && npx vitest run src/components/DocumentsSection.test.tsx`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/NewDocumentDialog.tsx web/src/components/DocumentsSection.tsx web/src/components/DocumentsSection.test.tsx
git commit -m "feat: let users create and upload documents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Edit mode, discard question and conflict notice in the modal

**Files:**
- Modify: `web/src/components/DocumentModal.tsx` (full replacement below)
- Test: `web/src/components/DocumentModal.test.tsx`

**Interfaces:**
- Consumes: Task 3 helpers.
- Produces: `DocumentModal` props become:
  ```ts
  {
    doc: DocumentMeta; documents: readonly DocumentMeta[]; owner: DocumentOwnerRef; ownerLabel: string; ownerNoun?: string;
    startEditing?: boolean;   // open straight into edit mode (a new document)
    deleted?: boolean;        // from useDocParam: gone while holding unsaved edits
    closeRequested?: boolean; // from useDocParam: Back dropped it while holding unsaved edits
    onClose: () => void; onCloseCancelled?: () => void; onDirtyChange?: (dirty: boolean) => void;
    onRenamed: (doc: DocumentMeta) => void; onDeleted: () => void;
    onRecreated: (doc: DocumentMeta) => void; // "Save as a new document" created this
  }
  ```

- [ ] **Step 1: Write the failing tests**

In `DocumentModal.test.tsx`, add `create: vi.fn()` to the mock, pass `owner={{ ticketId: "t1" }}` and `onRecreated` through `setup` (and in the `rerender` call), then add:

```tsx
describe("editing", () => {
  const load = (content: string, revision = 1) =>
    mockApi.documents.get.mockResolvedValue({ ...spec, revision, content });

  async function startEditing() {
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    return screen.findByRole("textbox", { name: "Document content" });
  }

  it("edits, previews and saves from the revision it started at", async () => {
    load("# Old");
    mockApi.documents.update.mockResolvedValue({ ...spec, revision: 2, content: "# New" });
    setup();
    const box = await startEditing();
    fireEvent.change(box, { target: { value: "# New" } });
    expect(screen.getByText("unsaved")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(screen.getByRole("heading", { name: "New" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull());
    expect(mockApi.documents.update).toHaveBeenCalledWith("d1", { content: "# New", expectedRevision: 1 });
    expect(screen.getByRole("heading", { name: "New" })).toBeTruthy();
  });

  it("asks before Cancel or Escape throws away unsaved text", async () => {
    load("old");
    const { onClose } = setup();
    const box = await startEditing();
    fireEvent.change(box, { target: { value: "mine" } });

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.getByRole("alertdialog", { name: "Discard your changes?" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect((screen.getByRole("textbox", { name: "Document content" }) as HTMLTextAreaElement).value).toBe("mine");

    fireEvent.keyDown(document.body, { key: "Escape" });
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onClose).toHaveBeenCalled();
  });

  it("shows the notice when someone else saves mid-edit, and can load theirs", async () => {
    load("old");
    const props = setup();
    const box = await startEditing();
    fireEvent.change(box, { target: { value: "mine" } });

    mockApi.documents.get.mockResolvedValue({ ...spec, revision: 2, content: "theirs" });
    const next = { ...spec, revision: 2 };
    props.rerender(
      <DocumentModal doc={next} documents={[next]} owner={{ ticketId: "t1" }} ownerLabel="ACP-84"
        onClose={props.onClose} onRenamed={props.onRenamed} onDeleted={props.onDeleted} onRecreated={props.onRecreated} />,
    );
    const notice = await screen.findByRole("status");
    expect(notice.textContent).toContain("This document changed while you were editing.");
    expect((screen.getByRole("textbox", { name: "Document content" }) as HTMLTextAreaElement).value).toBe("mine");

    fireEvent.click(screen.getByRole("button", { name: "Discard mine, load theirs" }));
    expect(await screen.findByText("theirs")).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull();
  });

  it("overwrites on save after 'Keep editing', sending the newer revision", async () => {
    load("old");
    mockApi.documents.update
      .mockRejectedValueOnce(new Error(`API error 409: ${JSON.stringify({ error: "changed", current: { ...spec, revision: 2, content: "theirs" } })}`))
      .mockResolvedValueOnce({ ...spec, revision: 3, content: "mine" });
    setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("status")).textContent).toContain("This document changed while you were editing.");

    fireEvent.click(screen.getByRole("button", { name: "Keep editing, overwrite on save" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mockApi.documents.update).toHaveBeenLastCalledWith("d1", { content: "mine", expectedRevision: 2 }));
  });

  it("offers to save a deleted document as a new one", async () => {
    load("old");
    const recreated = { ...spec, id: "d3", content: "mine" };
    mockApi.documents.create.mockResolvedValue(recreated);
    const props = setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    props.rerender(
      <DocumentModal doc={spec} documents={[]} owner={{ ticketId: "t1" }} ownerLabel="ACP-84" deleted
        onClose={props.onClose} onRenamed={props.onRenamed} onDeleted={props.onDeleted} onRecreated={props.onRecreated} />,
    );
    expect((await screen.findByRole("status")).textContent).toContain("This document was deleted while you were editing.");
    fireEvent.click(screen.getByRole("button", { name: "Save as a new document" }));
    await waitFor(() => expect(props.onRecreated).toHaveBeenCalledWith(recreated));
    expect(mockApi.documents.create).toHaveBeenCalledWith({ ticketId: "t1", name: "Design spec", format: "markdown", content: "mine" });
  });

  it("opens straight into edit mode for a new document", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "" });
    render(
      <DocumentModal doc={spec} documents={[spec]} owner={{ ticketId: "t1" }} ownerLabel="ACP-84" startEditing
        onClose={vi.fn()} onRenamed={vi.fn()} onDeleted={vi.fn()} onRecreated={vi.fn()} />,
    );
    expect(await screen.findByRole("textbox", { name: "Document content" })).toBeTruthy();
  });

  it("refuses to save more than 8 MB", async () => {
    load("old");
    setup();
    fireEvent.change(await startEditing(), { target: { value: "x".repeat(8 * 1024 * 1024 + 1) } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("alert")).textContent).toBe("This document is 8.1 MB. The limit is 8 MB.");
    expect(mockApi.documents.update).not.toHaveBeenCalled();
  });
});
```

(`setup` must return `rerender` and `onRecreated`; `render(...)` already returns `rerender`.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/components/DocumentModal.test.tsx`
Expected: FAIL, no Edit button.

- [ ] **Step 3: Replace the modal**

Replace `web/src/components/DocumentModal.tsx` with:

```tsx
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { Download, Eye, Pencil, Trash2, X } from "lucide-react";
import Markdown from "react-markdown";
import { api, type DocumentMeta, type DocumentOwnerRef, type DocumentWithContent } from "../api/client";
import { activityTime } from "../lib/activity";
import { conflictDocument, contentTooLarge, displayName, formatSize, isNotFound } from "../lib/documents";
import { serverMessage } from "../lib/epics";
import { useEscape } from "../lib/escapeStack";
import DeleteDocumentConfirm from "./DeleteDocumentConfirm";
import DocumentRenameField from "./DocumentRenameField";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
const ICON_BUTTON = "shrink-0 text-slate-500 transition-colors hover:text-slate-300 focus:text-slate-300 focus:outline-none";
const TOGGLE = "inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors";

type Saved = { revision: number; content: string };

// A document over the ticket editor. View mode renders it; edit mode (for
// markdown) has the description's Write/Preview toggle, Save and Cancel.
// The content is fetched on open and whenever its revision changes. While
// editing, a newer revision replaces nothing: with unsaved text it raises the
// conflict notice, without it the text quietly updates. Unsaved text is
// never dropped without asking, whatever asked for the close.
export default function DocumentModal({
  doc,
  documents,
  owner,
  ownerLabel,
  ownerNoun = "ticket",
  startEditing = false,
  deleted = false,
  closeRequested = false,
  onClose,
  onCloseCancelled,
  onDirtyChange,
  onRenamed,
  onDeleted,
  onRecreated,
}: {
  doc: DocumentMeta;
  documents: readonly DocumentMeta[];
  owner: DocumentOwnerRef;
  ownerLabel: string;
  ownerNoun?: string;
  startEditing?: boolean;
  deleted?: boolean;
  closeRequested?: boolean;
  onClose: () => void;
  onCloseCancelled?: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  onRenamed: (doc: DocumentMeta) => void;
  onDeleted: () => void;
  onRecreated: (doc: DocumentMeta) => void;
}) {
  const editable = doc.format === "markdown";
  const [saved, setSaved] = useState<Saved | null>(null);
  const [failed, setFailed] = useState(false);
  const [editing, setEditing] = useState(startEditing && editable);
  // What the draft started from: its revision is what a save says it is
  // overwriting, its content what "unsaved" compares against.
  const [base, setBase] = useState<Saved | null>(null);
  const [draft, setDraft] = useState("");
  const [mode, setMode] = useState<"write" | "preview">("write");
  const [conflict, setConflict] = useState<DocumentWithContent | null>(null);
  const [goneOnSave, setGoneOnSave] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const shown = displayName(doc);

  const dirty = editing && base !== null && draft !== base.content;
  const urlAsking = closeRequested && dirty;
  const asking = pending !== null || urlAsking;
  const isGone = deleted || goneOnSave;

  // The fetch reads these without re-running on each keystroke.
  const editingRef = useRef(editing);
  const baseRef = useRef(base);
  const dirtyRef = useRef(dirty);
  useEffect(() => {
    editingRef.current = editing;
    baseRef.current = base;
    dirtyRef.current = dirty;
  });

  useEffect(() => {
    let cancelled = false;
    Promise.resolve()
      .then(() => api.documents.get(doc.id))
      .then((full) => {
        if (cancelled) return;
        const next = { revision: full.revision, content: full.content };
        setSaved(next);
        setFailed(false);
        if (!editingRef.current) return;
        const from = baseRef.current;
        if (from === null || (!dirtyRef.current && full.revision !== from.revision)) {
          setBase(next);
          setDraft(full.content);
        } else if (full.revision !== from.revision) {
          setConflict(full);
        }
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [doc.id, doc.revision]);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.focus();
    return () => {
      if (previous?.isConnected) previous.focus();
    };
  }, []);

  const stopEditing = useCallback(() => {
    setEditing(false);
    setBase(null);
    setConflict(null);
    setGoneOnSave(false);
    setSaveError(null);
  }, []);

  const confirmDiscardThen = useCallback(
    (action: () => void) => {
      if (asking) return;
      if (!dirty) action();
      else setPending(() => action);
    },
    [asking, dirty],
  );

  const requestClose = useCallback(() => confirmDiscardThen(onClose), [confirmDiscardThen, onClose]);
  useEscape(requestClose);

  const acceptDiscard = () => {
    const action = pending ?? (urlAsking ? onClose : null);
    setPending(null);
    stopEditing();
    action?.();
  };
  const cancelDiscard = () => {
    const undoUrl = urlAsking;
    setPending(null);
    if (undoUrl) onCloseCancelled?.();
  };

  const startEdit = () => {
    setEditing(true);
    setMode("write");
    if (saved) {
      setBase(saved);
      setDraft(saved.content);
    }
  };

  const save = async () => {
    if (!base || saving) return;
    const tooLarge = contentTooLarge(draft);
    if (tooLarge) {
      setSaveError(tooLarge);
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      const updated = await api.documents.update(doc.id, { content: draft, expectedRevision: base.revision });
      setSaved({ revision: updated.revision, content: updated.content });
      stopEditing();
    } catch (err) {
      const current = conflictDocument(err);
      if (current) setConflict(current);
      else if (isNotFound(err)) setGoneOnSave(true);
      else setSaveError(serverMessage(err, "The document was not saved."));
    } finally {
      setSaving(false);
    }
  };

  const keepMine = () => {
    if (conflict && base) setBase({ revision: conflict.revision, content: base.content });
    setConflict(null);
  };
  const loadTheirs = () => {
    if (conflict) setSaved({ revision: conflict.revision, content: conflict.content });
    stopEditing();
  };
  const saveAsNew = async () => {
    try {
      const created = await api.documents.create({ ...owner, name: doc.name, format: doc.format, content: draft });
      onRecreated(created);
    } catch (err) {
      setSaveError(serverMessage(err, "The document was not saved."));
    }
  };

  const trapTab = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const root = dialogRef.current;
    if (e.key !== "Tab" || !root || deleting || asking) return;
    const focusable = Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && (active === first || active === root)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  };

  let body: React.ReactNode;
  if (editing && base !== null && mode === "write") {
    body = (
      <textarea
        aria-label="Document content"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        className="h-full min-h-[20rem] w-full resize-none rounded-lg border border-slate-700 bg-slate-800/50 px-3 py-2 font-mono text-sm text-slate-200 focus:outline-none focus:ring-1 focus:ring-blue-500"
      />
    );
  } else if (editing && base !== null) {
    body = draft ? (
      <div className="prose-card"><Markdown>{draft}</Markdown></div>
    ) : (
      <p className="text-sm text-slate-600">Nothing to preview.</p>
    );
  } else if (failed) body = <p className="text-sm text-slate-500">This document could not be loaded.</p>;
  else if (saved === null) body = <p className="text-sm text-slate-600">Loading…</p>;
  else if (saved.content === "") body = <p className="text-sm text-slate-600">This document is empty.</p>;
  else body = <div data-testid="document-content" className="prose-card"><Markdown>{saved.content}</Markdown></div>;

  return (
    <div className="fixed inset-0 z-[60] flex p-3 sm:p-6 lg:p-10">
      <div aria-hidden="true" className="absolute inset-0 bg-black/60" onClick={requestClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={renaming ? undefined : titleId}
        aria-label={renaming ? shown : undefined}
        tabIndex={-1}
        onKeyDown={trapTab}
        inert={asking}
        className="relative mx-auto flex h-full w-full max-w-[80rem] flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl focus:outline-none"
      >
        <header className="flex shrink-0 items-center gap-3 border-b border-slate-800 px-4 py-3 sm:px-6">
          <span className="shrink-0 font-mono text-xs text-slate-400">{ownerLabel}</span>
          <span aria-hidden="true" className="text-slate-600">›</span>
          {renaming ? (
            <DocumentRenameField doc={doc} documents={documents} ownerNoun={ownerNoun}
              onCancel={() => setRenaming(false)}
              onRenamed={(updated) => {
                setRenaming(false);
                onRenamed(updated);
              }}
            />
          ) : (
            <h2 id={titleId} className="min-w-0 flex-1 truncate text-sm font-semibold text-white">
              {shown}
              {dirty && <span className="ml-2 text-xs font-normal text-amber-400">unsaved</span>}
            </h2>
          )}
          {editing ? (
            <>
              <div className="flex items-center gap-1">
                <button type="button" aria-pressed={mode === "write"} onClick={() => setMode("write")}
                  className={`${TOGGLE} ${mode === "write" ? "bg-slate-700 text-white" : "text-slate-500 hover:text-slate-300"}`}>
                  <Pencil className="h-3 w-3" /> Write
                </button>
                <button type="button" aria-pressed={mode === "preview"} onClick={() => setMode("preview")}
                  className={`${TOGGLE} ${mode === "preview" ? "bg-slate-700 text-white" : "text-slate-500 hover:text-slate-300"}`}>
                  <Eye className="h-3 w-3" /> Preview
                </button>
              </div>
              <button type="button" onClick={() => confirmDiscardThen(stopEditing)} className="shrink-0 px-2 py-1 text-sm text-slate-400 hover:text-white">
                Cancel
              </button>
              <button type="button" onClick={save} disabled={saving || isGone}
                className="shrink-0 rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-60">
                Save
              </button>
            </>
          ) : (
            <>
              <span className="hidden shrink-0 whitespace-nowrap text-xs text-slate-500 sm:inline">
                {formatSize(doc.size)} · updated {activityTime(doc.updatedAt)}
              </span>
              {editable && (
                <button type="button" onClick={startEdit}
                  className="shrink-0 rounded-md border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800">
                  Edit
                </button>
              )}
              <a href={api.documents.downloadUrl(doc.id)} download={shown} aria-label={`Download ${shown}`} title="Download" className={ICON_BUTTON}>
                <Download className="h-4 w-4" />
              </a>
              <button type="button" aria-label={`Rename ${shown}`} title="Rename" onClick={() => setRenaming(true)} className={ICON_BUTTON}>
                <Pencil className="h-4 w-4" />
              </button>
              <button type="button" aria-label={`Delete ${shown}`} title="Delete" onClick={() => setDeleting(true)} className={`${ICON_BUTTON} hover:text-red-400`}>
                <Trash2 className="h-4 w-4" />
              </button>
            </>
          )}
          <button type="button" aria-label="Close document" title="Close" onClick={requestClose} className={ICON_BUTTON}>
            <X className="h-5 w-5" />
          </button>
        </header>

        {editing && isGone && (
          <div role="status" className="flex flex-wrap items-center gap-2 border-b border-amber-500/30 bg-amber-500/10 px-4 py-2 text-xs text-amber-200 sm:px-6">
            <span className="flex-1">This document was deleted while you were editing. Your text isn't saved yet.</span>
            <button type="button" onClick={saveAsNew} className="rounded-md border border-amber-500/40 px-2 py-1 font-medium text-amber-100 hover:bg-amber-500/20">Save as a new document</button>
            <button type="button" onClick={() => { stopEditing(); onClose(); }} className="rounded-md border border-amber-500/40 px-2 py-1 font-medium text-amber-100 hover:bg-amber-500/20">Discard</button>
          </div>
        )}
        {editing && !isGone && conflict && (
          <div role="status" className="flex flex-wrap items-center gap-2 border-b border-amber-500/30 bg-amber-500/10 px-4 py-2 text-xs text-amber-200 sm:px-6">
            <span className="flex-1">This document changed while you were editing (updated {activityTime(conflict.updatedAt)}). Your text isn't saved yet.</span>
            <button type="button" onClick={keepMine} className="rounded-md border border-amber-500/40 px-2 py-1 font-medium text-amber-100 hover:bg-amber-500/20">Keep editing, overwrite on save</button>
            <button type="button" onClick={loadTheirs} className="rounded-md border border-amber-500/40 px-2 py-1 font-medium text-amber-100 hover:bg-amber-500/20">Discard mine, load theirs</button>
          </div>
        )}
        {saveError && (
          <p role="alert" className="border-b border-red-500/40 bg-red-500/10 px-4 py-2 text-xs text-red-200 sm:px-6">{saveError}</p>
        )}

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4 sm:px-8 sm:py-6">{body}</div>
      </div>

      {deleting && (
        <DeleteDocumentConfirm doc={doc} onCancel={() => setDeleting(false)}
          onDeleted={() => {
            setDeleting(false);
            onDeleted();
          }}
        />
      )}
      {asking && <DiscardDocumentConfirm onCancel={cancelDiscard} onDiscard={acceptDiscard} />}
    </div>
  );
}

// "Discard your changes?" over the document. Focus starts on the safe choice.
function DiscardDocumentConfirm({ onCancel, onDiscard }: { onCancel: () => void; onDiscard: () => void }) {
  const ids = useId();
  const keepRef = useRef<HTMLButtonElement>(null);
  useEscape(onCancel);
  useEffect(() => keepRef.current?.focus(), []);
  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center p-4">
      <div aria-hidden="true" className="absolute inset-0 bg-slate-950/60" onClick={onCancel} />
      <div role="alertdialog" aria-modal="true" aria-labelledby={`${ids}-title`} aria-describedby={`${ids}-desc`}
        className="relative w-full max-w-sm rounded-xl border border-slate-700 bg-slate-900 p-5 shadow-2xl">
        <h3 id={`${ids}-title`} className="text-base font-semibold text-white">Discard your changes?</h3>
        <p id={`${ids}-desc`} className="mt-1.5 text-sm text-slate-400">Your edits to this document have not been saved.</p>
        <div className="mt-5 flex justify-end gap-2">
          <button ref={keepRef} type="button" onClick={onCancel}
            className="rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm font-medium text-slate-200 hover:bg-slate-700 focus:outline-none focus:ring-2 focus:ring-blue-500">
            Keep editing
          </button>
          <button type="button" onClick={onDiscard}
            className="rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-500 focus:outline-none focus:ring-2 focus:ring-red-400">
            Discard
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && npx vitest run src/components/DocumentModal.test.tsx`
Expected: PASS, the ticket 1 cases included (they pass `owner` and `onRecreated` now).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/DocumentModal.tsx web/src/components/DocumentModal.test.tsx
git commit -m "feat: edit documents in the modal with a conflict notice

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Wire editing into the ticket editor

**Files:**
- Modify: `web/src/components/TicketEditor.tsx`
- Test: `web/src/components/TicketEditor.test.tsx`

**Interfaces:**
- Consumes: Tasks 3–5.

- [ ] **Step 1: Write the failing test**

Add `create: vi.fn()` to `mockApi.documents` in `TicketEditor.test.tsx`, then inside `describe("documents")`:

```tsx
  it("opens a new document straight into edit mode", async () => {
    const created = { ...spec, id: "d9", name: "Rollout plan" };
    mockApi.documents.list.mockResolvedValueOnce([]).mockResolvedValue([created]);
    mockApi.documents.create.mockResolvedValue({ ...created, content: "" });
    mockApi.documents.get.mockResolvedValue({ ...created, content: "" });
    renderEditor();

    fireEvent.click(await screen.findByRole("button", { name: "New document" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Rollout plan" } });
    fireEvent.submit(screen.getByRole("textbox", { name: "Name" }));

    expect(await screen.findByRole("dialog", { name: "Rollout plan.md" })).toBeTruthy();
    expect(await screen.findByRole("textbox", { name: "Document content" })).toBeTruthy();
  });
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/TicketEditor.test.tsx -t "new document"`
Expected: FAIL (the section has no `owner`/`onCreated`, the modal no `owner`).

- [ ] **Step 3: Wire it**

In `web/src/components/TicketEditor.tsx`:

1. Next to the `docParam` lines add:

```tsx
  // A document just created with New opens in edit mode; remembered by id
  // until it has opened.
  const [editOnOpen, setEditOnOpen] = useState<string | null>(null);
```

2. Give `DocumentsSection` the new props:

```tsx
              owner={{ ticketId: ticket.id }}
              onCreated={async (doc, edit) => {
                await reloadDocuments();
                if (edit) {
                  setEditOnOpen(doc.id);
                  docParam.open(doc);
                }
              }}
```

3. Replace the `DocumentModal` element with:

```tsx
      {docParam.selected && (
        <DocumentModal
          key={docParam.selected.id}
          doc={docParam.selected}
          documents={documents ?? []}
          owner={{ ticketId: ticket.id }}
          ownerLabel={ticketKey}
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
            reloadDocuments();
          }}
          onDeleted={() => {
            docParam.close();
            reloadDocuments();
          }}
          onRecreated={async (doc) => {
            await reloadDocuments();
            docParam.renamed(doc);
          }}
        />
      )}
```

- [ ] **Step 4: Run the web suite, lint and types**

Run: `cd web && npm test && npm run lint && npx tsc -b`
Expected: PASS.

- [ ] **Step 5: Manual check on a throwaway board**

Seed as in ticket 1's Task 13 into `./.tmp/t2.db`, run `make frontend && make dev DEV_DB=./.tmp/t2.db DEV_PORT=3011`, and in the browser pane:

- New → "Rollout plan" → the modal opens in Write mode; type, Preview, Save; the list shows the new size.
- Edit a document, type, press Back: "Discard your changes?"; Keep editing puts `doc=` back; Discard closes it.
- Edit, type, then from a terminal `printf 'theirs\n' | go run ./cmd/taskboard --db ./.tmp/t2.db doc write <name> --ticket DEMO-1 --file -`: the notice appears; try both buttons on two runs.
- Edit, type, then `go run ./cmd/taskboard --db ./.tmp/t2.db doc delete <name> --ticket DEMO-1`: the deleted notice; Save as a new document brings it back under the same name.
- Upload a `.md` file; upload a `.txt` (refused); upload a file whose name is taken (server message).

Stop the server with `kill $(lsof -t -- ./.tmp/t2.db)`.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/TicketEditor.tsx web/src/components/TicketEditor.test.tsx
git commit -m "feat: open new documents for editing from the ticket editor

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
