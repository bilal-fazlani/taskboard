#!/usr/bin/env bash
# Tests for scripts/claude-guard.sh: sample commands in, allow or deny out.
# Every case runs through each JSON reader (jq, python3, raw) that is available;
# deny cases also run through the naive fallback lexer, which must never allow
# what the awk lexer denies. Nothing here executes the sample commands.
#
# Usage: bash scripts/claude-guard_test.sh   (or: make test-guard)
set -uo pipefail

guard=$(cd "$(dirname "$0")" && pwd)/claude-guard.sh
shell=${GUARD_BASH:-bash} # e.g. GUARD_BASH=/bin/bash to test with the bash 3.2 macOS ships
failures=0
cases=0

parsers=(raw)
command -v jq >/dev/null 2>&1 && parsers+=(jq)
command -v python3 >/dev/null 2>&1 && parsers+=(python3)
command -v python3 >/dev/null 2>&1 || { echo "python3 is needed to build the hook JSON" >&2; exit 1; }

hook_json() {
  python3 -c 'import json, sys; print(json.dumps({"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": {"command": sys.argv[1]}}))' "$1"
}

# expect allow|deny <command>
expect() {
  local want=$1 command=$2 json parser status got
  json=$(hook_json "$command")
  for parser in "${parsers[@]}"; do
    cases=$((cases + 1))
    printf '%s' "$json" | CLAUDE_GUARD_PARSER=$parser "$shell" "$guard" >/dev/null 2>"${TMPDIR:-/tmp}/claude-guard-test.err"
    status=$?
    case $status in 0) got=allow ;; 2) got=deny ;; *) got="exit $status" ;; esac
    if [ "$got" != "$want" ]; then
      failures=$((failures + 1))
      echo "FAIL [$parser] want $want, got $got: $command" >&2
      sed 's/^/    /' "${TMPDIR:-/tmp}/claude-guard-test.err" >&2
    fi
  done
  if [ "$want" = deny ]; then
    cases=$((cases + 1))
    printf '%s' "$json" | CLAUDE_GUARD_LEXER=bash "$shell" "$guard" >/dev/null 2>&1
    status=$?
    if [ "$status" -ne 2 ]; then
      failures=$((failures + 1))
      echo "FAIL [bash lexer] want deny, got exit $status: $command" >&2
    fi
  fi
}

