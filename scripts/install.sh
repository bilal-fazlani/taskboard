#!/usr/bin/env bash
# Install a taskboard build over the running one: stop every process using the
# live database, back up the database and the previous binary, swap the new
# binary in, then start the server and wait until it answers.
#
# Usage: scripts/install.sh <built taskboard binary>
#   INSTALL_DIR  where the binary goes   (default: ~/.local/bin)
#   PORT         port for the web server (default: 3010)
set -euo pipefail

src=${1:?usage: install.sh <built taskboard binary>}
INSTALL_DIR=${INSTALL_DIR:-$HOME/.local/bin}
PORT=${PORT:-3010}
target="$INSTALL_DIR/taskboard"

# Must match os.UserConfigDir, which is where the binary keeps its database.
case "$(uname -s)" in
  Darwin) data_dir="$HOME/Library/Application Support/taskboard" ;;
  *)      data_dir="${XDG_CONFIG_HOME:-$HOME/.config}/taskboard" ;;
esac
db="$data_dir/taskboard.db"

for tool in lsof curl; do
  command -v "$tool" >/dev/null || { echo "error: $tool is required" >&2; exit 1; }
done
[ -f "$src" ] || { echo "error: no binary at $src (run make install, which builds the marked binary first)" >&2; exit 1; }

# Processes holding the live database: the web server and every `taskboard mcp`
# a Claude client started. Instances on their own --db path are not included.
holders() {
  [ -e "$db" ] || return 0
  lsof -t -- "$db" 2>/dev/null | sort -u || true
}

# 1. Stop. Anything still running old code against a migrated database breaks.
stopped_mcp=0
pids=$(holders)
if [ -n "$pids" ]; then
  for pid in $pids; do
    case "$(ps -o command= -p "$pid" 2>/dev/null)" in *" mcp"*) stopped_mcp=$((stopped_mcp + 1)) ;; esac
  done
  echo "Stopping taskboard processes using the database: $(echo $pids)"
  kill -TERM $pids 2>/dev/null || true
  for _ in $(seq 1 20); do
    [ -z "$(holders)" ] && break
    sleep 0.25
  done
  left=$(holders)
  if [ -n "$left" ]; then
    echo "Forcing stop of: $(echo $left)"
    kill -KILL $left 2>/dev/null || true
    sleep 0.5
  fi
  left=$(holders)
  [ -z "$left" ] || { echo "error: still holding $db: $(echo $left)" >&2; exit 1; }
fi

# 2. Back up. Migrations are one-way, so this is the rollback point.
backup=""
if [ -e "$db" ]; then
  backup="$HOME/taskboard-backups/$(date +%Y-%m-%dT%H%M%S)"
  mkdir -p "$backup"
  cp -p "$db"* "$backup/"
  [ -x "$target" ] && cp -p "$target" "$backup/taskboard.previous"
  echo "Backed up database$([ -x "$target" ] && echo " and previous binary") to $backup"
fi

# 3. Install under a temporary name first, so a failed copy never leaves a
# half-written binary where the old one was.
mkdir -p "$INSTALL_DIR"
tmp="$INSTALL_DIR/.taskboard.new.$$"
trap 'rm -f "$tmp"' EXIT
cp "$src" "$tmp"
chmod 0755 "$tmp"
mv -f "$tmp" "$target"
echo "Installed $target"

# 4. Start. The daemon discards its own output and `start` returns before the
# server has opened the database, so success is only proven by it answering.
fail_start() {
  echo "error: taskboard did not come up on port $PORT" >&2
  echo "Re-running a command in the foreground to show the error:" >&2
  "$target" project list >/dev/null || true
  [ -n "$backup" ] && echo "Database backup (with the previous binary) is in $backup" >&2
  exit 1
}
"$target" start --port "$PORT" || fail_start

pid_file="$data_dir/taskboard.pid"
up=""
for _ in $(seq 1 40); do
  pid=$(cat "$pid_file" 2>/dev/null || true)
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null \
    && [ "$(lsof -tiTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | head -1 || true)" = "$pid" ] \
    && curl -fsS -o /dev/null "http://localhost:$PORT/api/projects"; then
    up=1
    break
  fi
  sleep 0.25
done
[ -n "$up" ] || fail_start

echo "Taskboard running at http://localhost:$PORT (pid $pid)"
if [ "$stopped_mcp" -gt 0 ]; then
  echo "Stopped $stopped_mcp taskboard MCP server(s): reconnect the Claude clients that were using them."
fi
