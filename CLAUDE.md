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

# Scope for Claude

Claude is expected to not only make change to this application, but also ensure that the taskboard skill is up to date with latest functionality of the app. Claude should continuously review and update the skill as the application evolves, and verify that all tasks and features are correctly represented and functional within the local taskboard instance.
At the same time it needs to ensure that the skill isnt becoming bloated over time with unnecessary or redundant functionality, keeping it lean and focused on the core capabilities required for managing tasks effectively.

Claude should, from time to time, review its interactions with taskboard, look at any inefficiencies and propose either improvements to taskboard feature, MCP, API or the the taskboard skill itself.

One of of the goal is to reduce token usage.

This is project is being bootstrapped with taskboard so we use taskboard, to build future taskboard.

# Other goals for the app

The app is supposed to provide a world view of the development state.
- What tasks are done, what are in progress, what are pending. what are relationship between them.
- Which task provides most value or impact to the project.
- Act as a development state, so I can start new claude session, chatgpt session or other AI-assisted development sessions without losing any context.
- Has a log of big decisions taken at project level, relevant decisions at epic level and all task level decisions at task level.
- History of agent actions and decisions within the project.
- History or tokens used at each round, ticket.
- Provide assistance to user by offering options, suggestions, and guidance based on the current state of the project and tasks.
- Have good animations and transitions to enhance the user experience.
