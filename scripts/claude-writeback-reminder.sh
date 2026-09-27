#!/usr/bin/env bash
# Claude Code hook (wired up in .claude/settings.json) that reminds the agent to
# write back to the taskboard, following the write-back rules in the taskboard
# skill's Standing rules. It only reminds; it never calls the taskboard, opens a
# port or touches a database. It reads the hook input on stdin and the session's
# transcript, and keeps small markers in its state directory, nothing else.
#
# A session has touched a ticket through taskboard calls that act on one: a call
# whose input names a ticket (ticket, ticketId, or the ticket and subtask tools'
# own id) or that creates one. Board-level reads such as list_projects, get_now or
# get_board don't count. They are read from the tool_use blocks of the transcript
# and its subagents' transcripts; a session with none gets no reminder at all.
#
# Every reminder starts a window: the state file <session>.state holds the ticket
# calls counted at the last reminder, the turns ended since, and whether the calls
# the agent makes answering it are still to be absorbed. Those calls are absorbed
# at the next Stop (the stop-hook continuation after a Stop reminder, or the turn
# after a compaction reminder), so they never count as new work.
#
#   Stop: after the turn, hands the agent the reminder as hook feedback, so it
#     takes one more turn to write back while it still can (SessionEnd runs after
#     the conversation and cannot make the agent act). It fires at the first turn
#     end once the session has touched a ticket, then again only after new ticket
#     calls since the last reminder and at least $rearm_turns turns since it. Turns
#     are counted here, one per Stop that is not a stop-hook continuation (Stop
#     fires once per finished turn); the transcript's turn records are undocumented
#     and may lag. No reminder during a continuation, or while background tasks or
#     scheduled wakeups are pending, since the session is only paused then (those
#     turns still count).
#   PreCompact, trigger "manual" (/compact): blocks the compaction when there are
#     ticket calls since the last reminder, so the agent writes back before the
#     detail is summarised away. The reason is shown to the person and lands in the
#     conversation, where the agent reads it on the next turn; /compact again goes
#     through, as the block starts a new window.
#   PreCompact, trigger "auto": never blocks (a blocked auto-compaction never
#     reaches the agent: it is skipped silently or, when the context is already
#     full, the request fails). It only notes the compaction for SessionStart.
#   SessionStart, source "compact": after an auto-compaction noted above, hands
#     the agent the reminder as context, once per compaction, so it writes back
#     what the summary kept.
#
# State lives under $CLAUDE_WRITEBACK_STATE_DIR (default
# ${TMPDIR:-/tmp}/claude-writeback-reminder). Hook calls inside a subagent
# (agent_id set) are skipped.
#
# Exit 0 always; the reminder is JSON on stdout. Without jq or python3 it stays silent.
set -u

rearm_turns=20
payload=$(cat)

# CLAUDE_WRITEBACK_PARSER=jq|python3 forces one JSON reader (the test runs both).
parser=${CLAUDE_WRITEBACK_PARSER:-}
if [ "$parser" != python3 ] && command -v jq >/dev/null 2>&1; then
  parser=jq
elif [ "$parser" != jq ] && command -v python3 >/dev/null 2>&1; then
  parser=python3
else
  exit 0
fi

