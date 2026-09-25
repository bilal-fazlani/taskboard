# Ticket 1: Markdown documents on tickets — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agents (MCP, CLI) attach markdown documents to tickets and write, rename and delete them; users see a paperclip count on cards, a Documents section in the ticket editor, and open a document in a large modal whose state is in the URL, with download, rename and delete.

**Architecture:** Migration 008 adds a `documents` table built for both owners (ticket now, epic in ticket 4) and both formats (markdown now, HTML in ticket 3). `internal/db/documents.go` holds every rule. HTTP exposes `/api/tickets/{id}/documents` and `/api/documents/{ref}` (plus `/download`). The web editor loads the list itself (refreshing on the live change feed), keeps the open document in the `doc` query parameter through `useOverlayHistory` (ticket 0), and renders the modal outside the editor dialog so each keeps its own focus trap.

**Tech Stack:** Go (database/sql, modernc SQLite, chi, cobra), React 19, React Router 7, react-markdown, lucide-react, Vitest + jsdom.

**Spec:** `docs/superpowers/specs/2026-09-25-documents-design.md`, sections "Data model", "Rules" and "Ticket 1".

**Depends on:** ticket 0 (`useOverlayHistory`, `withTicket` dropping `doc`).

## Global constraints

See [README.md](README.md#global-constraints). In this ticket only the `markdown` format is accepted; `html` is refused with `Format must be "markdown".`, and only `.md` filenames convert.

## Review Focus

- A name that is all spaces, or that becomes all spaces after a filename is cleaned (`...md`, `-.md` keeps `-`, `%%.md`), must be refused with "Enter a name", not stored empty. Pinned in Task 2.
- Two names differing only in non-ASCII case (`Étude` / `étude`) are the same name. SQLite's NOCASE would miss it; the Go fold must catch it. Pinned in Task 3.
- A document reached as `?doc=Design spec.html` when it is markdown must be "not found", not opened. Pinned in Task 8.
- An agent renaming the document a user has open must not close it: the URL follows the new name. Pinned in Task 9.
- Escape with the delete confirm open over the document modal closes only the confirm, and Escape in the document modal never closes the ticket editor under it. Pinned in Tasks 10 and 11.

---

### Task 1: Migration and document model

**Files:**
- Create: `internal/db/migrations/008_documents.sql`
- Create: `internal/models/document.go`
- Test: `internal/models/document_test.go`
- Modify: `internal/models/models.go` (Ticket gains `DocumentCount`, `Documents`)

**Interfaces:**
- Produces (package `models`):
  ```go
  const DocumentFormatMarkdown = "markdown"
  const DocumentFormatHTML = "html"
  const MaxDocumentBytes = 8 << 20
  func DocumentExtension(format string) string        // ".md" or ".html"
  func DocumentDisplayName(name, format string) string // name + extension
  func FormatSize(bytes int) string                    // "0 B", "14 KB", "8.1 MB"
  type DocumentMeta struct { ID, TicketID, Name, Format string; Size, Revision int; CreatedAt, UpdatedAt time.Time; URL string }
  type Document struct { DocumentMeta; Content string }
  type CreateDocumentRequest struct { TicketID, Name, Format, Content string }
  type UpdateDocumentRequest struct { Name, Content *string }
  // on Ticket:
  DocumentCount int            `json:"documentCount"`
  Documents     []DocumentMeta `json:"documents,omitempty"`
  ```

- [ ] **Step 1: Write the failing test**

```go
// internal/models/document_test.go
package models

import "testing"

func TestDocumentDisplayName(t *testing.T) {
	if got := DocumentDisplayName("Design spec", DocumentFormatMarkdown); got != "Design spec.md" {
		t.Fatalf("markdown display name = %q", got)
	}
	if got := DocumentDisplayName("Report", DocumentFormatHTML); got != "Report.html" {
		t.Fatalf("html display name = %q", got)
	}
}

func TestFormatSize(t *testing.T) {
	for _, tc := range []struct {
		bytes int
		want  string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1 KB"},
		{1025, "2 KB"},
		{14 * 1024, "14 KB"},
		{8 << 20, "8.0 MB"},
		{(8 << 20) + 1, "8.1 MB"},
	} {
		if got := FormatSize(tc.bytes); got != tc.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/models -run 'TestDocumentDisplayName|TestFormatSize' -v`
Expected: FAIL, `undefined: DocumentDisplayName`.

- [ ] **Step 3: Write the migration**

```sql
-- internal/db/migrations/008_documents.sql
-- Documents attached to a ticket or, from the epics ticket on, to an epic.
-- Exactly one owner is set. revision counts content saves, so a client can
-- tell whether the content changed since it read it; a rename does not bump
-- it. Names are unique per owner ignoring case: the store compares with a
-- Unicode-aware fold before writing, and these NOCASE indexes (ASCII only)
-- are the backstop, as for epics.
CREATE TABLE IF NOT EXISTS documents (
    id         TEXT PRIMARY KEY,
    ticket_id  TEXT REFERENCES tickets(id) ON DELETE CASCADE,
    epic_id    TEXT REFERENCES epics(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    format     TEXT NOT NULL CHECK (format IN ('markdown', 'html')),
    content    TEXT NOT NULL DEFAULT '',
    revision   INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    CHECK ((ticket_id IS NULL) != (epic_id IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_documents_ticket_id ON documents(ticket_id);
CREATE INDEX IF NOT EXISTS idx_documents_epic_id ON documents(epic_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_ticket_name
    ON documents(ticket_id, name COLLATE NOCASE) WHERE ticket_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_epic_name
    ON documents(epic_id, name COLLATE NOCASE) WHERE epic_id IS NOT NULL;
```

- [ ] **Step 4: Write the model**

```go
// internal/models/document.go
package models

import (
	"fmt"
	"math"
	"time"
)

// Document formats. The format is chosen when a document is created and never
// changes; the name carries no extension, and the UI shows one based on this.
const (
	DocumentFormatMarkdown = "markdown"
	DocumentFormatHTML     = "html"
)

// MaxDocumentBytes is the largest content a document may hold, in bytes of
// UTF-8. A larger save is refused, never truncated.
const MaxDocumentBytes = 8 << 20

// DocumentExtension is the extension a format is shown and downloaded with.
func DocumentExtension(format string) string {
	if format == DocumentFormatHTML {
		return ".html"
	}
	return ".md"
}

// DocumentDisplayName is how a document is shown, linked and downloaded:
// its name plus its format's extension, "Design spec.md".
func DocumentDisplayName(name, format string) string {
	return name + DocumentExtension(format)
}

// FormatSize renders a byte count the way the UI and the CLI show it. Sizes
// round up, so a document just over the limit never reads as the limit.
func FormatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1<<20:
		return fmt.Sprintf("%d KB", int(math.Ceil(float64(bytes)/1024)))
	default:
		return fmt.Sprintf("%.1f MB", math.Ceil(float64(bytes)*10/(1<<20))/10)
	}
}

// DocumentMeta is a document without its content: what lists and a ticket's
// details carry. Size is the content's length in bytes. Revision counts
// content saves, starting at 1; a rename leaves it alone.
type DocumentMeta struct {
	ID        string    `json:"id"`
	TicketID  string    `json:"ticketId,omitempty"`
	Name      string    `json:"name"`
	Format    string    `json:"format"`
	Size      int       `json:"size"`
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// URL opens the document in the web UI. The CLI and MCP fill it in, like
	// Ticket.URL; the HTTP API leaves it empty.
	URL string `json:"url,omitempty"`
}

// Document is a document with its content.
type Document struct {
	DocumentMeta
	Content string `json:"content"`
}

type CreateDocumentRequest struct {
	// TicketID is the owning ticket's id; callers resolve display keys first.
	TicketID string `json:"ticketId"`
	Name     string `json:"name"`
	// Format defaults to markdown when empty.
	Format  string `json:"format,omitempty"`
	Content string `json:"content"`
}

// UpdateDocumentRequest changes only the fields that are non-nil. Content
// replaces the whole document.
type UpdateDocumentRequest struct {
	Name    *string `json:"name,omitempty"`
	Content *string `json:"content,omitempty"`
}
```

In `internal/models/models.go`, add to `Ticket` right after `ReviewRounds`:

```go
	// DocumentCount is how many documents the ticket has. Lists and the full
	// ticket both carry it; the card's paperclip reads it.
	DocumentCount int `json:"documentCount"`

	// Documents lists the ticket's documents in the order they were added,
	// without their content. Only the full ticket carries it.
	Documents []DocumentMeta `json:"documents,omitempty"`
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/models -v && go test ./internal/db -run TestMigrations -v`
Expected: PASS; the migration applies on a fresh temp database.

- [ ] **Step 6: Commit**

```bash
git add internal/db/migrations/008_documents.sql internal/models/document.go internal/models/document_test.go internal/models/models.go
git commit -m "feat: add the documents table and document model

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Name rules and filename conversion

**Files:**
- Create: `internal/db/documents.go` (rules only in this task)
- Test: `internal/db/documents_test.go`

**Interfaces:**
- Produces (package `db`):
  ```go
  func validateDocumentName(raw string) (string, error)                        // trimmed name or ErrInvalidInput
  func DocumentNameFromFilename(filename string) (name, format string, err error)
  ```

- [ ] **Step 1: Write the failing test**

```go
// internal/db/documents_test.go
package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func wantInvalid(t *testing.T, err error, msg string) {
	t.Helper()
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want ErrInvalidInput %q", err, msg)
	}
	if invalid.Msg != msg {
		t.Fatalf("message = %q, want %q", invalid.Msg, msg)
	}
}

func TestValidateDocumentName(t *testing.T) {
	for _, ok := range []struct{ raw, want string }{
		{"Design spec", "Design spec"},
		{"  padded  ", "padded"},
		{"api-design_v2", "api-design_v2"},
		{"Étude 2", "Étude 2"},
		{"設計", "設計"},
		{strings.Repeat("a", 200), strings.Repeat("a", 200)},
	} {
		got, err := validateDocumentName(ok.raw)
		if err != nil || got != ok.want {
			t.Errorf("validateDocumentName(%q) = %q, %v; want %q", ok.raw, got, err, ok.want)
		}
	}

	_, err := validateDocumentName("")
	wantInvalid(t, err, msgDocNameRequired)
	_, err = validateDocumentName("   ")
	wantInvalid(t, err, msgDocNameRequired)
	for _, bad := range []string{"plan.md", "a/b", "tab\there", "emoji 🙂", "semi;colon"} {
		_, err = validateDocumentName(bad)
		wantInvalid(t, err, msgDocNameChars)
	}
	_, err = validateDocumentName(strings.Repeat("a", 201))
	wantInvalid(t, err, msgDocNameTooLong)
}

func TestDocumentNameFromFilename(t *testing.T) {
	for _, tc := range []struct{ file, name string }{
		{"api-design_v2.md", "api-design_v2"},
		{"notes v1.2.md", "notes v1 2"},
		{"/tmp/x/Design spec.MD", "Design spec"},
		{"  spaced .md", "spaced"},
	} {
		name, format, err := DocumentNameFromFilename(tc.file)
		if err != nil || name != tc.name || format != models.DocumentFormatMarkdown {
			t.Errorf("DocumentNameFromFilename(%q) = %q, %q, %v; want %q, markdown", tc.file, name, format, err, tc.name)
		}
	}

	_, _, err := DocumentNameFromFilename("notes.txt")
	wantInvalid(t, err, msgDocExtension)
	_, _, err = DocumentNameFromFilename("README")
	wantInvalid(t, err, msgDocExtension)
	_, _, err = DocumentNameFromFilename("report.html")
	wantInvalid(t, err, msgDocExtension)
	// Nothing left once the extension and symbols go.
	_, _, err = DocumentNameFromFilename("%%.md")
	wantInvalid(t, err, msgDocNameRequired)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/db -run 'TestValidateDocumentName|TestDocumentNameFromFilename' -v`
Expected: FAIL, `undefined: validateDocumentName`.

- [ ] **Step 3: Write the rules**

```go
// internal/db/documents.go
package db

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tcarac/taskboard/internal/models"
)

// The texts of the document rules. They are the whole error message, so the
// HTTP API, the MCP tools and the CLI all report exactly the same words.
const (
	msgDocNameRequired    = "Enter a name"
	msgDocNameChars       = "Use letters, digits, spaces, _ and - only."
	msgDocNameTooLong     = "Keep the name to 200 characters or fewer."
	msgDocRefRequired     = "Enter a document name or id."
	msgDocNothingToUpdate = "nothing to update: provide a name and/or content"
	msgDocFormat          = `Format must be "markdown".`
	msgDocExtension       = "Only .md files can be attached."
)

const maxDocumentNameRunes = 200

// documentFormats are the formats a document may be created with.
var documentFormats = []string{models.DocumentFormatMarkdown}

// formatByExtension maps a filename's extension, lower-cased, to a format.
var formatByExtension = map[string]string{".md": models.DocumentFormatMarkdown}

// isDocumentNameRune reports whether r may appear in a document name:
// letters in any script (with their combining marks), decimal digits, space,
// underscore and hyphen. Nothing that could pass for an extension or a path.
func isDocumentNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) || r == ' ' || r == '_' || r == '-'
}

// validateDocumentName applies the rules a name must meet on its own, and
// returns it trimmed, which is the form that is stored.
func validateDocumentName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", invalidInput(msgDocNameRequired)
	}
	if utf8.RuneCountInString(name) > maxDocumentNameRunes {
		return "", invalidInput(msgDocNameTooLong)
	}
	for _, r := range name {
		if !isDocumentNameRune(r) {
			return "", invalidInput(msgDocNameChars)
		}
	}
	return name, nil
}

