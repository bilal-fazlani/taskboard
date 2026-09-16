# Taskboard

A local, self-hosted project management tool with a dependency graph for a home page, a Kanban board, a full CLI, and a built-in [MCP](https://modelcontextprotocol.io/) server that lets AI assistants manage your projects, tickets, and labels directly.

Single binary. SQLite-backed. No Docker, no external database, no runtime dependencies.

## Screenshots

![Dependency Graph](screenshots/graph.png)

![Kanban Board](screenshots/board.png)

![Ticket Detail](screenshots/ticket-detail.png)

## Features

- **Dependency Graph** — the home page puts open tickets in columns by how many steps of unfinished work block them, with arrows from each blocker and cycles drawn in red. Hover a card to trace what blocks it and what it blocks; pan, zoom or fit the graph to the screen
- **Kanban Board** — drag-and-drop ticket management across Todo, In Progress, and Done columns, at `/board`
- **Projects** — organize work with customizable projects (icons, colors, prefixes)
- **Tickets** — priority levels, due dates, labels, subtasks, dependencies, and one or more repos
- **Embedded Terminal** — run AI coding agents (opencode, Claude Code) directly from the web UI
- **CLI** — manage everything from the terminal
- **MCP Server** — 17 tools for AI-native project management via Model Context Protocol
- **Self-Hosted** — your data stays on your machine in a SQLite database
- **Single Binary** — one `brew install` and you're running

## Install

### Homebrew

```bash
brew install bilal-fazlani/tap/taskboard
```

Or tap first, then install by name:

```bash
brew tap bilal-fazlani/tap
brew trust bilal-fazlani/tap
brew install taskboard
```

`brew trust` is needed on Homebrew 5.1.15 and later, which refuse to load a
formula from a tap you haven't trusted. Older Homebrew doesn't have the
command, so leave that line out there. The one-line install above needs no
extra step: installing by the full name trusts the formula.

Upgrade to the latest release with:

```bash
brew upgrade taskboard
```

### From source

```bash
git clone https://github.com/bilal-fazlani/taskboard.git
cd taskboard
make build
```

Requires Go 1.24+ and Node.js 22+.

`make build`, `go build` and `go install` produce a development build: it never opens the default database, so every command needs `--db`, and `start` defaults to port 3011. The examples below, which rely on the default database and port 3010, assume a release binary or `make install`, which builds the marked binary, installs it to `~/.local/bin` and restarts the server.

## Usage

### Web UI

```bash
taskboard start
# => http://localhost:3010

taskboard start --port 8080
```

### CLI

```bash
taskboard project create "Auth System" --prefix AUTH --icon "🔐"
taskboard project list

taskboard ticket create --project <ID> --title "Implement login" --priority high
taskboard ticket list --project <ID> --status todo
taskboard ticket move <ID> --status done

taskboard label create bug --color "#ef4444"
taskboard label list

taskboard ticket create --project <ID> --title "Implement login" --priority high \
  --label backend --repo acme/auth-api --repo acme/auth-web
taskboard ticket update <ID> --labels backend,urgent --depends-on AUTH-1
taskboard ticket update <ID> --repo acme/auth-api,acme/auth-web  # replaces the set
taskboard ticket list --repo acme/auth-api                       # tickets touching that repo
```

### MCP Server (for AI assistants)

```bash
taskboard mcp
```

#### Claude Code

```bash
claude mcp add taskboard -- /path/to/taskboard mcp
```

#### Claude Desktop

Add to `~/.claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "taskboard": {
      "command": "/path/to/taskboard",
      "args": ["mcp"]
    }
  }
}
```

#### Data Hierarchy

```
Project → Ticket → Subtask
(initiative)  (task)    (step)
```

- **Projects** are the top-level grouping (like epics/initiatives). Create one per body of work.
- **Tickets** are concrete, actionable tasks within a project. Don't create "epic" tickets — use projects.
- **Subtasks** are checklist steps within a ticket, for breaking work into verifiable pieces.

#### Dependencies

A ticket can declare that it depends on other tickets, by ID or by display key
such as `AUTH-1`. Dependencies may cross projects. They are informational: a
ticket whose dependencies are unfinished can still be moved to any status.

The reverse direction is derived, not stored. When `BILL-5` depends on `BILL-2`,
`BILL-2` lists `BILL-5` under *blocks* automatically, and that list is read-only.

#### Available MCP Tools (17)

| Tool                    | Description                                      |
| ----------------------- | ------------------------------------------------ |
| **Projects**            |                                                  |
| `list_projects`         | List all projects with optional status filter    |
| `get_project`           | Get project details by ID                        |
| `create_project`        | Create a new project (use for epics/initiatives) |
| `update_project`        | Update project properties                        |
| `delete_project`        | Delete a project and all its tickets             |
| **Labels**              |                                                  |
| `list_labels`           | List all labels with ticket counts               |
| **Tickets**             |                                                  |
| `list_tickets`          | List tickets with filters                        |
| `get_ticket`            | Get ticket details with subtasks and labels      |
| `create_ticket`         | Create a ticket (task) within a project          |
| `update_ticket`         | Update ticket properties                         |
| `move_ticket`           | Move ticket to different status column           |
| `delete_ticket`         | Delete a ticket                                  |
| **Board**               |                                                  |
| `get_board`             | Get full Kanban board grouped by status          |
| **Subtasks**            |                                                  |
| `create_subtask`        | Add a subtask to a ticket                        |
| `batch_create_subtasks` | Add multiple subtasks to a ticket at once        |
| `toggle_subtask`        | Toggle subtask completion                        |
| `delete_subtask`        | Remove a subtask from a ticket                   |

#### Example Prompts

Once the MCP is connected, you can talk to your AI assistant in high-level terms and let it figure out the breakdown:

```
I'm building a SaaS billing system. Set up the project and break the work
into tickets covering Stripe integration, usage metering, invoice generation,
and a customer billing portal. Prioritize accordingly.
```

```
I need to ship a password reset flow. Think through what's involved — API
endpoints, email templates, token handling, UI screens, tests — and create
tickets with subtasks for each piece.
```

```
Look at my board and figure out what's blocking progress. If anything in
"in progress" has been sitting there without subtasks, break it down into
concrete next steps.
```

Here's what that looks like — a project and tickets created entirely by an AI assistant via MCP:

![Project created by AI](screenshots/project.png)

![Tickets list](screenshots/tickets.png)

### Embedded Terminal

The web UI includes a built-in terminal. Click **Terminal** in the sidebar to open a full PTY shell with color support and a resizable panel. Run `opencode`, `claude`, or any command directly from the browser.

![Embedded Terminal](screenshots/terminal.png)

The agent shares the same SQLite database via MCP, so tickets it creates show up on your board immediately.

## Data Storage

All data is stored in a SQLite database at:

- **macOS**: `~/Library/Application Support/taskboard/taskboard.db`
- **Linux**: `~/.config/taskboard/taskboard.db`

Only release binaries and `make install` use this default. A development build (`make build`, `go build`, `go install`) exits with an error unless you pass `--db`.

Migrations run automatically on first start.

### Custom Database Path

Use `--db` to point any command at a different database file:

```bash
taskboard --db /path/to/other.db start
taskboard --db /path/to/other.db mcp
taskboard --db /path/to/other.db ticket list
```

### Clearing Data

To wipe all projects, tickets, and labels while keeping the schema intact:

```bash
taskboard clear        # prompts for confirmation
taskboard clear -f     # skip confirmation
```

This also respects `--db`, so you can clear a specific database file:

```bash
taskboard --db /tmp/test.db clear -f
```

## Tech Stack

| Layer        | Technology                                          |
| ------------ | --------------------------------------------------- |
| Language     | Go                                                  |
| Database     | SQLite (via modernc.org/sqlite, pure Go)            |
| CLI          | cobra                                               |
| HTTP         | chi                                                 |
| Frontend     | React, TypeScript, Tailwind CSS v4, dnd-kit         |
| Terminal     | xterm.js, gorilla/websocket, creack/pty             |
| MCP          | JSON-RPC over stdio                                 |
| Distribution | Single binary with embedded frontend via `embed.FS` |

## Development

```bash
# Run backend on :3011 against a throwaway database at ./.tmp/dev.db
# (never the live board on :3010). Override with DEV_PORT=... / DEV_DB=...
make dev

# Run frontend dev server (proxies API to the make dev backend)
make dev-frontend

# Build everything
make build

# Clean
make clean
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

[MIT](LICENSE)
