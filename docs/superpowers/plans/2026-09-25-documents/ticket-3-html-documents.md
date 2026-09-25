# Ticket 3: HTML documents — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agents and users attach HTML documents, which the web UI shows full size in a sandboxed frame that may run scripts and load from the internet but cannot act as the board; users never edit them.

**Architecture:** The store accepts the `html` format and `.html`/`.htm` filenames. A new `GET /api/documents/{ref}/raw` serves the content with a `Content-Security-Policy: sandbox …` header, and the modal points an `<iframe sandbox="allow-scripts …">` (no `allow-same-origin`) at it, so the page runs in an opaque origin whether it is framed or opened on its own. The board's API stays out of reach because it sends no CORS headers and `rejectCrossOriginWrites` refuses a write with `Origin: null`.

**Tech Stack:** Go, React 19, Vitest + jsdom.

**Spec:** `docs/superpowers/specs/2026-09-25-documents-design.md`, section "Ticket 3".

**Depends on:** tickets 1 and 2.

## Global constraints

See [README.md](README.md#global-constraints). The sandbox tokens are exactly `allow-scripts allow-forms allow-modals allow-downloads`, the same in the header and the iframe attribute. Never add `allow-same-origin`, `allow-top-navigation*` or `allow-popups-to-escape-sandbox`.

## Review Focus

- A write from a sandboxed frame arrives with `Origin: null`; it must get 403, not reach a handler. Pinned in Task 2.
- The raw page opened directly in a tab (not framed) must still be sandboxed: the CSP header does that. Pinned in Task 2.
- An HTML document must never show an Edit button, even though ticket 2 added one for markdown. Pinned in Task 3.
- An upload of `Report.HTM` (upper case) is accepted as HTML named "Report". Pinned in Tasks 1 and 3.
- An agent saving a new revision while the frame is open reloads the frame. Pinned in Task 3.

---

### Task 1: Accept the HTML format

**Files:**
- Modify: `internal/db/documents.go` (`documentFormats`, `formatByExtension`, `msgDocFormat`, `msgDocExtension`)
- Modify: `internal/db/documents_test.go`, `internal/cli/document_test.go` (messages that changed)
- Modify: `internal/mcp/documents.go` (format enum and descriptions)
- Modify: `internal/cli/document.go` (`--format` usage)
- Test: `internal/db/documents_test.go`, `internal/mcp/documents_test.go`, `internal/cli/document_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/db/documents_test.go`:

- In `TestDocumentNameFromFilename`, replace the `report.html` refusal with:

```go
	for _, tc := range []struct{ file, name string }{
		{"report.html", "report"},
		{"Load test.HTM", "Load test"},
	} {
		name, format, err := DocumentNameFromFilename(tc.file)
		if err != nil || name != tc.name || format != models.DocumentFormatHTML {
			t.Errorf("DocumentNameFromFilename(%q) = %q, %q, %v; want %q, html", tc.file, name, format, err, tc.name)
		}
	}
```

- In `TestCreateDocumentRules`, replace the HTML refusal with an acceptance, and add an unknown format:

```go
	html, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Report", Format: models.DocumentFormatHTML, Content: "<h1>x</h1>"})
	if err != nil || html.Format != models.DocumentFormatHTML {
		t.Fatalf("html create = %+v, %v", html, err)
	}
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Deck", Format: "pdf"})
	wantInvalid(t, err, `Format must be "markdown" or "html".`)
	// A name is taken whatever the format: Report.md cannot join Report.html.
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "report"})
	wantInvalid(t, err, `This ticket already has a document called "Report.html".`)
```

In `internal/cli/document_test.go`, change the `.txt` expectation to `"Only .md, .html and .htm files can be attached."` and add to `TestDocCommands`:

```go
	page := filepath.Join(t.TempDir(), "Load test.html")
	os.WriteFile(page, []byte("<h1>ok</h1>"), 0o644)
	addedHTML := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1", "--file", page); err != nil {
			t.Fatalf("doc add html: %v", err)
		}
	})
	if !strings.Contains(addedHTML, "Added document Load test.html") {
		t.Fatalf("doc add html printed %q", addedHTML)
	}
```

In `internal/mcp/documents_test.go`, add:

```go
func TestCreateHTMLDocumentTool(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)
	got, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"ticket": "DOC-1", "name": "Report", "format": "html", "content": "<h1>x</h1>",
	}))
	if err != nil {
		t.Fatal(err)
	}
	d := got.(*models.Document)
	if d.Format != models.DocumentFormatHTML || !strings.HasSuffix(d.URL, "doc=Report.html") {
		t.Fatalf("created = %+v", d.DocumentMeta)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db ./internal/mcp ./internal/cli -run 'Document|TestDoc' -v`
Expected: FAIL with the old format and extension messages.

- [ ] **Step 3: Implement**

In `internal/db/documents.go`:

```go
	msgDocFormat          = `Format must be "markdown" or "html".`
	msgDocExtension       = "Only .md, .html and .htm files can be attached."
```

```go
var documentFormats = []string{models.DocumentFormatMarkdown, models.DocumentFormatHTML}

var formatByExtension = map[string]string{
	".md":   models.DocumentFormatMarkdown,
	".html": models.DocumentFormatHTML,
	".htm":  models.DocumentFormatHTML,
}
```

In `internal/mcp/documents.go`:

- `create_document` `format`: `Enum: []string{models.DocumentFormatMarkdown, models.DocumentFormatHTML}`, description `"markdown (default) or html. HTML is shown in a sandboxed frame that may run scripts and load from the internet; people cannot edit it in the web UI."`.
- `create_document` description: "Attach a markdown or HTML document …"; `content`: "The whole document, as markdown or HTML".
- `documentIDDescription`: "…(with or without its .md or .html extension)…".

In `internal/cli/document.go`: `--format` usage `"document format (markdown|html); defaults to the file's extension, else markdown"`, and the `add` Long text "(.md, .html or .htm)".

- [ ] **Step 4: Run the tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/documents.go internal/db/documents_test.go internal/mcp/documents.go internal/mcp/documents_test.go internal/cli/document.go internal/cli/document_test.go
git commit -m "feat: accept HTML documents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Serve the raw document in a sandbox

**Files:**
- Modify: `internal/server/documents.go` (`rawDocument`, `documentSandbox`)
- Modify: `internal/server/server.go` (one route)
- Test: `internal/server/documents_test.go`

**Interfaces:**
- Produces: `GET /api/documents/{ref}/raw` → the content inline, `Content-Type` by format (`text/html; charset=utf-8` or `text/plain; charset=utf-8`), `Content-Security-Policy: sandbox allow-scripts allow-forms allow-modals allow-downloads`, `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`, `Referrer-Policy: no-referrer`.
- Exports for the web: the sandbox token list lives in both places; keep them identical.

- [ ] **Step 1: Write the failing tests**

```go
func TestRawDocumentIsSandboxed(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)
	page, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{
		TicketID: tk.ID, Name: "Report", Format: models.DocumentFormatHTML, Content: "<script>1</script>",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(r.url + "/api/documents/" + page.ID + "/raw")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "<script>1</script>" {
		t.Fatalf("raw: %d %q", resp.StatusCode, body)
	}
	for header, want := range map[string]string{
		"Content-Type":            "text/html; charset=utf-8",
		"Content-Security-Policy": "sandbox allow-scripts allow-forms allow-modals allow-downloads",
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want none", got)
	}
}

// A sandboxed frame has an opaque origin, which the browser sends as
// "Origin: null". Its scripts must not be able to write to the board.
func TestWriteFromASandboxedFrameIsRefused(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/documents/" + d.ID, `{"content":"pwned"}`},
		{http.MethodDelete, "/api/documents/" + d.ID, ""},
		{http.MethodPost, "/api/tickets", `{"projectId":"DOC","title":"pwned"}`},
	} {
		req, err := http.NewRequest(tc.method, r.url+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "null")
		req.Header.Set("Content-Type", "text/plain")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s from Origin: null = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
	if got, _ := r.srv.store.GetDocument(d.ID); got == nil || got.Content != "# Spec\n" {
		t.Fatal("the document changed")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/server -run 'TestRawDocumentIsSandboxed|TestWriteFromASandboxedFrameIsRefused' -v`
Expected: `TestRawDocumentIsSandboxed` FAILS (404/405). `TestWriteFromASandboxedFrameIsRefused` should already PASS; if it does not, stop and fix `rejectCrossOriginWrites` before going on, since the trust boundary depends on it.

- [ ] **Step 3: Implement**

In `internal/server/documents.go`:

```go
// documentSandbox is the sandbox an HTML document runs in, framed or opened
// on its own: scripts, forms, dialogs and downloads work; there is no
// same-origin grant, so the page has an opaque origin and cannot read the
// board's storage or act as the board; no top navigation, so it cannot
// navigate the board away. It must match the iframe's sandbox attribute in
// web/src/components/DocumentModal.tsx.
const documentSandbox = "sandbox allow-scripts allow-forms allow-modals allow-downloads"

// rawDocument serves a document's content inline, for the HTML frame.
func (s *Server) rawDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	d, err := s.store.GetDocument(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	contentType := "text/plain; charset=utf-8"
	if d.Format == models.DocumentFormatHTML {
		contentType = "text/html; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", documentSandbox)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, d.Content)
}
```

In `setupRoutes`, add `r.Get("/{ref}/raw", s.rawDocument)` to the `/documents` route.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/server -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/documents.go internal/server/server.go internal/server/documents_test.go
git commit -m "feat: serve HTML documents inside a sandbox

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Show HTML documents in the web UI

**Files:**
- Modify: `web/src/api/client.ts` (`api.documents.rawUrl`)
- Modify: `web/src/lib/documents.ts` (+ test): extensions and message
- Modify: `web/src/components/DocumentsSection.tsx` (+ test): icon, upload accept
- Modify: `web/src/components/DocumentModal.tsx` (+ test): the frame

**Interfaces:**
- Produces: `api.documents.rawUrl(id: string, revision: number): string` → `/api/documents/{id}/raw?rev={revision}`; `DOCUMENT_SANDBOX = "allow-scripts allow-forms allow-modals allow-downloads"` exported from `lib/documents.ts`.

- [ ] **Step 1: Write the failing tests**

`web/src/lib/documents.test.ts`, in `describe("nameFromFilename")`:

```ts
  it("takes .html and .htm as HTML, in any case", () => {
    expect(nameFromFilename("Load test.HTM")).toEqual({ name: "Load test", format: "html" });
    expect(nameFromFilename("report.html")).toEqual({ name: "report", format: "html" });
    expect(nameFromFilename("notes.txt")).toEqual({ error: "Only .md, .html and .htm files can be attached." });
  });
```

(and update the earlier `.txt`/`README` expectations to the new message).

`web/src/components/DocumentsSection.test.tsx`:

```tsx
  it("marks HTML documents with their own icon and accepts HTML uploads", () => {
    setup([spec, { ...notes, name: "Report", format: "html" }]);
    expect(screen.getByRole("button", { name: "Report.html" })).toBeTruthy();
    expect(screen.getAllByTestId("document-icon-html")).toHaveLength(1);
    expect(screen.getByLabelText("Upload a document").getAttribute("accept")).toBe(".md,.html,.htm");
  });
```

`web/src/components/DocumentModal.test.tsx`:

```tsx
describe("HTML documents", () => {
  const page: DocumentMeta = { ...spec, id: "h1", name: "Report", format: "html", revision: 3 };

  it("shows the page in a sandboxed frame, without an Edit button", () => {
    mockApi.documents.get.mockResolvedValue({ ...page, content: "<h1>x</h1>" });
    render(
      <DocumentModal doc={page} documents={[page]} owner={{ ticketId: "t1" }} ownerLabel="ACP-84"
        onClose={vi.fn()} onRenamed={vi.fn()} onDeleted={vi.fn()} onRecreated={vi.fn()} />,
    );
    const frame = screen.getByTitle("Report.html");
    expect(frame.tagName).toBe("IFRAME");
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts allow-forms allow-modals allow-downloads");
    expect(frame.getAttribute("src")).toBe("/api/documents/h1/raw?rev=3");
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer");
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.getByRole("link", { name: "Download Report.html" })).toBeTruthy();
  });
});
```

Add `rawUrl: (id: string, rev: number) => \`/api/documents/${id}/raw?rev=${rev}\`` to that file's `mockApi.documents`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/documents.test.ts src/components/DocumentsSection.test.tsx src/components/DocumentModal.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement**

`web/src/api/client.ts`, in `api.documents`:

```ts
    rawUrl: (id: string, revision: number) => `/api/documents/${encodeURIComponent(id)}/raw?rev=${revision}`,
```

`web/src/lib/documents.ts`:

```ts
const FORMAT_BY_EXTENSION: Record<string, DocumentFormat> = { ".md": "markdown", ".html": "html", ".htm": "html" };
const EXTENSION_MESSAGE = "Only .md, .html and .htm files can be attached.";

/**
 * The sandbox an HTML document's frame runs in. It must match the server's
 * Content-Security-Policy (documentSandbox in internal/server/documents.go).
 * Never add allow-same-origin: that would let the page act as the board.
 */
export const DOCUMENT_SANDBOX = "allow-scripts allow-forms allow-modals allow-downloads";
```

`web/src/components/DocumentsSection.tsx`: import `FileCode` from lucide; replace the row icon with

```tsx
              {doc.format === "html" ? (
                <FileCode aria-hidden="true" data-testid="document-icon-html" className="h-4 w-4 shrink-0 text-slate-500" />
              ) : (
                <FileText aria-hidden="true" className="h-4 w-4 shrink-0 text-slate-500" />
              )}
```

and set the upload input's `accept=".md,.html,.htm"`.

`web/src/components/DocumentModal.tsx`: import `DOCUMENT_SANDBOX`; in the body chain, before the `failed` branch, add:

```tsx
  } else if (doc.format === "html") {
    body = (
      <iframe
        title={shown}
        src={api.documents.rawUrl(doc.id, doc.revision)}
        sandbox={DOCUMENT_SANDBOX}
        referrerPolicy="no-referrer"
        className="h-full min-h-[20rem] w-full rounded-lg border border-slate-800 bg-white"
      />
    );
```

and give the scroll container no padding for HTML so the page fills the modal:

```tsx
        <div className={`min-h-0 flex-1 overflow-y-auto ${doc.format === "html" ? "p-0" : "px-4 py-4 sm:px-8 sm:py-6"}`}>{body}</div>
```

The content fetch is only needed for markdown: wrap the fetch effect's body in `if (doc.format !== "markdown") return;` (keep the dependency list as it is). The `?rev=` in the frame's `src` changes with each save, which reloads the frame.

- [ ] **Step 4: Run the web suite, lint and types**

Run: `cd web && npm test && npm run lint && npx tsc -b`
Expected: PASS.

- [ ] **Step 5: Manual check on a throwaway board**

```bash
mkdir -p .tmp && rm -f .tmp/t3.db
go run ./cmd/taskboard --db ./.tmp/t3.db project create Demo --prefix DEMO
go run ./cmd/taskboard --db ./.tmp/t3.db ticket create --project DEMO --title "Report"
cat > .tmp/load-test.html <<'EOF'
<!doctype html><html><head>
<script src="https://cdn.jsdelivr.net/npm/chart.js"></script></head>
<body><h1>Load test</h1><canvas id="c"></canvas>
<p id="probe">probing…</p>
<script>
new Chart(document.getElementById('c'), {type:'bar', data:{labels:['a','b'], datasets:[{data:[3,5]}]}});
fetch('/api/tickets', {method:'POST', body:'{"projectId":"DEMO","title":"pwned"}'})
  .then(r => document.getElementById('probe').textContent = 'write status ' + r.status)
  .catch(e => document.getElementById('probe').textContent = 'write blocked: ' + e);
</script></body></html>
EOF
go run ./cmd/taskboard --db ./.tmp/t3.db doc add DEMO-1 --file .tmp/load-test.html
make frontend && make dev DEV_DB=./.tmp/t3.db DEV_PORT=3011
```

In the browser pane, open DEMO-1 → `load-test.html`: the chart renders (scripts and CDN work); the probe line reads "write status 403" or "write blocked"; no "pwned" ticket appears on the board. Open `http://localhost:3011/api/documents/<id>/raw` directly: the page runs, and its probe is still refused. There is no Edit button. Stop the server with `kill $(lsof -t -- ./.tmp/t3.db)`.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/client.ts web/src/lib/documents.ts web/src/lib/documents.test.ts web/src/components/DocumentsSection.tsx web/src/components/DocumentsSection.test.tsx web/src/components/DocumentModal.tsx web/src/components/DocumentModal.test.tsx
git commit -m "feat: show HTML documents in a sandboxed frame

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
