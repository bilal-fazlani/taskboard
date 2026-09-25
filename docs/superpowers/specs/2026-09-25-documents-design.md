# Documents on tickets and epics — design

Date: 2026-09-25
Status: draft, awaiting review

## Summary

Agents and users can attach named documents to tickets, and later to epics.
A document is markdown or HTML. Users preview it in a large modal, download it,
rename it and delete it; agents do all of that and write its content. Users
gain editing in a later ticket.

The work ships as six tickets, in this order:

0. **Back and Forward through tickets and documents.** Browser history works
   for in-app navigation.
1. **Markdown documents on tickets.** Agents create and write; users view,
   download, rename and delete.
2. **Users edit documents.** Users create, upload and edit markdown, with a
   conflict notice when someone else saved in between.
3. **HTML documents.** Shown in an isolated frame, never edited by users.
4. **Documents on epics.** Everything above, on epics, through a new epic modal.
5. **Search includes documents.** The web search matches document names and
   their readable text.

## Purpose

Agents produce longer artifacts (plans, research notes, findings, test
reports) that clutter a ticket's description and get lost in chat
transcripts. Documents keep them on the ticket or epic they belong to, where
the person supervising the agents can read them, and where the next agent
picks them up.

## Non-goals

- **No version history.** A save replaces the content. The only protection is
  the conflict notice in ticket 2.
- **No record of who last edited a document.** The last-updated time is kept.
- **No partial edits or appends by agents.** Every save sends the whole content.
- **No non-text attachments** (images, PDFs, archives).
- **No document entries in Activity.** Activity stays the status history.
- **No MCP or CLI search**, and no "matched in document X" hint in ticket 5.
- **No side-by-side compare** in the conflict notice.

## Data model

One new **documents** table. Each document belongs to exactly one owner: a
ticket, or an epic (from ticket 4). It has a name, a format (markdown, or HTML
from ticket 3), its content, and created and updated times. The table is
built for both owners and both formats in ticket 1, so tickets 3 and 4 need
no storage change beyond allowing the new values.

Deleting a ticket or an epic deletes its documents. Moving a ticket to
another project takes its documents with it. Deleting an epic deletes the
epic's documents, not its tickets' documents.

Documents live in the same SQLite database as everything else, so the CLI,
MCP and HTTP server share them as they share tickets, and `--db` isolation,
backups and the change feed cover them without extra work.

## Rules

### Names

- A name holds letters (any script), digits, spaces, underscores and hyphens.
  Nothing else: no dots, slashes or other symbols.
- A name is trimmed before it is checked. Empty, or only spaces, is refused.
- The limit is 200 characters.
- Names are unique among one owner's documents, ignoring case. Two different
  tickets may each have a "Plan"; one ticket may not have "Plan" and "plan".
- The same rules apply to create and to rename. A refused name gets a message
  that says which characters are allowed, or that the name is taken.
- **A name taken from a filename** (the CLI given a file, or a user upload)
  loses its extension, which sets the format: `.md` is markdown, `.html` and
  `.htm` are HTML (from ticket 3). Any other extension is refused. Every other
  character the name rule does not allow becomes a space, and the result is
  trimmed: `api-design_v2.md` becomes "api-design_v2", `notes v1.2.md` becomes
  "notes v1 2". A name someone types is checked strictly, never rewritten.

### Format and display

- The format is set when a document is created and never changes.
- The UI always shows a document as its name plus the format's extension,
  "Design spec.md" or "Load test report.html". The extension is display only
  and cannot be edited.
- A download is named the same way.

### Size

- A document's content can be up to 8 MB, in either format. A larger save is
  refused with a message giving the size and the limit. Nothing is truncated.

### Order and listing

- Documents are listed in the order they were added.
- Board and list payloads carry only a ticket's document count. A ticket's
  full details carry each document's name, format, size and updated time, not
  its content. Content is fetched only to view, edit or download a document.

### Addressing

- Every surface (web, HTTP, CLI, MCP) can name a document by its ID, or by its
  owner plus its name (ignoring case).
- The web URL names an open document by name plus extension:
  `?ticket=ACP-84&doc=Design spec.md` (URL-encoded). Renaming breaks old links;
  that is accepted. A `doc` value whose extension does not match the
  document's format, or that names no document, opens the ticket with a short
  "document not found" notice.

### Live updates

- Every create, save, rename and delete, from any surface, reaches open
  browsers within a second through the existing change feed.