# Normal work must stay allowed.
expect allow 'go test ./...'
expect allow 'go test -count=1 ./...'
expect allow 'go vet ./...'
expect allow 'make test-install'
expect allow 'make dev'
expect allow 'make build'
expect allow 'make installer'
expect allow 'make -C web test-install'
expect allow 'lsof -t -- ./.tmp/dev.db'
expect allow 'kill 12345'
expect allow 'kill $(lsof -t -- ./.tmp/dev.db)'
expect allow 'kill $(lsof -t -- /Users/bilal/Projects/taskboard-worktrees/acp-99-cleanup/.tmp/dev.db)'
expect allow 'curl -s http://localhost:3011/api/projects'
expect allow 'curl -s -X POST -d "{}" http://localhost:3011/api/projects'
expect allow 'curl -s http://localhost:3010/api/projects'
expect allow 'curl -fsS localhost:3010/api/projects | jq .'
expect allow 'curl -X GET localhost:3010/api/projects'
expect allow 'curl -sX GET "http://localhost:3010/api/tickets/$(echo 01ABC)"'
expect allow 'curl --request=GET http://127.0.0.1:3010/api/projects'
expect allow 'curl -X "$(echo POST)" localhost:3011/api/projects'
expect allow '~/.local/bin/taskboard --help'
expect allow '~/.local/bin/taskboard ticket list'
expect allow "$HOME/.local/bin/taskboard mcp"
expect allow 'go run ./cmd/taskboard --db ./.tmp/x.db start --port 3011'
expect allow 'go run ./cmd/taskboard --db=./.tmp/x.db ticket list'
expect allow 'go build -o taskboard ./cmd/taskboard && ./taskboard --help'
expect allow './taskboard --db ./.tmp/x.db start --foreground --port 3012 &'
expect allow './taskboard --db "$(pwd)/.tmp/x.db" ticket list'
expect allow './taskboard --db $(pwd)/.tmp/x.db ticket list'
expect allow 'go run ./cmd/taskboard --db $(mktemp -d)/x.db start --port 3011'
expect allow 'cd /Users/bilal/Projects/taskboard && git status'
expect allow 'git log --oneline -5'
expect allow 'lsof -nP -iTCP:3010 -sTCP:LISTEN'
expect allow 'lsof -ti :3010'
expect allow 'kill $(lsof -ti tcp:3041)'
expect allow 'lsof -ti :30100 | xargs kill'
expect allow 'ps -o pid,command -p 12345'
expect allow 'grep -rn taskboard internal/cli'
# Reading about kill in paths that contain "taskboard" is not killing anything.
expect allow 'rg -n "kill" /Users/bilal/Projects/taskboard-worktrees/acp-31/scripts'
expect allow 'grep -n kill /Users/bilal/Projects/taskboard-worktrees/acp-31/scripts/install.sh'
expect allow 'sed -n 1,40p /Users/bilal/Projects/taskboard-worktrees/acp-31/internal/cli/root.go | grep -n kill'
# A GET to 3010 piped into tools whose flags look like curl data flags.
expect allow 'curl -s localhost:3010/api/board | sort -T /tmp'
expect allow 'curl -s localhost:3010/api/board | cut -d, -f1'
expect allow 'curl -s localhost:3010/api/board | grep -F x'
# "make install" as text in quotes, commit messages and heredoc bodies.
expect allow 'git commit -m "docs: explain why; make install stays manual"'
expect allow "git commit -m 'build: make install | tee log && make install'"
expect allow $'git commit -F - <<\'MSG\'\nbuild: document the install flow\n\nmake install now builds the marked binary itself.\nNever `make install` from a worktree; it is Bilal\'s action.\nMSG'
expect allow $'git commit -m "$(cat <<\'EOF\'\nfix: guard the live board\n\nDon\'t run `make install`; it restarts 3010.\nmake install is Bilal\'s action.\nEOF\n)"'
expect allow $'cat > notes.md <<EOF\n- make install\n- pkill taskboard\n- ./taskboard ticket list\nEOF\nwc -l notes.md'
expect allow $'git commit -F - <<-\'MSG\'\n\tmake install\n\tMSG\ngit log -1'
expect allow $'mkdir -p .tmp\ngo run ./cmd/taskboard --db ./.tmp/dev.db project list'
# A substitution as the command word that names neither make nor taskboard,
# or runs taskboard with --db.
expect allow '$(which go) test ./...'
expect allow '`command -v make` test-install'
expect allow '$(which taskboard) --db ./.tmp/x.db ticket list'
expect allow '"$(git rev-parse --show-toplevel)/scripts/claude-guard_test.sh"'
expect allow '$(git -C /Users/bilal/Projects/taskboard-worktrees/acp-31 rev-parse --show-toplevel)/scripts/claude-guard_test.sh'
expect allow 'echo "$(which make) install"'
# Arithmetic, with and without substitutions inside.
expect allow 'echo $(( (1 + 2) * 3 ))'
expect allow 'echo $(( $(wc -l < notes.md) + 1 ))'
expect allow '(( n = $(git rev-list --count HEAD) ))'
expect allow $'echo $(( 1 << 2 ))\ncat notes.md'
expect allow 'echo "$(( $(date +%s) - 60 )) make install"'

# make install
expect deny 'make install'
expect deny 'make build install'
expect deny 'make PORT=3010 install'
expect deny 'cd ../taskboard && make install'
expect deny 'INSTALL_DIR=/tmp/x make -C . install'
expect deny 'bash -c "make install"'
expect deny $'go test ./...\nmake install'
expect deny 'git commit -m "x" && make install'
expect deny 'echo "$(make install)"'
expect deny 'echo "`true; make install`"'
expect deny $'echo $((1<<2))\nmake install'
expect deny $'(( y <<= 1 ))\nmake install'
expect deny $'bash <<\'EOF\'\nmake install\nEOF'
expect deny $'cat > notes.md <<EOF\nmake install\nEOF\nmake install'
expect deny $'git commit -F - <<\'MSG\'\nnotes\nMSG\nmake install'
# A substitution inside arithmetic, and $(( that bash reads as $( plus a
# subshell because it doesn't close with )).
expect deny 'echo $(( $(make install) ))'
expect deny 'echo "$(( 1 + $(make install) ))"'
expect deny 'echo $(( `make install` + 1 ))'
expect deny '(( n = $(make install) ))'
expect deny 'echo $(( $(( 2 * $(make install) )) ))'
expect deny '$((make install) )'
expect deny 'echo $((make install) )'
expect deny '((make install) )'