// DocumentNameFromFilename turns a file's name into a document name and
// format: the extension picks the format and is removed, every character the
// name rules refuse becomes a space, and the result is trimmed and checked.
// The CLI calls it for `doc add --file` without --name.
func DocumentNameFromFilename(filename string) (name, format string, err error) {
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	format, ok := formatByExtension[strings.ToLower(ext)]
	if !ok {
		return "", "", invalidInput(msgDocExtension)
	}
	cleaned := strings.Map(func(r rune) rune {
		if isDocumentNameRune(r) {
			return r
		}
		return ' '
	}, strings.TrimSuffix(base, ext))
	name, err = validateDocumentName(cleaned)
	if err != nil {
		return "", "", err
	}
	return name, format, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/db -run 'TestValidateDocumentName|TestDocumentNameFromFilename' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/documents.go internal/db/documents_test.go
git commit -m "feat: add document name rules and filename conversion

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Document store operations, counts and ticket details

**Files:**
- Modify: `internal/db/documents.go`
- Modify: `internal/db/store.go` (`ClearData` table list; `attachListDetails`; `GetTicket`)
- Test: `internal/db/documents_test.go`

**Interfaces:**
- Consumes: Task 1 model, Task 2 rules.
- Produces (package `db`):
  ```go
  type DocumentOwner struct{ TicketID string }
  func (s *Store) ListDocuments(owner DocumentOwner) ([]models.DocumentMeta, error)   // never nil; unknown owner → ErrInvalidInput "ticket not found"
  func (s *Store) GetDocument(id string) (*models.Document, error)                    // (nil, nil) for unknown id
  func (s *Store) ResolveDocumentRef(owner DocumentOwner, ref string) (string, error) // id, name, or name + its extension; ignoring case
  func (s *Store) CreateDocument(req models.CreateDocumentRequest) (*models.Document, error)
  func (s *Store) UpdateDocument(id string, req models.UpdateDocumentRequest) (*models.Document, error) // (nil, nil) unknown id
  func (s *Store) DeleteDocument(id string) (bool, error)                             // false for unknown id
  ```
  `ListTickets`/`GetBoard` fill `Ticket.DocumentCount`; `GetTicket` fills `DocumentCount` and `Documents`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/db/documents_test.go`:

```go
func seedDocument(t *testing.T, s *Store, ticketID, name, content string) *models.Document {
	t.Helper()
	d, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: ticketID, Name: name, Content: content})
	if err != nil {
		t.Fatalf("seeding document %q: %v", name, err)
	}
	return d
}

func TestCreateAndGetDocument(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")

	d := seedDocument(t, s, tk.ID, "  Design spec ", "# Hello\n")
	if d.Name != "Design spec" || d.Format != models.DocumentFormatMarkdown || d.TicketID != tk.ID {
		t.Fatalf("created = %+v", d.DocumentMeta)
	}
	if d.Size != len("# Hello\n") || d.Revision != 1 || d.Content != "# Hello\n" {
		t.Fatalf("size/revision/content = %d/%d/%q", d.Size, d.Revision, d.Content)
	}

	got, err := s.GetDocument(d.ID)
	if err != nil || got == nil || got.Content != "# Hello\n" {
		t.Fatalf("GetDocument = %+v, %v", got, err)
	}
	missing, err := s.GetDocument("nope")
	if err != nil || missing != nil {
		t.Fatalf("GetDocument(unknown) = %+v, %v; want nil, nil", missing, err)
	}
}

func TestCreateDocumentRules(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	other := seedTicket(t, s, p.ID, "Other")
	seedDocument(t, s, tk.ID, "Étude", "")

	_, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "étude"})
	wantInvalid(t, err, `This ticket already has a document called "Étude.md".`)
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "plan.md"})
	wantInvalid(t, err, msgDocNameChars)
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Report", Format: models.DocumentFormatHTML})
	wantInvalid(t, err, msgDocFormat)
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Big", Content: strings.Repeat("x", models.MaxDocumentBytes+1)})
	wantInvalid(t, err, "This document is 8.1 MB. The limit is 8 MB.")
	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: "nope", Name: "Plan"})
	wantInvalid(t, err, "ticket not found")

	// Exactly at the limit is fine, and another ticket may reuse a name.
	seedDocument(t, s, tk.ID, "Max", strings.Repeat("x", models.MaxDocumentBytes))
	seedDocument(t, s, other.ID, "Étude", "")
}

func TestListDocumentsInOrderAdded(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	seedDocument(t, s, tk.ID, "Zeta", "")
	seedDocument(t, s, tk.ID, "Alpha", "")

	docs, err := s.ListDocuments(DocumentOwner{TicketID: tk.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Name != "Zeta" || docs[1].Name != "Alpha" {
		t.Fatalf("order = %+v", docs)
	}

	empty, err := s.ListDocuments(DocumentOwner{TicketID: seedTicket(t, s, p.ID, "Bare").ID})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty list = %#v, %v; want [], nil", empty, err)
	}
	_, err = s.ListDocuments(DocumentOwner{TicketID: "nope"})
	wantInvalid(t, err, "ticket not found")
}

func TestResolveDocumentRef(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	d := seedDocument(t, s, tk.ID, "Design spec", "")
	owner := DocumentOwner{TicketID: tk.ID}

	for _, ref := range []string{d.ID, "Design spec", "design SPEC", "Design spec.md", " design spec.MD "} {
		id, err := s.ResolveDocumentRef(owner, ref)
		if err != nil || id != d.ID {
			t.Errorf("ResolveDocumentRef(%q) = %q, %v", ref, id, err)
		}
	}
	_, err := s.ResolveDocumentRef(owner, "Design spec.html")
	wantInvalid(t, err, `This ticket has no document called "Design spec.html".`)
	_, err = s.ResolveDocumentRef(owner, " ")
	wantInvalid(t, err, msgDocRefRequired)
}

func TestUpdateDocument(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	d := seedDocument(t, s, tk.ID, "Plan", "v1")
	seedDocument(t, s, tk.ID, "Notes", "")

	content := "v2"
	updated, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &content})
	if err != nil || updated.Content != "v2" || updated.Revision != 2 {
		t.Fatalf("content update = %+v, %v", updated, err)
	}
	if !updated.UpdatedAt.After(d.UpdatedAt) && !updated.UpdatedAt.Equal(d.UpdatedAt) {
		t.Fatalf("updatedAt went backwards")
	}

	// A rename keeps the revision; a new case of its own name is allowed.
	name := "PLAN"
	renamed, err := s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Name: &name})
	if err != nil || renamed.Name != "PLAN" || renamed.Revision != 2 {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}

	taken := "notes"
	_, err = s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Name: &taken})
	wantInvalid(t, err, `This ticket already has a document called "Notes.md".`)
	big := strings.Repeat("x", models.MaxDocumentBytes+1)
	_, err = s.UpdateDocument(d.ID, models.UpdateDocumentRequest{Content: &big})
	wantInvalid(t, err, "This document is 8.1 MB. The limit is 8 MB.")
	_, err = s.UpdateDocument(d.ID, models.UpdateDocumentRequest{})
	wantInvalid(t, err, msgDocNothingToUpdate)

	missing, err := s.UpdateDocument("nope", models.UpdateDocumentRequest{Content: &content})
	if err != nil || missing != nil {
		t.Fatalf("update unknown = %+v, %v; want nil, nil", missing, err)
	}
}

