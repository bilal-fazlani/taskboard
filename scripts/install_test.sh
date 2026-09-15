#!/usr/bin/env bash
# End-to-end test for scripts/install.sh. Every case runs in a throwaway sandbox:
# HOME points at a temp dir, so taskboard's default database, pid file and the
# backup folder all live inside it, and the live board is never touched.
#
# Usage: scripts/install_test.sh <live taskboard binary, as built by make test-install>
set -euo pipefail

real_bin=$(cd "$(dirname "${1:?usage: install_test.sh <built taskboard binary>}")" && pwd)/$(basename "$1")
script=$(cd "$(dirname "$0")" && pwd)/install.sh
REAL_HOME=$HOME
failures=0

fail() { echo "  FAIL: $*" >&2; failures=$((failures + 1)); }
pass() { echo "  ok: $*"; }

free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }

# wait_for <description> <command...>: retry a check for up to 10 seconds.
wait_for() {
  local what=$1; shift
  for _ in $(seq 1 40); do "$@" >/dev/null 2>&1 && return 0; sleep 0.25; done
  echo "  timed out waiting for: $what" >&2
  return 1
}

# new_sandbox sets S, HOME, DATA_DIR, BIN_DIR and PORT for one case.
new_sandbox() {
  S=$(mktemp -d "${TMPDIR:-/tmp}/taskboard-install-test.XXXXXX")
  export HOME="$S/home"
  mkdir -p "$HOME"
  case "$(uname -s)" in
    Darwin) DATA_DIR="$HOME/Library/Application Support/taskboard" ;;
    *)      unset XDG_CONFIG_HOME; DATA_DIR="$HOME/.config/taskboard" ;;
  esac
  # Refuse to go any further unless the database really resolves into the sandbox.
  case "$DATA_DIR" in "$S"/*) ;; *) echo "sandbox escape: $DATA_DIR" >&2; exit 2 ;; esac
  [ "$HOME" != "$REAL_HOME" ] || { echo "HOME was not redirected" >&2; exit 2; }
  BIN_DIR="$S/bin"
  PORT=$(free_port)
  SANDBOX_PIDS=()
}

cleanup_sandbox() {
  [ -n "${S:-}" ] || return 0
  local pids=("${SANDBOX_PIDS[@]+"${SANDBOX_PIDS[@]}"}")
  [ -f "$DATA_DIR/taskboard.pid" ] && pids+=("$(cat "$DATA_DIR/taskboard.pid")")
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  exec 3>&-
  export HOME=$REAL_HOME
  rm -rf "$S"
  S=
}
trap cleanup_sandbox EXIT

run_install() {
  set +e
  OUT=$(INSTALL_DIR="$BIN_DIR" PORT="$PORT" "$script" "$1" 2>&1)
  STATUS=$?
  set -e
}

holders() { lsof -t -- "$DATA_DIR/taskboard.db" 2>/dev/null | sort -u || true; }
listening_pid() { lsof -tiTCP:"$1" -sTCP:LISTEN 2>/dev/null | head -1 || true; }

echo "== the binary under test is the live build =="
# Only a binary marked by `make install` resolves the default database; an
# unmarked build refuses without --db, and every case below would fail on it.
new_sandbox
mkdir -p "$BIN_DIR"; cp "$real_bin" "$BIN_DIR/taskboard"
if "$BIN_DIR/taskboard" project list >"$S/out" 2>&1; then
  pass "runs without --db"
else
  fail "refuses the default database, so it is not the marked live build: $(head -1 "$S/out")"
  echo "run this through make test-install, which builds what make install ships" >&2
  cleanup_sandbox
  exit 1
fi
[ -f "$DATA_DIR/taskboard.db" ] && pass "default database resolves into the config dir" \
  || fail "no database at $DATA_DIR/taskboard.db"
case "$("$BIN_DIR/taskboard" start --help)" in *"(default 3010)"*) pass "start defaults to port 3010" ;;
  *) fail "start does not default to port 3010" ;; esac
cleanup_sandbox

echo "== upgrade over a running instance =="
new_sandbox
mkdir -p "$BIN_DIR"; cp "$real_bin" "$BIN_DIR/taskboard"
old_inode=$(ls -i "$BIN_DIR/taskboard" | awk '{print $1}')
"$BIN_DIR/taskboard" project create Sandbox --prefix SBX >/dev/null
"$BIN_DIR/taskboard" start --port "$PORT" >/dev/null
old_server=$(cat "$DATA_DIR/taskboard.pid")
mkfifo "$S/mcp.in"
"$BIN_DIR/taskboard" mcp <"$S/mcp.in" >/dev/null 2>&1 &
old_mcp=$!
exec 3>"$S/mcp.in"
dev_port=$(free_port)
"$BIN_DIR/taskboard" --db "$S/dev.db" start --foreground --port "$dev_port" >/dev/null 2>&1 &
dev=$!
SANDBOX_PIDS+=("$old_server" "$old_mcp" "$dev")
wait_for "server and mcp holding the database" sh -c "lsof -t -- '$DATA_DIR/taskboard.db' | grep -qx $old_server && lsof -t -- '$DATA_DIR/taskboard.db' | grep -qx $old_mcp"
wait_for "dev instance listening" curl -fsS "http://localhost:$dev_port/api/projects"

run_install "$real_bin"
[ "$STATUS" -eq 0 ] && pass "install exited 0" || { fail "install exited $STATUS"; echo "$OUT" >&2; }
kill -0 "$old_server" 2>/dev/null && fail "old server $old_server still running" || pass "old server stopped"
kill -0 "$old_mcp" 2>/dev/null && fail "old mcp $old_mcp still running" || pass "old mcp stopped"
kill -0 "$dev" 2>/dev/null && curl -fsS -o /dev/null "http://localhost:$dev_port/api/projects" \
  && pass "dev instance on its own --db left running" || fail "dev instance was disturbed"
new_inode=$(ls -i "$BIN_DIR/taskboard" | awk '{print $1}')
[ -x "$BIN_DIR/taskboard" ] && [ "$new_inode" != "$old_inode" ] && pass "binary replaced" || fail "binary not replaced"
new_server=$(cat "$DATA_DIR/taskboard.pid" 2>/dev/null || true)
[ -n "$new_server" ] && [ "$new_server" != "$old_server" ] && kill -0 "$new_server" 2>/dev/null \
  && pass "new server running (pid $new_server)" || fail "no new server process"
if [ -n "$new_server" ] && [ "$new_server" != "$old_server" ]; then
  [ "$(listening_pid "$PORT")" = "$new_server" ] && pass "new server owns port $PORT" || fail "port $PORT not served by new server"
  curl -fsS "http://localhost:$PORT/api/projects" | grep -q '"prefix":"SBX"' \
    && pass "data served after restart" || fail "project missing after restart"
else
  fail "port and data checks skipped: no new server"
fi
backup=$(ls -d "$HOME"/taskboard-backups/*/ 2>/dev/null | head -1 || true)
if [ -n "$backup" ] && [ -f "$backup/taskboard.db" ] && [ -x "$backup/taskboard.previous" ]; then
  pass "database and previous binary backed up"