# Selecting processes by the name taskboard.
expect deny 'pkill taskboard'
expect deny 'pkill -f taskboard'
expect deny 'killall taskboard'
expect deny 'sudo killall -9 taskboard'
expect deny 'kill $(pgrep taskboard)'
expect deny 'kill -9 `pgrep -f "taskboard start"`'
expect deny 'pgrep -f taskboard | xargs kill'
expect deny 'ps aux | grep taskboard | awk "{print \$2}" | xargs kill -9'
expect deny 'kill $(lsof -t -c taskboard)'
expect deny 'kill "$(pgrep -f taskboard)"'
expect deny $'sh -s <<EOF\npkill taskboard\nEOF'

# Selecting processes by the live port.
expect deny 'lsof -ti :3010 | xargs kill'
expect deny 'kill $(lsof -ti tcp:3010)'
expect deny 'kill -9 $(lsof -t -iTCP:3010 -sTCP:LISTEN)'
expect deny 'lsof -ti:3010 | xargs kill -9'
expect deny 'kill `lsof -t -i @localhost:3010`'

# The live data directory.
expect deny 'ls ~/Library/Application\ Support/taskboard'
expect deny 'sqlite3 "$HOME/Library/Application Support/taskboard/taskboard.db" .tables'
expect deny 'cat ~/"Library/Application Support"/taskboard/taskboard.pid'
expect deny $'git commit -F - <<\'MSG\'\nkeeps ~/Library/Application Support/taskboard intact\nMSG'
expect deny 'go run ./cmd/taskboard --db "$HOME/Library/Application Support/taskboard/taskboard.db" ticket list'

# Writes to the live API.
expect deny 'curl -X POST http://localhost:3010/api/projects -d "{}"'
expect deny 'curl -XDELETE localhost:3010/api/tickets/1'
expect deny 'curl --request PATCH http://127.0.0.1:3010/api/tickets/1'
expect deny 'curl -s -H "Content-Type: application/json" --data @t.json http://localhost:3010/api/tickets'
expect deny 'curl -sd "{}" localhost:3010/api/projects'
expect deny 'curl --json "{}" http://127.0.0.1:3010/api/projects'
expect deny 'http PUT localhost:3010/api/tickets/1 title=x'
expect deny 'wget --post-data "x=1" http://localhost:3010/api/projects'
expect deny 'curl -s -H "Content-Type: application/json" -d "$(cat t.json)" http://localhost:3010/api/tickets'
expect deny 'curl --json "$(jq -n {})" localhost:3010/api/projects'
expect deny 'curl -d `cat t.json` localhost:3010/api/tickets'
expect deny 'curl -d $(cat t.json) localhost:3010/api/tickets'
# A method the guard can't read counts as a write.
expect deny 'curl localhost:3010/api/x -X "$(echo POST)"'
expect deny 'curl -X "$METHOD" http://localhost:3010/api/tickets/1'
expect deny 'curl --request=${m} http://127.0.0.1:3010/api/tickets/1'
expect deny 'curl --request `echo DELETE` localhost:3010/api/tickets/1'
expect deny 'curl -X$(echo PUT) localhost:3010/api/tickets/1'
expect deny 'curl -sX "$(echo POST)" localhost:3010/api/projects'
expect deny 'curl -sXPOST localhost:3010/api/projects'
expect deny 'curl -dXYZ localhost:3010/api/projects'
expect deny 'wget --method="$(echo DELETE)" http://localhost:3010/api/tickets/1'

# Development builds without --db.
expect deny './taskboard ticket list'
expect deny './taskboard start'
expect deny '.tmp/live/taskboard project list'
expect deny 'go run ./cmd/taskboard start --port 3011'
expect deny 'go run ./cmd/taskboard ticket list'
expect deny 'go run github.com/tcarac/taskboard/cmd/taskboard mcp'
expect deny 'go build -o taskboard ./cmd/taskboard && ./taskboard clear --force'
expect deny 'nohup ./taskboard start --foreground > server.log 2>&1 &'
expect deny 'git commit -m "$(printf %s ok)" && ./taskboard ticket list'

# A substitution as the command word: the guard can't tell what it runs, so
# it goes by what the substitution names.
expect deny '$(which taskboard) ticket list'
expect deny '`command -v make` install'
expect deny '"$(command -v taskboard)" start --foreground'
expect deny 'sudo $(which gmake) -C . install'
expect deny '$(dirname $(which taskboard))/taskboard ticket list'
expect deny 'echo x | `which make` install'

echo "$cases guard checks, $failures failed (readers: ${parsers[*]})"
[ "$failures" -eq 0 ]