func TestDeleteDocumentAndCascade(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	d := seedDocument(t, s, tk.ID, "Plan", "")
	kept := seedDocument(t, s, tk.ID, "Kept", "")

	deleted, err := s.DeleteDocument(d.ID)
	if err != nil || !deleted {
		t.Fatalf("DeleteDocument = %v, %v", deleted, err)
	}
	again, err := s.DeleteDocument(d.ID)
	if err != nil || again {
		t.Fatalf("second delete = %v, %v; want false, nil", again, err)
	}

	if err := s.DeleteTicket(tk.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetDocument(kept.ID); got != nil {
		t.Fatal("deleting the ticket should delete its documents")
	}
}

func TestTicketsCarryDocuments(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	bare := seedTicket(t, s, p.ID, "Bare")
	seedDocument(t, s, tk.ID, "One", "1")
	seedDocument(t, s, tk.ID, "Two", "22")

	list, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, x := range list {
		counts[x.ID] = x.DocumentCount
		if x.Documents != nil {
			t.Fatal("list payloads must not carry the document list")
		}
	}
	if counts[tk.ID] != 2 || counts[bare.ID] != 0 {
		t.Fatalf("counts = %v", counts)
	}

	full, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.DocumentCount != 2 || len(full.Documents) != 2 || full.Documents[1].Size != 2 {
		t.Fatalf("full ticket documents = %d %+v", full.DocumentCount, full.Documents)
	}

	if err := s.ClearData(); err != nil {
		t.Fatalf("ClearData with documents: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db -run 'Document|TicketsCarryDocuments' -v`
Expected: FAIL, `undefined: seedDocument`'s callees (`s.CreateDocument` …).

- [ ] **Step 3: Write the store operations**

Add to the imports of `internal/db/documents.go`: `"database/sql"`, `"fmt"`, `"slices"`, `"time"`. Then append:

```go
func msgDocNameTaken(owner DocumentOwner, existing models.DocumentMeta) string {
	return fmt.Sprintf(`This %s already has a document called "%s".`,
		owner.noun(), models.DocumentDisplayName(existing.Name, existing.Format))
}

func msgDocTooLarge(size int) string {
	return fmt.Sprintf("This document is %s. The limit is 8 MB.", models.FormatSize(size))
}

// DocumentOwner names what a document belongs to. Callers resolve display
// keys to ids first (ResolveTicketID).
type DocumentOwner struct {
	TicketID string
}

// noun is how the owner is named in messages.
func (o DocumentOwner) noun() string { return "ticket" }

// where is the owner's condition on the documents table and its argument.
func (o DocumentOwner) where() (string, any) { return "ticket_id = ?", o.TicketID }

// checkOwnerExists turns an unknown owner into an ErrInvalidInput rather than
// a foreign key failure or an empty list.
func checkOwnerExists(q dbtx, owner DocumentOwner) error {
	var one int
	err := q.QueryRow("SELECT 1 FROM tickets WHERE id = ?", owner.TicketID).Scan(&one)
	if err == sql.ErrNoRows {
		return invalidInput("%s not found", owner.noun())
	}
	return err
}

// documentMetaColumns reads everything but the content. Size is measured in
// bytes, which is what the limit counts.
const documentMetaColumns = `id, COALESCE(ticket_id, ''), name, format,
	LENGTH(CAST(content AS BLOB)), revision, created_at, updated_at`

func scanDocumentMeta(row interface{ Scan(...any) error }, extra ...any) (models.DocumentMeta, error) {
	var d models.DocumentMeta
	dest := append([]any{&d.ID, &d.TicketID, &d.Name, &d.Format, &d.Size, &d.Revision, &d.CreatedAt, &d.UpdatedAt}, extra...)
	err := row.Scan(dest...)
	return d, err
}

// loadOwnerDocuments reads an owner's documents in the order they were
// added. The result is never nil.
func loadOwnerDocuments(q dbtx, owner DocumentOwner) ([]models.DocumentMeta, error) {
	cond, arg := owner.where()
	rows, err := q.Query("SELECT "+documentMetaColumns+" FROM documents WHERE "+cond+" ORDER BY created_at, id", arg)
	if err != nil {
		return nil, fmt.Errorf("loading documents: %w", err)
	}
	defer rows.Close()
	docs := []models.DocumentMeta{}
	for rows.Next() {
		d, err := scanDocumentMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning document: %w", err)
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

// checkDocumentName applies the name rules and the owner's uniqueness, and
// returns the name trimmed. exceptID is the document being renamed, so it may
// keep its own name in different capitals; "" on create. Names are folded in
// Go with strings.EqualFold because SQLite's NOCASE folds ASCII only.
func checkDocumentName(q dbtx, owner DocumentOwner, raw, exceptID string) (string, error) {
	name, err := validateDocumentName(raw)
	if err != nil {
		return "", err
	}
	docs, err := loadOwnerDocuments(q, owner)
	if err != nil {
		return "", err
	}
	for _, d := range docs {
		if d.ID != exceptID && strings.EqualFold(d.Name, name) {
			return "", invalidInput("%s", msgDocNameTaken(owner, d))
		}
	}
	return name, nil
}

// ListDocuments returns an owner's documents without their content, in the
// order they were added. An unknown owner is an ErrInvalidInput.
func (s *Store) ListDocuments(owner DocumentOwner) ([]models.DocumentMeta, error) {
	if err := checkOwnerExists(s.db, owner); err != nil {
		return nil, err
	}
	return loadOwnerDocuments(s.db, owner)
}

// GetDocument returns (nil, nil) for an unknown id, like GetTicket.
func (s *Store) GetDocument(id string) (*models.Document, error) {
	var content string
	meta, err := scanDocumentMeta(
		s.db.QueryRow("SELECT "+documentMetaColumns+", content FROM documents WHERE id = ?", id),
		&content,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.Document{DocumentMeta: meta, Content: content}, nil
}

// ResolveDocumentRef finds one of an owner's documents by id, or by name
// ignoring case, with or without its format's extension ("Plan", "plan.md").
// A name with the wrong extension names nothing.
func (s *Store) ResolveDocumentRef(owner DocumentOwner, ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", invalidInput(msgDocRefRequired)
	}
	docs, err := s.ListDocuments(owner)
	if err != nil {
		return "", err
	}
	for _, d := range docs {
		if d.ID == trimmed {
			return d.ID, nil
		}
	}
	for _, d := range docs {
		if strings.EqualFold(d.Name, trimmed) || strings.EqualFold(models.DocumentDisplayName(d.Name, d.Format), trimmed) {
			return d.ID, nil
		}
	}
	return "", invalidInput(`This %s has no document called "%s".`, owner.noun(), trimmed)
}

// CreateDocument checks the name and inserts in one transaction, so two
// writers cannot both pass the duplicate check.
func (s *Store) CreateDocument(req models.CreateDocumentRequest) (*models.Document, error) {
	format := req.Format
	if format == "" {
		format = models.DocumentFormatMarkdown
	}
	if !slices.Contains(documentFormats, format) {
		return nil, invalidInput(msgDocFormat)
	}
	if len(req.Content) > models.MaxDocumentBytes {
		return nil, invalidInput("%s", msgDocTooLarge(len(req.Content)))
	}
	owner := DocumentOwner{TicketID: req.TicketID}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if err := checkOwnerExists(tx, owner); err != nil {
		return nil, err
	}
	name, err := checkDocumentName(tx, owner, req.Name, "")
	if err != nil {
		return nil, err
	}
	now := time.Now()
	id := newID()
	if _, err := tx.Exec(
		`INSERT INTO documents (id, ticket_id, name, format, content, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		id, owner.TicketID, name, format, req.Content, now, now,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing document: %w", err)
	}
	return s.GetDocument(id)
}

// UpdateDocument renames a document and/or replaces its content. It returns
// (nil, nil) for an unknown id. A content save bumps the revision; a rename
// alone does not. Either bumps updated_at.
func (s *Store) UpdateDocument(id string, req models.UpdateDocumentRequest) (*models.Document, error) {
	if req.Name == nil && req.Content == nil {
		return nil, invalidInput(msgDocNothingToUpdate)
	}
	if req.Content != nil && len(*req.Content) > models.MaxDocumentBytes {
		return nil, invalidInput("%s", msgDocTooLarge(len(*req.Content)))
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	var owner DocumentOwner
	var name string
	err = tx.QueryRow("SELECT COALESCE(ticket_id, ''), name FROM documents WHERE id = ?", id).Scan(&owner.TicketID, &name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		if name, err = checkDocumentName(tx, owner, *req.Name, id); err != nil {
			return nil, err
		}
	}

	set := "name = ?, updated_at = ?"
	args := []any{name, time.Now()}
	if req.Content != nil {
		set += ", content = ?, revision = revision + 1"
		args = append(args, *req.Content)
	}
	args = append(args, id)
	if _, err := tx.Exec("UPDATE documents SET "+set+" WHERE id = ?", args...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing document: %w", err)
	}
	return s.GetDocument(id)
}

// DeleteDocument removes a document for good. It reports false, with no
// error, for an unknown id.
func (s *Store) DeleteDocument(id string) (bool, error) {
	res, err := s.db.Exec("DELETE FROM documents WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// attachDocumentCounts fills DocumentCount for a page of tickets in one
// grouped query. index, placeholders and ids are attachListDetails' own.
func (s *Store) attachDocumentCounts(tickets []models.Ticket, index map[string]int, placeholders string, ids []any) error {
	rows, err := s.db.Query(`SELECT ticket_id, COUNT(*) FROM documents
		WHERE ticket_id IN (`+placeholders+`) GROUP BY ticket_id`, ids...)
	if err != nil {
		return fmt.Errorf("counting documents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID string
		var n int
		if err := rows.Scan(&ticketID, &n); err != nil {
			return err
		}
		if i, ok := index[ticketID]; ok {
			tickets[i].DocumentCount = n
		}
	}
	return rows.Err()
}
```

- [ ] **Step 4: Wire tickets and ClearData**

In `internal/db/store.go`:

1. `ClearData`: put `"documents",` first in `tables`.
2. `attachListDetails`: in the reset loop add `tickets[i].DocumentCount = 0`, and right after the `attachReviewRounds` call add:

```go
	if err := s.attachDocumentCounts(tickets, index, placeholders, ids); err != nil {
		return err
	}
```

   Update its doc comment's first line to list `DocumentCount` too.
3. `GetTicket`: after the `ReviewRounds` lookup add:

```go
	if t.Documents, err = loadOwnerDocuments(s.db, DocumentOwner{TicketID: t.ID}); err != nil {
		return nil, err
	}
	t.DocumentCount = len(t.Documents)
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/db -v`
Expected: PASS, the whole package.

- [ ] **Step 6: Commit**

```bash
git add internal/db/documents.go internal/db/documents_test.go internal/db/store.go
git commit -m "feat: store documents on tickets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Document links

**Files:**
- Modify: `internal/weburl/weburl.go`
- Test: `internal/weburl/weburl_test.go`

**Interfaces:**
- Produces: `func TicketDocument(base, ticketRef, displayName string) string`; `Fill` also fills `t.Documents[i].URL`.

- [ ] **Step 1: Write the failing test**

Append to `internal/weburl/weburl_test.go` (it already imports `testing` and `models`; add `strings` if missing):

```go
func TestTicketDocumentURL(t *testing.T) {
	got := TicketDocument("http://localhost:3011", "ACP-84", "Design spec.md")
	want := "http://localhost:3011/?ticket=ACP-84&doc=Design+spec.md"
	if got != want {
		t.Fatalf("TicketDocument = %q, want %q", got, want)
	}
}

func TestFillSetsDocumentURLs(t *testing.T) {
	t.Setenv(BaseEnv, "http://board.test")
	tk := &models.Ticket{ID: "01X", Number: 84, ProjectPrefix: "ACP", Documents: []models.DocumentMeta{
		{ID: "d1", Name: "Design spec", Format: models.DocumentFormatMarkdown},
	}}
	Fill(tk)
	if tk.Documents[0].URL != "http://board.test/?ticket=ACP-84&doc=Design+spec.md" {
		t.Fatalf("document URL = %q", tk.Documents[0].URL)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/weburl -run 'Document' -v`
Expected: FAIL, `undefined: TicketDocument`.

- [ ] **Step 3: Implement**

In `internal/weburl/weburl.go`, add after `Ticket`:

```go
// TicketDocument is the URL that opens a ticket with one of its documents on
// top, named by its display name ("Design spec.md") as the web UI's `doc`
// parameter expects.
func TicketDocument(base, ticketRef, displayName string) string {
	return Ticket(base, ticketRef) + "&doc=" + url.QueryEscape(displayName)
}
```

and change `Fill` to:

```go
func Fill(t *models.Ticket) *models.Ticket {
	if t == nil {
		return nil
	}
	base := Base()
	ref := Ref(*t)
	t.URL = Ticket(base, ref)
	for i := range t.Documents {
		d := &t.Documents[i]
		d.URL = TicketDocument(base, ref, models.DocumentDisplayName(d.Name, d.Format))
	}
	return t
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/weburl -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/weburl/weburl.go internal/weburl/weburl_test.go
git commit -m "feat: build links that open a ticket's document

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: HTTP routes

**Files:**
- Create: `internal/server/documents.go`
- Modify: `internal/server/server.go` (`setupRoutes` only)
- Test: `internal/server/documents_test.go`

**Interfaces:**
- Consumes: Task 3 store methods.
- Produces:
  - `GET /api/tickets/{id}/documents` → `200 []DocumentMeta`; unknown ticket `404`.
  - `GET /api/documents/{ref}` → `200 Document` (with content). `{ref}` is an id, or a name (with or without extension) when `?ticket=<id or key>` is given. Unresolvable → `404 {"error": msg}`.
  - `PUT /api/documents/{ref}` body `UpdateDocumentRequest` → `200 Document`; rule failures `400`; unknown `404`.
  - `DELETE /api/documents/{ref}` → `204`; unknown `404`.
  - `GET /api/documents/{ref}/download` → the content as an attachment named with the display name.

- [ ] **Step 1: Write the failing tests**

```go
// internal/server/documents_test.go
package server

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func seedTicketDocument(t *testing.T, r *running) (*models.Ticket, *models.Document) {
	t.Helper()
	p, err := r.srv.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := r.srv.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Has docs"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.srv.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Design spec", Content: "# Spec\n"})
	if err != nil {
		t.Fatal(err)
	}
	return tk, d
}

func TestListTicketDocuments(t *testing.T) {
	r := serve(t)
	tk, d := seedTicketDocument(t, r)

	docs, status := doRequest[[]map[string]any](t, http.MethodGet, r.url+"/api/tickets/"+tk.ID+"/documents", "")
	if status != http.StatusOK || len(docs) != 1 || docs[0]["id"] != d.ID {
		t.Fatalf("status %d, docs %+v", status, docs)
	}
	if _, has := docs[0]["content"]; has {
		t.Fatal("the list must not carry content")
	}
	if docs[0]["size"] != float64(len("# Spec\n")) {
		t.Fatalf("size = %v", docs[0]["size"])
	}

	_, status = errorBody(t, http.MethodGet, r.url+"/api/tickets/nope/documents", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown ticket status = %d, want 404", status)
	}
}

func TestGetDocumentByIDOrTicketAndName(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	byID, status := doRequest[models.Document](t, http.MethodGet, r.url+"/api/documents/"+d.ID, "")
	if status != http.StatusOK || byID.Content != "# Spec\n" {
		t.Fatalf("by id: %d %+v", status, byID)
	}
	for _, name := range []string{"Design spec", "design spec.md"} {
		got, status := doRequest[models.Document](t, http.MethodGet,
			r.url+"/api/documents/"+url.PathEscape(name)+"?ticket=doc-1", "")
		if status != http.StatusOK || got.ID != d.ID {
			t.Fatalf("by name %q: %d %+v", name, status, got)
		}
	}
	body, status := errorBody(t, http.MethodGet, r.url+"/api/documents/"+url.PathEscape("Design spec.html")+"?ticket=DOC-1", "")
	if status != http.StatusNotFound || body.Error != `This ticket has no document called "Design spec.html".` {
		t.Fatalf("wrong extension: %d %q", status, body.Error)
	}
	_, status = errorBody(t, http.MethodGet, r.url+"/api/documents/nope", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown id status = %d", status)
	}
}

func TestRenameDocument(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	got, status := doRequest[models.Document](t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"name":" Plan "}`)
	if status != http.StatusOK || got.Name != "Plan" || got.Revision != 1 {
		t.Fatalf("rename: %d %+v", status, got)
	}
	body, status := errorBody(t, http.MethodPut, r.url+"/api/documents/"+d.ID, `{"name":"plan.md"}`)
	if status != http.StatusBadRequest || body.Error != "Use letters, digits, spaces, _ and - only." {
		t.Fatalf("bad name: %d %q", status, body.Error)
	}
	_, status = errorBody(t, http.MethodPut, r.url+"/api/documents/nope", `{"name":"Plan"}`)
	if status != http.StatusNotFound {
		t.Fatalf("unknown id status = %d", status)
	}
}

func TestDeleteDocument(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	_, status := doRequest[any](t, http.MethodDelete, r.url+"/api/documents/"+d.ID, "")
	if status != http.StatusNoContent {
		t.Fatalf("delete status = %d", status)
	}
	_, status = errorBody(t, http.MethodDelete, r.url+"/api/documents/"+d.ID, "")
	if status != http.StatusNotFound {
		t.Fatalf("second delete status = %d", status)
	}
}

func TestDownloadDocument(t *testing.T) {
	r := serve(t)
	_, d := seedTicketDocument(t, r)

	resp, err := http.Get(r.url + "/api/documents/" + d.ID + "/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "# Spec\n" {
		t.Fatalf("download: %d %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename="Design spec.md"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/markdown") {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestTicketListCarriesDocumentCount(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)

	tickets, status := doRequest[[]models.Ticket](t, http.MethodGet, r.url+"/api/tickets", "")
	if status != http.StatusOK || len(tickets) != 1 || tickets[0].ID != tk.ID || tickets[0].DocumentCount != 1 {
		t.Fatalf("tickets = %d %+v", status, tickets)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/server -run 'Document' -v`
Expected: FAIL with 404/405 statuses (routes missing).

- [ ] **Step 3: Write the handlers**

```go
// internal/server/documents.go
package server

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// maxDocumentRequestBytes bounds a document write's request body. JSON
// escaping can make content several times its stored size, so this sits well
// above models.MaxDocumentBytes; the store enforces the real limit on the
// decoded content.
const maxDocumentRequestBytes = 64 << 20

// writeLookupError answers a reference that names nothing with 404 and the
// store's message, and anything else with 500.
func writeLookupError(w http.ResponseWriter, err error) {
	var invalid *db.ErrInvalidInput
	if errors.As(err, &invalid) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// documentID resolves a document route's {ref}: a document id, or, with
// ?ticket=<id or key>, the name of one of that ticket's documents.
func (s *Server) documentID(w http.ResponseWriter, r *http.Request) (string, bool) {
	ref := chi.URLParam(r, "ref")
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ticket == "" {
		return ref, true
	}
	ticketID, err := s.store.ResolveTicketID(ticket)
	if err != nil {
		writeLookupError(w, err)
		return "", false
	}
	id, err := s.store.ResolveDocumentRef(db.DocumentOwner{TicketID: ticketID}, ref)
	if err != nil {
		writeLookupError(w, err)
		return "", false
	}
	return id, true
}

func (s *Server) listTicketDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := s.store.ListDocuments(db.DocumentOwner{TicketID: chi.URLParam(r, "id")})
	if err != nil {
		writeLookupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *Server) getDocument(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) updateDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentRequestBytes)
	var req models.UpdateDocumentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	d, err := s.store.UpdateDocument(id, req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.documentID(w, r)
	if !ok {
		return
	}
	deleted, err := s.store.DeleteDocument(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// documentMediaType is the Content-Type a document is served with.
func documentMediaType(format string) string {
	if format == models.DocumentFormatHTML {
		return "text/html; charset=utf-8"
	}
	return "text/markdown; charset=utf-8"
}

// downloadDocument sends the content as a file named with the display name.
// mime.FormatMediaType quotes the name and switches to the RFC 2231 form for
// non-ASCII letters, which names may hold.
func (s *Server) downloadDocument(w http.ResponseWriter, r *http.Request) {
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
	filename := models.DocumentDisplayName(d.Name, d.Format)
	w.Header().Set("Content-Type", documentMediaType(d.Format))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, d.Content)
}
```

- [ ] **Step 4: Register the routes**

In `setupRoutes` in `internal/server/server.go`, inside `r.Route("/tickets", …)` add `r.Get("/{id}/documents", s.listTicketDocuments)`, and after the `/epics` route add:

```go
		r.Route("/documents", func(r chi.Router) {
			r.Get("/{ref}", s.getDocument)
			r.Put("/{ref}", s.updateDocument)
			r.Delete("/{ref}", s.deleteDocument)
			r.Get("/{ref}/download", s.downloadDocument)
		})
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/server -v`
Expected: PASS, the whole package.

- [ ] **Step 6: Commit**

```bash
git add internal/server/documents.go internal/server/documents_test.go internal/server/server.go
git commit -m "feat: serve documents over the HTTP API

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: MCP document tools

**Files:**
- Create: `internal/mcp/documents.go`
- Modify: `internal/mcp/mcp.go` (`callTool` default branch, `toolDefinitions` return, `get_ticket` description)
- Test: `internal/mcp/documents_test.go`

**Interfaces:**
- Consumes: Task 3 store methods, Task 4 `weburl.TicketDocument`, existing `resolveTicketRefOrError`.
- Produces tools: `get_document {id, ticket?}`, `create_document {ticket, name, content, format?}`, `update_document {id, ticket?, name?, content?}`, `delete_document {id, ticket?}`. Every result that is a document carries `url`. `get_ticket` results list `documents` with URLs (via `weburl.Fill`).

- [ ] **Step 1: Write the failing tests**

```go
// internal/mcp/documents_test.go
package mcp

import (
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

func seedMCPTicket(t *testing.T, s *MCPServer) *models.Ticket {
	t.Helper()
	p, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Docs", Prefix: "DOC"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := s.store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Has docs"})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestDocumentTools(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	seedMCPTicket(t, s)

	created, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"ticket": "doc-1", "name": "Design spec", "content": "# v1",
	}))
	if err != nil {
		t.Fatalf("create_document: %v", err)
	}
	d := created.(*models.Document)
	if d.URL != "http://board.test/?ticket=DOC-1&doc=Design+spec.md" || d.Format != models.DocumentFormatMarkdown {
		t.Fatalf("created = %+v", d.DocumentMeta)
	}

	for _, args := range []map[string]any{
		{"id": d.ID},
		{"id": "design spec", "ticket": "DOC-1"},
		{"id": "Design spec.md", "ticket": "DOC-1"},
	} {
		got, err := s.callTool("get_document", mustJSON(t, args))
		if err != nil || got.(*models.Document).Content != "# v1" {
			t.Fatalf("get_document %v = %+v, %v", args, got, err)
		}
	}

	updated, err := s.callTool("update_document", mustJSON(t, map[string]any{
		"id": "Design spec", "ticket": "DOC-1", "content": "# v2", "name": "Plan",
	}))
	if err != nil {
		t.Fatalf("update_document: %v", err)
	}
	u := updated.(*models.Document)
	if u.Name != "Plan" || u.Content != "# v2" || u.Revision != 2 || !strings.HasSuffix(u.URL, "doc=Plan.md") {
		t.Fatalf("updated = %+v", u)
	}

	if _, err := s.callTool("delete_document", mustJSON(t, map[string]any{"id": u.ID})); err != nil {
		t.Fatalf("delete_document: %v", err)
	}
	if _, err := s.callTool("get_document", mustJSON(t, map[string]any{"id": u.ID})); err == nil {
		t.Fatal("get_document after delete should fail")
	}
}

func TestDocumentToolErrors(t *testing.T) {
	s := newTestServer(t)
	seedMCPTicket(t, s)

	_, err := s.callTool("create_document", mustJSON(t, map[string]any{"ticket": "DOC-1", "name": "plan.md"}))
	if err == nil || err.Error() != "Use letters, digits, spaces, _ and - only." {
		t.Fatalf("bad name error = %v", err)
	}
	_, err = s.callTool("create_document", mustJSON(t, map[string]any{"name": "Plan"}))
	if err == nil || err.Error() != "ticket is required" {
		t.Fatalf("missing ticket error = %v", err)
	}
	_, err = s.callTool("get_document", mustJSON(t, map[string]any{"id": "Plan"}))
	if err == nil || !strings.Contains(err.Error(), "pass ticket") {
		t.Fatalf("name without ticket error = %v", err)
	}
	_, err = s.callTool("update_document", mustJSON(t, map[string]any{"id": "x"}))
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("empty update error = %v", err)
	}
}

func TestGetTicketToolListsDocuments(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)
	if _, err := s.store.CreateDocument(models.CreateDocumentRequest{TicketID: tk.ID, Name: "Plan", Content: "x"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.callTool("get_ticket", mustJSON(t, map[string]any{"id": "DOC-1"}))
	if err != nil {
		t.Fatal(err)
	}
	full := got.(*models.Ticket)
	if full.DocumentCount != 1 || len(full.Documents) != 1 || full.Documents[0].URL == "" {
		t.Fatalf("get_ticket documents = %d %+v", full.DocumentCount, full.Documents)
	}
}

func TestDocumentToolsAreListed(t *testing.T) {
	s := newTestServer(t)
	names := map[string]bool{}
	for _, def := range s.toolDefinitions() {
		names[def.Name] = true
	}
	for _, want := range []string{"get_document", "create_document", "update_document", "delete_document"} {
		if !names[want] {
			t.Errorf("tools/list is missing %s", want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcp -run 'Document' -v`
Expected: FAIL, `unknown tool: create_document`.

- [ ] **Step 3: Write the tools**

```go
// internal/mcp/documents.go
package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

const documentIDDescription = "Document ID, or its name (with or without its .md extension) together with ticket"
const documentTicketDescription = "Ticket ID or display key (e.g. BILL-2), case-insensitive; required when id is a name"

func (s *MCPServer) documentToolDefinitions() []toolDef {
	return []toolDef{
		{
			Name: "get_document",
			Description: "Read one document attached to a ticket, with its full content. get_ticket lists a ticket's " +
				"documents (name, format, size, updated time, link) without content.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":     {Type: "string", Description: documentIDDescription},
					"ticket": {Type: "string", Description: documentTicketDescription},
				},
				Required: []string{"id"},
			},
		},
		{
			Name: "create_document",
			Description: "Attach a markdown document to a ticket, for longer write-ups (plans, research notes, " +
				"findings, reports) that would clutter the description. Names hold letters, digits, spaces, _ and - " +
				"only, with no extension, and are unique per ticket ignoring case. Content is at most 8 MB. " +
				"Returns the document with a url that opens it in the web UI.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"ticket":  {Type: "string", Description: "Ticket ID or display key (e.g. BILL-2), case-insensitive"},
					"name":    {Type: "string", Description: "Document name: letters, digits, spaces, _ and - only; no extension"},
					"content": {Type: "string", Description: "The whole document, as markdown"},
					"format":  {Type: "string", Description: "Document format; defaults to markdown", Enum: []string{models.DocumentFormatMarkdown}},
				},
				Required: []string{"ticket", "name", "content"},
			},
		},
		{
			Name: "update_document",
			Description: "Rename a document and/or replace its content. content replaces the whole document; there " +
				"are no partial edits. The format never changes.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":      {Type: "string", Description: documentIDDescription},
					"ticket":  {Type: "string", Description: documentTicketDescription},
					"name":    {Type: "string", Description: "New name: letters, digits, spaces, _ and - only; no extension"},
					"content": {Type: "string", Description: "The whole new content"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "delete_document",
			Description: "Delete a document for good. It cannot be restored.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":     {Type: "string", Description: documentIDDescription},
					"ticket": {Type: "string", Description: documentTicketDescription},
				},
				Required: []string{"id"},
			},
		},
	}
}

// callDocumentTool handles the document tools. ok is false for any other
// name, so callTool can report the tool as unknown.
func (s *MCPServer) callDocumentTool(name string, args json.RawMessage) (result any, ok bool, err error) {
	switch name {
	case "get_document":
		var a struct {
			ID     string `json:"id"`
			Ticket string `json:"ticket"`
		}
		json.Unmarshal(args, &a)
		id, err := s.resolveDocumentRefOrError(a.ID, a.Ticket)
		if err != nil {
			return nil, true, err
		}
		d, err := s.store.GetDocument(id)
		if err != nil {
			return nil, true, err
		}
		if d == nil {
			return nil, true, fmt.Errorf("document not found")
		}
		return s.withDocumentURL(d), true, nil

	case "create_document":
		var a struct {
			Ticket  string `json:"ticket"`
			Name    string `json:"name"`
			Format  string `json:"format"`
			Content string `json:"content"`
		}
		json.Unmarshal(args, &a)
		if strings.TrimSpace(a.Ticket) == "" {
			return nil, true, fmt.Errorf("ticket is required")
		}
		ticketID, err := s.store.ResolveTicketID(a.Ticket)
		if err != nil {
			return nil, true, err
		}
		d, err := s.store.CreateDocument(models.CreateDocumentRequest{
			TicketID: ticketID, Name: a.Name, Format: a.Format, Content: a.Content,
		})
		if err != nil {
			return nil, true, err
		}
		return s.withDocumentURL(d), true, nil

	case "update_document":
		var a struct {
			ID      string  `json:"id"`
			Ticket  string  `json:"ticket"`
			Name    *string `json:"name"`
			Content *string `json:"content"`
		}
		json.Unmarshal(args, &a)
		if a.Name == nil && a.Content == nil {
			return nil, true, fmt.Errorf("nothing to update: provide name and/or content")
		}
		id, err := s.resolveDocumentRefOrError(a.ID, a.Ticket)
		if err != nil {
			return nil, true, err
		}
		d, err := s.store.UpdateDocument(id, models.UpdateDocumentRequest{Name: a.Name, Content: a.Content})
		if err != nil {
			return nil, true, err
		}
		if d == nil {
			return nil, true, fmt.Errorf("document not found")
		}
		return s.withDocumentURL(d), true, nil

	case "delete_document":
		var a struct {
			ID     string `json:"id"`
			Ticket string `json:"ticket"`
		}
		json.Unmarshal(args, &a)
		id, err := s.resolveDocumentRefOrError(a.ID, a.Ticket)
		if err != nil {
			return nil, true, err
		}
		deleted, err := s.store.DeleteDocument(id)
		if err != nil {
			return nil, true, err
		}
		if !deleted {
			return nil, true, fmt.Errorf("document not found")
		}
		return map[string]bool{"deleted": true}, true, nil
	}
	return nil, false, nil
}

// resolveDocumentRefOrError resolves a document argument: an id on its own,
// or a name together with the ticket it belongs to, since a name is only
// unique within its ticket.
func (s *MCPServer) resolveDocumentRefOrError(ref, ticketRef string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("id is required")
	}
	if strings.TrimSpace(ticketRef) != "" {
		ticketID, err := s.store.ResolveTicketID(ticketRef)
		if err != nil {
			return "", err
		}
		return s.store.ResolveDocumentRef(db.DocumentOwner{TicketID: ticketID}, ref)
	}
	d, err := s.store.GetDocument(ref)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("no document matches %q; pass ticket when addressing by name", ref)
	}
	return d.ID, nil
}

