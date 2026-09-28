#!/usr/bin/env bash
# Tests for scripts/claude-writeback-reminder.sh: sample hook input JSON and
# transcripts in, reminder or silence out. Every case runs through each JSON
# reader (jq, python3) that is available, each with a fresh state directory.
# Nothing here calls the taskboard; transcripts and state live in a temp dir.
#
# Usage: bash scripts/claude-writeback-reminder_test.sh   (or: make test-writeback)
set -uo pipefail

hook=$(cd "$(dirname "$0")" && pwd)/claude-writeback-reminder.sh
shell=${HOOK_BASH:-bash} # e.g. HOOK_BASH=/bin/bash to test with the bash 3.2 macOS ships
failures=0
cases=0

command -v python3 >/dev/null 2>&1 || { echo "python3 is needed to build the hook JSON" >&2; exit 1; }
parsers=(python3)
command -v jq >/dev/null 2>&1 && parsers+=(jq)

work=$(mktemp -d "${TMPDIR:-/tmp}/claude-writeback-test.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT

# Transcript lines as Claude Code writes them: compact JSON, one entry per line.
# tool <name> <input JSON>: an assistant entry holding one taskboard tool_use.
tool() {
  printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_%s","name":"mcp__taskboard__%s","input":%s}]}}' "$RANDOM" "$1" "$2"
}
call_line=$(tool get_ticket '{"id":"ACP-1"}')
# A write-back, with its keys in another order than Claude Code writes today.
writeback_line='{"message":{"content":[{"input":{"appendDescription":"Decision: x.","id":"ACP-1"},"name":"mcp__taskboard__update_ticket","id":"toolu_9","type":"tool_use"}],"role":"assistant"},"type":"assistant"}'
other_line='{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"go test ./..."}}]}}'
mention_line='{"type":"user","message":{"content":"call {\"type\":\"tool_use\",\"name\":\"mcp__taskboard__get_ticket\",\"input\":{\"id\":\"ACP-1\"}} later"}}'
# A loaded tool schema, not a call; Claude Code writes these again after a compaction.
schema_line='{"type":"attachment","attachment":{"type":"deferred_tools_record","entries":[{"name":"mcp__taskboard__get_ticket","description":"Get a ticket."}]}}'

# transcript <name> <line>...: writes a transcript, prints its path.
transcript() {
  local f=$work/$1.jsonl
  shift
  printf '%s\n' "$other_line" "$@" >"$f"
  printf '%s' "$f"
}
add() { printf '%s\n' "$2" >>"$1"; }

# hook_json <event> <session> <transcript> [extra JSON object merged in]
hook_json() {
  python3 -c 'import json, sys
d = {"hook_event_name": sys.argv[1], "session_id": sys.argv[2], "transcript_path": sys.argv[3], "cwd": "/repo"}
if sys.argv[1] == "Stop":
    d.update({"stop_hook_active": False, "last_assistant_message": "done", "background_tasks": [], "session_crons": []})
if sys.argv[1] == "PreCompact":
    d.update({"trigger": "manual", "custom_instructions": None})
if sys.argv[1] == "SessionStart":
    d.update({"source": "compact", "model": "claude-opus-5-5"})
d.update(json.loads(sys.argv[4]) if len(sys.argv) > 4 else {})
print(json.dumps(d))' "$@"
}
continuation='{"stop_hook_active": true}'
background='{"background_tasks": [{"id": "t1", "type": "shell", "status": "running"}]}'
auto='{"trigger": "auto"}'

state=
run_parser=
# fire <json>: runs the hook, sets $out and $status.
fire() {
  out=$(printf '%s' "$1" | CLAUDE_WRITEBACK_STATE_DIR=$state CLAUDE_WRITEBACK_PARSER=$run_parser "$shell" "$hook" 2>"$work/stderr")
  status=$?
}

fail() {
  failures=$((failures + 1))
  echo "FAIL [$run_parser] $*" >&2
}

# expect silent|precompact|stop|sessionstart <label> <json>
expect() {
  local want=$1 label=$2 json=$3 check name
  cases=$((cases + 1))
  fire "$json"
  if [ "$status" -ne 0 ]; then fail "$label: exit $status"; return; fi
  if [ -s "$work/stderr" ]; then fail "$label: stderr: $(cat "$work/stderr")"; return; fi
  case $want in
    silent)
      [ -z "$out" ] || fail "$label: want silence, got: $out"
      return
      ;;
    # The reminder points at the skill's rules rather than repeating them.
    precompact) check='set(d) == {"decision", "reason"} and d["decision"] == "block" and rules in d["reason"]' ;;
    stop | sessionstart)
      [ "$want" = stop ] && name=Stop || name=SessionStart
      check="set(d) == {'hookSpecificOutput'} and d['hookSpecificOutput']['hookEventName'] == '$name' and rules in d['hookSpecificOutput']['additionalContext']"
      ;;
  esac
  printf '%s' "$out" | python3 -c "import json, sys
