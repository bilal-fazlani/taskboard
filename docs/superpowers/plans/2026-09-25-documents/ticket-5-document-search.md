# Ticket 5: Search includes documents — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The web search box also matches tickets whose documents' names or content contain the text, on every ticket view.

**Architecture:** The search stays in the browser for key, title and description. For document text, which can be megabytes, the browser asks a new `GET /api/documents/search?q=&projectId=` which ticket ids match, debounced and refreshed on the live change feed, and `matchesFilters` takes that set as an extra way to match. Epic documents are not searched.

**Tech Stack:** Go, React 19, Vitest + jsdom.

**Spec:** `docs/superpowers/specs/2026-09-25-documents-design.md`, section "Ticket 5".

**Depends on:** tickets 1 and 4 (epic documents must be excluded).

## Global constraints

See [README.md](README.md#global-constraints). Matching is a substring match ignoring case with Unicode folding (Go `strings.ToLower`, JS `toLowerCase`), over the display name (`Design spec.md`) and the content. No visible UI change.

## Review Focus

- `ÉTUDE` must find a document containing `étude`: SQLite's `LIKE`/`lower()` fold ASCII only, so matching happens in Go. Pinned in Task 1.
- An epic document containing the text must not make any ticket match. Pinned in Task 1.
- While the server's answer for a new query is on its way, cards matched by the previous answer stay (no flicker to empty and back). Pinned in Task 2.
- A search for text only a document contains must count the ticket in "N of M tickets". Pinned in Task 3.
- An empty or whitespace search never calls the server. Pinned in Task 2.

---

### Task 1: Store search and the HTTP route

**Files:**
- Modify: `internal/db/documents.go`
- Modify: `internal/server/documents.go`, `internal/server/server.go`
- Test: `internal/db/documents_test.go`, `internal/server/documents_test.go`

**Interfaces:**
- Produces:
  ```go
  func (s *Store) SearchDocumentTickets(q, projectRef string) ([]string, error) // never nil; sorted by ticket id
  ```
  `GET /api/documents/search?q=<text>&projectId=<id or prefix>` → `200 {"ticketIds": [...]}`.

- [ ] **Step 1: Write the failing tests**

```go
func TestSearchDocumentTickets(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	other := seedProject(t, s, "Other", "OTH")
	named := seedTicket(t, s, p.ID, "Named")
	inContent := seedTicket(t, s, p.ID, "In content")
	seedTicket(t, s, p.ID, "No documents")
	elsewhere := seedTicket(t, s, other.ID, "Elsewhere")
	e := seedEpic(t, s, p.ID, "Launch")

	seedDocument(t, s, named.ID, "Storage plan", "nothing here")
	seedDocument(t, s, inContent.ID, "Notes", "We compared the ÉTUDE results.")
	seedDocument(t, s, elsewhere.ID, "Storage", "")
	if _, err := s.CreateDocument(models.CreateDocumentRequest{EpicID: e.ID, Name: "Epic storage", Content: "étude"}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		q, project string
		want       []string
	}{
		{"storage", "DOC", []string{named.ID}},
		{"  STORAGE ", "", sortedIDs(named.ID, elsewhere.ID)},
		{"étude", "doc", []string{inContent.ID}},
		{"plan.md", "DOC", []string{named.ID}},
		{"nothing here", "DOC", []string{named.ID}},
		{"absent", "DOC", []string{}},
		{"   ", "DOC", []string{}},
		{"storage", "NOPE", []string{}},
	} {
		got, err := s.SearchDocumentTickets(tc.q, tc.project)
		if err != nil {
			t.Fatalf("search %q: %v", tc.q, err)
		}
		if got == nil || !slices.Equal(got, tc.want) {
			t.Errorf("search %q in %q = %#v, want %#v", tc.q, tc.project, got, tc.want)
		}
	}
}

func sortedIDs(ids ...string) []string {
	slices.Sort(ids)
	return ids
}
```

(add `"slices"` to the test file's imports.)

```go
func TestSearchDocumentsOverHTTP(t *testing.T) {
	r := serve(t)
	tk, _ := seedTicketDocument(t, r)

	type result struct {
		TicketIDs []string `json:"ticketIds"`
	}
	got, status := doRequest[result](t, http.MethodGet, r.url+"/api/documents/search?q=SPEC&projectId=DOC", "")
	if status != http.StatusOK || len(got.TicketIDs) != 1 || got.TicketIDs[0] != tk.ID {
		t.Fatalf("search: %d %+v", status, got)
	}
	got, status = doRequest[result](t, http.MethodGet, r.url+"/api/documents/search?q=", "")
	if status != http.StatusOK || got.TicketIDs == nil || len(got.TicketIDs) != 0 {
		t.Fatalf("empty search: %d %#v", status, got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db ./internal/server -run 'Search' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`internal/db/documents.go`:

```go
// SearchDocumentTickets returns the ids of the tickets that have a document
// whose display name ("Plan.md") or content contains q, ignoring case, in
// one project when projectRef (id or prefix) is given. Epic documents are
// not searched. q is trimmed; an empty q, or a project that matches nothing,
// matches no tickets. Folding happens here rather than in SQL, because
// SQLite's lower() and LIKE fold ASCII only. The result is never nil.
func (s *Store) SearchDocumentTickets(q, projectRef string) ([]string, error) {
	ids := []string{}
	needle := strings.ToLower(strings.TrimSpace(q))
	if needle == "" {
		return ids, nil
	}
	query := `SELECT d.ticket_id, d.name, d.format, d.content FROM documents d
		JOIN tickets t ON t.id = d.ticket_id`
	var args []any
	if strings.TrimSpace(projectRef) != "" {
		projectID, ok, err := resolveProjectFilterID(s.db, projectRef)
		if err != nil {
			return nil, err
		}
		if !ok {
			return ids, nil
		}
		query += " WHERE t.project_id = ?"
		args = append(args, projectID)
	}
	query += " ORDER BY d.ticket_id"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("searching documents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID, name, format, content string
		if err := rows.Scan(&ticketID, &name, &format, &content); err != nil {
			return nil, err
		}
		if len(ids) > 0 && ids[len(ids)-1] == ticketID {
			continue
		}
		if strings.Contains(strings.ToLower(models.DocumentDisplayName(name, format)), needle) ||
			strings.Contains(strings.ToLower(content), needle) {
			ids = append(ids, ticketID)
		}
	}
	return ids, rows.Err()
}
```

`internal/server/documents.go`:

```go
func (s *Server) searchDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ids, err := s.store.SearchDocumentTickets(q.Get("q"), q.Get("projectId"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"ticketIds": ids})
}
```

`setupRoutes`: in `/documents` add `r.Get("/search", s.searchDocuments)` (chi prefers the static segment over `{ref}`).

- [ ] **Step 4: Run the tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/documents.go internal/db/documents_test.go internal/server/documents.go internal/server/server.go internal/server/documents_test.go
git commit -m "feat: find tickets by their documents' names and content

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `matchesFilters` takes document matches, and `useDocumentMatches`

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/lib/filters.ts` (+ `filters.test.ts`)
- Create: `web/src/hooks/useDocumentMatches.ts` (+ `useDocumentMatches.dom.test.tsx`)

**Interfaces:**
- Produces:
  ```ts
  api.documents.search(q: string, projectId: string): Promise<{ ticketIds: string[] }>
  // FilterableTicket gains id?: string
  matchesFilters(ticket, filters, docMatches?: ReadonlySet<string> | null): boolean
  export const DOCUMENT_SEARCH_DEBOUNCE_MS = 250;
  useDocumentMatches(q: string, project: string): ReadonlySet<string> | null
  ```

- [ ] **Step 1: Write the failing tests**

In `web/src/lib/filters.test.ts`, inside `describe("matchesFilters")`:

```ts
  it("matches the search through a ticket's documents too", () => {
    const t = ticket({ id: "t1", title: "Plain", description: "" });
    expect(matchesFilters(t, f({ q: "storage" }))).toBe(false);
    expect(matchesFilters(t, f({ q: "storage" }), new Set(["t1"]))).toBe(true);
    expect(matchesFilters(t, f({ q: "storage" }), new Set(["other"]))).toBe(false);
    // Document matches never widen the other filters.
    expect(matchesFilters(t, f({ q: "storage", status: "done" }), new Set(["t1"]))).toBe(false);
  });
```

```tsx
// web/src/hooks/useDocumentMatches.dom.test.tsx
// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";

const mockApi = vi.hoisted(() => ({ documents: { search: vi.fn() } }));
vi.mock("../api/client", () => ({ api: mockApi }));
vi.mock("./useLiveRefresh", () => ({ useLiveRefresh: () => {} }));

import { DOCUMENT_SEARCH_DEBOUNCE_MS, useDocumentMatches } from "./useDocumentMatches";

let result: ReadonlySet<string> | null = null;
function Harness({ q }: { q: string }) {
  result = useDocumentMatches(q, "ACP");
  return null;
}

beforeEach(() => {
  vi.useFakeTimers();
  result = null;
});
afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

async function flush(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

describe("useDocumentMatches", () => {
  it("asks once for the settled query, and keeps the last answer while the next loads", async () => {
    mockApi.documents.search.mockResolvedValueOnce({ ticketIds: ["t1"] });
    const view = render(<Harness q="sto" />);
    view.rerender(<Harness q="stor" />);
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    expect(mockApi.documents.search).toHaveBeenCalledTimes(1);
    expect(mockApi.documents.search).toHaveBeenCalledWith("stor", "ACP");
    expect([...(result ?? [])]).toEqual(["t1"]);

    mockApi.documents.search.mockResolvedValueOnce({ ticketIds: ["t2"] });
    view.rerender(<Harness q="storage" />);
    expect([...(result ?? [])]).toEqual(["t1"]);
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    expect([...(result ?? [])]).toEqual(["t2"]);
  });

  it("never asks for an empty search", async () => {
    render(<Harness q="   " />);
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS * 2);
    expect(mockApi.documents.search).not.toHaveBeenCalled();
    expect(result).toBeNull();
  });

  it("keeps matching on the text alone when the server fails", async () => {
    mockApi.documents.search.mockRejectedValue(new Error("API error 500: x"));
    render(<Harness q="storage" />);
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    expect(result).toBeNull();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/filters.test.ts src/hooks/useDocumentMatches.dom.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement**

`web/src/api/client.ts`, in `api.documents`:

```ts
    search: (q: string, projectId: string) =>
      request<{ ticketIds: string[] }>(
        `/api/documents/search?${new URLSearchParams({ q, projectId }).toString()}`,
      ),
```

`web/src/lib/filters.ts`: add `id?: string;` to `FilterableTicket` (first field, with the comment `/** Needed only to match the search through the ticket's documents. */`), and change `matchesFilters`:

```ts
/**
 * … (keep the existing paragraph) …
 * `docMatches` holds the ids of tickets whose documents match the search
 * (see useDocumentMatches); it widens only the search, never another filter.
 */
export function matchesFilters(
  ticket: FilterableTicket,
  filters: Filters,
  docMatches?: ReadonlySet<string> | null,
): boolean {
  // … every filter check as it is …
  const q = filters.q.trim().toLowerCase();
  if (q) {
    const haystacks = [ticketKey(ticket), ticket.title, ticket.description ?? ""];
    const inText = haystacks.some((h) => h.toLowerCase().includes(q));
    const inDocuments = ticket.id !== undefined && docMatches?.has(ticket.id) === true;
    if (!inText && !inDocuments) return false;
  }
  return true;
}
```

`web/src/hooks/useDocumentMatches.ts`:

```ts
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import { useLiveRefresh } from "./useLiveRefresh";

/** How long the search waits after the last keystroke before asking. */
export const DOCUMENT_SEARCH_DEBOUNCE_MS = 250;

/**
 * The ids of the tickets in `project` whose documents' names or content hold
 * the search text, for matchesFilters. Document content can be megabytes, so
 * the server searches it; this asks once the typing settles and again on
 * every live change. Null for an empty search, and until a first answer
 * arrives; after that the last answer stands while the next one loads, so
 * cards matched by a document don't blink out and back while typing. A
 * failed request leaves the search to the text the browser already has.
 */
export function useDocumentMatches(q: string, project: string): ReadonlySet<string> | null {
  const query = q.trim();
  const [ids, setIds] = useState<ReadonlySet<string> | null>(null);
  const seq = useRef(0);

  const load = useCallback(() => {
    if (!query) return;
    const n = ++seq.current;
    Promise.resolve()
      .then(() => api.documents.search(query, project))
      .then((result) => {
        if (n === seq.current) setIds(new Set(result?.ticketIds ?? []));
      })
      .catch(() => {});
  }, [query, project]);

  useEffect(() => {
    if (!query) {
      seq.current++;
      setIds(null);
      return;
    }
    const timer = setTimeout(load, DOCUMENT_SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [load, query]);

  useLiveRefresh(load);

  return query ? ids : null;
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && npx vitest run src/lib/filters.test.ts src/hooks/useDocumentMatches.dom.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/client.ts web/src/lib/filters.ts web/src/lib/filters.test.ts web/src/hooks/useDocumentMatches.ts web/src/hooks/useDocumentMatches.dom.test.tsx
git commit -m "feat: let the search match tickets through their documents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Every ticket view uses the document matches

**Files:**
- Modify: `web/src/pages/Board.tsx`, `web/src/pages/Tickets.tsx`, `web/src/pages/Graph.tsx`
- Test: `web/src/pages/projectScope.dom.test.tsx`

- [ ] **Step 1: Write the failing test**

In `web/src/pages/projectScope.dom.test.tsx`, add `documents: { search: vi.fn() }` to `mockApi`, default it in `beforeEach` with `mockApi.documents.search.mockResolvedValue({ ticketIds: [] });`, and add inside the `describe.each(views)` block (so it runs on all three views):

```tsx
  it("finds a ticket by text only its documents hold", async () => {
    mockApi.documents.search.mockImplementation((q: string) =>
      Promise.resolve({ ticketIds: q === "rollout" ? [ACP2.id] : [] }),
    );
    await mount(page(), `${path}?project=ACP&q=rollout`);
    // The document search is debounced; let it run.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300));
    });
    expect(count()).toBe("1 of 3 tickets");
    expect(mockApi.documents.search).toHaveBeenCalledWith("rollout", "ACP");
  });
```

(If the Dependencies view reports its count differently when a search dims rather than hides, assert on the view's own matched-count text the same way the file's "dims the cards the other filters don't match" test does.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/projectScope.dom.test.tsx -t "only its documents"`
Expected: FAIL, `0 of 3 tickets`.

- [ ] **Step 3: Wire the three pages**

In each of `Board.tsx`, `Tickets.tsx` and `Graph.tsx`:

1. `import { useDocumentMatches } from "../hooks/useDocumentMatches";`
2. Right after `const { filters } = filterState;`: `const docMatches = useDocumentMatches(filters.q, filters.project);`
3. Pass `docMatches` as the third argument of every `matchesFilters(…, filters)` call in the file, and add `docMatches` to the dependency list of any `useMemo`/`useCallback` wrapping such a call:
   - `Board.tsx`: `isShown` and `shownCount`.
   - `Tickets.tsx`: the `filtered` memo.
   - `Graph.tsx`: the `hiding` filter memo and the matched-ids set.

- [ ] **Step 4: Run the web suite, lint and types**

Run: `cd web && npm test && npm run lint && npx tsc -b`
Expected: PASS.

- [ ] **Step 5: Manual check on a throwaway board**

```bash
mkdir -p .tmp && rm -f .tmp/t5.db
go run ./cmd/taskboard --db ./.tmp/t5.db project create Demo --prefix DEMO
go run ./cmd/taskboard --db ./.tmp/t5.db ticket create --project DEMO --title "First"
go run ./cmd/taskboard --db ./.tmp/t5.db ticket create --project DEMO --title "Second"
printf 'The étude covers rollout risk.\n' | go run ./cmd/taskboard --db ./.tmp/t5.db doc add DEMO-2 --file - --name "Findings"
make frontend && make dev DEV_DB=./.tmp/t5.db DEV_PORT=3011
```

On Kanban, Table and Dependencies for `?project=DEMO`: typing `ÉTUDE` leaves DEMO-2 showing and the count reads "1 of 2 tickets"; `findings.md` also finds it; `First` still finds DEMO-1 by title. Then `printf 'nothing\n' | go run ./cmd/taskboard --db ./.tmp/t5.db doc write Findings --ticket DEMO-2 --file -`: with `ÉTUDE` still typed, DEMO-2 drops out within a second or two. Stop the server with `kill $(lsof -t -- ./.tmp/t5.db)`.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/Board.tsx web/src/pages/Tickets.tsx web/src/pages/Graph.tsx web/src/pages/projectScope.dom.test.tsx
git commit -m "feat: search ticket views through document content

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
