# Taskboard

Go CLI, HTTP and MCP server (cmd/, internal/) with a React web UI (web/) embedded into one binary.
Run it with `make dev`; test with `go test ./...`.

# Skills to use

- taskboard - always
- superpowers - try not to. if you must, use ask first

## Live board safety

Bilal's real taskboard runs from `~/.local/bin/taskboard` on port 3010. Never touch it.

- Always pass `--db` with a path inside the worktree (`./.tmp/…`), and use ports 3011 and above.
- Browser and Playwright checks go only to the dev port. Screenshots come from a seeded throwaway DB.
- Never run `make install`. It is Bilal's action.
- Stop processes only by PID started by the task itself, or by `lsof -t -- <own db path>`. Never select them by name.
- Go tests use `t.TempDir()`. Web tests mock the API and never hit a real server.

Development builds refuse to run without `--db` and default to port 3011; only release binaries and the binary `make install` builds may use the default database. The `.claude/settings.json` hook (`scripts/claude-guard.sh`) also blocks the riskiest commands, but it only matches command strings and a script file gets around it. The code guard is the real protection.