- An open document refreshes in place when someone else saves it. If it is
  deleted, its modal closes with a short notice. While a user is editing, the
  ticket 2 conflict notice replaces the in-place refresh.

## Ticket 0: Back and Forward

Today, following a link from one ticket to another replaces the history
entry (`useTicketParam.switchTo`), so Back skips every ticket visited. This
changes to:

- Every open pushes a history entry: opening a ticket, following a link to
  another ticket (Depends on, Blocks, and later links from documents), and
  opening a document.
- Back undoes one step, Forward redoes it. Board → A → B → B's document: Back
  closes the document, Back again shows A, Back once more shows the board.
- × or Esc on a document closes only the document, like one Back.
- × or Esc on the ticket editor closes it completely and returns to the view
  it was opened from, popping every ticket and document entry it pushed, so
  history is as it was before the editor opened.
- An editor or document opened straight from a link pushed nothing; closing
  it replaces the entry with the plain view.
- Unsaved edits still ask first, whether the trigger is Back, ×, Esc or a
  link. Choosing to stay leaves the user where they were.

## Ticket 1: markdown documents on tickets

**Agents (MCP and CLI):** list a ticket's documents (in its details), read
one document's content, add a document (name, format, content), replace its
content, rename it and delete it. Every write returns the document's web link.
The CLI gains `doc` commands for the same actions. Content comes from a file
or from standard input; reading prints to standard output. A file given
without a name supplies the name and format as above.

**Users (web):**

- **Card:** a paperclip icon and the document count, next to subtask progress.
  Hidden when there are none.
- **Ticket editor:** a Documents section between Subtasks and Activity. Each
  row shows the file icon, the name with its extension, the size and the
  updated time, plus download, rename and delete. Rename happens inline and
  shows the extension beside the field, with the name-rule error under it.
  Delete asks "Delete Design spec.md? This can't be undone." With no
  documents, the section reads "No documents yet. Agents can attach them." No
  add button in this ticket.
- **Document modal:** a large modal like the ticket editor, stacked on it,
  with the ticket key, the name, size and updated time, and download, rename,
  delete and close. It renders the markdown the same way the description
  preview does, and scrolls inside the modal. Its state is in the URL.

## Ticket 2: users edit documents

- The Documents section gains **New** (asks for a name, then opens an empty
  document in edit mode) and **Upload** (a `.md` file; the name comes from the
  filename as above). A taken name is refused with a message.
- The document modal gains an **Edit** button, separate from rename. Edit
  mode has the description's Write/Preview toggle, an "unsaved" marker, and
  Save and Cancel. Esc, ×, Back or Cancel with unsaved text asks "Discard your
  changes?" first.
- **Conflict notice:** if the document changes while the user is editing,
  a warning appears at once (and again at Save, if the change arrived in
  between): "This document changed while you were editing. Your text isn't
  saved yet." The choices are **Keep editing, overwrite on save** and
  **Discard mine, load theirs**. If the document was deleted, the choices are
  **Save as a new document** and **Discard**.

## Ticket 3: HTML documents

- Agents create HTML documents with the same actions, with the format set to
  HTML; the CLI takes it from a `.html` or `.htm` filename. Upload in the web
  accepts `.html` and `.htm` too.
- HTML rows get a code-file icon. The document modal shows the page at full
  width and height in a frame.
- **Trust boundary:** the page may run its own scripts and load from the
  internet (CDNs, fonts, images), and nothing more. It is served from
  `GET /api/documents/{ref}/raw` with `Content-Security-Policy: sandbox
  allow-scripts`, and the frame carries `sandbox="allow-scripts"`: scripts
  only, the same in both places (narrowed by Bilal on 2026-09-25). There is
  no `allow-same-origin`, `allow-top-navigation`, `allow-popups`,
  `allow-forms`, `allow-modals` or `allow-downloads`, and tests assert the
  exact header and attribute and that those tokens are absent. So the page
  runs in an origin separate from the board, framed or opened on its own: it
  cannot read the board's storage or navigate the board away, its forms don't
  submit, `alert`/`confirm`/`prompt`/`print` are blocked, and it cannot start
  a download. The board's API stays out of reach because the server sends no
  CORS headers and `rejectCrossOriginWrites` refuses cross-origin writes. A
  sandboxed frame sends `Origin: null`, and a test must prove that such a
  write is refused.