fields() {
  if [ "$parser" = jq ]; then
    printf '%s' "$payload" | jq -r '[
      (.hook_event_name // ""), (.session_id // ""), (.transcript_path // ""),
      (.agent_id // ""), (.stop_hook_active == true),
      (((.background_tasks // []) | length) + ((.session_crons // []) | length)),
      (.trigger // ""), (.source // "")
    ] | map(tostring) | join("\u001f")' 2>/dev/null
  else
    printf '%s' "$payload" | python3 -c 'import json, sys
d = json.load(sys.stdin)
s = lambda k: d.get(k) if isinstance(d.get(k), str) else ""
n = lambda k: len(d.get(k) or [])
print("\x1f".join([s("hook_event_name"), s("session_id"), s("transcript_path"), s("agent_id"),
    "true" if d.get("stop_hook_active") is True else "false",
    str(n("background_tasks") + n("session_crons")), s("trigger"), s("source")]))' 2>/dev/null
  fi
}

# Fields are split on the unit separator, not a tab: IFS whitespace would merge an
# empty field into its neighbour.
IFS=$'\x1f' read -r event session transcript agent active pending trigger source <<<"$(fields)" || exit 0

[ -z "$agent" ] || exit 0
case $session in '' | *[!A-Za-z0-9_-]*) exit 0 ;; esac
case $event/$trigger$source in
  PreCompact/manual | PreCompact/auto | SessionStart/compact | Stop/) ;;
  *) exit 0 ;;
esac
[ -n "$transcript" ] && [ -f "$transcript" ] || exit 0

# ticket_calls: prints how many taskboard tool_use blocks act on a ticket. Lines
# are parsed as JSON, so key order and text that merely mentions a tool don't matter.
ticket_tools='((get|create|update|move|delete)_ticket|(create|delete|toggle)_subtask|batch_create_subtasks)'
ticket_calls() {
  shopt -s nullglob
  grep -h -s -F '"mcp__taskboard__' "$transcript" "${transcript%.jsonl}"/subagents/*.jsonl |
    if [ "$parser" = jq ]; then
      jq -R -n --arg tools "^$ticket_tools\$" '[inputs | fromjson?
        | select(type == "object" and .type == "assistant") | .message.content? | arrays | .[]
        | select(type == "object" and .type == "tool_use" and (.name | type) == "string")
        | select(.name | startswith("mcp__taskboard__"))
        | select((.name | ltrimstr("mcp__taskboard__") | test($tools))
            or ((.input | type) == "object" and ([.input.ticket, .input.ticketId] | any(type == "string" and length > 0))))
      ] | length' 2>/dev/null
    else
      python3 -c 'import json, re, sys
tools = re.compile(sys.argv[1])
n = 0
for line in sys.stdin:
    try:
        d = json.loads(line)
    except ValueError:
        continue
    m = d.get("message") if isinstance(d, dict) and d.get("type") == "assistant" else None
    c = m.get("content") if isinstance(m, dict) else None
    for b in c if isinstance(c, list) else []:
        name = b.get("name") if isinstance(b, dict) and b.get("type") == "tool_use" else None
        if not isinstance(name, str) or not name.startswith("mcp__taskboard__"):
            continue
        i = b.get("input")
        named = isinstance(i, dict) and any(isinstance(i.get(k), str) and i.get(k) for k in ("ticket", "ticketId"))
        if tools.fullmatch(name[len("mcp__taskboard__"):]) or named:
            n += 1
print(n)' "$ticket_tools" 2>/dev/null
    fi
}
calls=$(ticket_calls)
case ${calls:-x} in *[!0-9]*) exit 0 ;; esac

state=${CLAUDE_WRITEBACK_STATE_DIR:-${TMPDIR:-/tmp}/claude-writeback-reminder}
file=$state/$session.state
last=0 turns=0 absorb=0 known=false
if [ -f "$file" ]; then
  known=true
  read -r last turns absorb <"$file" 2>/dev/null
  case ${last:-x} in *[!0-9]*) last=0 ;; esac
  case ${turns:-x} in *[!0-9]*) turns=0 ;; esac
  case ${absorb:-x} in 1) ;; *) absorb=0 ;; esac
fi
save() { # save <last> <turns> <absorb>
  mkdir -p "$state" 2>/dev/null && printf '%s %s %s\n' "$@" >"$file.$$" && mv -f "$file.$$" "$file"
}

rules="per the write-back rules in the taskboard skill's Standing rules"
remind() { # remind <event> <text>: starts a new window, then prints the reminder
  save "$calls" 0 1 || exit 0
  if [ "$1" = PreCompact ]; then
    printf '{"decision":"block","reason":"%s"}\n' "$2"
  else
    printf '{"hookSpecificOutput":{"hookEventName":"%s","additionalContext":"%s"}}\n' "$1" "$2"
  fi
  exit 0
}

case $event/$trigger$source in
  SessionStart/compact)
    # Once per compaction: the marker PreCompact left is consumed.
    rm "$state/$session.compacted" 2>/dev/null || exit 0
    remind SessionStart "Taskboard write-back check after compaction: this session used taskboard tickets. Decisions, corrections and learnings the summary above kept, and where unfinished work stopped with its next step, go on the board $rules. When they are already there, nothing more is needed."
    ;;
  PreCompact/auto)
    [ "$calls" -gt 0 ] || exit 0
    mkdir -p "$state" 2>/dev/null && : >"$state/$session.compacted"
    exit 0
    ;;
  PreCompact/manual)
    [ "$calls" -gt "$last" ] || exit 0
    remind PreCompact "Taskboard write-back first: this session used taskboard tickets. Before the detail is summarised away, the agent records on the board what was decided and rejected, what the person corrected and what was learned, $rules. Then /compact again."
    ;;
esac

# Stop. Calls made answering the last reminder are absorbed at the first Stop after it.
if [ "$active" = true ]; then
  [ "$absorb" = 1 ] && save "$calls" "$turns" 0
  exit 0
fi
[ "$calls" -gt 0 ] || exit 0
stop_text="Taskboard write-back check: this session used taskboard tickets. Decisions, corrections and learnings that live only in this chat, and where unfinished work stopped with its next step, go on the board $rules. When they are already there, nothing more is needed."
if [ "$known" = false ]; then
  [ "$pending" = 0 ] || exit 0
  remind Stop "$stop_text"
fi
turns=$((turns + 1))
[ "$absorb" = 1 ] && last=$calls absorb=0
if [ "$pending" = 0 ] && [ "$calls" -gt "$last" ] && [ "$turns" -ge "$rearm_turns" ]; then
  remind Stop "$stop_text"
fi
save "$last" "$turns" "$absorb"
exit 0
