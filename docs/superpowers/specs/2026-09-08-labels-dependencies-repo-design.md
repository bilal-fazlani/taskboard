# Labels, task dependencies, and repo — design

Date: 2026-09-08
Status: approved for planning

## Summary

Three ticket-level capabilities ship together, and one existing feature is removed:

1. **Labels** — named, colored tags on tickets, global across projects.
2. **Dependencies** — a ticket can declare it depends on other tickets. Informational, never enforced.
3. **Repo** — a free-form string on a ticket naming where the work lives.
4. **Teams removal** — the teams feature is deleted from every layer.

Labels and dependencies already exist in the database schema and in the store and
HTTP layers. Neither is reachable from the web UI, the CLI, or MCP. Most of the
work is exposing what exists, correcting its shape, and building the UI.

## Problem

The initial migration created `labels`, `ticket_labels`, and `ticket_dependencies`.
`Store.GetTicket` populates `Labels` and `BlockedBy`, and ticket create and update
accept both. The HTTP API exposes full label CRUD. Above that layer nothing uses
any of it: no pickers, no chips, no MCP tool arguments, no CLI flags. A user cannot
label a ticket or record a dependency through any interface the product ships.

Teams, by contrast, is wired through every layer but earns no keep. It is being cut
rather than maintained.

## Non-goals

- **No enforcement of dependencies.** A ticket whose dependencies are unfinished can
  still be moved to any status, from any surface. No warnings, no confirmation
  dialogs, no rejected moves.
- **No cycle detection.** A depends on B and B depends on A is permitted. Because
  dependencies carry no behavior, a cycle causes no infinite loop and no wrong state.
- **No per-project label scoping.** Labels stay global. A project-scoped label
  namespace is a larger change and is not justified yet.
- **No repo behavior.** Repo is a string. It is not a URL, is not validated, is not
  cloned, and does not set the embedded terminal's working directory.

## Safety constraint

**No development build, test, or migration may open the live database at
`~/Library/Application Support/taskboard/taskboard.db`.** That is the path
`db.DefaultDBPath()` returns, and the user runs a real instance against it.
Migration 003 drops the teams table, which is irreversible.

Every invocation of a locally built binary passes an explicit `--db` pointing inside
the worktree, for example `--db ./.tmp/dev.db`. Tests open temp files or in-memory
databases. Reading the user's real board happens only through the HTTP API on
`http://localhost:3010`, never through the file.

## Data model

### Migration `003_labels_repo_drop_teams.sql`

Migrations are embedded from `internal/db/migrations/*.sql` and tracked in
`schema_migrations` by filename, applied in lexical order. One new file:

```sql
DROP INDEX IF EXISTS idx_tickets_team_id;
ALTER TABLE tickets DROP COLUMN team_id;
DROP TABLE IF EXISTS teams;
ALTER TABLE tickets ADD COLUMN repo TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_tickets_repo ON tickets(repo);
CREATE INDEX IF NOT EXISTS idx_ticket_deps_blocked_by ON ticket_dependencies(blocked_by_id);
```

**Statement order is load-bearing.** SQLite refuses to drop a column while an index
references it, failing with `error in index idx_tickets_team_id after drop column`.
The index must be dropped first. `team_id` carries a foreign key to `teams`, so the
column is dropped before the table it points at.

This exact SQL was verified against `modernc.org/sqlite` v1.45.0, the driver the
project uses, running inside a transaction the way `runMigrations` does. On a
database seeded with tickets, dependencies, and labels, it preserved all rows, left
`PRAGMA foreign_key_check` clean, and accepted new ticket inserts with foreign keys
enabled. No table rebuild is needed.

`idx_ticket_deps_blocked_by` is what makes the reverse lookup for *blocks* cheap.
The forward index already exists.

### Model changes

`Ticket` loses `TeamID`. It gains:

```go
Repo      string     `json:"repo,omitempty"`
DependsOn []TicketRef `json:"dependsOn,omitempty"`
Blocks    []TicketRef `json:"blocks,omitempty"`
```

