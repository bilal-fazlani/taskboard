# Labels, Dependencies, and Repo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose ticket labels and task dependencies through every surface, add a free-form repo string to tickets, and delete the teams feature.

**Architecture:** The database already has `labels`, `ticket_labels`, and `ticket_dependencies` from migration 001, and the store already reads and writes them. Work proceeds bottom-up: two migrations, then store-level name and key resolution so callers pass human-readable values instead of internal IDs, then the three consuming surfaces (HTTP, MCP, CLI), then the web UI. Teams is removed first because deleting `Ticket.TeamID` breaks every layer at once, and a green tree is easier to keep if that break happens in a single task.

**Tech Stack:** Go 1.24, `modernc.org/sqlite` v1.45.0, chi router, cobra CLI, React 19 + TypeScript + Vite + Tailwind, lucide-react icons.

**Spec:** `docs/superpowers/specs/2026-09-08-labels-dependencies-repo-design.md`

## Global Constraints

- **Never open the live database.** No test, build, or command may touch `~/Library/Application Support/taskboard/taskboard.db`, the path `db.DefaultDBPath()` returns. Tests construct stores with `OpenAt(filepath.Join(t.TempDir(), "test.db"))`. Any manual run of the binary passes `--db ./.tmp/dev.db`. Never run a bare `taskboard start`, `taskboard ticket`, or `taskboard mcp` from a dev build.
- **Dependencies are informational.** No surface may block, warn about, or reject a status change because a dependency is unfinished. No cycle detection.
- **Labels are global**, not scoped per project. Default color for an auto-created label is `#6B7280`.
- **Replace semantics on update.** A non-nil `labels` or `dependsOn` slice replaces the whole set. `nil` leaves the existing set untouched.
- **Cross-project dependencies are allowed.** Display keys resolve on prefix and number together.
- **Migration statement order is load-bearing.** SQLite refuses to drop an indexed column, so the index is dropped first.
- Go tests run with `go test ./...`. The web build is verified with `cd web && npx tsc -b && npm run build`.
- Commit after every task. Conventional commit prefixes (`feat:`, `refactor:`, `test:`, `docs:`).

---

## File Structure

**Created:**
- `internal/db/migrations/003_drop_teams.sql` — removes the teams table and column
- `internal/db/migrations/004_add_ticket_repo.sql` — adds `tickets.repo`, its index, and the reverse dependency index
- `internal/db/store_test.go` — store tests and the temp-database harness
- `internal/cli/label.go` — `taskboard label list|create|delete`
- `web/src/pages/Labels.tsx` — label management page
- `web/src/components/LabelPicker.tsx` — chip list with type-to-create
- `web/src/components/DependencyPicker.tsx` — dependency rows plus ticket search
- `web/src/components/DependencyBand.tsx` — the tinted band on board cards

**Deleted:**
- `internal/cli/team.go`
- `web/src/pages/Teams.tsx`

**Modified:**
- `internal/models/models.go` — drop team types, add `TicketRef`, `Repo`, `DependsOn`, `Blocks`
- `internal/db/store.go` — resolution helpers, batch reads, teams removal
- `internal/server/server.go` — drop team routes, extend ticket and label handlers
- `internal/mcp/mcp.go` — drop 5 team tools, add `list_labels`, extend ticket tools
- `internal/cli/root.go`, `internal/cli/ticket.go` — registration and flags
- `web/src/api/client.ts`, `web/src/App.tsx`, `web/src/components/Layout.tsx`, `web/src/components/TicketPanel.tsx`, `web/src/components/CreateTicketModal.tsx`, `web/src/pages/Board.tsx`, `web/src/pages/Tickets.tsx`
- `README.md`

---

### Task 1: Test harness, migration 003, and teams removal from the Go backend

Removing `Ticket.TeamID` breaks the model, store, server, MCP, and CLI in the same compile. They are fixed together so the tree stays green. The test harness lands here because every later task needs it.

**Files:**
- Create: `internal/db/migrations/003_drop_teams.sql`
- Create: `internal/db/store_test.go`
- Modify: `internal/models/models.go`
- Modify: `internal/db/store.go`
- Modify: `internal/server/server.go`
- Modify: `internal/mcp/mcp.go`
- Modify: `internal/cli/root.go`, `internal/cli/ticket.go`
- Delete: `internal/cli/team.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `newTestStore(t *testing.T) *Store`, `seedProject(t *testing.T, s *Store, name, prefix string) *models.Project`, `seedTicket(t *testing.T, s *Store, projectID, title string) *models.Ticket`. Every later task's tests use these three.

- [ ] **Step 1: Write the failing test**

Create `internal/db/store_test.go`:

```go
package db

import (
	"path/filepath"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// newTestStore returns a Store backed by a throwaway database in the test's
// temp dir. It must never call db.Open(), which resolves to the user's real
// database.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := OpenAt(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewStore(database)
}

func seedProject(t *testing.T, s *Store, name, prefix string) *models.Project {
	t.Helper()
	p, err := s.CreateProject(models.CreateProjectRequest{Name: name, Prefix: prefix})
	if err != nil {
		t.Fatalf("seeding project %s: %v", prefix, err)
	}
	return p
}

func seedTicket(t *testing.T, s *Store, projectID, title string) *models.Ticket {
	t.Helper()
	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: projectID, Title: title})
	if err != nil {
		t.Fatalf("seeding ticket %q: %v", title, err)
	}
	return tk
}

func TestMigrationsDropTeams(t *testing.T) {
	s := newTestStore(t)

	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='teams'`,
	).Scan(&n)
	if err != nil {
		t.Fatalf("querying sqlite_master: %v", err)
	}
	if n != 0 {
		t.Fatalf("teams table still exists after migrations")
	}

	var col int
	err = s.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('tickets') WHERE name='team_id'`,
	).Scan(&col)
	if err != nil {
		t.Fatalf("querying pragma_table_info: %v", err)
	}
	if col != 0 {
		t.Fatalf("tickets.team_id still exists after migrations")
	}
}

func TestTicketRoundTripAfterTeamsRemoval(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice export")

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got == nil {
		t.Fatal("GetTicket returned nil")
	}
	if got.Title != "Invoice export" {
		t.Fatalf("title = %q, want %q", got.Title, "Invoice export")
	}
	if got.DisplayKey() != "BILL-1" {
		t.Fatalf("key = %q, want BILL-1", got.DisplayKey())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db/ -run 'TestMigrationsDropTeams|TestTicketRoundTrip' -v`

Expected: FAIL. `TestMigrationsDropTeams` reports the teams table still exists, because migration 003 does not exist yet.

- [ ] **Step 3: Write migration 003**

Create `internal/db/migrations/003_drop_teams.sql`. The index must be dropped before the column or SQLite fails with `error in index idx_tickets_team_id after drop column`. The column is dropped before the table it references.

```sql
DROP INDEX IF EXISTS idx_tickets_team_id;
ALTER TABLE tickets DROP COLUMN team_id;
DROP TABLE IF EXISTS teams;
```

- [ ] **Step 4: Remove team types from the model**

In `internal/models/models.go`, delete the `Team`, `CreateTeamRequest`, and `UpdateTeamRequest` structs entirely. Delete the `TeamID` field from `Ticket`, from `CreateTicketRequest`, from `UpdateTicketRequest`, and from `TicketFilter`.

- [ ] **Step 5: Remove teams from the store**

In `internal/db/store.go`:

Delete the `ListTeams`, `GetTeam`, `CreateTeam`, `UpdateTeam`, and `DeleteTeam` methods.

Remove `"teams"` from the `tables` slice in `ClearData`.

In `ListTickets`, delete the `filter.TeamID` branch, remove `t.team_id` from the SELECT column list, and remove `&t.TeamID` from the `rows.Scan` argument list.

In `GetTicket`, remove `t.team_id` from the SELECT and `&t.TeamID` from the `Scan`.

In `CreateTicket`, remove `TeamID: req.TeamID` from the struct literal, remove `team_id` from the INSERT column list, remove the matching `?` placeholder, and remove `t.TeamID` from the argument list.

In `UpdateTicket`, delete the `if req.TeamID != nil` branch, remove `team_id=?` from the UPDATE statement, and remove `t.TeamID` from the argument list.

- [ ] **Step 6: Remove teams from the HTTP server**

In `internal/server/server.go`, delete the entire `r.Route("/teams", ...)` block and the five handlers `listTeams`, `getTeam`, `createTeam`, `updateTeam`, and `deleteTeam`. In `listTickets`, delete the `TeamID: r.URL.Query().Get("teamId"),` line.

- [ ] **Step 7: Remove teams from MCP**

In `internal/mcp/mcp.go`, delete the five tool definitions named `list_teams`, `get_team`, `create_team`, `update_team`, and `delete_team` from the tools slice, and delete their five `case` blocks from the dispatch switch. In the `create_ticket` and `update_ticket` input schemas, delete the `"teamId"` property. In the `list_tickets` schema, delete the `"teamId"` property.

- [ ] **Step 8: Remove teams from the CLI**

Delete `internal/cli/team.go`. In `internal/cli/root.go`, remove `teamCommands()` from the command registration. In `internal/cli/ticket.go`, remove the `createTeam` variable, the `--team` flag registration, and the `if createTeam != "" { req.TeamID = &createTeam }` block. In `root.go`, the clear-command confirmation text mentions teams; change it to `"This will delete all projects, tickets, and labels. Continue? [y/N] "`.

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/db/ -v`

Expected: PASS for both tests, and a clean build with no unused-variable or undefined-field errors.

- [ ] **Step 10: Commit**

```bash
git add internal/ docs/
git commit -m "refactor: remove the teams feature and add a store test harness

Migration 003 drops idx_tickets_team_id, then tickets.team_id, then the
teams table. That order matters: SQLite refuses to drop an indexed column.

Adds internal/db/store_test.go with a temp-database harness so no test can
reach the user's real database."
```

---

### Task 2: Migration 004 and the repo field

**Files:**
- Create: `internal/db/migrations/004_add_ticket_repo.sql`
- Modify: `internal/models/models.go`, `internal/db/store.go`, `internal/server/server.go`, `internal/mcp/mcp.go`, `internal/cli/ticket.go`
- Test: `internal/db/store_test.go`

**Interfaces:**
- Consumes: `newTestStore`, `seedProject` from Task 1.
- Produces: `Ticket.Repo string`, `CreateTicketRequest.Repo string`, `UpdateTicketRequest.Repo *string`, `TicketFilter.Repo string`.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/store_test.go`:

```go
func TestTicketRepoRoundTrip(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Invoice export",
		Repo:      "acme/billing-api",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if tk.Repo != "acme/billing-api" {
		t.Fatalf("repo after create = %q, want acme/billing-api", tk.Repo)
	}

	newRepo := "acme/billing-web"
	updated, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Repo: &newRepo})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if updated.Repo != "acme/billing-web" {
		t.Fatalf("repo after update = %q, want acme/billing-web", updated.Repo)
	}

	// A nil Repo must leave the existing value alone.
	title := "Renamed"
	untouched, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title})
	if err != nil {
		t.Fatalf("UpdateTicket (title only): %v", err)
	}
	if untouched.Repo != "acme/billing-web" {
		t.Fatalf("repo was cleared by an unrelated update: %q", untouched.Repo)
	}
}