// withDocumentURL fills in the link that opens the document in the web UI.
func (s *MCPServer) withDocumentURL(d *models.Document) *models.Document {
	t, err := s.store.GetTicket(d.TicketID)
	if err == nil && t != nil {
		d.URL = weburl.TicketDocument(weburl.Base(), weburl.Ref(*t), models.DocumentDisplayName(d.Name, d.Format))
	}
	return d
}
```

- [ ] **Step 4: Delegate from mcp.go**

In `internal/mcp/mcp.go`:

1. In `callTool`, replace the `default:` branch with:

```go
	default:
		if result, ok, err := s.callDocumentTool(name, args); ok {
			return result, err
		}
		return nil, fmt.Errorf("unknown tool: %s", name)
```

2. In `toolDefinitions`, change `return []toolDef{` to `defs := []toolDef{`, and after that literal's closing `}` add `return append(defs, s.documentToolDefinitions()...)`.
3. In the `get_ticket` definition's `Description`, after "the tickets it blocks, " insert `its documents (name, format, size, updated time and a link; read one with get_document), `.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/mcp -v`
Expected: PASS, the whole package (including `TestToolDescriptionsExplainEpics`).

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/documents.go internal/mcp/documents_test.go internal/mcp/mcp.go
git commit -m "feat: add MCP tools for ticket documents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: CLI `doc` commands

**Files:**
- Create: `internal/cli/document.go`
- Modify: `internal/cli/root.go` (register `documentCommands()`)
- Test: `internal/cli/document_test.go`

**Interfaces:**
- Consumes: Task 2 `db.DocumentNameFromFilename`, Task 3 store methods, Task 4 `weburl.TicketDocument`.
- Produces: `taskboard doc list <ticket>`, `doc show <document> [--ticket T]`, `doc add <ticket> --file PATH|- [--name N] [--format markdown]`, `doc write <document> [--ticket T] --file PATH|-`, `doc rename <document> <new-name> [--ticket T]`, `doc delete <document> [--ticket T]`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/document_test.go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLIWithInput runs the CLI with stdin set to input, for --file -.
func runCLIWithInput(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCmd(nil)
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(input))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestDocCommands(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	if _, err := runCLI(t, "--db", path, "project", "create", "Docs", "--prefix", "DOC"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "DOC", "--title", "Has docs"); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(t.TempDir(), "api-design_v1.2.md")
	if err := os.WriteFile(file, []byte("# Design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1", "--file", file); err != nil {
			t.Fatalf("doc add: %v", err)
		}
	})
	if !strings.Contains(added, "Added document api-design_v1 2.md") || !strings.Contains(added, "doc=api-design_v1+2.md") {
		t.Fatalf("doc add printed %q", added)
	}
	docID := lastParenthesized(t, added)

	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "list", "DOC-1"); err != nil {
			t.Fatalf("doc list: %v", err)
		}
	})
	if !strings.Contains(listed, "api-design_v1 2.md") || !strings.Contains(listed, "9 B") || !strings.Contains(listed, docID) {
		t.Fatalf("doc list printed %q", listed)
	}

	shown := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "show", "api-design_v1 2.md", "--ticket", "doc-1"); err != nil {
			t.Fatalf("doc show: %v", err)
		}
	})
	if shown != "# Design\n" {
		t.Fatalf("doc show printed %q", shown)
	}

	if _, err := runCLIWithInput(t, "# Design v2\n", "--db", path, "doc", "write", docID, "--file", "-"); err != nil {
		t.Fatalf("doc write from stdin: %v", err)
	}
	shown = captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "show", docID); err != nil {
			t.Fatalf("doc show: %v", err)
		}
	})
	if shown != "# Design v2\n" {
		t.Fatalf("after write, doc show printed %q", shown)
	}

	renamed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "rename", docID, "Plan"); err != nil {
			t.Fatalf("doc rename: %v", err)
		}
	})
	if !strings.Contains(renamed, "Renamed to Plan.md") {
		t.Fatalf("doc rename printed %q", renamed)
	}

	if _, err := runCLIWithInput(t, "notes", "--db", path, "doc", "add", "DOC-1", "--file", "-", "--name", "Notes"); err != nil {
		t.Fatalf("doc add from stdin: %v", err)
	}
	deleted := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "delete", "notes", "--ticket", "DOC-1"); err != nil {
			t.Fatalf("doc delete: %v", err)
		}
	})
	if !strings.Contains(deleted, "Deleted Notes.md") {
		t.Fatalf("doc delete printed %q", deleted)
	}
}