`BlockedBy []string` is replaced by `DependsOn []TicketRef`. `TicketRef` is new:

```go
type TicketRef struct {
    ID     string `json:"id"`
    Key    string `json:"key"`    // "BILL-2"
    Title  string `json:"title"`
    Status string `json:"status"`
}
```

The ref carries status because the board colors the dependency band by whether every
dependency is done. A list of bare IDs would force the client to cross-reference
tickets it may not have loaded.

`Team`, `CreateTeamRequest`, and `UpdateTeamRequest` are deleted. `TeamID` is removed
from both ticket requests. `TicketFilter` drops `TeamID` and gains `Label` and `Repo`.

Create and update requests keep flat string slices, since callers supply names and
keys rather than internal IDs:

```go
Labels    []string `json:"labels,omitempty"`     // names, case-insensitive
DependsOn []string `json:"dependsOn,omitempty"`  // ticket IDs or display keys
Repo      *string  `json:"repo,omitempty"`       // update only; create uses string
```

Both fields keep replace semantics on update: a non-nil slice replaces the whole set,
and `nil` leaves it untouched. This matches how the store already treats `Labels`.

## Store layer

### Label resolution

A new unexported `resolveLabels(names []string) ([]string, error)` maps names to
label IDs, matching case-insensitively on `name`. Any name with no match is created
with the default color `#6B7280`. The stored name keeps the caller's original casing;
matching ignores it, so "Backend" and "backend" resolve to one label.

This is what lets an agent write `labels: ["bug"]` without a lookup round trip.

### Dependency resolution

`resolveTicketRefs(refs []string) ([]string, error)` accepts either a raw ticket ID
or a display key like `BILL-2`, resolved by joining `projects.prefix` and
`tickets.number`. Rules:

- An unresolvable reference is an error naming the offending value.
- A ticket depending on itself is an error.
- Duplicates collapse silently.
- **Cross-project dependencies are allowed.** Keys are unique per project, so the
  resolver matches on prefix and number together, and the picker searches all
  projects.

### Reads

`getTicketDependsOn(ticketID)` and `getTicketBlocks(ticketID)` both return
`[]TicketRef`, reading the same table in opposite directions. `blocks` has no
storage of its own and is never written directly; it appears on ticket A precisely
when some ticket B lists A under its own `dependsOn`.

Population differs by call site:

| Call | Labels | DependsOn | Blocks |
|---|---|---|---|
| `GetTicket` (detail) | yes | yes | yes |
| `ListTickets`, `GetBoard` | yes | yes | no |

`Blocks` is omitted from list results because no list view renders it. `DependsOn`
is included because the card band needs it.

### Query shape

`ListTickets` and `GetBoard` currently call `getTicketLabels` and
`getTicketBlockedBy` once per ticket inside a loop, so a 50-ticket board runs about
100 extra queries. Both are replaced by two batch queries per list call, one for
labels and one for dependencies, each filtered by the result's ticket IDs and
grouped in Go. The board gets cheaper than it is today despite carrying more data.

Errors from the label and dependency writes in `CreateTicket` and `UpdateTicket` are
currently discarded. They are returned.

## HTTP API

Removed: the whole `/api/teams` route group and its five handlers.

Unchanged in shape: `/api/labels` CRUD already exists and stays as is, keyed by
label ID. It backs the Labels management page.

`GET /api/labels` gains a ticket count per label for the management page, via a
grouped join. The response becomes `{id, name, color, ticketCount}`.

Ticket create and update accept `labels`, `dependsOn`, and `repo` as described
above. Ticket list accepts `label` and `repo` query parameters. A resolution failure
returns 400 with the offending value in the message.

## MCP

Teams costs five tools. Labels adds one. The count goes from 22 to 18.

- **Removed:** `list_teams`, `get_team`, `create_team`, `update_team`, `delete_team`.
- **Added:** `list_labels`, returning every label with its ticket count.
- **`create_ticket` and `update_ticket`** gain `labels` (array of names, created on
  demand), `dependsOn` (array of ticket IDs or keys), and `repo` (string). The
  `teamId` property is removed.
