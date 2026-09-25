# Documents on tickets and epics: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agents and users attach named markdown and HTML documents to tickets and epics, preview them in a large modal addressed by the URL, download, rename, delete and (users, markdown only) edit them, with browser Back/Forward working through tickets and documents and the web search matching document content.

**Architecture:** One new `documents` table owned by a ticket or an epic, with every rule enforced in `internal/db` so HTTP, CLI and MCP agree. The web UI reads documents over new `/api/documents` routes and puts the open document in a `doc` query parameter. A shared overlay-history hook makes each opened ticket, document and epic its own browser history entry.

**Tech Stack:** Go 1.x, chi router, modernc SQLite, cobra CLI, hand-rolled MCP JSON-RPC; React 19, React Router 7, Tailwind 4, react-markdown, lucide-react, Vitest with jsdom.

**Spec:** `docs/superpowers/specs/2026-09-25-documents-design.md`

## The six tickets

Each ticket is its own plan file and ships on its own, in this order. Later
files assume the earlier ones have landed and name the functions they add.

| # | File | Delivers |
|---|------|----------|
| 0 | [ticket-0-history.md](ticket-0-history.md) | Back and Forward through tickets (and, later, documents and epics) |
| 1 | [ticket-1-markdown-documents.md](ticket-1-markdown-documents.md) | Markdown documents on tickets: store, HTTP, MCP, CLI, card badge, Documents section, document modal |
| 2 | [ticket-2-user-editing.md](ticket-2-user-editing.md) | Users create, upload and edit markdown documents, with the conflict notice |
| 3 | [ticket-3-html-documents.md](ticket-3-html-documents.md) | HTML documents in a sandboxed frame |
| 4 | [ticket-4-epic-documents.md](ticket-4-epic-documents.md) | Documents on epics, the epic modal and its URL |
| 5 | [ticket-5-document-search.md](ticket-5-document-search.md) | The web search matches document names and content |

## File map (all tickets)

**Go**
- `internal/db/migrations/008_documents.sql` (T1): the table, its indexes and constraints.
- `internal/models/document.go` (T1, T3, T4): formats, size limit, meta and request types, display name, size text.
- `internal/db/documents.go` (T1–T5): name rules, filename conversion, CRUD, lookup by id or owner plus name, counts, search.
- `internal/db/store.go` (T1): `ClearData`, `attachListDetails`, `GetTicket` carry documents.
- `internal/db/epics.go` (T4): epics carry document counts and lists.
- `internal/server/documents.go` (T1–T5): the `/api/documents` handlers.
- `internal/server/server.go` (T1–T5): route registration only.
- `internal/mcp/documents.go` (T1, T3, T4): document tools; `internal/mcp/mcp.go` delegates to it.
- `internal/cli/document.go` (T1, T3, T4): `taskboard doc …`; `internal/cli/root.go` registers it.
- `internal/weburl/weburl.go` (T1, T4): document links.

**Web**
- `web/src/lib/overlayHistory.ts`, `web/src/hooks/useOverlayHistory.ts` (T0): history entries for overlays.
- `web/src/hooks/useTicketParam.ts`, `useFilters.ts`, `useUnmatched.ts`, `web/src/lib/latestSearch.ts` (T0).
- `web/src/api/client.ts` (T1–T5): document types and calls.
- `web/src/lib/documents.ts` (T1–T3): display name, name rule, size text, `doc` parameter, filename conversion.
- `web/src/hooks/useOwnerDocuments.ts`, `useDocParam.ts` (T1, T2, T4).
- `web/src/components/DocumentsSection.tsx`, `DocumentModal.tsx`, `DocumentRenameField.tsx`, `DeleteDocumentConfirm.tsx` (T1–T4).
- `web/src/components/TicketEditor.tsx`, `TicketCard.tsx` (T1).
- `web/src/pages/Epics.tsx`, `web/src/components/EpicModal.tsx`, `web/src/hooks/useEpicParam.ts` (T4).
- `web/src/lib/filters.ts`, `web/src/hooks/useDocumentMatches.ts`, the three ticket pages (T5).

## Global constraints

These apply to every task in every ticket file.

- **Live board safety (CLAUDE.md):** never touch `~/.local/bin/taskboard` or port 3010. Every manual run passes `--db ./.tmp/<name>.db` and uses port 3011 or above. Never run `make install`. Stop processes only by the PID you started or by `lsof -t -- <own db path>`.
- Go tests use `t.TempDir()` databases. Web tests mock `../api/client` and never reach a server.
- Document names: letters (any script, combining marks included), digits, spaces, `_` and `-` only; trimmed first; empty or only spaces refused; at most 200 characters; unique per owner ignoring case (Unicode-aware, `strings.EqualFold` in Go, `toLowerCase` in TS).
- Formats: `markdown` (T1), `html` (T3). Set at creation, never changed. Display extension `.md` / `.html`.
- Filename to name: extension removed and mapped to a format (`.md`; `.html`/`.htm` from T3); any other character the name rule refuses becomes a space; then trimmed. A typed name is never rewritten.
- Size limit: 8 MB (`8 << 20` bytes of UTF-8) per document. Larger saves are refused, never truncated.
- Documents are listed in the order they were added.
- The web `doc` parameter is the display name, `doc=Design spec.md`. A mismatched extension or unknown name is "not found".
- No Activity entries for documents. No version history. Saves replace the whole content.
- Error texts are the whole message and identical on every surface (the store produces them).
- Commits: conventional prefix (`feat:`, `fix:`, `test:`, `refactor:`), no ticket keys, and end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Run `go test ./...` and `cd web && npm test` before a ticket is called done; `cd web && npm run lint` and `npx tsc -b` must pass too.