- Downloading the document itself from the Documents row or the modal header
  uses the board's own download route and still works; only the page's own
  downloads are blocked.
- The frame's address carries the revision (`?rev=`), so an agent's save
  reloads it.
- Users never edit HTML, even after ticket 2. Rename, delete and download
  work as for markdown.

## Ticket 4: documents on epics

- Epics get the same documents, rules and agent actions, with an epic as the
  owner, addressed by ID or by epic plus name.
- **Epics page row:** a paperclip button with the count. The row itself still
  opens the epic's tickets.
- **Epic modal:** replaces today's small Edit epic dialog. It holds the name
  and description (with Save and Cancel, which apply only to them) and the same
  Documents section as the ticket editor. Document actions take effect at
  once, as subtasks do. The paperclip and the menu's Edit both open it.
- The epic modal has its own URL (`?epic=<name>`, plus `doc=`), which is safe
  because epic names are unique within a project. Back, Forward, links and the
  document modal work as on tickets.

## Ticket 5: search includes documents

- The web search runs in the browser over each ticket's key, title and
  description. A non-empty search now also asks the server which tickets have
  a document whose display name ("Plan.md") or readable text contains the
  text, and those tickets match too.
- The readable text is what a person reads, not how the document is written
  (changed by Bilal 2026-09-25, after the plan was written, so "style" never
  finds an HTML page just because it has a `<style>` tag):
  - **HTML:** visible text only. Tags, attribute values, `<style>`,
    `<script>`, `<noscript>`, `<template>` and comments are ignored (so are
    `<head>`, `<title>` and `<iframe>` fallback text, which never show). Text
    a script draws at runtime, such as chart labels, is not searchable.
  - **Markdown:** the rendered text. Markup (`#`, `**`, `_`, backticks, list
    and table syntax) is ignored, and so are link and image addresses; link
    text, image alt text and the contents of code blocks and inline code
    stay. Raw HTML inside markdown reads as written, since the web shows it
    as literal text.
  - Block elements (paragraphs, headings, list items, table cells, line
    breaks) separate words; inline elements do not, so `<b>sty</b>le` reads
    "style". Entities are decoded.
- Matching is the same as the ticket search: the text can appear anywhere,
  even inside a word ("style" matches "stylesheet"), ignoring case with a
  Unicode fold done in Go, since SQLite folds ASCII only.
- The server works out a document's readable text when it is created and
  whenever its content is saved (a rename leaves it alone), and keeps it with
  the document, so a search never parses documents that can be 8 MB. Opening
  the database fills in the text of documents that have none, or whose text
  is from an older revision, so documents that exist when this ships become
  searchable, as do any written later by an older build.
- `GET /api/documents/search?q=&projectId=` returns the matching ticket ids.
  The browser asks it 250 ms after typing settles and again on each live
  change, keeping the last answer while the next loads. All three views
  (Kanban, Table, Dependencies) use it, and the "N of M tickets" count
  includes document matches.
- Only tickets' own documents count. Epic documents are not searched.
- No visible UI change: a ticket matched through a document shows up like any
  other result. MCP and the CLI get no search.

## Error handling

- Invalid names, taken names, unknown formats and oversize content are refused
  on every surface with a message that says what to change. The store enforces
  every rule, so the web, HTTP, CLI and MCP cannot disagree.
- A document named by owner plus name that does not exist is a not-found
  error, not an empty result.
- A write to a document deleted in the meantime is a not-found error. The web
  reports it with the ticket 2 notice, or a short message in ticket 1.

## Testing

- **Store (Go, `t.TempDir()`):** name rules, uniqueness ignoring case per
  owner, filename-to-name conversion, the 8 MB limit, format fixed after
  create, order, cascade on ticket and epic delete, documents following a
  moved ticket, lookup by ID and by owner plus name.
- **HTTP, CLI and MCP:** each action end to end against a temp database,
  including errors, the download filename, and the link returned on writes.
- **Web (mocked API):** card badge, Documents section, rename validation,
  delete confirmation, document modal, the `doc` URL parameter (including a
  mismatched extension), history (ticket 0's Back/Forward/close sequences),
  live refresh and deletion of an open document, edit mode and both conflict
  paths, the HTML frame's sandbox attributes, the epic modal and its URL, and
  the search merge.
- Manual checks run against a seeded throwaway database with `--db ./.tmp/…`
  on port 3011 or above, never the live board.