- **`list_tickets`** gains `label` and `repo` filters and loses `teamId`.
- **`get_ticket`** needs no schema change and now returns both dependency directions.

Tool descriptions state plainly that dependencies are informational and never block
a status change, so an agent does not infer permission semantics that do not exist.

## CLI

`internal/cli/team.go` is deleted along with its registration in `root.go`.

New `internal/cli/label.go`:

```
taskboard label list                          # name, color, ticket count
taskboard label create <name> [--color HEX]
taskboard label delete <id>
```

`ticket create` gains `--label` (repeatable), `--depends-on` (repeatable), and
`--repo`. It loses `--team`.

New `ticket update <id>` covering `--title`, `--description`, `--status`,
`--priority`, `--due`, `--repo`, `--labels`, and `--depends-on`. The last two take
comma-separated values and replace the existing set, matching API semantics. This
command does not exist today, so labels and dependencies would otherwise be
unsettable from the CLI after creation.

`ticket list` gains `--label` and `--repo`, drops team from its output, and appends
label names plus a `depends on` note to each row.

## Web UI

### Removed

`web/src/pages/Teams.tsx`, its route in `App.tsx`, its sidebar entry in
`Layout.tsx`, the `Team` type and API client group, the team select in
`TicketPanel.tsx` and `CreateTicketModal.tsx`, and the team chip and column in
`Board.tsx` and `Tickets.tsx`.

### Board card

Final layout, top to bottom: ticket key and priority, title, dependency band, label
chips, due date, subtask progress.

- **Dependency band.** A full-width tinted strip with a 2px left rule, square on the
  left and rounded on the right, so it never reads as a chip. Red tint with a red
  rule while any dependency is unfinished; neutral slate once all are done. Absent
  entirely when the ticket has no dependencies. Content is the words "depends on"
  followed by the blocker keys in monospace, capped at three keys with a `+N`
  suffix so a heavily blocked ticket cannot stretch the card.
- **Labels** are the only chips on the card, in the label's own color at low opacity.
- **Not on the card:** repo, project, team.

### Full task view

Adds a `Repo` text input beside Due Date, with suggestions drawn from repo values
already in use so spelling stays consistent. Adds a `Labels` section with removable
chips and an add control that offers existing labels and creates on free text. Adds
a `Depends on` section listing each dependency as a row with key, title, live status,
and a remove control, plus a search field over tickets across all projects. Adds a
read-only `Blocks` section in the same row format.

Project moves here as a static line, since it left the card.

### Tickets page

The Team column is replaced by Labels and Repo columns. A dependency note appears
under the title. Label and repo filters join the existing status and priority
filters.

### Labels page

Takes the sidebar slot Teams vacates. A create row with a name field and color
swatch, then a list of labels showing the chip, ticket count, rename, and delete.
Deleting a label removes it from every ticket by cascade, which the confirm copy
states.

## Testing

The repo has no tests today. This adds a Go test file for the store, run against
temp-file SQLite databases created per test, never the default path.

Cases:

- Label resolution matches case-insensitively and does not duplicate.
- An unknown label name is created with the default color.
- A dependency given as a display key resolves to the right ticket.
- A dependency given as a raw ID resolves.
- An unresolvable key returns an error naming the value.
- A self-dependency returns an error.
- A cross-project dependency resolves.
- `blocks` on A is populated after B declares a dependency on A, with no direct write.
- Deleting B removes it from A's `blocks`.
- Update with a nil label slice leaves labels untouched; a non-nil slice replaces them.
- List results carry labels and dependencies but not blocks.
- Migration 003 applies to a database seeded with the 001 and 002 schema, and the
  teams table is gone afterward.

Web changes are verified by `tsc` and the production build, plus a manual run of the
built binary against a scratch database.

## Documentation

`README.md` needs the teams rows removed from the MCP tool table, the count changed
from 22 to 18, the features list updated, and the CLI examples reworked. The
`Data Hierarchy` section stays accurate. Screenshots show teams and will be stale;
they are regenerated at the end against a scratch database with seeded demo data.