else
  fail "backup missing or incomplete: '${backup}'"
fi
echo "$OUT" | grep -qi "reconnect" && pass "reminds to reconnect Claude clients" || fail "no reconnect reminder after stopping an mcp process"
cleanup_sandbox

echo "== fresh install, nothing running and no database =="
new_sandbox
run_install "$real_bin"
[ "$STATUS" -eq 0 ] && pass "install exited 0" || { fail "install exited $STATUS"; echo "$OUT" >&2; }
[ -x "$BIN_DIR/taskboard" ] && pass "binary installed" || fail "binary not installed"
wait_for "fresh server" curl -fsS "http://localhost:$PORT/api/projects" && pass "server running" || fail "server not running"
[ -f "$DATA_DIR/taskboard.db" ] && [ -f "$DATA_DIR/taskboard.pid" ] \
  && pass "server uses the default database, pid file beside it" || fail "default database or its pid file missing in $DATA_DIR"
[ -d "$HOME/taskboard-backups" ] && fail "created a backup with no database to back up" || pass "no backup when there is no database"
cleanup_sandbox

echo "== new binary that fails to start =="
new_sandbox
mkdir -p "$BIN_DIR"; cp "$real_bin" "$BIN_DIR/taskboard"
"$BIN_DIR/taskboard" project create Sandbox --prefix SBX >/dev/null
printf '#!/bin/sh\necho "simulated migration failure" >&2\nexit 1\n' >"$S/broken"
chmod +x "$S/broken"
run_install "$S/broken"
[ "$STATUS" -ne 0 ] && pass "install exited non-zero ($STATUS)" || fail "install reported success for a binary that cannot start"
echo "$OUT" | grep -q "simulated migration failure" && pass "surfaces the underlying error" || fail "underlying error not shown"
backup=$(ls -d "$HOME"/taskboard-backups/*/ 2>/dev/null | head -1 || true)
[ -n "$backup" ] && echo "$OUT" | grep -qF "${backup%/}" && pass "points at the backup" || fail "backup path not reported"
cleanup_sandbox

echo "== new binary whose daemon starts but never serves =="
# The realistic failure: `start` forks the daemon and returns 0, then the
# daemon dies opening the database. Only the health check can notice.
new_sandbox
mkdir -p "$BIN_DIR"; cp "$real_bin" "$BIN_DIR/taskboard"
"$BIN_DIR/taskboard" project create Sandbox --prefix SBX >/dev/null
printf '#!/bin/sh\ncase "$1" in start) exit 0 ;; esac\necho "simulated migration failure" >&2\nexit 1\n' >"$S/dies"
chmod +x "$S/dies"
run_install "$S/dies"
[ "$STATUS" -ne 0 ] && pass "install exited non-zero ($STATUS)" || fail "install reported success although nothing is serving"
echo "$OUT" | grep -q "did not come up" && pass "reports the server never came up" || fail "no health-check failure reported"
echo "$OUT" | grep -q "simulated migration failure" && pass "surfaces the underlying error" || fail "underlying error not shown"
cleanup_sandbox

echo
if [ "$failures" -eq 0 ]; then echo "all install tests passed"; else echo "$failures install test(s) failed" >&2; exit 1; fi