func TestDocCommandErrors(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	if _, err := runCLI(t, "--db", path, "project", "create", "Docs", "--prefix", "DOC"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "create", "--project", "DOC", "--title", "Has docs"); err != nil {
		t.Fatal(err)
	}

	if _, err := runCLIWithInput(t, "x", "--db", path, "doc", "add", "DOC-1", "--file", "-"); err == nil ||
		!strings.Contains(err.Error(), "--name") {
		t.Fatalf("stdin without --name: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1"); err == nil || !strings.Contains(err.Error(), "--file") {
		t.Fatalf("no --file: %v", err)
	}
	txt := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(txt, []byte("x"), 0o644)
	if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1", "--file", txt); err == nil ||
		err.Error() != "Only .md files can be attached." {
		t.Fatalf("txt file: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "doc", "show", "Plan"); err == nil || !strings.Contains(err.Error(), "--ticket") {
		t.Fatalf("name without --ticket: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli -run 'TestDoc' -v`
Expected: FAIL, `unknown command "doc"`.

- [ ] **Step 3: Write the commands**

```go
// internal/cli/document.go
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

func documentCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Manage documents attached to tickets",
	}

	listCmd := &cobra.Command{
		Use:   "list [ticket]",
		Short: "List a ticket's documents, by ticket id or key, in the order they were added",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			ticketID, err := store.ResolveTicketID(args[0])
			if err != nil {
				return err
			}
			docs, err := store.ListDocuments(db.DocumentOwner{TicketID: ticketID})
			if err != nil {
				return err
			}
			if len(docs) == 0 {
				fmt.Println("No documents.")
			}
			for _, d := range docs {
				fmt.Printf("%s  %s  updated %s (%s)\n  %s\n",
					models.DocumentDisplayName(d.Name, d.Format), models.FormatSize(d.Size),
					d.UpdatedAt.Local().Format("2006-01-02 15:04"), d.ID, documentURL(store, &d))
			}
			return nil
		},
	}

	var showTicket string
	showCmd := &cobra.Command{
		Use:   "show [document]",
		Short: "Print a document's content, by id, or by name with --ticket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			d, err := loadDocumentArg(store, args[0], showTicket)
			if err != nil {
				return err
			}
			fmt.Print(d.Content)
			return nil
		},
	}
	showCmd.Flags().StringVar(&showTicket, "ticket", "", documentTicketFlagUsage)

	var addFile, addName, addFormat string
	addCmd := &cobra.Command{
		Use:   "add [ticket]",
		Short: "Attach a document to a ticket from a file (--file PATH) or standard input (--file -)",
		Long: "Attach a document to a ticket. Without --name, the name and format come from the file's name: " +
			"the extension (.md) is removed and other symbols become spaces. Names hold letters, digits, " +
			"spaces, _ and - only.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, format := addName, addFormat
			if name == "" {
				if addFile == "" || addFile == "-" {
					if addFile == "" {
						return fmt.Errorf("provide --file PATH, or --file - to read standard input")
					}
					return fmt.Errorf("provide --name when reading standard input")
				}
				n, f, err := db.DocumentNameFromFilename(addFile)
				if err != nil {
					return err
				}
				name = n
				if format == "" {
					format = f
				}
			}
			content, err := readDocumentContent(cmd, addFile)
			if err != nil {
				return err
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			ticketID, err := store.ResolveTicketID(args[0])
			if err != nil {
				return err
			}
			d, err := store.CreateDocument(models.CreateDocumentRequest{
				TicketID: ticketID, Name: name, Format: format, Content: content,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Added document %s (%s)\n%s\n",
				models.DocumentDisplayName(d.Name, d.Format), d.ID, documentURL(store, &d.DocumentMeta))
			return nil
		},
	}
	addCmd.Flags().StringVar(&addFile, "file", "", "file to read, or - for standard input")
	addCmd.Flags().StringVar(&addName, "name", "", "document name; required with --file -")
	addCmd.Flags().StringVar(&addFormat, "format", "", "document format (markdown); defaults to the file's extension, else markdown")

	var writeTicket, writeFile string
	writeCmd := &cobra.Command{
		Use:   "write [document]",
		Short: "Replace a document's whole content from a file (--file PATH) or standard input (--file -)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readDocumentContent(cmd, writeFile)
			if err != nil {
				return err
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			id, err := resolveDocumentArg(store, args[0], writeTicket)
			if err != nil {
				return err
			}
			d, err := store.UpdateDocument(id, models.UpdateDocumentRequest{Content: &content})
			if err != nil {
				return err
			}
			if d == nil {
				return fmt.Errorf("document not found: %s", args[0])
			}
			fmt.Printf("Saved %s (%s)\n%s\n",
				models.DocumentDisplayName(d.Name, d.Format), d.ID, documentURL(store, &d.DocumentMeta))
			return nil
		},
	}
	writeCmd.Flags().StringVar(&writeTicket, "ticket", "", documentTicketFlagUsage)
	writeCmd.Flags().StringVar(&writeFile, "file", "", "file to read, or - for standard input")

	var renameTicket string
	renameCmd := &cobra.Command{
		Use:   "rename [document] [new-name]",
		Short: "Rename a document, by id, or by name with --ticket",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			id, err := resolveDocumentArg(store, args[0], renameTicket)
			if err != nil {
				return err
			}
			d, err := store.UpdateDocument(id, models.UpdateDocumentRequest{Name: &args[1]})
			if err != nil {
				return err
			}
			if d == nil {
				return fmt.Errorf("document not found: %s", args[0])
			}
			fmt.Printf("Renamed to %s (%s)\n", models.DocumentDisplayName(d.Name, d.Format), d.ID)
			return nil
		},
	}
	renameCmd.Flags().StringVar(&renameTicket, "ticket", "", documentTicketFlagUsage)

	var deleteTicket string
	deleteCmd := &cobra.Command{
		Use:   "delete [document]",
		Short: "Delete a document for good, by id, or by name with --ticket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			d, err := loadDocumentArg(store, args[0], deleteTicket)
			if err != nil {
				return err
			}
			if _, err := store.DeleteDocument(d.ID); err != nil {
				return err
			}
			fmt.Printf("Deleted %s\n", models.DocumentDisplayName(d.Name, d.Format))
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&deleteTicket, "ticket", "", documentTicketFlagUsage)

	cmd.AddCommand(listCmd, showCmd, addCmd, writeCmd, renameCmd, deleteCmd)
	return cmd
}

const documentTicketFlagUsage = "ticket ID or key; addresses the document by name instead of id"

// resolveDocumentArg resolves a document argument to an id: directly when it
// is an id, or by name within ticketRef when one is given.
func resolveDocumentArg(store *db.Store, ref, ticketRef string) (string, error) {
	if ticketRef != "" {
		ticketID, err := store.ResolveTicketID(ticketRef)
		if err != nil {
			return "", err
		}
		return store.ResolveDocumentRef(db.DocumentOwner{TicketID: ticketID}, ref)
	}
	d, err := store.GetDocument(ref)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("document not found: %q (to use a name, pass --ticket)", ref)
	}
	return d.ID, nil
}

func loadDocumentArg(store *db.Store, ref, ticketRef string) (*models.Document, error) {
	id, err := resolveDocumentArg(store, ref, ticketRef)
	if err != nil {
		return nil, err
	}
	d, err := store.GetDocument(id)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("document not found: %s", ref)
	}
	return d, nil
}

// readDocumentContent reads --file: a path, or - for standard input.
func readDocumentContent(cmd *cobra.Command, file string) (string, error) {
	switch file {
	case "":
		return "", fmt.Errorf("provide --file PATH, or --file - to read standard input")
	case "-":
		data, err := io.ReadAll(cmd.InOrStdin())
		return string(data), err
	default:
		data, err := os.ReadFile(file)
		return string(data), err
	}
}

// documentURL is the link that opens a document in the web UI, or "" if its
// ticket cannot be read.
func documentURL(store *db.Store, d *models.DocumentMeta) string {
	t, err := store.GetTicket(d.TicketID)
	if err != nil || t == nil {
		return ""
	}
	return weburl.TicketDocument(weburl.Base(), weburl.Ref(*t), models.DocumentDisplayName(d.Name, d.Format))
}
```

In `internal/cli/root.go`, add `root.AddCommand(documentCommands())` after `root.AddCommand(epicCommands())`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/cli -v`
Expected: PASS, the whole package.

- [ ] **Step 5: Run every Go test**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/document.go internal/cli/document_test.go internal/cli/root.go
git commit -m "feat: add CLI commands for ticket documents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Web client types and document helpers

**Files:**
- Modify: `web/src/api/client.ts`
- Create: `web/src/lib/documents.ts`
- Test: `web/src/lib/documents.test.ts`

**Interfaces:**
- Produces (`client.ts`):
  ```ts
  export type DocumentFormat = "markdown" | "html";
  export interface DocumentMeta { id: string; ticketId?: string; name: string; format: DocumentFormat; size: number; revision: number; createdAt: string; updatedAt: string; }
  export interface DocumentWithContent extends DocumentMeta { content: string }
  export type DocumentOwnerRef = { ticketId: string };
  // Ticket gains: documentCount?: number; documents?: DocumentMeta[];
  api.documents.list(owner: DocumentOwnerRef): Promise<DocumentMeta[]>
  api.documents.get(id: string): Promise<DocumentWithContent>
  api.documents.update(id: string, data: { name?: string }): Promise<DocumentWithContent>
  api.documents.delete(id: string): Promise<void>
  api.documents.downloadUrl(id: string): string
  ```
- Produces (`lib/documents.ts`): `DOC_PARAM`, `MAX_DOCUMENT_BYTES`, `extensionFor`, `displayName`, `documentNameError`, `formatSize`, `findDocument`, `withDoc`, `withoutDoc`, `ownerKey`.

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/documents.test.ts
import { describe, expect, it } from "vitest";
import type { DocumentMeta } from "../api/client";
import {
  displayName,
  documentNameError,
  findDocument,
  formatSize,
  ownerKey,
  withDoc,
  withoutDoc,
} from "./documents";

function doc(id: string, name: string, format: DocumentMeta["format"] = "markdown"): DocumentMeta {
  return { id, name, format, size: 0, revision: 1, createdAt: "", updatedAt: "" };
}

describe("displayName", () => {
  it("adds the format's extension", () => {
    expect(displayName(doc("1", "Design spec"))).toBe("Design spec.md");
    expect(displayName(doc("2", "Report", "html"))).toBe("Report.html");
  });
});

describe("documentNameError", () => {
  const docs = [doc("1", "Étude"), doc("2", "Plan")];

  it("accepts letters in any script, digits, spaces, _ and -", () => {
    expect(documentNameError("  api-design_v2 ")).toBeNull();
    expect(documentNameError("設計 2")).toBeNull();
    expect(documentNameError("a".repeat(200))).toBeNull();
  });

  it("refuses empty, symbols and overlong names", () => {
    expect(documentNameError("")).toBe("Enter a name");
    expect(documentNameError("   ")).toBe("Enter a name");
    expect(documentNameError("plan.md")).toBe("Use letters, digits, spaces, _ and - only.");
    expect(documentNameError("a/b")).toBe("Use letters, digits, spaces, _ and - only.");
    expect(documentNameError("smile 🙂")).toBe("Use letters, digits, spaces, _ and - only.");
    expect(documentNameError("a".repeat(201))).toBe("Keep the name to 200 characters or fewer.");
  });

  it("refuses a name another document has, ignoring case, but not the document's own", () => {
    expect(documentNameError("étude", docs)).toBe('This ticket already has a document called "Étude.md".');
    expect(documentNameError("PLAN", docs, "2")).toBeNull();
  });
});

describe("formatSize", () => {
  it("matches the server's sizes", () => {
    expect(formatSize(0)).toBe("0 B");
    expect(formatSize(1023)).toBe("1023 B");
    expect(formatSize(1025)).toBe("2 KB");
    expect(formatSize(8 * 1024 * 1024)).toBe("8.0 MB");
    expect(formatSize(8 * 1024 * 1024 + 1)).toBe("8.1 MB");
  });
});

describe("findDocument", () => {
  const docs = [doc("1", "Design spec"), doc("2", "Report", "html")];

  it("finds by display name, ignoring case", () => {
    expect(findDocument(docs, "design SPEC.md")?.id).toBe("1");
    expect(findDocument(docs, " Report.html ")?.id).toBe("2");
  });

  it("finds nothing for the wrong extension or a bare name", () => {
    expect(findDocument(docs, "Design spec.html")).toBeUndefined();
    expect(findDocument(docs, "Design spec")).toBeUndefined();
    expect(findDocument(docs, "")).toBeUndefined();
  });
});

describe("doc parameter", () => {
  it("sets and drops doc, keeping the rest", () => {
    const params = new URLSearchParams("project=ACP&ticket=ACP-7");
    expect(withDoc(params, doc("1", "Design spec")).get("doc")).toBe("Design spec.md");
    expect(withoutDoc(new URLSearchParams("ticket=ACP-7&doc=x.md&q=a")).toString()).toBe("ticket=ACP-7&q=a");
  });
});

describe("ownerKey", () => {
  it("names the owner", () => {
    expect(ownerKey({ ticketId: "t1" })).toBe("ticket:t1");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/documents.test.ts`
Expected: FAIL, cannot resolve `./documents`.

- [ ] **Step 3: Add the client types and calls**

In `web/src/api/client.ts`, add after `StatusChange`:

```ts
export type DocumentFormat = "markdown" | "html";

/** A document without its content, as lists carry it. size is in bytes. */
export interface DocumentMeta {
  id: string;
  ticketId?: string;
  name: string;
  format: DocumentFormat;
  size: number;
  /** Counts content saves; a rename leaves it alone. */
  revision: number;
  createdAt: string;
  updatedAt: string;
}

export interface DocumentWithContent extends DocumentMeta {
  content: string;
}

/** What a document belongs to. */
export type DocumentOwnerRef = { ticketId: string };
```

Add to `Ticket`:

```ts
  /** How many documents the ticket has. Lists and the full ticket carry it. */
  documentCount?: number;
  /** The documents, without content. Only the full ticket carries it. */
  documents?: DocumentMeta[];
```

Add to `api`, after `epics`:

```ts
  documents: {
    list: (owner: DocumentOwnerRef) => request<DocumentMeta[]>(`/api/tickets/${owner.ticketId}/documents`),
    get: (id: string) => request<DocumentWithContent>(`/api/documents/${encodeURIComponent(id)}`),
    update: (id: string, data: { name?: string }) =>
      request<DocumentWithContent>(`/api/documents/${encodeURIComponent(id)}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (id: string) => request<void>(`/api/documents/${encodeURIComponent(id)}`, { method: "DELETE" }),
    downloadUrl: (id: string) => `/api/documents/${encodeURIComponent(id)}/download`,
  },
```

- [ ] **Step 4: Write the helpers**

```ts
// web/src/lib/documents.ts
// The document rules the web UI checks before it asks the server, worded
// exactly as the server words them (internal/db/documents.go), plus the `doc`
// query parameter that names the open document.
import type { DocumentFormat, DocumentMeta, DocumentOwnerRef } from "../api/client";

/** The query parameter naming the open document by its display name. */
export const DOC_PARAM = "doc";

export const MAX_DOCUMENT_BYTES = 8 * 1024 * 1024;
const MAX_NAME_LENGTH = 200;

// Letters in any script with their combining marks, decimal digits, space,
// underscore and hyphen: Go's unicode.IsLetter/IsMark/IsDigit.
const NAME_CHAR = /^[\p{L}\p{M}\p{Nd} _-]$/u;

export function extensionFor(format: DocumentFormat): string {
  return format === "html" ? ".html" : ".md";
}

/** How a document is shown, linked and downloaded: "Design spec.md". */
export function displayName(doc: Pick<DocumentMeta, "name" | "format">): string {
  return doc.name + extensionFor(doc.format);
}

/**
 * Why a name would be refused, or null. `others` are the owner's documents,
 * `exceptId` the one being renamed, which may keep its own name.
 */
export function documentNameError(
  raw: string,
  others: readonly Pick<DocumentMeta, "id" | "name" | "format">[] = [],
  exceptId?: string,
  ownerNoun = "ticket",
): string | null {
  const name = raw.trim();
  if (name === "") return "Enter a name";
  const chars = [...name];
  if (chars.length > MAX_NAME_LENGTH) return "Keep the name to 200 characters or fewer.";
  if (!chars.every((c) => NAME_CHAR.test(c))) return "Use letters, digits, spaces, _ and - only.";
  const lower = name.toLowerCase();
  const taken = others.find((d) => d.id !== exceptId && d.name.toLowerCase() === lower);
  if (taken) return `This ${ownerNoun} already has a document called "${displayName(taken)}".`;
  return null;
}

/** A byte count as the server shows it (models.FormatSize): sizes round up. */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${(Math.ceil((bytes * 10) / (1024 * 1024)) / 10).toFixed(1)} MB`;
}

/** The document a `doc` value names: its display name, ignoring case. */
export function findDocument<T extends Pick<DocumentMeta, "name" | "format">>(
  docs: readonly T[],
  ref: string,
): T | undefined {
  const wanted = ref.trim().toLowerCase();
  if (!wanted) return undefined;
  return docs.find((d) => displayName(d).toLowerCase() === wanted);
}

export function withDoc(params: URLSearchParams, doc: Pick<DocumentMeta, "name" | "format">): URLSearchParams {
  const next = new URLSearchParams(params);
  next.set(DOC_PARAM, displayName(doc));
  return next;
}

export function withoutDoc(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(DOC_PARAM);
  return next;
}

/** A stable key for an owner, for effects and caches. */
export function ownerKey(owner: DocumentOwnerRef): string {
  return `ticket:${owner.ticketId}`;
}
```

- [ ] **Step 5: Run the tests**

Run: `cd web && npx vitest run src/lib/documents.test.ts && npx tsc -b`
Expected: PASS; no type errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/client.ts web/src/lib/documents.ts web/src/lib/documents.test.ts
git commit -m "feat: add document types, calls and rules to the web client

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Loading documents, and the `doc` parameter

**Files:**
- Create: `web/src/lib/escapeStack.ts` (used from Task 10 on; created here with its test)
- Test: `web/src/lib/escapeStack.dom.test.ts`
- Create: `web/src/hooks/useOwnerDocuments.ts`
- Create: `web/src/hooks/useDocParam.ts`
- Test: `web/src/hooks/useDocParam.dom.test.tsx`

**Interfaces:**
- Consumes: Task 8, ticket 0 `useOverlayHistory`, `useLiveRefresh`.
- Produces:
  ```ts
  function pushEscape(handler: () => void): () => void   // topmost handler wins; calls preventDefault
  function useEscape(handler: () => void): void           // pushEscape for a component's lifetime
  function useOwnerDocuments(owner: DocumentOwnerRef): { documents: DocumentMeta[] | null; failed: boolean; reload: () => void }
  interface DocParamState {
    selected: DocumentMeta | null;
    notice: string | null;
    dismissNotice: () => void;
    open: (doc: DocumentMeta) => void;
    close: () => void;
    renamed: (doc: DocumentMeta) => void;
  }
  function useDocParam(documents: readonly DocumentMeta[] | null, ownerNoun?: string): DocParamState
  ```

- [ ] **Step 1: Write the failing tests**

```ts
// web/src/lib/escapeStack.dom.test.ts
// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { pushEscape } from "./escapeStack";

const escape = () => {
  const event = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
  document.body.dispatchEvent(event);
  return event;
};

describe("pushEscape", () => {
  it("calls only the topmost handler and marks the event handled", () => {
    const below = vi.fn();
    const top = vi.fn();
    const popBelow = pushEscape(below);
    const popTop = pushEscape(top);
    expect(escape().defaultPrevented).toBe(true);
    expect(top).toHaveBeenCalledTimes(1);
    expect(below).not.toHaveBeenCalled();

    popTop();
    escape();
    expect(below).toHaveBeenCalledTimes(1);
    popBelow();
    expect(escape().defaultPrevented).toBe(false);
  });
});
```

```tsx
// web/src/hooks/useDocParam.dom.test.tsx
// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import type { DocumentMeta } from "../api/client";
import { useDocParam, type DocParamState } from "./useDocParam";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let container: HTMLDivElement;
let state: DocParamState;

const spec: DocumentMeta = { id: "d1", name: "Design spec", format: "markdown", size: 1, revision: 1, createdAt: "", updatedAt: "" };
const notes: DocumentMeta = { ...spec, id: "d2", name: "Notes" };

function Harness({ docs }: { docs: DocumentMeta[] | null }) {
  const s = useDocParam(docs);
  useEffect(() => {
    state = s;
  });
  return null;
}

async function settle() {
  for (let i = 0; i < 30; i++) await new Promise((resolve) => setTimeout(resolve, 5));
}

async function render(docs: DocumentMeta[] | null) {
  await act(async () => {
    root.render(
      <BrowserRouter>
        <Harness docs={docs} />
      </BrowserRouter>,
    );
  });
}

async function mount(url: string, docs: DocumentMeta[] | null) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await render(docs);
}

const url = () => window.location.pathname + decodeURIComponent(window.location.search.replace(/\+/g, " "));

beforeEach(() => window.history.replaceState(null, "", "/"));
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe("useDocParam", () => {
  it("opens a document as a new entry named by its display name, and Back closes it", async () => {
    await mount("/?ticket=ACP-7", [spec, notes]);
    await act(async () => state.open(spec));
    expect(url()).toBe("/?ticket=ACP-7&doc=Design spec.md");
    expect(state.selected?.id).toBe("d1");

    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/?ticket=ACP-7");
    expect(state.selected).toBeNull();
  });

  it("closes by going back one entry", async () => {
    await mount("/?ticket=ACP-7", [spec]);
    await act(async () => state.open(spec));
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/?ticket=ACP-7");
  });

  it("opens the document a link names, and closes it in place", async () => {
    await mount("/?ticket=ACP-7&doc=design%20SPEC.md", [spec]);
    expect(state.selected?.id).toBe("d1");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/?ticket=ACP-7");
  });

  it("waits for the documents before deciding a link names nothing", async () => {
    await mount("/?ticket=ACP-7&doc=Design%20spec.md", null);
    expect(state.selected).toBeNull();
    expect(state.notice).toBeNull();
    expect(url()).toBe("/?ticket=ACP-7&doc=Design spec.md");
  });

  it("says a link's document is not there, for a wrong extension too, and drops the parameter", async () => {
    await mount("/?ticket=ACP-7&doc=Design%20spec.html", [spec]);
    await act(settle);
    expect(state.selected).toBeNull();
    expect(state.notice).toBe("Couldn't find Design spec.html on this ticket.");
    expect(url()).toBe("/?ticket=ACP-7");
    await act(async () => state.dismissNotice());
    expect(state.notice).toBeNull();
  });

  it("follows a rename made elsewhere, keeping the document open", async () => {
    await mount("/?ticket=ACP-7", [spec]);
    await act(async () => state.open(spec));
    await render([{ ...spec, name: "Plan" }]);
    await act(settle);
    expect(state.selected?.name).toBe("Plan");
    expect(url()).toBe("/?ticket=ACP-7&doc=Plan.md");
  });

  it("says the open document was deleted and closes it", async () => {
    await mount("/?ticket=ACP-7", [spec, notes]);
    await act(async () => state.open(spec));
    await render([notes]);
    await act(settle);
    expect(state.selected).toBeNull();
    expect(state.notice).toBe("Design spec.md was deleted.");
    expect(url()).toBe("/?ticket=ACP-7");
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/escapeStack.dom.test.ts src/hooks/useDocParam.dom.test.tsx`
Expected: FAIL, cannot resolve the modules.

- [ ] **Step 3: Write the escape stack**

```ts
// web/src/lib/escapeStack.ts
// Escape closes the topmost layer only: a confirm over the document modal,
// the modal over the ticket editor. Layers register here and one capturing
// listener calls the newest. It marks the event handled, so the ticket
// editor's own Escape listener, which skips handled events, leaves it alone.
import { useEffect, useRef } from "react";

const stack: Array<() => void> = [];

function onKeyDown(e: KeyboardEvent) {
  if (e.key !== "Escape" || e.isComposing || stack.length === 0) return;
  e.preventDefault();
  stack[stack.length - 1]();
}

export function pushEscape(handler: () => void): () => void {
  if (stack.length === 0) document.addEventListener("keydown", onKeyDown, true);
  stack.push(handler);
  return () => {
    const i = stack.lastIndexOf(handler);
    if (i >= 0) stack.splice(i, 1);
    if (stack.length === 0) document.removeEventListener("keydown", onKeyDown, true);
  };
}

/** Escape calls `handler` while the component is mounted and on top. */
export function useEscape(handler: () => void): void {
  const ref = useRef(handler);
  useEffect(() => {
    ref.current = handler;
  }, [handler]);
  useEffect(() => pushEscape(() => ref.current()), []);
}
```

- [ ] **Step 4: Write the hooks**

```ts
// web/src/hooks/useOwnerDocuments.ts
import { useCallback, useEffect, useRef, useState } from "react";
import { api, type DocumentMeta, type DocumentOwnerRef } from "../api/client";
import { ownerKey } from "../lib/documents";
import { useLiveRefresh } from "./useLiveRefresh";

/**
 * An owner's documents, loaded on mount and again on every live change, so
 * an agent's new document shows up without a reload. Each load keeps only
 * its newest reply. Null until the first load; a failed first load sets
 * `failed`, and a failed reload keeps what is on screen.
 */
export function useOwnerDocuments(owner: DocumentOwnerRef) {
  const { ticketId } = owner;
  const key = ownerKey(owner);
  const [loaded, setLoaded] = useState<{ key: string; documents: DocumentMeta[] } | null>(null);
  const [failedKey, setFailedKey] = useState<string | null>(null);
  const seq = useRef(0);

  const reload = useCallback(() => {
    const n = ++seq.current;
    // Through a promise so a test's API mock without `documents` fails the
    // load rather than the render.
    Promise.resolve()
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

  useEffect(() => {
    reload();
  }, [reload]);
  useLiveRefresh(reload);

  const documents = loaded?.key === key ? loaded.documents : null;
  return { documents, failed: documents === null && failedKey === key, reload };
}
```

```ts
// web/src/hooks/useDocParam.ts
import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import type { DocumentMeta } from "../api/client";
import { DOC_PARAM, displayName, findDocument, withDoc, withoutDoc } from "../lib/documents";
import { latestSearchParams } from "../lib/latestSearch";
import { useOverlayHistory } from "./useOverlayHistory";

export interface DocParamState {
  /** The open document, or null. */
  selected: DocumentMeta | null;
  /** Why a document the URL named is not open, until dismissed. */
  notice: string | null;
  dismissNotice: () => void;
  /** Open a document as a new history entry. */
  open: (doc: DocumentMeta) => void;
  /** Close it: one Back, or a replace when it came from a link. */
  close: () => void;
  /** Point the URL at a document's new name after a rename from the UI. */
  renamed: (doc: DocumentMeta) => void;
}

/**
 * The open document, named in the URL by its display name (`doc=Plan.md`),
 * over the ticket editor. Nothing is decided until the documents have
 * loaded. Once one is open it is followed by id, so a rename elsewhere moves
 * the URL to the new name instead of closing it; one that leaves the list
 * was deleted, and closes with a notice. A name that never matched (a bad
 * link, a wrong extension) gets its own notice.
 */
export function useDocParam(documents: readonly DocumentMeta[] | null, ownerNoun = "ticket"): DocParamState {
  const [params] = useSearchParams();
  const history = useOverlayHistory();
  const ref = params.get(DOC_PARAM) ?? "";
  const found = documents && ref ? (findDocument(documents, ref) ?? null) : null;

  const [shownId, setShownId] = useState<string | null>(null);
  if (found && shownId !== found.id) setShownId(found.id);
  if (!ref && shownId !== null) setShownId(null);
  const followed = !found && ref && shownId && documents ? (documents.find((d) => d.id === shownId) ?? null) : null;
  const selected = found ?? followed;

  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (followed) history.replace(withDoc(latestSearchParams(params), followed));
  }, [followed, params, history]);

  const missing = ref !== "" && documents !== null && !found && !followed;
  useEffect(() => {
    if (!missing) return;
    setNotice(shownId ? `${ref} was deleted.` : `Couldn't find ${ref} on this ${ownerNoun}.`);
    setShownId(null);
    history.replace(withoutDoc(latestSearchParams(params)));
  }, [missing, shownId, ref, ownerNoun, params, history]);

  const open = useCallback(
    (doc: DocumentMeta) => {
      setNotice(null);
      history.push(withDoc(latestSearchParams(params), doc));
    },
    [params, history],
  );
  const close = useCallback(() => history.closeOne(withoutDoc(latestSearchParams(params))), [params, history]);
  const renamed = useCallback(
    (doc: DocumentMeta) => {
      setShownId(doc.id);
      history.replace(withDoc(latestSearchParams(params), doc));
    },
    [params, history],
  );
  const dismissNotice = useCallback(() => setNotice(null), []);

  return { selected, notice, dismissNotice, open, close, renamed };
}
```

Remove `displayName` from this file's import list if nothing else in it uses it.

- [ ] **Step 5: Run the tests**

Run: `cd web && npx vitest run src/lib/escapeStack.dom.test.ts src/hooks/useDocParam.dom.test.tsx`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/escapeStack.ts web/src/lib/escapeStack.dom.test.ts web/src/hooks/useOwnerDocuments.ts web/src/hooks/useDocParam.ts web/src/hooks/useDocParam.dom.test.tsx
git commit -m "feat: load a ticket's documents and keep the open one in the URL

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Documents section, rename field and delete confirm

**Files:**
- Create: `web/src/components/DocumentRenameField.tsx`
- Create: `web/src/components/DeleteDocumentConfirm.tsx`
- Create: `web/src/components/DocumentsSection.tsx`
- Test: `web/src/components/DocumentsSection.test.tsx`

**Interfaces:**
- Consumes: Task 8 (`api.documents`, helpers), Task 9 (`useEscape`), `activityTime` from `lib/activity`, `serverMessage` from `lib/epics`.
- Produces:
  ```tsx
  <DocumentRenameField doc documents ownerNoun? onCancel onRenamed={(doc: DocumentMeta) => void} />
  <DeleteDocumentConfirm doc onCancel onDeleted />
  <DocumentsSection documents failed notice onDismissNotice onOpen={(doc) => void} onChanged={() => void} ownerNoun? />
  ```

- [ ] **Step 1: Write the failing tests**

```tsx
// web/src/components/DocumentsSection.test.tsx
// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { DocumentMeta } from "../api/client";

const mockApi = vi.hoisted(() => ({
  documents: {
    update: vi.fn(),
    delete: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import DocumentsSection from "./DocumentsSection";

const spec: DocumentMeta = {
  id: "d1", name: "Design spec", format: "markdown", size: 14 * 1024, revision: 1,
  createdAt: "2026-09-25T09:00:00Z", updatedAt: "2026-09-25T09:00:00Z",
};
const notes: DocumentMeta = { ...spec, id: "d2", name: "Notes", size: 10 };

function setup(documents: DocumentMeta[] | null = [spec, notes], extra: Partial<Parameters<typeof DocumentsSection>[0]> = {}) {
  const onOpen = vi.fn();
  const onChanged = vi.fn();
  const onDismissNotice = vi.fn();
  render(
    <DocumentsSection
      documents={documents}
      failed={false}
      notice={null}
      onDismissNotice={onDismissNotice}
      onOpen={onOpen}
      onChanged={onChanged}
      {...extra}
    />,
  );
  return { onOpen, onChanged, onDismissNotice };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("DocumentsSection", () => {
  it("lists documents with their extension and size, and opens one", () => {
    const { onOpen } = setup();
    expect(screen.getAllByTestId("document-row")).toHaveLength(2);
    expect(screen.getByText("14 KB", { exact: false })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Design spec.md" }));
    expect(onOpen).toHaveBeenCalledWith(spec);
  });

  it("links each download to the file", () => {
    setup();
    const link = screen.getByRole("link", { name: "Download Design spec.md" });
    expect(link.getAttribute("href")).toBe("/api/documents/d1/download");
  });

  it("invites agents when there are none", () => {
    setup([]);
    expect(screen.getByText("No documents yet. Agents can attach them.")).toBeTruthy();
  });

  it("says when the documents could not be loaded", () => {
    setup(null, { failed: true });
    expect(screen.getByText("The documents could not be loaded.")).toBeTruthy();
  });

  it("shows a notice that can be dismissed", () => {
    const { onDismissNotice } = setup([spec], { notice: "Design spec.md was deleted." });
    expect(screen.getByRole("status").textContent).toContain("Design spec.md was deleted.");
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismissNotice).toHaveBeenCalled();
  });

  it("checks a new name before asking the server", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Rename Design spec.md" }));
    const input = screen.getByRole("textbox", { name: "Document name" });
    fireEvent.change(input, { target: { value: "plan.md" } });
    fireEvent.submit(input);
    expect(screen.getByRole("alert").textContent).toBe("Use letters, digits, spaces, _ and - only.");
    fireEvent.change(input, { target: { value: "notes" } });
    fireEvent.submit(input);
    expect(screen.getByRole("alert").textContent).toBe('This ticket already has a document called "Notes.md".');
    expect(mockApi.documents.update).not.toHaveBeenCalled();
  });

  it("renames through the API and reports the change", async () => {
    mockApi.documents.update.mockResolvedValue({ ...spec, name: "Plan", content: "" });
    const { onChanged } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Rename Design spec.md" }));
    const input = screen.getByRole("textbox", { name: "Document name" });
    fireEvent.change(input, { target: { value: " Plan " } });
    fireEvent.submit(input);
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    expect(mockApi.documents.update).toHaveBeenCalledWith("d1", { name: "Plan" });
  });

  it("shows the server's reason when a rename is refused", async () => {
    mockApi.documents.update.mockRejectedValue(new Error('API error 400: {"error":"Enter a name"}'));
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Rename Design spec.md" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Document name" }), { target: { value: "Plan" } });
    fireEvent.submit(screen.getByRole("textbox", { name: "Document name" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("Enter a name"));
  });

  it("asks before deleting, and Escape cancels only the question", async () => {
    mockApi.documents.delete.mockResolvedValue(undefined);
    const { onChanged } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Delete Design spec.md" }));
    const dialog = screen.getByRole("alertdialog");
    expect(dialog.textContent).toContain("Delete Design spec.md?");
    expect(dialog.textContent).toContain("This can't be undone.");

    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("alertdialog")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Delete Design spec.md" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    expect(mockApi.documents.delete).toHaveBeenCalledWith("d1");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/DocumentsSection.test.tsx`
Expected: FAIL, cannot resolve `./DocumentsSection`.

- [ ] **Step 3: Write the rename field**

```tsx
// web/src/components/DocumentRenameField.tsx
import { useId, useState } from "react";
import { api, type DocumentMeta } from "../api/client";
import { documentNameError, extensionFor } from "../lib/documents";
import { serverMessage } from "../lib/epics";
import { useEscape } from "../lib/escapeStack";

// Renames a document in place: the name without its extension, which shows
// beside the field and cannot be edited. The rules are checked here first,
// in the server's words, then by the server. Escape or Cancel leaves it.
export default function DocumentRenameField({
  doc,
  documents,
  ownerNoun = "ticket",
  onCancel,
  onRenamed,
}: {
  doc: DocumentMeta;
  documents: readonly DocumentMeta[];
  ownerNoun?: string;
  onCancel: () => void;
  onRenamed: (doc: DocumentMeta) => void;
}) {
  const [value, setValue] = useState(doc.name);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const errorId = useId();
  useEscape(onCancel);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const local = documentNameError(value, documents, doc.id, ownerNoun);
    if (local) {
      setError(local);
      return;
    }
    const name = value.trim();
    if (name === doc.name) {
      onCancel();
      return;
    }
    setSaving(true);
    try {
      onRenamed(await api.documents.update(doc.id, { name }));
    } catch (err) {
      setError(serverMessage(err, "The document was not renamed."));
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={submit} noValidate className="flex min-w-0 flex-1 flex-col gap-1">
      <div className="flex items-center gap-2">
        <input
          aria-label="Document name"
          autoFocus
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            setError(null);
          }}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : undefined}
          className={`min-w-0 flex-1 rounded-md border bg-slate-800 px-2 py-1 text-sm text-white focus:outline-none focus:ring-1 ${
            error ? "border-red-500/60 focus:ring-red-500" : "border-slate-700 focus:ring-blue-500"
          }`}
        />
        <span className="shrink-0 font-mono text-xs text-slate-500">{extensionFor(doc.format)}</span>
        <button
          type="submit"
          disabled={saving}
          className="shrink-0 rounded-md bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-500 disabled:opacity-60"
        >
          Save
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="shrink-0 rounded-md px-2 py-1 text-xs text-slate-400 hover:text-white"
        >
          Cancel
        </button>
      </div>
      {error && (
        <p id={errorId} role="alert" className="text-xs text-red-400">
          {error}
        </p>
      )}
    </form>
  );
}
```

- [ ] **Step 4: Write the delete confirm**

```tsx
// web/src/components/DeleteDocumentConfirm.tsx
import { useEffect, useId, useRef, useState } from "react";
import { api, type DocumentMeta } from "../api/client";
import { displayName } from "../lib/documents";
import { serverMessage } from "../lib/epics";
import { useEscape } from "../lib/escapeStack";

// "Delete Design spec.md?" over everything else. Focus starts on Cancel; a
// click beside the box or Escape cancels. Deleting is permanent.
export default function DeleteDocumentConfirm({
  doc,
  onCancel,
  onDeleted,
}: {
  doc: DocumentMeta;
  onCancel: () => void;
  onDeleted: () => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const ids = useId();
  useEscape(onCancel);
  useEffect(() => cancelRef.current?.focus(), []);

  const confirm = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await api.documents.delete(doc.id);
      onDeleted();
    } catch (err) {
      setError(serverMessage(err, "The document was not deleted."));
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center p-4">
      <div aria-hidden="true" className="absolute inset-0 bg-slate-950/60" onClick={onCancel} />
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={`${ids}-title`}
        aria-describedby={`${ids}-desc`}
        className="relative w-full max-w-sm rounded-xl border border-slate-700 bg-slate-900 p-5 shadow-2xl"
      >
        <h3 id={`${ids}-title`} className="text-base font-semibold text-white">
          Delete {displayName(doc)}?
        </h3>
        <p id={`${ids}-desc`} className="mt-1.5 text-sm text-slate-400">
          This can't be undone.
        </p>
        {error && (
          <p role="alert" className="mt-3 text-xs text-red-400">
            {error}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button
            ref={cancelRef}
            type="button"
            onClick={onCancel}
            className="rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm font-medium text-slate-200 hover:bg-slate-700 focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={confirm}
            disabled={busy}
            className="rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-500 focus:outline-none focus:ring-2 focus:ring-red-400 disabled:opacity-60"
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 5: Write the section**

```tsx
// web/src/components/DocumentsSection.tsx
import { useState } from "react";
import { Download, FileText, Pencil, Trash2, X } from "lucide-react";
import { api, type DocumentMeta } from "../api/client";
import { activityTime } from "../lib/activity";
import { displayName, formatSize } from "../lib/documents";
import DeleteDocumentConfirm from "./DeleteDocumentConfirm";
import DocumentRenameField from "./DocumentRenameField";

const SECTION_HEADING = "text-[11px] font-semibold uppercase tracking-wider text-slate-500 mb-2.5";
const ICON_BUTTON = "shrink-0 text-slate-500 transition-colors hover:text-slate-300 focus:text-slate-300 focus:outline-none";

// A ticket's documents, in the order they were added: each opens in the
// document modal, and can be downloaded, renamed in place or deleted.
export default function DocumentsSection({
  documents,
  failed,
  notice,
  onDismissNotice,
  onOpen,
  onChanged,
  ownerNoun = "ticket",
}: {
  documents: readonly DocumentMeta[] | null;
  failed: boolean;
  notice: string | null;
  onDismissNotice: () => void;
  onOpen: (doc: DocumentMeta) => void;
  /** A rename or delete went through; the owner reloads the list. */
  onChanged: () => void;
  ownerNoun?: string;
}) {
  const [renaming, setRenaming] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<DocumentMeta | null>(null);

  let body: React.ReactNode = null;
  if (failed) {
    body = <p className="text-sm text-slate-500">The documents could not be loaded.</p>;
  } else if (documents && documents.length === 0) {
    body = <p className="text-sm text-slate-600">No documents yet. Agents can attach them.</p>;
  } else if (documents) {
    body = (
      <ul className="divide-y divide-slate-800 rounded-lg border border-slate-800">
        {documents.map((doc) => {
          const shown = displayName(doc);
          return (
            <li key={doc.id} data-testid="document-row" className="flex items-center gap-2.5 px-3 py-2">
              <FileText aria-hidden="true" className="h-4 w-4 shrink-0 text-slate-500" />
              {renaming === doc.id ? (
                <DocumentRenameField
                  doc={doc}
                  documents={documents}
                  ownerNoun={ownerNoun}
                  onCancel={() => setRenaming(null)}
                  onRenamed={() => {
                    setRenaming(null);
                    onChanged();
                  }}
                />
              ) : (
                <>
                  <button
                    type="button"
                    onClick={() => onOpen(doc)}
                    className="min-w-0 flex-1 truncate text-left text-sm text-blue-400 hover:underline focus:underline focus:outline-none"
                  >
                    {shown}
                  </button>
                  <span className="shrink-0 whitespace-nowrap text-xs text-slate-500">
                    {formatSize(doc.size)} · {activityTime(doc.updatedAt)}
                  </span>
                  <a href={api.documents.downloadUrl(doc.id)} download={shown} aria-label={`Download ${shown}`} title="Download" className={ICON_BUTTON}>
                    <Download className="h-3.5 w-3.5" />
                  </a>
                  <button type="button" aria-label={`Rename ${shown}`} title="Rename" onClick={() => setRenaming(doc.id)} className={ICON_BUTTON}>
                    <Pencil className="h-3.5 w-3.5" />
                  </button>
                  <button type="button" aria-label={`Delete ${shown}`} title="Delete" onClick={() => setDeleting(doc)} className={`${ICON_BUTTON} hover:text-red-400`}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </>
              )}
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <div>
      <h3 className={SECTION_HEADING}>Documents</h3>
      {notice && (
        <div role="status" className="mb-2 flex items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-200">
          <span className="flex-1">{notice}</span>
          <button type="button" aria-label="Dismiss" onClick={onDismissNotice} className="text-amber-200/70 hover:text-amber-100">
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
      {body}
      {deleting && (
        <DeleteDocumentConfirm
          doc={deleting}
          onCancel={() => setDeleting(null)}
          onDeleted={() => {
            setDeleting(null);
            onChanged();
          }}
        />
      )}
    </div>
  );
}
```

- [ ] **Step 6: Run the tests**

Run: `cd web && npx vitest run src/components/DocumentsSection.test.tsx`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/DocumentRenameField.tsx web/src/components/DeleteDocumentConfirm.tsx web/src/components/DocumentsSection.tsx web/src/components/DocumentsSection.test.tsx
git commit -m "feat: add the Documents section with rename and delete

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Document modal

**Files:**
- Create: `web/src/components/DocumentModal.tsx`
- Test: `web/src/components/DocumentModal.test.tsx`

**Interfaces:**
- Consumes: Tasks 8–10.
- Produces:
  ```tsx
  <DocumentModal
    doc={DocumentMeta} documents={readonly DocumentMeta[]} ownerLabel={string /* "ACP-84" */} ownerNoun?
    onClose={() => void} onRenamed={(doc: DocumentMeta) => void} onDeleted={() => void} />
  ```
  It fetches content on open and whenever `doc.revision` changes.

- [ ] **Step 1: Write the failing tests**

```tsx
// web/src/components/DocumentModal.test.tsx
// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { DocumentMeta } from "../api/client";

const mockApi = vi.hoisted(() => ({
  documents: {
    get: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import DocumentModal from "./DocumentModal";

const spec: DocumentMeta = {
  id: "d1", name: "Design spec", format: "markdown", size: 20, revision: 1,
  createdAt: "2026-09-25T09:00:00Z", updatedAt: "2026-09-25T09:00:00Z",
};

function setup(doc = spec) {
  const props = { onClose: vi.fn(), onRenamed: vi.fn(), onDeleted: vi.fn() };
  const utils = render(<DocumentModal doc={doc} documents={[doc]} ownerLabel="ACP-84" {...props} />);
  return { ...props, ...utils };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("DocumentModal", () => {
  it("renders the document's markdown under its name and ticket", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "# Storage\n\nOne owner." });
    setup();
    expect(screen.getByRole("dialog", { name: "Design spec.md" })).toBeTruthy();
    expect(screen.getByText("ACP-84")).toBeTruthy();
    await waitFor(() => expect(screen.getByRole("heading", { name: "Storage" })).toBeTruthy());
  });

  it("refetches when an agent saves a new revision", async () => {
    mockApi.documents.get.mockResolvedValueOnce({ ...spec, content: "old" });
    const { rerender, onClose, onRenamed, onDeleted } = setup();
    await waitFor(() => expect(screen.getByText("old")).toBeTruthy());
    mockApi.documents.get.mockResolvedValueOnce({ ...spec, revision: 2, content: "new" });
    const next = { ...spec, revision: 2 };
    rerender(<DocumentModal doc={next} documents={[next]} ownerLabel="ACP-84" onClose={onClose} onRenamed={onRenamed} onDeleted={onDeleted} />);
    await waitFor(() => expect(screen.getByText("new")).toBeTruthy());
    expect(mockApi.documents.get).toHaveBeenCalledTimes(2);
  });

  it("says when the content could not be loaded", async () => {
    mockApi.documents.get.mockRejectedValue(new Error("API error 500: boom"));
    setup();
    await waitFor(() => expect(screen.getByText("This document could not be loaded.")).toBeTruthy());
  });

  it("closes on Escape and on the close button", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    const { onClose } = setup();
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Close document" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("lets Escape cancel the delete question without closing the document", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    const { onClose } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Delete Design spec.md" }));
    expect(screen.getByRole("alertdialog")).toBeTruthy();
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("renames from the header", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    mockApi.documents.update.mockResolvedValue({ ...spec, name: "Plan", content: "x" });
    const { onRenamed } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Rename Design spec.md" }));
    const input = screen.getByRole("textbox", { name: "Document name" });
    fireEvent.change(input, { target: { value: "Plan" } });
    fireEvent.submit(input);
    await waitFor(() => expect(onRenamed).toHaveBeenCalledWith(expect.objectContaining({ name: "Plan" })));
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/DocumentModal.test.tsx`
Expected: FAIL, cannot resolve `./DocumentModal`.

- [ ] **Step 3: Write the modal**

```tsx
// web/src/components/DocumentModal.tsx
import { useEffect, useId, useRef, useState } from "react";
import { Download, Pencil, Trash2, X } from "lucide-react";
import Markdown from "react-markdown";
import { api, type DocumentMeta } from "../api/client";
import { activityTime } from "../lib/activity";
import { displayName, formatSize } from "../lib/documents";
import { useEscape } from "../lib/escapeStack";
import DeleteDocumentConfirm from "./DeleteDocumentConfirm";
import DocumentRenameField from "./DocumentRenameField";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
const ICON_BUTTON = "shrink-0 text-slate-500 transition-colors hover:text-slate-300 focus:text-slate-300 focus:outline-none";

// A document over the ticket editor, as large as the editor. The content is
// fetched on open and again when its revision changes, which is how an
// agent's save reaches an open document. Escape and × close it (the caller
// goes back one history entry); the header renames, downloads and deletes.
export default function DocumentModal({
  doc,
  documents,
  ownerLabel,
  ownerNoun = "ticket",
  onClose,
  onRenamed,
  onDeleted,
}: {
  doc: DocumentMeta;
  documents: readonly DocumentMeta[];
  ownerLabel: string;
  ownerNoun?: string;
  onClose: () => void;
  onRenamed: (doc: DocumentMeta) => void;
  onDeleted: () => void;
}) {
  const [content, setContent] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const shown = displayName(doc);
  useEscape(onClose);

  useEffect(() => {
    let cancelled = false;
    Promise.resolve()
      .then(() => api.documents.get(doc.id))
      .then((full) => {
        if (cancelled) return;
        setContent(full.content);
        setFailed(false);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [doc.id, doc.revision]);

  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.focus();
    return () => {
      if (previous?.isConnected) previous.focus();
    };
  }, []);

  const trapTab = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const root = dialogRef.current;
    if (e.key !== "Tab" || !root || deleting) return;
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
  if (failed) body = <p className="text-sm text-slate-500">This document could not be loaded.</p>;
  else if (content === null) body = <p className="text-sm text-slate-600">Loading…</p>;
  else if (content === "") body = <p className="text-sm text-slate-600">This document is empty.</p>;
  else
    body = (
      <div data-testid="document-content" className="prose-card">
        <Markdown>{content}</Markdown>
      </div>
    );

  return (
    <div className="fixed inset-0 z-[60] flex p-3 sm:p-6 lg:p-10">
      <div aria-hidden="true" className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onKeyDown={trapTab}
        className="relative mx-auto flex h-full w-full max-w-[80rem] flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl focus:outline-none"
      >
        <header className="flex shrink-0 items-center gap-3 border-b border-slate-800 px-4 py-3 sm:px-6">
          <span className="shrink-0 font-mono text-xs text-slate-400">{ownerLabel}</span>
          <span aria-hidden="true" className="text-slate-600">›</span>
          {renaming ? (
            <DocumentRenameField
              doc={doc}
              documents={documents}
              ownerNoun={ownerNoun}
              onCancel={() => setRenaming(false)}
              onRenamed={(updated) => {
                setRenaming(false);
                onRenamed(updated);
              }}
            />
          ) : (
            <h2 id={titleId} className="min-w-0 flex-1 truncate text-sm font-semibold text-white">
              {shown}
            </h2>
          )}
          <span className="hidden shrink-0 whitespace-nowrap text-xs text-slate-500 sm:inline">
            {formatSize(doc.size)} · updated {activityTime(doc.updatedAt)}
          </span>
          <a href={api.documents.downloadUrl(doc.id)} download={shown} aria-label={`Download ${shown}`} title="Download" className={ICON_BUTTON}>
            <Download className="h-4 w-4" />
          </a>
          <button type="button" aria-label={`Rename ${shown}`} title="Rename" onClick={() => setRenaming(true)} className={ICON_BUTTON}>
            <Pencil className="h-4 w-4" />
          </button>
          <button type="button" aria-label={`Delete ${shown}`} title="Delete" onClick={() => setDeleting(true)} className={`${ICON_BUTTON} hover:text-red-400`}>
            <Trash2 className="h-4 w-4" />
          </button>
          <button type="button" aria-label="Close document" title="Close" onClick={onClose} className={ICON_BUTTON}>
            <X className="h-5 w-5" />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4 sm:px-8 sm:py-6">{body}</div>
      </div>
      {deleting && (
        <DeleteDocumentConfirm
          doc={doc}
          onCancel={() => setDeleting(false)}
          onDeleted={() => {
            setDeleting(false);
            onDeleted();
          }}
        />
      )}
    </div>
  );
}
```

When the header is in rename mode the `h2` is gone, so give the dialog `aria-label={shown}` as well as `aria-labelledby`; keep `aria-labelledby` only when not renaming: `aria-labelledby={renaming ? undefined : titleId}` and `aria-label={renaming ? shown : undefined}`.

- [ ] **Step 4: Run the tests**

Run: `cd web && npx vitest run src/components/DocumentModal.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/DocumentModal.tsx web/src/components/DocumentModal.test.tsx
git commit -m "feat: add the document modal

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Wire documents into the ticket editor and the card

**Files:**
- Modify: `web/src/components/TicketEditor.tsx`
- Modify: `web/src/components/TicketEditor.test.tsx`
- Modify: `web/src/components/TicketCard.tsx`
- Modify: `web/src/components/TicketCard.test.tsx`

**Interfaces:**
- Consumes: Tasks 9–11.
- Produces: no new exports. The editor shows a Documents section between Subtasks and Activity and the document modal over itself; the card shows a paperclip count.

- [ ] **Step 1: Write the failing tests**

In `web/src/components/TicketEditor.test.tsx`:

1. Add to `mockApi`: `documents: { list: vi.fn(), get: vi.fn(), update: vi.fn(), delete: vi.fn(), downloadUrl: (id: string) => \`/api/documents/${id}/download\` },`
2. In the file's `beforeEach` (create one if there is none) add `mockApi.documents.list.mockResolvedValue([]);`.
3. Import `MemoryRouter` from `react-router-dom` and render the editor with `render(<TicketEditor … />, { wrapper: MemoryRouter })` in `renderEditor`, so `rerender` keeps the router.
4. Add:

```tsx
describe("documents", () => {
  const spec = {
    id: "d1", name: "Design spec", format: "markdown" as const, size: 10, revision: 1,
    createdAt: "2026-09-25T09:00:00Z", updatedAt: "2026-09-25T09:00:00Z",
  };

  it("lists the ticket's documents and opens one over the editor", async () => {
    mockApi.documents.list.mockResolvedValue([spec]);
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "# Hello" });
    const { onClose } = renderEditor();

    fireEvent.click(await screen.findByRole("button", { name: "Design spec.md" }));
    expect(await screen.findByRole("dialog", { name: "Design spec.md" })).toBeTruthy();
    expect(await screen.findByRole("heading", { name: "Hello" })).toBeTruthy();

    // Escape closes the document, not the editor.
    fireEvent.keyDown(document.body, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Design spec.md" })).toBeNull());
    expect(onClose).not.toHaveBeenCalled();
  });

  it("puts the Documents section between Subtasks and Activity", async () => {
    renderEditor();
    const headings = (await screen.findAllByRole("heading", { level: 3 })).map((h) => h.textContent);
    const subtasks = headings.indexOf("Subtasks");
    expect(headings.indexOf("Documents")).toBe(subtasks + 1);
    expect(headings.indexOf("Activity")).toBe(subtasks + 2);
  });
});
```

(`renderEditor` must return `onClose`; it already builds it. If it does not return it, add it to the returned object.)

In `web/src/components/TicketCard.test.tsx`, add (the file's ticket factory is `makeTicket`):

```tsx
it("shows a paperclip with the document count, and nothing without documents", () => {
  const { unmount } = render(<TicketCard ticket={makeTicket({ documentCount: 2 })} />);
  expect(screen.getByTestId("card-documents").textContent).toBe("2");
  unmount();
  render(<TicketCard ticket={makeTicket({ documentCount: 0 })} />);
  expect(screen.queryByTestId("card-documents")).toBeNull();
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/components/TicketEditor.test.tsx src/components/TicketCard.test.tsx`
Expected: the new tests FAIL (no Documents section, no `card-documents`).

- [ ] **Step 3: Wire the editor**

In `web/src/components/TicketEditor.tsx`:

1. Imports:

```tsx
import DocumentModal from "./DocumentModal";
import DocumentsSection from "./DocumentsSection";
import { useDocParam } from "../hooks/useDocParam";
import { useOwnerDocuments } from "../hooks/useOwnerDocuments";
```

2. After the `history` effect, add:

```tsx
  // The ticket's documents, loaded here and refreshed on every live change,
  // and the one the URL has open over the editor.
  const { documents, failed: documentsFailed, reload: reloadDocuments } = useOwnerDocuments({ ticketId: ticket.id });
  const docParam = useDocParam(documents);
  const docOpen = docParam.selected !== null;
```

3. On the dialog `div` (`ref={dialogRef}`), add `inert={docOpen}` so the editor underneath takes no focus or clicks while a document is open.
4. Between the Subtasks `div` and the Activity comment, add:

```tsx
            <DocumentsSection
              documents={documents}
              failed={documentsFailed}
              notice={docParam.notice}
              onDismissNotice={docParam.dismissNotice}
              onOpen={docParam.open}
              onChanged={reloadDocuments}
            />
```

5. As the last child of the outermost `fixed inset-0 z-50` div (a sibling of the dialog, not inside it, so the editor's Tab trap never sees the modal's keys), add:

```tsx
      {docParam.selected && (
        <DocumentModal
          doc={docParam.selected}
          documents={documents ?? []}
          ownerLabel={ticketKey}
          onClose={docParam.close}
          onRenamed={(doc) => {
            docParam.renamed(doc);
            reloadDocuments();
          }}
          onDeleted={() => {
            docParam.close();
            reloadDocuments();
          }}
        />
      )}
```

The editor's own Escape listener already skips events with `defaultPrevented`, which the escape stack sets, so no change is needed there.

- [ ] **Step 4: Wire the card**

In `web/src/components/TicketCard.tsx`, add `Paperclip` to the lucide import, add this component after `SubtaskProgress`:

```tsx
// Subtask progress and, beside it, a paperclip with how many documents the
// ticket has. Either can be missing; with neither, nothing shows.
function CardFooter({ ticket }: { ticket: Ticket }) {
  const documents = ticket.documentCount ?? 0;
  const hasSubtasks = (ticket.subtasks?.length ?? 0) > 0;
  if (!hasSubtasks && documents === 0) return null;
  return (
    <div className="flex items-center gap-3">
      <div className="min-w-0 flex-1">
        <SubtaskProgress subtasks={ticket.subtasks} />
      </div>
      {documents > 0 && (
        <span
          data-testid="card-documents"
          title={`${documents} document${documents === 1 ? "" : "s"}`}
          className="inline-flex shrink-0 items-center gap-1 text-xs text-slate-500"
        >
          <Paperclip aria-hidden="true" className="h-3 w-3" />
          {documents}
        </span>
      )}
    </div>
  );
}
```

and replace `<SubtaskProgress subtasks={ticket.subtasks} />` in the card's render with `<CardFooter ticket={ticket} />`.

- [ ] **Step 5: Run the web suite, lint and types**

Run: `cd web && npm test && npm run lint && npx tsc -b`
Expected: PASS. If a page test (`viewState`, `projectScope`, `newTicket`) renders the editor without `documents` in its API mock, it still passes: `useOwnerDocuments` and `DocumentModal` load through `Promise.resolve().then(...)`, so a missing mock only marks the list as failed.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/TicketEditor.tsx web/src/components/TicketEditor.test.tsx web/src/components/TicketCard.tsx web/src/components/TicketCard.test.tsx
git commit -m "feat: show documents in the ticket editor and on cards

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: End-to-end check on a throwaway board

**Files:** none changed unless a defect turns up.

- [ ] **Step 1: Seed a throwaway board**

```bash
mkdir -p .tmp && rm -f .tmp/t1.db
go run ./cmd/taskboard --db ./.tmp/t1.db project create Demo --prefix DEMO
go run ./cmd/taskboard --db ./.tmp/t1.db ticket create --project DEMO --title "Design documents"
printf '# Storage\n\n- one owner per document\n- names unique per ticket\n' > .tmp/design-spec.md
go run ./cmd/taskboard --db ./.tmp/t1.db doc add DEMO-1 --file .tmp/design-spec.md
printf 'Findings go here.\n' | go run ./cmd/taskboard --db ./.tmp/t1.db doc add DEMO-1 --file - --name "Research notes"
```

Expected: two "Added document …" lines, each with a `http://localhost:3011/?ticket=DEMO-1&doc=…` link.

- [ ] **Step 2: Run the dev server**

```bash
make frontend && make dev DEV_DB=./.tmp/t1.db DEV_PORT=3011
```

- [ ] **Step 3: Check in the browser pane (`http://localhost:3011/?project=DEMO`)**

- The DEMO-1 card shows a paperclip with 2.
- Open DEMO-1: the Documents section lists `design-spec.md` and `Research notes.md` in that order, with sizes.
- Click `design-spec.md`: the modal opens, the URL gains `&doc=design-spec.md`, the heading "Storage" renders.
- Copy the URL into a new tab: the same ticket and document open.
- Back closes the document; Back again closes the editor.
- In a terminal, `go run ./cmd/taskboard --db ./.tmp/t1.db doc rename design-spec --ticket DEMO-1 Spec` while the document is open: the modal stays open and the URL becomes `doc=Spec.md`.
- `printf '# Changed\n' | go run ./cmd/taskboard --db ./.tmp/t1.db doc write Spec --ticket DEMO-1 --file -`: the open modal shows "Changed" within a second or two.
- Rename `Research notes` to `bad.name` in the list: the error "Use letters, digits, spaces, _ and - only." shows and nothing is saved.
- Download a document: the file is named `Spec.md`.
- Delete `Research notes.md` from the modal: the confirm reads "Delete Research notes.md? This can't be undone."; Escape cancels only the confirm; Delete removes it and the card's count drops to 1.
- Open `http://localhost:3011/?ticket=DEMO-1&doc=Spec.html`: the editor opens with "Couldn't find Spec.html on this ticket." and no modal.

- [ ] **Step 4: Stop the server by PID**

```bash
kill $(lsof -t -- ./.tmp/t1.db)
```

- [ ] **Step 5: Final suite**

Run: `go test ./... && cd web && npm test && npm run lint && npx tsc -b`
Expected: PASS.