func TestListTicketsFilterByRepo(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "API work", Repo: "acme/billing-api",
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Web work", Repo: "acme/billing-web",
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := s.ListTickets(models.TicketFilter{Repo: "acme/billing-api"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(got) != 1 || got[0].Title != "API work" {
		t.Fatalf("repo filter returned %d tickets, want 1 (API work)", len(got))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db/ -run 'Repo' -v`

Expected: FAIL to compile, with `unknown field Repo in struct literal`.

- [ ] **Step 3: Write migration 004**

Create `internal/db/migrations/004_add_ticket_repo.sql`:

```sql
ALTER TABLE tickets ADD COLUMN repo TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_tickets_repo ON tickets(repo);
CREATE INDEX IF NOT EXISTS idx_ticket_deps_blocked_by ON ticket_dependencies(blocked_by_id);
```

The second index serves the reverse *blocks* lookup added in Task 3. It is not
optional: the table's primary key is `(ticket_id, blocked_by_id)`, so a query
filtering on `blocked_by_id` alone cannot use it and degrades to a full scan.
Confirm with `EXPLAIN QUERY PLAN` if you want to see the difference.

- [ ] **Step 4: Add repo to the model**

In `internal/models/models.go`, add to `Ticket` after `Priority`:

```go
Repo string `json:"repo,omitempty"`
```

Add to `CreateTicketRequest`:

```go
Repo string `json:"repo,omitempty"`
```

Add to `UpdateTicketRequest`:

```go
Repo *string `json:"repo,omitempty"`
```

Add to `TicketFilter`:

```go
Repo string
```

- [ ] **Step 5: Wire repo through the store**

In `internal/db/store.go`:

In `ListTickets`, add `t.repo` to the SELECT list and `&t.Repo` to the `rows.Scan` args, both immediately after the priority column. Add this filter branch alongside the others:

```go
if filter.Repo != "" {
	query += " AND t.repo = ?"
	args = append(args, filter.Repo)
}
```

In `GetTicket`, add `t.repo` to the SELECT and `&t.Repo` to the `Scan`, in the same position.

In `CreateTicket`, add `Repo: req.Repo` to the `models.Ticket` literal, add `repo` to the INSERT column list with a matching `?`, and add `t.Repo` to the argument list.

In `UpdateTicket`, add before the UPDATE statement:

```go
if req.Repo != nil {
	t.Repo = *req.Repo
}
```

and add `repo=?` to the SET clause with `t.Repo` in the matching argument position.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/db/ -v`

Expected: PASS, including the two new repo tests.

- [ ] **Step 7: Expose repo on the HTTP, MCP, and CLI surfaces**

In `internal/server/server.go`, add to the `listTickets` filter literal:

```go
Repo: r.URL.Query().Get("repo"),
```

In `internal/mcp/mcp.go`, add to the `create_ticket` and `update_ticket` input schema properties:

```go
"repo": {Type: "string", Description: "Free-form repository identifier, for example acme/billing-api"},
```

and to the `list_tickets` properties:

```go
"repo": {Type: "string", Description: "Filter by exact repo string"},
```

In `internal/cli/ticket.go`, declare `createRepo` alongside the other create flag variables, register it, and set it on the request:

```go
createCmd.Flags().StringVar(&createRepo, "repo", "", "repository identifier")
```

```go
req.Repo = createRepo
```

Add `--repo` to `listCmd` bound to `filter.Repo` via a `listRepo` variable:

```go
listCmd.Flags().StringVar(&listRepo, "repo", "", "filter by repo")
```

- [ ] **Step 8: Verify the build**

Run: `go build ./... && go test ./...`

Expected: clean build, all tests pass.

- [ ] **Step 9: Commit**

```bash
git add internal/
git commit -m "feat: add a free-form repo field to tickets

Migration 004 adds tickets.repo with an index, plus an index on
ticket_dependencies.blocked_by_id so the reverse lookup is not a full scan. Exposed on ticket create,
update, and list across HTTP, MCP, and CLI. Repo is a plain string with no
validation and no behavior."
```

---

### Task 3: TicketRef and both dependency directions

Replaces `Ticket.BlockedBy []string` with `DependsOn []TicketRef` and adds the reverse `Blocks []TicketRef`. Both read the same table in opposite directions.

**Files:**
- Modify: `internal/models/models.go`, `internal/db/store.go`
- Test: `internal/db/store_test.go`

**Interfaces:**
- Consumes: `newTestStore`, `seedProject`, `seedTicket` from Task 1.
- Produces: `models.TicketRef{ID, Key, Title, Status string}`, `Ticket.DependsOn []TicketRef`, `Ticket.Blocks []TicketRef`, `CreateTicketRequest.DependsOn []string`, `UpdateTicketRequest.DependsOn []string`, and the store methods `getTicketDependsOn(ticketID string) ([]models.TicketRef, error)` and `getTicketBlocks(ticketID string) ([]models.TicketRef, error)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/store_test.go`:

```go
func TestDependencyDirections(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Subscription lifecycle")
	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Portal UI",
		DependsOn: []string{blocker.ID},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	// Forward direction on the ticket that declared the dependency.
	got, err := s.GetTicket(dependent.ID)
	if err != nil {
		t.Fatalf("GetTicket(dependent): %v", err)
	}
	if len(got.DependsOn) != 1 {
		t.Fatalf("dependsOn = %d refs, want 1", len(got.DependsOn))
	}
	ref := got.DependsOn[0]
	if ref.ID != blocker.ID {
		t.Fatalf("dependsOn[0].ID = %q, want %q", ref.ID, blocker.ID)
	}
	if ref.Key != "BILL-1" {
		t.Fatalf("dependsOn[0].Key = %q, want BILL-1", ref.Key)
	}
	if ref.Title != "Subscription lifecycle" {
		t.Fatalf("dependsOn[0].Title = %q", ref.Title)
	}
	if ref.Status != "todo" {
		t.Fatalf("dependsOn[0].Status = %q, want todo", ref.Status)
	}

	// Reverse direction appears on the blocker without any write to it.
	back, err := s.GetTicket(blocker.ID)
	if err != nil {
		t.Fatalf("GetTicket(blocker): %v", err)
	}
	if len(back.Blocks) != 1 || back.Blocks[0].ID != dependent.ID {
		t.Fatalf("blocks = %+v, want one ref to the dependent ticket", back.Blocks)
	}
	if len(back.DependsOn) != 0 {
		t.Fatalf("blocker should depend on nothing, got %d", len(back.DependsOn))
	}
}

func TestDependencyStatusReflectsBlocker(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker")
	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{blocker.ID},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	if _, err := s.MoveTicket(blocker.ID, models.MoveTicketRequest{Status: "done"}); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	got, err := s.GetTicket(dependent.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.DependsOn[0].Status != "done" {
		t.Fatalf("status = %q, want done", got.DependsOn[0].Status)
	}
}

func TestDeletingBlockerRemovesReverseLink(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker")
	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{blocker.ID},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	if err := s.DeleteTicket(blocker.ID); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}

	got, err := s.GetTicket(dependent.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if len(got.DependsOn) != 0 {
		t.Fatalf("dependsOn = %d, want 0 after the blocker was deleted", len(got.DependsOn))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db/ -run 'Dependency|Blocker' -v`

Expected: FAIL to compile with `unknown field DependsOn in struct literal`.

- [ ] **Step 3: Add TicketRef and swap the model fields**

In `internal/models/models.go`, add:

```go
// TicketRef is a lightweight pointer to another ticket, carrying enough
// context for a client to render it without a second fetch.
type TicketRef struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Title  string `json:"title"`
	Status string `json:"status"`
}
```

In `Ticket`, replace `BlockedBy []string \`json:"blockedBy,omitempty"\`` with:

```go
DependsOn []TicketRef `json:"dependsOn,omitempty"`
Blocks    []TicketRef `json:"blocks,omitempty"`
```

In `CreateTicketRequest` and `UpdateTicketRequest`, rename `BlockedBy []string` to:

```go
DependsOn []string `json:"dependsOn,omitempty"`
```

Delete the now-unused `TicketDependency` struct.

- [ ] **Step 4: Write the two directional readers**

In `internal/db/store.go`, delete `getTicketBlockedBy` and add:

```go
const ticketRefSelect = `SELECT t.id,
	COALESCE(p.prefix, '') || '-' || t.number AS key,
	t.title, t.status
	FROM tickets t LEFT JOIN projects p ON t.project_id = p.id`

func scanTicketRefs(rows *sql.Rows) ([]models.TicketRef, error) {
	defer rows.Close()
	var refs []models.TicketRef
	for rows.Next() {
		var r models.TicketRef
		if err := rows.Scan(&r.ID, &r.Key, &r.Title, &r.Status); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// getTicketDependsOn returns the tickets this ticket declares a dependency on.
func (s *Store) getTicketDependsOn(ticketID string) ([]models.TicketRef, error) {
	rows, err := s.db.Query(ticketRefSelect+
		` JOIN ticket_dependencies d ON d.blocked_by_id = t.id
		WHERE d.ticket_id = ? ORDER BY t.number`, ticketID)
	if err != nil {
		return nil, err
	}
	return scanTicketRefs(rows)
}

// getTicketBlocks returns the tickets that declare a dependency on this one.
// Nothing writes this direction; it is the same table read backwards.
func (s *Store) getTicketBlocks(ticketID string) ([]models.TicketRef, error) {
	rows, err := s.db.Query(ticketRefSelect+
		` JOIN ticket_dependencies d ON d.ticket_id = t.id
		WHERE d.blocked_by_id = ? ORDER BY t.number`, ticketID)
	if err != nil {
		return nil, err
	}
	return scanTicketRefs(rows)
}
```

- [ ] **Step 5: Update the call sites**

In `GetTicket`, replace the `t.BlockedBy, _ = s.getTicketBlockedBy(t.ID)` line with:

```go
if t.DependsOn, err = s.getTicketDependsOn(t.ID); err != nil {
	return nil, err
}
if t.Blocks, err = s.getTicketBlocks(t.ID); err != nil {
	return nil, err
}
```

In `ListTickets`, replace `tickets[i].BlockedBy, _ = s.getTicketBlockedBy(...)` with a `DependsOn` read only. `Blocks` is deliberately not populated for lists, because no list view renders it:

```go
tickets[i].DependsOn, _ = s.getTicketDependsOn(tickets[i].ID)
```

In `CreateTicket` and `UpdateTicket`, rename `req.BlockedBy` to `req.DependsOn` in the two dependency-insert blocks.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/db/ -v`

Expected: PASS. `TestDeletingBlockerRemovesReverseLink` passes because `ticket_dependencies` already cascades on delete.

- [ ] **Step 7: Commit**

```bash
git add internal/
git commit -m "feat: expose ticket dependencies in both directions

Replaces BlockedBy []string with DependsOn []TicketRef and adds the reverse
Blocks []TicketRef. Both read ticket_dependencies in opposite directions, so
the two lists cannot disagree. Blocks is populated on detail reads only."
```

---

### Task 4: Resolve labels by name with auto-create

**Files:**
- Modify: `internal/db/store.go`
- Test: `internal/db/store_test.go`

**Interfaces:**
- Consumes: the Task 1 harness.
- Produces: `(s *Store) resolveLabelNames(names []string) ([]string, error)`, returning label IDs. Tasks 6, 7, and 8 call it indirectly through create and update.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/store_test.go`:

```go
func TestLabelsResolveByNameAndAutoCreate(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Invoice export",
		Labels:    []string{"bug", "backend"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(tk.Labels) != 2 {
		t.Fatalf("labels = %d, want 2", len(tk.Labels))
	}

	all, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("global labels = %d, want 2", len(all))
	}
	for _, l := range all {
		if l.Color != "#6B7280" {
			t.Fatalf("auto-created label %q color = %q, want #6B7280", l.Name, l.Color)
		}
	}
}

func TestLabelMatchingIsCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "First", Labels: []string{"Backend"},
	}); err != nil {
		t.Fatalf("CreateTicket(first): %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Second", Labels: []string{"backend"},
	}); err != nil {
		t.Fatalf("CreateTicket(second): %v", err)
	}

	all, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("labels = %d, want 1; casing must not create a duplicate", len(all))
	}
	if all[0].Name != "Backend" {
		t.Fatalf("stored name = %q, want the original casing Backend", all[0].Name)
	}
}

func TestLabelUpdateReplaceSemantics(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "T", Labels: []string{"bug", "backend"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	// A nil slice leaves labels untouched.
	title := "Renamed"
	got, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: &title})
	if err != nil {
		t.Fatalf("UpdateTicket(title): %v", err)
	}
	if len(got.Labels) != 2 {
		t.Fatalf("nil labels cleared the set: got %d, want 2", len(got.Labels))
	}

	// A non-nil slice replaces the whole set.
	got, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Labels: []string{"docs"}})
	if err != nil {
		t.Fatalf("UpdateTicket(labels): %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "docs" {
		t.Fatalf("labels = %+v, want exactly [docs]", got.Labels)
	}

	// An empty non-nil slice clears the set.
	got, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Labels: []string{}})
	if err != nil {
		t.Fatalf("UpdateTicket(empty): %v", err)
	}
	if len(got.Labels) != 0 {
		t.Fatalf("labels = %d, want 0", len(got.Labels))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db/ -run 'Label' -v`

Expected: FAIL. Labels are currently treated as raw IDs, so `ListLabels` returns 0 and the assertions on count and color fail.

**Note on the empty-slice case:** `[]string{}` and `nil` are distinguishable in Go but both marshal to a missing or empty JSON array. `json.Unmarshal` of `"labels": []` produces a non-nil empty slice, and of an absent key leaves the field `nil`, so the distinction survives the API boundary as the spec requires.

- [ ] **Step 3: Write the resolver**

In `internal/db/store.go`, add:

```go
const defaultLabelColor = "#6B7280"

// resolveLabelNames maps label names to label IDs, matching case-insensitively.
// Names with no existing label are created with the default color, keeping the
// caller's original casing. This is what lets an agent pass ["bug"] without a
// lookup round trip.
func (s *Store) resolveLabelNames(names []string) ([]string, error) {
	ids := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))

	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true

		var id string
		err := s.db.QueryRow(
			"SELECT id FROM labels WHERE LOWER(name) = ?", key,
		).Scan(&id)
		switch {
		case err == sql.ErrNoRows:
			id = newID()
			if _, err := s.db.Exec(
				"INSERT INTO labels (id, name, color) VALUES (?, ?, ?)",
				id, trimmed, defaultLabelColor,
			); err != nil {
				return nil, fmt.Errorf("creating label %q: %w", trimmed, err)
			}
		case err != nil:
			return nil, fmt.Errorf("looking up label %q: %w", trimmed, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
```

Add `"strings"` to the import block.

- [ ] **Step 4: Call the resolver from create and update**

In `CreateTicket`, replace the whole `if len(req.Labels) > 0 { ... }` block with:

```go
if len(req.Labels) > 0 {
	labelIDs, err := s.resolveLabelNames(req.Labels)
	if err != nil {
		return nil, err
	}
	for _, labelID := range labelIDs {
		if _, err := s.db.Exec(
			"INSERT OR IGNORE INTO ticket_labels (ticket_id, label_id) VALUES (?, ?)",
			t.ID, labelID,
		); err != nil {
			return nil, fmt.Errorf("attaching label: %w", err)
		}
	}
}
```

In `UpdateTicket`, replace the `if req.Labels != nil { ... }` block with:

```go
if req.Labels != nil {
	labelIDs, err := s.resolveLabelNames(req.Labels)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec("DELETE FROM ticket_labels WHERE ticket_id = ?", id); err != nil {
		return nil, fmt.Errorf("clearing labels: %w", err)
	}
	for _, labelID := range labelIDs {
		if _, err := s.db.Exec(
			"INSERT OR IGNORE INTO ticket_labels (ticket_id, label_id) VALUES (?, ?)",
			id, labelID,
		); err != nil {
			return nil, fmt.Errorf("attaching label: %w", err)
		}
	}
}
```

Both blocks return errors that the old code discarded.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/db/ -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/
git commit -m "feat: resolve ticket labels by name, creating missing ones

Label names match case-insensitively so Backend and backend are one label.
Unknown names are created with the default color. Label write errors that
were previously discarded are now returned."
```

---

### Task 5: Resolve dependencies by ID or display key

**Files:**
- Modify: `internal/db/store.go`, `internal/server/server.go`
- Test: `internal/db/store_test.go`

**Interfaces:**
- Consumes: the Task 1 harness, `TicketRef` from Task 3.
- Produces: `(s *Store) resolveTicketRefs(selfID string, refs []string) ([]string, error)`, returning ticket IDs. `selfID` is the ticket declaring the dependency, used to reject self-reference; pass `""` when the ticket has no ID yet. Also produces the exported `db.ErrInvalidInput` type, which `internal/server` uses to map a bad reference onto HTTP 400.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/store_test.go`:

```go
func TestDependencyResolvesByDisplayKey(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker") // BILL-1

	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{"BILL-1"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(dependent.DependsOn) != 1 || dependent.DependsOn[0].ID != blocker.ID {
		t.Fatalf("dependsOn = %+v, want one ref to %s", dependent.DependsOn, blocker.ID)
	}
}

func TestDependencyKeyIsCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "Blocker")

	got, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{"bill-1"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(got.DependsOn) != 1 {
		t.Fatalf("lowercase key did not resolve: %+v", got.DependsOn)
	}
}

func TestDependencyCrossProject(t *testing.T) {
	s := newTestStore(t)
	billing := seedProject(t, s, "Billing", "BILL")
	auth := seedProject(t, s, "Auth", "AUTH")
	authTicket := seedTicket(t, s, auth.ID, "Login") // AUTH-1

	got, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: billing.ID, Title: "Portal", DependsOn: []string{"AUTH-1"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(got.DependsOn) != 1 || got.DependsOn[0].ID != authTicket.ID {
		t.Fatalf("cross-project dependency did not resolve: %+v", got.DependsOn)
	}
	if got.DependsOn[0].Key != "AUTH-1" {
		t.Fatalf("key = %q, want AUTH-1", got.DependsOn[0].Key)
	}
}

func TestDependencyUnresolvableIsAnError(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	_, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []string{"NOPE-42"},
	})
	if err == nil {
		t.Fatal("expected an error for an unresolvable dependency")
	}
	if !strings.Contains(err.Error(), "NOPE-42") {
		t.Fatalf("error %q must name the offending value NOPE-42", err)
	}
}

func TestDependencySelfReferenceIsAnError(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Solo")

	_, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{
		DependsOn: []string{tk.ID},
	})
	if err == nil {
		t.Fatal("expected an error for a self-dependency")
	}
	if !strings.Contains(err.Error(), "itself") {
		t.Fatalf("error %q should explain the self-reference", err)
	}
}

func TestDependencyDuplicatesCollapse(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker")

	got, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent",
		DependsOn: []string{blocker.ID, "BILL-1", blocker.ID},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if len(got.DependsOn) != 1 {
		t.Fatalf("dependsOn = %d, want 1 after duplicates collapse", len(got.DependsOn))
	}
}
```

Add `"strings"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db/ -run 'Dependency' -v`

Expected: FAIL. Display keys are currently inserted verbatim as IDs, so `TestDependencyResolvesByDisplayKey` finds zero refs and the error cases return nil.

- [ ] **Step 3: Write the resolver**

In `internal/db/store.go`, first add an error type so callers can tell a bad
user-supplied reference from a database failure. The HTTP layer needs this to
return 400 rather than 500:

```go
// ErrInvalidInput marks a caller-supplied value that could not be resolved, as
// opposed to an internal failure. The HTTP layer maps it to 400.
type ErrInvalidInput struct{ Msg string }

func (e *ErrInvalidInput) Error() string { return e.Msg }

func invalidInput(format string, a ...any) error {
	return &ErrInvalidInput{Msg: fmt.Sprintf(format, a...)}
}
```

Then add the resolver:

```go
// resolveTicketRefs maps ticket IDs or display keys like "BILL-2" to ticket IDs.
// Keys are unique per project, so a key resolves on prefix and number together,
// which is what allows dependencies to cross projects. selfID is the ticket
// declaring the dependency and may be empty when it has no ID yet.
func (s *Store) resolveTicketRefs(selfID string, refs []string) ([]string, error) {
	ids := make([]string, 0, len(refs))
	seen := make(map[string]bool, len(refs))

	for _, ref := range refs {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			continue
		}

		id, err := s.lookupTicketRef(trimmed)
		if err != nil {
			return nil, err
		}
		if selfID != "" && id == selfID {
			return nil, invalidInput("ticket %q cannot depend on itself", trimmed)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// lookupTicketRef resolves a single reference, trying a raw ID first and then a
// PREFIX-NUMBER display key.
func (s *Store) lookupTicketRef(ref string) (string, error) {
	var id string
	err := s.db.QueryRow("SELECT id FROM tickets WHERE id = ?", ref).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("looking up ticket %q: %w", ref, err)
	}

	prefix, numStr, ok := strings.Cut(ref, "-")
	if !ok {
		return "", invalidInput("no ticket matches %q", ref)
	}
	number, convErr := strconv.Atoi(numStr)
	if convErr != nil {
		return "", invalidInput("no ticket matches %q", ref)
	}

	err = s.db.QueryRow(
		`SELECT t.id FROM tickets t
		JOIN projects p ON t.project_id = p.id
		WHERE LOWER(p.prefix) = LOWER(?) AND t.number = ?`,
		prefix, number,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", invalidInput("no ticket matches %q", ref)
	}
	if err != nil {
		return "", fmt.Errorf("looking up ticket %q: %w", ref, err)
	}
	return id, nil
}
```

Add `"strconv"` to the import block.

- [ ] **Step 4: Call the resolver from create and update**

In `CreateTicket`, replace the `if len(req.BlockedBy) > 0 { ... }` block (renamed to `req.DependsOn` in Task 3) with:

```go
if len(req.DependsOn) > 0 {
	depIDs, err := s.resolveTicketRefs(t.ID, req.DependsOn)
	if err != nil {
		return nil, err
	}
	for _, depID := range depIDs {
		if _, err := s.db.Exec(
			"INSERT OR IGNORE INTO ticket_dependencies (ticket_id, blocked_by_id) VALUES (?, ?)",
			t.ID, depID,
		); err != nil {
			return nil, fmt.Errorf("attaching dependency: %w", err)
		}
	}
}
```

In `UpdateTicket`, replace the corresponding block with:

```go
if req.DependsOn != nil {
	depIDs, err := s.resolveTicketRefs(id, req.DependsOn)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec("DELETE FROM ticket_dependencies WHERE ticket_id = ?", id); err != nil {
		return nil, fmt.Errorf("clearing dependencies: %w", err)
	}
	for _, depID := range depIDs {
		if _, err := s.db.Exec(
			"INSERT OR IGNORE INTO ticket_dependencies (ticket_id, blocked_by_id) VALUES (?, ?)",
			id, depID,
		); err != nil {
			return nil, fmt.Errorf("attaching dependency: %w", err)
		}
	}
}
```

**Ordering note:** resolution happens before the DELETE, so a failed resolution leaves the existing dependencies intact rather than wiping them.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/db/ -v`

Expected: PASS, all dependency tests including the two error cases.

- [ ] **Step 6: Map invalid input to HTTP 400**

Both ticket handlers currently return 500 for every store error, so a typo in a
dependency key reads as a server fault. In `internal/server/server.go`, add a
helper and use it in `createTicket` and `updateTicket`:

```go
// writeStoreError maps a caller's bad input to 400 and everything else to 500.
func writeStoreError(w http.ResponseWriter, err error) {
	var invalid *db.ErrInvalidInput
	if errors.As(err, &invalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
```

Add `"errors"` to the import block. In `createTicket`, replace:

```go
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
```

with:

```go
	if err != nil {
		writeStoreError(w, err)
		return
	}
```

Make the same replacement in `updateTicket`.

- [ ] **Step 7: Verify the status codes against a scratch database**

Run:

```bash
go build -o /tmp/tb-dev ./cmd/taskboard
rm -f ./.tmp/dev.db
/tmp/tb-dev --db ./.tmp/dev.db start --port 3999 --foreground &
sleep 2
P=$(curl -s -X POST localhost:3999/api/projects -H 'Content-Type: application/json' \
  -d '{"name":"Billing","prefix":"BILL"}' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
curl -s -o /dev/null -w 'bad dependency -> %{http_code}\n' -X POST localhost:3999/api/tickets \
  -H 'Content-Type: application/json' \
  -d "{\"projectId\":\"$P\",\"title\":\"T\",\"dependsOn\":[\"NOPE-42\"]}"
kill %1
```

Expected: `bad dependency -> 400`. **Port 3999 and `--db` keep this off the real instance.**

- [ ] **Step 8: Commit**

```bash
git add internal/
git commit -m "feat: resolve dependencies by ticket ID or display key

Accepts raw IDs and PREFIX-NUMBER keys, matching prefix case-insensitively
so cross-project dependencies work. Rejects self-reference and unresolvable
values, naming the offending input. Resolution runs before the delete so a
bad input cannot wipe an existing dependency set.

Adds db.ErrInvalidInput so the HTTP layer can return 400 for an unresolvable
reference instead of 500."
```

---

### Task 6: Batch the list queries

`ListTickets` currently issues three queries per ticket. A 50-ticket board runs about 150 extra round trips. This replaces the loop with two batch queries.

**Files:**
- Modify: `internal/db/store.go`
- Test: `internal/db/store_test.go`

**Interfaces:**
- Consumes: `TicketRef` from Task 3.
- Produces: `(s *Store) attachListDetails(tickets []models.Ticket) error`.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/store_test.go`:

```go
func TestListTicketsCarriesLabelsAndDependenciesButNotBlocks(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Blocker")
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID,
		Title:     "Dependent",
		Labels:    []string{"frontend"},
		DependsOn: []string{blocker.ID},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	list, err := s.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("tickets = %d, want 2", len(list))
	}

	var dependent, blockerRow models.Ticket
	for _, tk := range list {
		switch tk.Title {
		case "Dependent":
			dependent = tk
		case "Blocker":
			blockerRow = tk
		}
	}

	if len(dependent.Labels) != 1 || dependent.Labels[0].Name != "frontend" {
		t.Fatalf("labels on list = %+v, want [frontend]", dependent.Labels)
	}
	if len(dependent.DependsOn) != 1 || dependent.DependsOn[0].Key != "BILL-1" {
		t.Fatalf("dependsOn on list = %+v, want [BILL-1]", dependent.DependsOn)
	}
	if len(blockerRow.Blocks) != 0 {
		t.Fatalf("blocks must not be populated on list results, got %+v", blockerRow.Blocks)
	}
}

func TestListTicketsFilterByLabel(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Tagged", Labels: []string{"bug"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Untagged",
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := s.ListTickets(models.TicketFilter{Label: "BUG"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Tagged" {
		t.Fatalf("label filter returned %d tickets, want 1 (Tagged)", len(got))
	}
}

func TestListTicketsEmptyResultDoesNotQuery(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListTickets(models.TicketFilter{})
	if err != nil {
		t.Fatalf("ListTickets on an empty database: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("tickets = %d, want 0", len(got))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db/ -run 'ListTickets' -v`

Expected: FAIL to compile with `unknown field Label in struct literal` for `TicketFilter`.

- [ ] **Step 3: Add the label filter field**

In `internal/models/models.go`, add to `TicketFilter`:

```go
Label string
```

- [ ] **Step 4: Write the batch loader**

In `internal/db/store.go`, add:

```go
// attachListDetails fills Labels, Subtasks, and DependsOn for a page of tickets
// using one query per relation rather than one per ticket. Blocks is not filled;
// no list view renders it.
func (s *Store) attachListDetails(tickets []models.Ticket) error {
	if len(tickets) == 0 {
		return nil
	}

	ids := make([]any, len(tickets))
	index := make(map[string]int, len(tickets))
	for i, t := range tickets {
		ids[i] = t.ID
		index[t.ID] = i
		tickets[i].Labels = nil
		tickets[i].Subtasks = nil
		tickets[i].DependsOn = nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")

	labelRows, err := s.db.Query(
		`SELECT tl.ticket_id, l.id, l.name, l.color
		FROM ticket_labels tl JOIN labels l ON l.id = tl.label_id
		WHERE tl.ticket_id IN (`+placeholders+`) ORDER BY l.name`, ids...)
	if err != nil {
		return fmt.Errorf("loading labels: %w", err)
	}
	for labelRows.Next() {
		var ticketID string
		var l models.Label
		if err := labelRows.Scan(&ticketID, &l.ID, &l.Name, &l.Color); err != nil {
			labelRows.Close()
			return err
		}
		if i, ok := index[ticketID]; ok {
			tickets[i].Labels = append(tickets[i].Labels, l)
		}
	}
	labelRows.Close()
	if err := labelRows.Err(); err != nil {
		return err
	}

	subtaskRows, err := s.db.Query(
		`SELECT id, ticket_id, title, completed, position FROM subtasks
		WHERE ticket_id IN (`+placeholders+`) ORDER BY position`, ids...)
	if err != nil {
		return fmt.Errorf("loading subtasks: %w", err)
	}
	for subtaskRows.Next() {
		var st models.Subtask
		if err := subtaskRows.Scan(&st.ID, &st.TicketID, &st.Title, &st.Completed, &st.Position); err != nil {
			subtaskRows.Close()
			return err
		}
		if i, ok := index[st.TicketID]; ok {
			tickets[i].Subtasks = append(tickets[i].Subtasks, st)
		}
	}
	subtaskRows.Close()
	if err := subtaskRows.Err(); err != nil {
		return err
	}

	// The key expression MUST stay character-identical to the one in
	// ticketRefSelect (Task 3). Both have a known cosmetic flaw for an empty
	// project prefix; keeping them identical means that is one fix, not two.
	depRows, err := s.db.Query(
		`SELECT d.ticket_id, t.id,
		COALESCE(p.prefix, '') || '-' || t.number, t.title, t.status
		FROM ticket_dependencies d
		JOIN tickets t ON t.id = d.blocked_by_id
		LEFT JOIN projects p ON p.id = t.project_id
		WHERE d.ticket_id IN (`+placeholders+`) ORDER BY t.number`, ids...)
	if err != nil {
		return fmt.Errorf("loading dependencies: %w", err)
	}
	defer depRows.Close()
	for depRows.Next() {
		var ticketID string
		var r models.TicketRef
		if err := depRows.Scan(&ticketID, &r.ID, &r.Key, &r.Title, &r.Status); err != nil {
			return err
		}
		if i, ok := index[ticketID]; ok {
			tickets[i].DependsOn = append(tickets[i].DependsOn, r)
		}
	}
	return depRows.Err()
}
```

- [ ] **Step 5: Replace the per-ticket loop**

In `ListTickets`, delete the whole `for i := range tickets { ... }` block that calls `getTicketLabels`, `getTicketSubtasks`, and `getTicketDependsOn`, and replace it with:

```go
if err := s.attachListDetails(tickets); err != nil {
	return nil, err
}
```

Add the label filter branch alongside the other filters. It uses `EXISTS` so a ticket carrying several labels is not duplicated:

```go
if filter.Label != "" {
	query += ` AND EXISTS (
		SELECT 1 FROM ticket_labels tl JOIN labels l ON l.id = tl.label_id
		WHERE tl.ticket_id = t.id AND LOWER(l.name) = LOWER(?))`
	args = append(args, filter.Label)
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/db/ -v`

Expected: PASS. `TestListTicketsEmptyResultDoesNotQuery` covers the empty-slice guard, which would otherwise build `IN ()` and fail to parse.

- [ ] **Step 7: Commit**

```bash
git add internal/
git commit -m "perf: batch the list queries for labels, subtasks, and dependencies

ListTickets issued three queries per ticket. It now issues three total per
call, grouped in Go. Adds a label filter using EXISTS so a multi-label
ticket is not returned twice."
```

---

### Task 7: Label counts and the label surfaces on HTTP, MCP, and CLI

**Files:**
- Modify: `internal/models/models.go`, `internal/db/store.go`, `internal/mcp/mcp.go`
- Create: `internal/cli/label.go`
- Modify: `internal/cli/root.go`
- Test: `internal/db/store_test.go`

Do not edit `internal/server/server.go` in this task: the label ticket count reaches
HTTP through the existing `listLabels` handler unchanged. Do not edit
`internal/cli/ticket.go` either; Task 8 owns that file.

**Interfaces:**
- Consumes: `resolveLabelNames` from Task 4.
- Produces: `Label.TicketCount int`, the `list_labels` MCP tool, and the `taskboard label` command group.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/store_test.go`:

```go
func TestListLabelsIncludesTicketCount(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "One", Labels: []string{"bug", "backend"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Two", Labels: []string{"bug"},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}

	counts := map[string]int{}
	for _, l := range labels {
		counts[l.Name] = l.TicketCount
	}
	if counts["bug"] != 2 {
		t.Fatalf("bug count = %d, want 2", counts["bug"])
	}
	if counts["backend"] != 1 {
		t.Fatalf("backend count = %d, want 1", counts["backend"])
	}
}

func TestDeletingLabelDetachesItFromTickets(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Tagged", Labels: []string{"bug"},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	labels, err := s.ListLabels()
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if err := s.DeleteLabel(labels[0].ID); err != nil {
		t.Fatalf("DeleteLabel: %v", err)
	}

	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if len(got.Labels) != 0 {
		t.Fatalf("labels = %d, want 0 after the label was deleted", len(got.Labels))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/db/ -run 'Label' -v`

Expected: FAIL to compile with `l.TicketCount undefined`.

- [ ] **Step 3: Add the count to the model and query**

In `internal/models/models.go`, add to `Label`:

```go
TicketCount int `json:"ticketCount"`
```

In `internal/db/store.go`, change the `ListLabels` query to a left join so labels with no tickets still return a zero count:

```go
func (s *Store) ListLabels() ([]models.Label, error) {
	rows, err := s.db.Query(
		`SELECT l.id, l.name, l.color, COUNT(tl.ticket_id)
		FROM labels l LEFT JOIN ticket_labels tl ON tl.label_id = l.id
		GROUP BY l.id, l.name, l.color
		ORDER BY l.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []models.Label
	for rows.Next() {
		var l models.Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color, &l.TicketCount); err != nil {
			return nil, err
		}
		labels = append(labels, l)
	}
	return labels, rows.Err()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/db/ -v`

Expected: PASS. `TestDeletingLabelDetachesItFromTickets` passes on the existing `ON DELETE CASCADE`.

- [ ] **Step 5: Add the MCP list_labels tool**

In `internal/mcp/mcp.go`, add to the tools slice after the project tools:

```go
{
	Name:        "list_labels",
	Description: "List all labels with the number of tickets carrying each. Labels are global across projects.",
	InputSchema: jsonSchema{Type: "object", Properties: map[string]schemaProp{}},
},
```

Add to the dispatch switch:

```go
case "list_labels":
	return s.store.ListLabels()
```

Add `labels` and `dependsOn` to the `create_ticket` and `update_ticket` schemas:

```go
"labels": {
	Type:        "array",
	Description: "Label names. Matched case-insensitively; unknown names are created automatically.",
	Items:       &jsonSchema{Type: "string"},
},
"dependsOn": {
	Type:        "array",
	Description: "Ticket IDs or display keys like BILL-2 that this ticket depends on. Informational only: dependencies never block a status change.",
	Items:       &jsonSchema{Type: "string"},
},
```

**Note the element type.** `schemaProp` already has an `Items` field, and it is typed
`*jsonSchema`, not `*schemaProp`. Use `&jsonSchema{Type: "string"}` exactly as written
above. `&schemaProp{...}` does not compile.

Add to the `list_tickets` schema:

```go
"label": {Type: "string", Description: "Filter by label name, case-insensitive"},
```

Update the `get_ticket` description to `"Get detailed ticket information including subtasks, labels, the tickets it depends on, and the tickets it blocks"`.

- [ ] **Step 6: Add the label CLI command group**

Create `internal/cli/label.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/models"
)

func labelCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Manage labels",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List labels",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			labels, err := store.ListLabels()
			if err != nil {
				return err
			}
			if len(labels) == 0 {
				fmt.Println("No labels found.")
				return nil
			}
			for _, l := range labels {
				fmt.Printf("%-20s %-8s %d tickets (%s)\n", l.Name, l.Color, l.TicketCount, l.ID)
			}
			return nil
		},
	}

	var color string
	createCmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			l, err := store.CreateLabel(models.CreateLabelRequest{Name: args[0], Color: color})
			if err != nil {
				return err
			}
			fmt.Printf("Created label %s (%s)\n", l.Name, l.ID)
			return nil
		},
	}
	createCmd.Flags().StringVar(&color, "color", "#6B7280", "hex color")

	deleteCmd := &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a label and remove it from all tickets",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			if err := store.DeleteLabel(args[0]); err != nil {
				return err
			}
			fmt.Println("Label deleted.")
			return nil
		},
	}

	cmd.AddCommand(listCmd, createCmd, deleteCmd)
	return cmd
}
```

In `internal/cli/root.go`, register `labelCommands()` where `teamCommands()` used to be.

- [ ] **Step 7: Verify the build and the CLI against a scratch database**

Run:

```bash
go build -o /tmp/tb-dev ./cmd/taskboard
/tmp/tb-dev --db ./.tmp/dev.db label create bug --color '#ef4444'
/tmp/tb-dev --db ./.tmp/dev.db label list
```

Expected: the label is created and listed with `0 tickets`. **The `--db` flag is mandatory.** Without it the command writes to the real database.

- [ ] **Step 8: Commit**

```bash
git add internal/
git commit -m "feat: add label ticket counts and label surfaces on MCP and CLI

ListLabels reports how many tickets carry each label via a left join.
Adds the list_labels MCP tool and a taskboard label command group, and
teaches create_ticket and update_ticket to accept labels and dependsOn."
```

---

### Task 8: CLI ticket update and the new ticket flags

Labels and dependencies are unsettable from the CLI after creation, because no `ticket update` command exists.

**Files:**
- Modify: `internal/cli/ticket.go`

**Interfaces:**
- Consumes: `resolveLabelNames` and `resolveTicketRefs` indirectly through `UpdateTicket`.
- Produces: the `taskboard ticket update` command.

- [ ] **Step 1: Add the create flags**

In `internal/cli/ticket.go`, declare alongside the other create variables:

```go
var createLabels, createDependsOn []string
```

Register them and set them on the request inside `createCmd`'s `RunE`, before the `store.CreateTicket` call:

```go
createCmd.Flags().StringSliceVar(&createLabels, "label", nil, "label name (repeatable)")
createCmd.Flags().StringSliceVar(&createDependsOn, "depends-on", nil, "ticket ID or key this depends on (repeatable)")
```

```go
req.Labels = createLabels
req.DependsOn = createDependsOn
```

- [ ] **Step 2: Add the update command**

Add before the `cmd.AddCommand(...)` line:

```go
var (
	updTitle, updDescription, updStatus, updPriority, updDue, updRepo string
	updLabels, updDependsOn                                           []string
)
updateCmd := &cobra.Command{
	Use:   "update [id]",
	Short: "Update ticket fields",
	Long: "Update ticket fields. --labels and --depends-on replace the existing " +
		"set; omit a flag to leave that field untouched.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openStore()
		if err != nil {
			return err
		}

		var req models.UpdateTicketRequest
		if cmd.Flags().Changed("title") {
			req.Title = &updTitle
		}
		if cmd.Flags().Changed("description") {
			req.Description = &updDescription
		}
		if cmd.Flags().Changed("status") {
			req.Status = &updStatus
		}
		if cmd.Flags().Changed("priority") {
			req.Priority = &updPriority
		}
		if cmd.Flags().Changed("due") {
			req.DueDate = &updDue
		}
		if cmd.Flags().Changed("repo") {
			req.Repo = &updRepo
		}
		if cmd.Flags().Changed("labels") {
			req.Labels = updLabels
			if req.Labels == nil {
				req.Labels = []string{}
			}
		}
		if cmd.Flags().Changed("depends-on") {
			req.DependsOn = updDependsOn
			if req.DependsOn == nil {
				req.DependsOn = []string{}
			}
		}

		t, err := store.UpdateTicket(args[0], req)
		if err != nil {
			return err
		}
		if t == nil {
			return fmt.Errorf("ticket not found")
		}
		fmt.Printf("Updated %s: %s\n", t.DisplayKey(), t.Title)
		return nil
	},
}
updateCmd.Flags().StringVar(&updTitle, "title", "", "new title")
updateCmd.Flags().StringVar(&updDescription, "description", "", "new description")
updateCmd.Flags().StringVar(&updStatus, "status", "", "status (todo|in_progress|done)")
updateCmd.Flags().StringVar(&updPriority, "priority", "", "priority (urgent|high|medium|low)")
updateCmd.Flags().StringVar(&updDue, "due", "", "due date (YYYY-MM-DD)")
updateCmd.Flags().StringVar(&updRepo, "repo", "", "repository identifier")
updateCmd.Flags().StringSliceVar(&updLabels, "labels", nil, "replace labels (comma separated)")
updateCmd.Flags().StringSliceVar(&updDependsOn, "depends-on", nil, "replace dependencies (comma separated)")
```

Add `updateCmd` to the `cmd.AddCommand(listCmd, createCmd, moveCmd, deleteCmd)` call.

**Why `cmd.Flags().Changed`:** an unset string flag is `""`, which is indistinguishable from an intentional clear. `Changed` reports whether the user actually passed the flag, which is what maps onto the nil-versus-set pointer semantics the store expects.

- [ ] **Step 3: Enrich the list output**

Replace the print line in `listCmd`'s `RunE` loop with:

```go
for _, t := range tickets {
	line := fmt.Sprintf("[%s] %s - %s (%s", t.DisplayKey(), t.Title, t.Status, t.Priority)
	if t.Repo != "" {
		line += ", " + t.Repo
	}
	line += ")"
	if len(t.Labels) > 0 {
		names := make([]string, len(t.Labels))
		for i, l := range t.Labels {
			names[i] = l.Name
		}
		line += " [" + strings.Join(names, ", ") + "]"
	}
	if len(t.DependsOn) > 0 {
		keys := make([]string, len(t.DependsOn))
		for i, d := range t.DependsOn {
			keys[i] = d.Key
		}
		line += " depends on " + strings.Join(keys, ", ")
	}
	fmt.Printf("%s  (%s)\n", line, t.ID)
}
```

Add `"strings"` to the imports. Register the label filter flag:

```go
listCmd.Flags().StringVar(&listLabel, "label", "", "filter by label name")
```

with `listLabel` declared alongside the other list variables and passed as `Label: listLabel` in the `models.TicketFilter` literal.

- [ ] **Step 4: Verify against a scratch database**

Run:

```bash
go build -o /tmp/tb-dev ./cmd/taskboard
rm -f ./.tmp/dev.db
P=$(/tmp/tb-dev --db ./.tmp/dev.db project create Billing --prefix BILL | grep -o '[0-9A-Z]\{26\}')
/tmp/tb-dev --db ./.tmp/dev.db ticket create --project "$P" --title "Blocker"
T=$(/tmp/tb-dev --db ./.tmp/dev.db ticket create --project "$P" --title "Dependent" --label frontend --depends-on BILL-1 | grep -o '[0-9A-Z]\{26\}')
/tmp/tb-dev --db ./.tmp/dev.db ticket update "$T" --repo acme/billing-web --labels frontend,urgent
/tmp/tb-dev --db ./.tmp/dev.db ticket list --label frontend
```

Expected: the final list prints one ticket showing `acme/billing-web`, the labels `frontend, urgent`, and `depends on BILL-1`.

- [ ] **Step 5: Commit**

```bash
git add internal/
git commit -m "feat: add taskboard ticket update and label and dependency flags

ticket create gains repeatable --label and --depends-on. The new ticket
update command uses Flags().Changed so an omitted flag leaves a field
untouched while an explicit empty value clears it."
```

---

### Task 9: Remove teams from the web UI

**Files:**
- Delete: `web/src/pages/Teams.tsx`
- Modify: `web/src/api/client.ts`, `web/src/App.tsx`, `web/src/components/Layout.tsx`, `web/src/components/TicketPanel.tsx`, `web/src/components/CreateTicketModal.tsx`, `web/src/pages/Board.tsx`, `web/src/pages/Tickets.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: a `Ticket` type with no `teamId`, ready for the Task 10 field additions.

- [ ] **Step 1: Delete the page and its wiring**

Delete `web/src/pages/Teams.tsx`.

In `web/src/App.tsx`, remove the `Teams` import and the `<Route path="teams" element={<Teams />} />` line.

In `web/src/components/Layout.tsx`, remove the `{ to: "/teams", icon: Users, label: "Teams" }` entry from `navItems`, and remove `Users` from the `lucide-react` import if nothing else uses it.

- [ ] **Step 2: Remove teams from the API client**

In `web/src/api/client.ts`, delete the `Team` interface, delete `teamId?: string;` from `Ticket`, and delete the whole `teams: { ... }` group from the exported `api` object.

- [ ] **Step 3: Remove teams from the components**

In `web/src/components/TicketPanel.tsx`: remove the `teams` prop from the component's props type and signature, remove the `teamId` state and its `setTeamId` calls, remove `teamId` from the `onUpdate` payload, and delete the Team `<select>` block along with its wrapping `<div>` and label. The Due Date field that shared its two-column grid row now pairs with Repo in Task 11; until then leave Due Date spanning the row.

In `web/src/components/CreateTicketModal.tsx`: remove the `teams` prop, the team state, and the team `<select>` block.

In `web/src/pages/Board.tsx`: remove the `teams` state and its `api.teams.list()` fetch, remove `teams` from the props passed to `TicketPanel` and `CreateTicketModal`, delete the `const team = teams.find(...)` line, and delete the team `<span>` chip from the card. Update the surrounding condition `{(project || team) && (` to `{project && (`.

In `web/src/pages/Tickets.tsx`: remove the `teams` state and fetch. This table has no Team column or team filter to remove, despite what an earlier draft of this plan implied.

- [ ] **Step 4: Verify the build**

Run: `cd web && npx tsc -b && npm run build`

Expected: a clean type check and a successful production build with no unused-import or missing-prop errors.

- [ ] **Step 5: Commit**

```bash
git add web/
git commit -m "refactor: remove teams from the web UI

Deletes the Teams page, its route and sidebar entry, the Team type and API
client group, and every team select, chip, and column."
```

---

### Task 10: Web types and the board card dependency band

**Files:**
- Modify: `web/src/api/client.ts`, `web/src/pages/Board.tsx`
- Create: `web/src/components/DependencyBand.tsx`

**Interfaces:**
- Consumes: the HTTP shape produced by Tasks 2, 3, and 7.
- Produces: the `TicketRef` type, `Ticket.repo`, `Ticket.dependsOn`, `Ticket.blocks`, `Label.ticketCount`, and the `DependencyBand` component. Tasks 11 and 12 import all of them.

- [ ] **Step 1: Update the API client types**

In `web/src/api/client.ts`, add:

```ts
export interface TicketRef {
  id: string;
  key: string;
  title: string;
  status: string;
}
```

Add `ticketCount: number;` to `Label`. In `Ticket`, add `repo?: string;` and replace `blockedBy: string[];` with:

```ts
  dependsOn?: TicketRef[];
  blocks?: TicketRef[];
```

- [ ] **Step 2: Write the band component**

Create `web/src/components/DependencyBand.tsx`:

```tsx
import type { TicketRef } from "../api/client";

const MAX_KEYS = 3;

/**
 * The dependency band on a board card. Red while any dependency is unfinished,
 * neutral once all are done, absent when there are none. Deliberately a
 * full-width band rather than a pill, so it never reads as a label chip.
 */
export default function DependencyBand({ dependsOn }: { dependsOn?: TicketRef[] }) {
  if (!dependsOn || dependsOn.length === 0) return null;

  const outstanding = dependsOn.some((d) => d.status !== "done");
  const shown = dependsOn.slice(0, MAX_KEYS);
  const overflow = dependsOn.length - shown.length;

  return (
    <div
      title={dependsOn.map((d) => `${d.key} ${d.title} (${d.status})`).join("\n")}
      className={`flex items-center gap-1.5 rounded-r-md border-l-2 px-2 py-1 text-[10.5px] ${
        outstanding
          ? "border-red-500 bg-red-500/[0.08] text-red-300"
          : "border-slate-600 bg-slate-800/40 text-slate-500"
      }`}
    >
      <span>depends on</span>
      {shown.map((d, i) => (
        <span key={d.id} className="font-mono">
          {d.key}
          {i < shown.length - 1 ? "," : ""}
        </span>
      ))}
      {overflow > 0 && <span className="font-mono">+{overflow}</span>}
    </div>
  );
}
```

- [ ] **Step 3: Put the band and label chips on the card**

In `web/src/pages/Board.tsx`, import the band:

```tsx
import DependencyBand from "../components/DependencyBand";
```

Inside the card, immediately after the `<p>` holding the title, add:

```tsx
<DependencyBand dependsOn={ticket.dependsOn} />
```

Replace the project chip block (the `{project && (...)}` group left over from Task 9) with label chips. Labels are the only chips on a card:

```tsx
{ticket.labels && ticket.labels.length > 0 && (
  <div className="flex flex-wrap items-center gap-1.5">
    {ticket.labels.map((l) => (
      <span
        key={l.id}
        className="inline-flex items-center rounded px-1.5 py-0.5 text-[10.5px] font-medium"
        style={{ backgroundColor: l.color + "1f", color: l.color }}
      >
        {l.name}
      </span>
    ))}
  </div>
)}
```

Delete the now-unused `const project = projects.find(...)` line and the `FolderKanban` import if nothing else uses it.

- [ ] **Step 4: Verify the build and look at it**

Run:

```bash
cd web && npx tsc -b && npm run build && cd ..
make build
./taskboard --db ./.tmp/dev.db start --port 3999 --foreground
```

Open `http://localhost:3999`. Expected: cards show label chips, a red band on tickets whose dependencies are unfinished, and a slate band once the blocker is moved to Done. **Port 3999 and the `--db` flag keep this off the real instance on 3010.** Stop the server with Ctrl-C when done.

- [ ] **Step 5: Commit**

```bash
git add web/
git commit -m "feat: show labels and a dependency band on board cards

The band is a full-width tinted strip with a left rule, red while any
dependency is unfinished and neutral once all are done, capped at three
keys with a +N overflow. Labels are the only chips on a card."
```

---

### Task 11: Ticket panel sections

**Files:**
- Modify: `web/src/components/TicketPanel.tsx`, `web/src/components/CreateTicketModal.tsx`
- Create: `web/src/components/LabelPicker.tsx`, `web/src/components/DependencyPicker.tsx`

**Interfaces:**
- Consumes: `TicketRef`, `Label`, `Ticket` from Task 10.
- Produces: `LabelPicker` with props `{ value: string[]; onChange: (names: string[]) => void }` and `DependencyPicker` with props `{ value: TicketRef[]; onChange: (refs: TicketRef[]) => void; excludeTicketId: string }`.

- [ ] **Step 1: Write the label picker**

Create `web/src/components/LabelPicker.tsx`:

```tsx
import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { api, type Label } from "../api/client";

export default function LabelPicker({
  value,
  onChange,
}: {
  value: string[];
  onChange: (names: string[]) => void;
}) {
  const [all, setAll] = useState<Label[]>([]);
  const [draft, setDraft] = useState("");

  useEffect(() => {
    api.labels.list().then(setAll).catch(() => setAll([]));
  }, []);

  const colorFor = (name: string) =>
    all.find((l) => l.name.toLowerCase() === name.toLowerCase())?.color ?? "#6B7280";

  const add = (name: string) => {
    const trimmed = name.trim();
    if (!trimmed) return;
    if (value.some((v) => v.toLowerCase() === trimmed.toLowerCase())) {
      setDraft("");
      return;
    }
    onChange([...value, trimmed]);
    setDraft("");
  };

  const suggestions = all.filter(
    (l) =>
      l.name.toLowerCase().includes(draft.toLowerCase()) &&
      !value.some((v) => v.toLowerCase() === l.name.toLowerCase())
  );

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-1.5">
        {value.map((name) => (
          <span
            key={name}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium"
            style={{ backgroundColor: colorFor(name) + "1f", color: colorFor(name) }}
          >
            {name}
            <button
              type="button"
              onClick={() => onChange(value.filter((v) => v !== name))}
              className="opacity-60 hover:opacity-100"
              aria-label={`Remove ${name}`}
            >
              <X className="w-3 h-3" />
            </button>
          </span>
        ))}
      </div>
      <input
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            add(draft);
          }
        }}
        placeholder="Add a label and press Enter"
        className="w-full bg-slate-800 border border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-200 focus:outline-none focus:border-slate-600"
      />
      {draft && suggestions.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {suggestions.slice(0, 6).map((l) => (
            <button
              key={l.id}
              type="button"
              onClick={() => add(l.name)}
              className="rounded px-1.5 py-0.5 text-[11px]"
              style={{ backgroundColor: l.color + "1f", color: l.color }}
            >
              {l.name}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Write the dependency picker**

Create `web/src/components/DependencyPicker.tsx`:

```tsx
import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { api, type Ticket, type TicketRef } from "../api/client";

export default function DependencyPicker({
  value,
  onChange,
  excludeTicketId,
}: {
  value: TicketRef[];
  onChange: (refs: TicketRef[]) => void;
  excludeTicketId: string;
}) {
  const [all, setAll] = useState<Ticket[]>([]);
  const [query, setQuery] = useState("");

  useEffect(() => {
    api.tickets.list().then(setAll).catch(() => setAll([]));
  }, []);

  // Searches every project on purpose: dependencies may cross projects.
  const matches = query.trim()
    ? all
        .filter((t) => t.id !== excludeTicketId)
        .filter((t) => !value.some((v) => v.id === t.id))
        .filter((t) => {
          const key = `${t.projectPrefix}-${t.number}`.toLowerCase();
          const q = query.toLowerCase();
          return key.includes(q) || t.title.toLowerCase().includes(q);
        })
        .slice(0, 6)
    : [];

  return (
    <div className="space-y-2">
      {value.map((ref) => (
        <div
          key={ref.id}
          className="flex items-center gap-2 rounded-md border border-slate-800 bg-slate-900/60 px-2.5 py-1.5"
        >
          <span className="font-mono text-[11px] text-slate-400 min-w-[52px]">{ref.key}</span>
          <span className="flex-1 text-[12.5px] text-slate-300 truncate">{ref.title}</span>
          <span className="rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-400">
            {ref.status.replace("_", " ")}
          </span>
          <button
            type="button"
            onClick={() => onChange(value.filter((v) => v.id !== ref.id))}
            className="text-slate-600 hover:text-red-400"
            aria-label={`Remove dependency ${ref.key}`}
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      ))}
      <input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Search tickets to add..."
        className="w-full bg-slate-800 border border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-200 focus:outline-none focus:border-slate-600"
      />
      {matches.length > 0 && (
        <div className="rounded-md border border-slate-800 divide-y divide-slate-800">
          {matches.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => {
                onChange([
                  ...value,
                  {
                    id: t.id,
                    key: `${t.projectPrefix}-${t.number}`,
                    title: t.title,
                    status: t.status,
                  },
                ]);
                setQuery("");
              }}
              className="w-full flex items-center gap-2 px-2.5 py-1.5 text-left hover:bg-slate-800"
            >
              <span className="font-mono text-[11px] text-slate-400 min-w-[52px]">
                {t.projectPrefix}-{t.number}
              </span>
              <span className="flex-1 text-[12.5px] text-slate-300 truncate">{t.title}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 3: Wire the sections into the panel**

In `web/src/components/TicketPanel.tsx`, add imports:

```tsx
import LabelPicker from "./LabelPicker";
import DependencyPicker from "./DependencyPicker";
```

Add state beside the existing field state:

```tsx
const [repo, setRepo] = useState(ticket.repo || "");
const [labels, setLabels] = useState<string[]>((ticket.labels || []).map((l) => l.name));
const [dependsOn, setDependsOn] = useState(ticket.dependsOn || []);
```

Extend the `handleSave` payload:

```tsx
repo,
labels,
dependsOn: dependsOn.map((d) => d.id),
```

Add a Repo input in the grid row that Due Date now shares:

```tsx
<div>
  <label className="block text-xs font-medium text-slate-500 mb-1.5">Repo</label>
  <input
    value={repo}
    onChange={(e) => {
      setRepo(e.target.value);
      markDirty();
    }}
    placeholder="acme/billing-web"
    className="w-full bg-slate-800 border border-slate-700 rounded-md px-3 py-2 text-sm font-mono text-slate-200 focus:outline-none focus:border-slate-600"
  />
</div>
```

Add the three sections after the Project line and before Subtasks:

```tsx
<div>
  <h3 className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 mb-2.5">
    Labels
  </h3>
  <LabelPicker
    value={labels}
    onChange={(next) => {
      setLabels(next);
      markDirty();
    }}
  />
</div>

<div>
  <h3 className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 mb-2.5">
    Depends on
  </h3>
  <DependencyPicker
    value={dependsOn}
    excludeTicketId={ticket.id}
    onChange={(next) => {
      setDependsOn(next);
      markDirty();
    }}
  />
</div>

{ticket.blocks && ticket.blocks.length > 0 && (
  <div>
    <h3 className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 mb-2.5">
      Blocks
    </h3>
    {ticket.blocks.map((ref) => (
      <div
        key={ref.id}
        className="flex items-center gap-2 rounded-md border border-slate-800 bg-slate-900/60 px-2.5 py-1.5 mb-1.5 opacity-75"
      >
        <span className="font-mono text-[11px] text-slate-400 min-w-[52px]">{ref.key}</span>
        <span className="flex-1 text-[12.5px] text-slate-300 truncate">{ref.title}</span>
        <span className="rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-400">
          {ref.status.replace("_", " ")}
        </span>
      </div>
    ))}
    <p className="text-[11px] text-slate-600 mt-1">
      Read only. Derived from other tickets that depend on this one.
    </p>
  </div>
)}
```

**Note:** `Blocks` is read-only because it has no storage of its own. To change it, edit the ticket on the other side of the relationship.

- [ ] **Step 4: Add the label picker to the create modal**

In `web/src/components/CreateTicketModal.tsx`, add the import and state:

```tsx
import LabelPicker from "./LabelPicker";
```

```tsx
const [labels, setLabels] = useState<string[]>([]);
```

Add `labels` to the object passed to the create call in the submit handler, next
to `title` and `priority`:

```tsx
labels,
```

Render the picker in the form, after the priority and due-date row:

```tsx
<div>
  <label className="block text-xs font-medium text-slate-400 mb-1.5">Labels</label>
  <LabelPicker value={labels} onChange={setLabels} />
</div>
```

Reset it wherever the form clears after a successful create:

```tsx
setLabels([]);
```

Dependencies are deliberately not settable at creation time; they are added from
the ticket panel once the ticket exists.

- [ ] **Step 5: Verify the build and exercise it**

Run:

```bash
cd web && npx tsc -b && npm run build && cd ..
make build
./taskboard --db ./.tmp/dev.db start --port 3999 --foreground
```

Open `http://localhost:3999`, open a ticket, add a label by typing a new name, add a dependency by searching, save, then open the ticket you depended on and confirm it lists the first ticket under Blocks without you having edited it.

- [ ] **Step 6: Commit**

```bash
git add web/
git commit -m "feat: add repo, labels, and dependency sections to the ticket panel

LabelPicker offers existing labels and creates on free text. DependencyPicker
searches tickets across all projects. Blocks is read-only, derived from the
same table read backwards."
```

---

### Task 12: Labels page and the Tickets page columns

**Files:**
- Create: `web/src/pages/Labels.tsx`
- Modify: `web/src/App.tsx`, `web/src/components/Layout.tsx`, `web/src/pages/Tickets.tsx`

**Interfaces:**
- Consumes: `Label` with `ticketCount` from Task 10.
- Produces: the `/labels` route and a widened `api.tickets.list(params?)` signature that accepts `projectId`, `status`, `priority`, `label`, and `repo`.

- [ ] **Step 1: Write the Labels page**

Create `web/src/pages/Labels.tsx`:

```tsx
import { useEffect, useState } from "react";
import { Pencil, Trash2 } from "lucide-react";
import { api, type Label } from "../api/client";

export default function Labels() {
  const [labels, setLabels] = useState<Label[]>([]);
  const [name, setName] = useState("");
  const [color, setColor] = useState("#6B7280");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");

  const load = () => api.labels.list().then(setLabels).catch(() => setLabels([]));
  useEffect(() => {
    load();
  }, []);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    await api.labels.create({ name: name.trim(), color });
    setName("");
    setColor("#6B7280");
    load();
  };

  const remove = async (l: Label) => {
    const suffix =
      l.ticketCount > 0
        ? ` It will be removed from ${l.ticketCount} ticket${l.ticketCount === 1 ? "" : "s"}.`
        : "";
    if (!confirm(`Delete the label "${l.name}"?${suffix}`)) return;
    await api.labels.delete(l.id);
    load();
  };

  const saveEdit = async (l: Label) => {
    if (editName.trim() && editName !== l.name) {
      await api.labels.update(l.id, { name: editName.trim() });
    }
    setEditingId(null);
    load();
  };

  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-xl font-semibold text-white mb-6">Labels</h1>

      <form onSubmit={create} className="flex gap-2 mb-6">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Label name"
          className="flex-1 bg-slate-800 border border-slate-700 rounded-md px-3 py-2 text-sm text-slate-200 focus:outline-none focus:border-slate-600"
        />
        <input
          type="color"
          value={color}
          onChange={(e) => setColor(e.target.value)}
          className="w-12 h-10 bg-slate-800 border border-slate-700 rounded-md cursor-pointer"
          aria-label="Label color"
        />
        <button
          type="submit"
          className="bg-blue-600 hover:bg-blue-500 text-white text-sm px-4 rounded-md transition-colors"
        >
          Create
        </button>
      </form>

      <div className="border border-slate-800 rounded-lg bg-slate-900 divide-y divide-slate-800">
        {labels.length === 0 && (
          <p className="px-4 py-6 text-sm text-slate-500">No labels yet.</p>
        )}
        {labels.map((l) => (
          <div key={l.id} className="flex items-center gap-3 px-4 py-3">
            {editingId === l.id ? (
              <input
                autoFocus
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                onBlur={() => saveEdit(l)}
                onKeyDown={(e) => e.key === "Enter" && saveEdit(l)}
                className="bg-slate-800 border border-slate-700 rounded px-2 py-0.5 text-sm text-slate-200"
              />
            ) : (
              <span
                className="inline-flex items-center rounded px-2 py-0.5 text-[11px] font-medium"
                style={{ backgroundColor: l.color + "1f", color: l.color }}
              >
                {l.name}
              </span>
            )}
            <span className="flex-1 text-xs text-slate-500">
              {l.ticketCount} ticket{l.ticketCount === 1 ? "" : "s"}
            </span>
            <button
              onClick={() => {
                setEditingId(l.id);
                setEditName(l.name);
              }}
              className="text-slate-600 hover:text-slate-300"
              aria-label={`Rename ${l.name}`}
            >
              <Pencil className="w-3.5 h-3.5" />
            </button>
            <button
              onClick={() => remove(l)}
              className="text-slate-600 hover:text-red-400"
              aria-label={`Delete ${l.name}`}
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Route and navigate to it**

In `web/src/App.tsx`, import `Labels` and add `<Route path="labels" element={<Labels />} />`.

In `web/src/components/Layout.tsx`, add to `navItems` in the slot Teams vacated, importing `Tag` from `lucide-react`:

```tsx
{ to: "/labels", icon: Tag, label: "Labels" },
```

- [ ] **Step 3: Update the Tickets page**

In `web/src/pages/Tickets.tsx`, ADD `Labels` and `Repo` columns. Note: this table never
had a Team column, so there is nothing to replace. The existing headers are Key, Title,
Status, Priority, Due, plus a narrow trailing action column. Insert the two new headers
immediately after Title, giving Key, Title, Labels, Repo, Status, Priority, Due. Add the
matching cells in the same position in the body row:

```tsx
<td className="px-4 py-3">
  <div className="flex flex-wrap gap-1">
    {(ticket.labels || []).map((l) => (
      <span
        key={l.id}
        className="inline-flex items-center rounded px-1.5 py-0.5 text-[10.5px] font-medium"
        style={{ backgroundColor: l.color + "1f", color: l.color }}
      >
        {l.name}
      </span>
    ))}
  </div>
</td>
<td className="px-4 py-3 font-mono text-[11px] text-slate-400">{ticket.repo || ""}</td>
```

Under the title cell, add the dependency note:

```tsx
{ticket.dependsOn && ticket.dependsOn.length > 0 && (
  <div
    className={`text-[10.5px] mt-0.5 ${
      ticket.dependsOn.some((d) => d.status !== "done") ? "text-red-400" : "text-slate-500"
    }`}
  >
    depends on{" "}
    <span className="font-mono">{ticket.dependsOn.map((d) => d.key).join(", ")}</span>
  </div>
)}
```

Add a label filter. `api.tickets.list()` currently takes no arguments, so first
give it optional query parameters in `web/src/api/client.ts`, replacing the
existing `list` entry in the `tickets` group:

```ts
    list: (params?: {
      projectId?: string;
      status?: string;
      priority?: string;
      label?: string;
      repo?: string;
    }) => {
      const qs = new URLSearchParams(
        Object.entries(params ?? {}).filter(([, v]) => v) as [string, string][]
      ).toString();
      return request<Ticket[]>(`/api/tickets${qs ? `?${qs}` : ""}`);
    },
```

Existing no-argument callers, including `DependencyPicker`, keep working.

Then in `Tickets.tsx`, add the filter state, load the label list, and pass the
selection through:

```tsx
const [labelFilter, setLabelFilter] = useState("");
const [allLabels, setAllLabels] = useState<Label[]>([]);

useEffect(() => {
  api.labels.list().then(setAllLabels).catch(() => setAllLabels([]));
}, []);
```

Add `Label` to the type import from `../api/client`. Include `labelFilter` in the
dependency array of whichever effect already fetches tickets, and pass it in the
call:

```tsx
api.tickets.list({ label: labelFilter || undefined }).then(setTickets);
```

Render the select beside the existing status and priority filters:

```tsx
<select
  value={labelFilter}
  onChange={(e) => setLabelFilter(e.target.value)}
  className="bg-slate-800 border border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-300"
>
  <option value="">All labels</option>
  {allLabels.map((l) => (
    <option key={l.id} value={l.name}>
      {l.name}
    </option>
  ))}
</select>
```

- [ ] **Step 4: Verify the build and exercise it**

Run:

```bash
cd web && npx tsc -b && npm run build && cd ..
make build
./taskboard --db ./.tmp/dev.db start --port 3999 --foreground
```

Open `http://localhost:3999/labels`. Create a label, rename it inline, check the ticket count, and confirm the delete dialog names how many tickets are affected. Then open the Tickets page and filter by label.

- [ ] **Step 5: Commit**

```bash
git add web/
git commit -m "feat: add a Labels page and label and repo columns on Tickets

The Labels page takes the sidebar slot Teams vacated and shows per-label
ticket counts, inline rename, and a delete dialog that names how many
tickets lose the label."
```

---

### Task 13: Documentation

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: the finished tool set from Tasks 1 through 12.
- Produces: nothing.

- [ ] **Step 1: Count the MCP tools**

Run: `grep -c 'Name:' internal/mcp/mcp.go`

Expected: 17. Trust the grep over this number, and over the README.

The published count has been wrong since before this project: the README claims 22 in
two places, but its own table has only 21 rows and the code defined 21 tools. The
arithmetic is 21, minus the 5 team tools removed in Task 1, plus `list_labels` added in
Task 7, giving 17. If the grep disagrees, the grep wins and every count reference in the
README must match it.

- [ ] **Step 2: Update the README**

In `README.md`:

Remove the five team rows and the `**Teams**` section header from the MCP tool table. Add a `**Labels**` section with one row:

```markdown
| `list_labels`           | List all labels with ticket counts               |
```

Change both occurrences of `22` in the tool count, in the features list and the
`#### Available MCP Tools (22)` heading, to the number Step 1 counted. Do not assume the
old `22` was ever right; it was not.

In the features list, remove the `**Teams** — assign tickets to teams` bullet. Change the Tickets bullet to:

```markdown
- **Tickets** — priority levels, due dates, labels, subtasks, dependencies, and a repo field
```

In the CLI usage block, delete the two `taskboard team` lines and add:

```bash
taskboard label create bug --color "#ef4444"
taskboard label list

taskboard ticket create --project <ID> --title "Implement login" --priority high \
  --label backend --repo acme/auth-api
taskboard ticket update <ID> --labels backend,urgent --depends-on AUTH-1
```

Add a short subsection after the Data Hierarchy block:

```markdown
#### Dependencies

A ticket can declare that it depends on other tickets, by ID or by display key
such as `AUTH-1`. Dependencies may cross projects. They are informational: a
ticket whose dependencies are unfinished can still be moved to any status.

The reverse direction is derived, not stored. When `BILL-5` depends on `BILL-2`,
`BILL-2` lists `BILL-5` under *blocks* automatically, and that list is read-only.
```

- [ ] **Step 3: Verify no stale references remain**

Run: `grep -rn -i 'team' README.md`

Expected: no matches. If any remain, they are stale and must be removed.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document labels, dependencies, and repo; drop teams

Updates the MCP tool table and count, the features list, and the CLI
examples, and explains that dependencies are informational and that the
blocks direction is derived rather than stored."
```

---

## Screenshots

The four screenshots in `screenshots/` show the Teams nav item and team chips, so they are stale after Task 9. Regenerating them is optional and cosmetic, but if done:

1. Seed a scratch database: `./taskboard --db ./.tmp/demo.db ...` with a handful of projects, tickets, labels, and dependencies.
2. Run `./taskboard --db ./.tmp/demo.db start --port 3999 --foreground`.
3. Capture `board.png`, `ticket-detail.png`, `tickets.png`, and `project.png` at 1440x900.

**Never point the screenshot run at the default database.**

---

## Verification

Before calling the work done, run every check:

```bash
go build ./...
go test ./...
cd web && npx tsc -b && npm run build && cd ..
make build
grep -rn -i 'team' internal/ web/src/ README.md
```

The final grep must return nothing. `go test ./...` must pass with no skips.