d = json.load(sys.stdin)
rules = 'write-back rules in the taskboard skill'
sys.exit(0 if $check else 1)" 2>/dev/null || fail "$label: want $want reminder, got: $out"
  # Short: a few lines' worth, cheap in tokens.
  [ "${#out}" -le 500 ] || fail "$label: reminder is ${#out} chars, over 500"
}

# stops <count> <label> <session> <transcript> [extra]: that many silent Stops.
stops() {
  local i extra=${5:-}
  [ -n "$extra" ] || extra='{}'
  for i in $(seq 1 "$1"); do
    expect silent "$2 ($i)" "$(hook_json Stop "$3" "$4" "$extra")"
  done
}

for run_parser in "${parsers[@]}"; do
  state=$work/state-$run_parser
  rm -rf "$state"

  # Only calls that act on a ticket count: its input names a ticket, or it creates one.
  for t in 'get_ticket {"id":"ACP-1"}' 'update_ticket {"id":"ACP-1","status":"done"}' \
    'move_ticket {"key":"ACP-1","status":"done"}' 'create_ticket {"project":"ACP","title":"x"}' \
    'toggle_subtask {"id":"01M3","completed":true}' 'create_subtask {"ticketId":"ACP-1","title":"x"}' \
    'batch_create_subtasks {"ticketId":"ACP-1","subtasks":[]}' 'get_document {"ticket":"ACP-1","id":"Review 1"}' \
    'create_document {"ticket":"ACP-1","name":"Mock","content":"x"}'; do
    f=$(transcript "t-${t%% *}" "$(tool "${t%% *}" "${t#* }")")
    expect stop "ticket call ${t%% *}" "$(hook_json Stop "t-${t%% *}" "$f")"
  done
  f=$(transcript reordered "$writeback_line")
  expect stop 'ticket call with its keys in another order' "$(hook_json Stop reordered "$f")"
  f=$(transcript board "$(tool list_projects '{}')" "$(tool get_now '{}')" \
    "$(tool list_tickets '{"project":"ACP","status":"todo"}')" "$(tool list_epics '{"project":"ACP"}')" \
    "$(tool list_labels '{}')" "$(tool get_project '{"id":"ACP"}')" "$(tool get_board '{"project":"ACP"}')" \
    "$(tool list_entries '{"project":"ACP"}')" "$(tool get_document '{"epic":"Write-back","project":"ACP","id":"Spec"}')")
  expect silent 'board-level reads only: turn end' "$(hook_json Stop board "$f")"
  expect silent 'board-level reads only: /compact' "$(hook_json PreCompact board "$f")"
  expect silent 'board-level reads only: auto-compaction' "$(hook_json PreCompact board "$f" "$auto")"
  expect silent 'board-level reads only: after it' "$(hook_json SessionStart board "$f")"
  f=$(transcript projects "$(tool list_projects '{}')")
  expect silent 'only list_projects' "$(hook_json Stop projects "$f")"
  f=$(transcript mentioned "$mention_line" "$schema_line")
  expect silent 'ticket call only mentioned in text or loaded' "$(hook_json Stop mentioned "$f")"
  f=$(transcript via-subagent)
  mkdir -p "$work/via-subagent/subagents"
  printf '%s\n' "$call_line" >"$work/via-subagent/subagents/agent-a1.jsonl"
  expect stop 'ticket call only in a subagent transcript' "$(hook_json Stop via-subagent "$f")"

  # The turn-end reminder: at the first turn end once a ticket was touched.
  f=$(transcript s1)
  expect silent 'turn end before any ticket call' "$(hook_json Stop s1 "$f")"
  add "$f" "$call_line"
  expect stop 'turn end after a ticket call' "$(hook_json Stop s1 "$f")"
  expect silent 'its continuation' "$(hook_json Stop s1 "$f" "$continuation")"
  stops 3 'later turn ends' s1 "$f"

  # No reminder, and no reminder used up, while continuing or paused.
  f=$(transcript s3 "$call_line")
  expect silent 'stop hook continuation' "$(hook_json Stop s3 "$f" "$continuation")"
  expect silent 'background task in flight' "$(hook_json Stop s3 "$f" "$background")"
  expect silent 'scheduled wakeup pending' "$(hook_json Stop s3 "$f" '{"session_crons": [{"id": "c1", "schedule": "*/5 * * * *", "recurring": true}]}')"
  expect stop 'turn end once nothing is pending' "$(hook_json Stop s3 "$f")"

  # Re-arming: again only after new ticket calls since the last reminder and at
  # least 20 turns since it. The write-back that answers a reminder is no new work.
  f=$(transcript r1 "$call_line")
  expect stop 're-arm: first reminder' "$(hook_json Stop r1 "$f")"
  add "$f" "$writeback_line"
  expect silent 're-arm: write-back in the continuation' "$(hook_json Stop r1 "$f" "$continuation")"
  stops 25 're-arm: turns after the write-back, no new calls' r1 "$f"
  add "$f" "$call_line"
  expect stop 're-arm: new calls after 26 turns' "$(hook_json Stop r1 "$f")"
  add "$f" "$writeback_line"
  expect silent 're-arm: second write-back in the continuation' "$(hook_json Stop r1 "$f" "$continuation")"
  add "$f" "$call_line"
  stops 19 're-arm: new calls but under 20 turns' r1 "$f"
  expect stop 're-arm: new calls and 20 turns' "$(hook_json Stop r1 "$f")"
  add "$f" "$schema_line" # a compaction's schema record is no call
  stops 25 're-arm: turns with no new calls' r1 "$f"

  # A continuation that never ran its Stop: the next turn end absorbs the write-back.
  f=$(transcript r3 "$call_line")
  expect stop 'absorb: reminder' "$(hook_json Stop r3 "$f")"
  add "$f" "$writeback_line"
  stops 25 'absorb: write-back seen first at a plain turn end' r3 "$f"

  # Continuations are not turns; paused turns count but never fire.
  f=$(transcript r2 "$call_line")
  expect stop 'continuations: first reminder' "$(hook_json Stop r2 "$f")"
  expect silent 'continuations: its continuation' "$(hook_json Stop r2 "$f" "$continuation")"
  add "$f" "$call_line"
  stops 17 'continuations: turn' r2 "$f"
  for i in 1 2 3; do
    expect silent "continuations: continuation $i" "$(hook_json Stop r2 "$f" "$continuation")"
  done
  expect silent 'continuations: turn 18' "$(hook_json Stop r2 "$f")"
  expect silent 'continuations: turn 19, background task in flight' "$(hook_json Stop r2 "$f" "$background")"
  expect silent 'continuations: turn 20, background task in flight' "$(hook_json Stop r2 "$f" "$background")"
  expect stop 'continuations: turn 21, nothing pending' "$(hook_json Stop r2 "$f")"

  # Manual /compact: blocked while there are ticket calls since the last reminder;
  # the block starts a new window, so /compact again goes through.
  f=$(transcript m1 "$call_line")
  expect precompact '/compact after ticket calls' "$(hook_json PreCompact m1 "$f")"
  expect silent '/compact again' "$(hook_json PreCompact m1 "$f")"
  expect silent 'no reminder after a manual compaction' "$(hook_json SessionStart m1 "$f")"
  add "$f" "$writeback_line"
  expect silent 'turn that writes back after the block' "$(hook_json Stop m1 "$f")"
  expect silent '/compact after only the write-back' "$(hook_json PreCompact m1 "$f")"
  stops 25 'turns after the write-back, no new calls' m1 "$f"
  add "$f" "$call_line"
  expect precompact 'later /compact after new ticket calls' "$(hook_json PreCompact m1 "$f")"
  expect silent 'and /compact again' "$(hook_json PreCompact m1 "$f")"
  f=$(transcript m2 "$call_line")
  expect stop '/compact right after a turn-end reminder: reminder' "$(hook_json Stop m2 "$f")"
  expect silent '/compact right after a turn-end reminder' "$(hook_json PreCompact m2 "$f")"
  f=$(transcript m3)
  expect silent '/compact without ticket calls' "$(hook_json PreCompact m3 "$f")"

  # After an auto-compaction: once per compaction, in sessions that touched a ticket.
  f=$(transcript a1 "$call_line")
  expect silent 'auto-compaction is not blocked' "$(hook_json PreCompact a1 "$f" "$auto")"
  expect silent 'resume before the compaction finished' "$(hook_json SessionStart a1 "$f" '{"source": "resume"}')"
  expect sessionstart 'after an auto-compaction' "$(hook_json SessionStart a1 "$f")"
  expect silent 'second start after the same compaction' "$(hook_json SessionStart a1 "$f")"
  add "$f" "$writeback_line"
  stops 25 'turns after the write-back it prompted' a1 "$f"
  expect silent 'second auto-compaction' "$(hook_json PreCompact a1 "$f" "$auto")"
  expect sessionstart 'after the second auto-compaction' "$(hook_json SessionStart a1 "$f")"
  f=$(transcript a2)
  expect silent 'auto-compaction without ticket calls' "$(hook_json PreCompact a2 "$f" "$auto")"
  expect silent 'after it' "$(hook_json SessionStart a2 "$f")"
  f=$(transcript a3 "$call_line")
  expect silent 'auto-compaction inside a subagent' "$(hook_json PreCompact a3 "$f" '{"trigger": "auto", "agent_id": "a1"}')"
  expect silent 'after it' "$(hook_json SessionStart a3 "$f")"
  expect silent 'compaction with an unknown trigger' "$(hook_json PreCompact a3 "$f" '{"trigger": "other"}')"

  # Hook calls inside a subagent, other events and bad input stay silent.
  f=$(transcript s6 "$call_line")
  expect silent 'turn end inside a subagent' "$(hook_json Stop s6 "$f" '{"agent_id": "a1", "agent_type": "general-purpose"}')"
  expect silent 'session end' "$(hook_json SessionEnd s6 "$f" '{"reason": "prompt_input_exit"}')"
  expect silent 'missing transcript' "$(hook_json Stop s6 "$work/nope.jsonl")"
  expect silent 'no transcript path' "$(hook_json Stop s6 "")"
  expect silent 'session ID with a path in it' "$(hook_json Stop ../../s7 "$f")"
  expect silent 'input that is not JSON' 'not json'
  expect silent 'empty input' ''

  # State stays in the state directory: one file per reminded session, nothing left over.
  cases=$((cases + 1))
  leftovers=$(cd "$state" && ls | grep -v '\.state$' | tr '\n' ' ')
  [ -z "$leftovers" ] || fail "files other than state: $leftovers"
done

echo "$cases write-back reminder checks, $failures failed (readers: ${parsers[*]})"
[ "$failures" -eq 0 ]
