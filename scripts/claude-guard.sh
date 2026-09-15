#!/usr/bin/env bash
# Claude Code PreToolUse hook for the Bash tool (wired up in .claude/settings.json).
# A tripwire that refuses shell commands likely to reach the live taskboard:
#
#   - make install (replaces the live binary and restarts the live server)
#   - pkill/killall taskboard, or kill fed by pgrep/pidof/ps/grep/lsof -c taskboard
#   - anything mentioning "Application Support/taskboard" (the live database)
#   - POST/PUT/PATCH/DELETE to localhost:3010 or 127.0.0.1:3010
#   - running a taskboard binary other than ~/.local/bin/taskboard without --db
#
# This is string matching on the command line, and it is easy to get around: a
# script file, eval, variables, aliases, a binary with another name, `go run .`
# inside cmd/taskboard, `cat <<EOF | sh`, a heredoc written before the command
# word (`<<EOF bash`), $'...' quoting with \' inside. The real protection is in
# the code: only a release binary or the one `make install` builds opens the
# default database; every other build needs --db and defaults to port 3011.
# See "Live board safety" in CLAUDE.md.
#
# Input: the hook event JSON on stdin; the command is .tool_input.command.
# Exit 0 allows the call; exit 2 with a reason on stderr blocks it.
#
# The JSON is read with jq, else python3, else a regex. The command is split
# into statements and simple commands by a small awk lexer that respects quotes,
# $(...) and heredocs (a body is skipped unless it feeds a shell), else by naive
# splitting. A missing tool only ever causes extra denials, never an allow-all.
# CLAUDE_GUARD_PARSER=jq|python3|raw and CLAUDE_GUARD_LEXER=bash force a
# fallback; scripts/claude-guard_test.sh uses them.
#
# Written for bash 3.2, which is what macOS ships as /bin/bash, and without a
# fork per word, so long commands stay fast.
set -f -o pipefail

payload=$(cat)
q="'"
dq='"'
bs='\'
nl=$'\n'
S=$'\036' # statement separator in lexed output
C=$'\037' # simple-command separator in lexed output

read_jq() {
  command -v jq >/dev/null 2>&1 || return 1
  printf '%s' "$payload" | jq -r '.tool_input.command | if type == "string" then . else "" end' 2>/dev/null
}

read_python() {
  command -v python3 >/dev/null 2>&1 || return 1
  printf '%s' "$payload" | python3 -c 'import json, sys
c = json.load(sys.stdin)["tool_input"].get("command", "")
sys.stdout.write(c if isinstance(c, str) else "")' 2>/dev/null
}

# Last resort without a JSON parser: the "command" string pulled out with a
# regex, JSON escapes undone. If even that fails, the whole payload is checked,
# which can only cause extra denials.
read_raw() {
  local s=$payload
  local field_re='"command"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)"'
  [[ $s =~ $field_re ]] && s=${BASH_REMATCH[1]}
  s=${s//"\\\\"/$'\035'}
  s=${s//"\\\""/$dq}
  s=${s//"\\n"/$nl}
  s=${s//"\\t"/ }
  s=${s//$'\035'/"$bs"}
  printf '%s' "$s"
}

case "${CLAUDE_GUARD_PARSER:-}" in
  jq) cmd=$(read_jq) || exit 1 ;;
  python3) cmd=$(read_python) || exit 1 ;;
  raw) cmd=$(read_raw) ;;
  *) cmd=$(read_jq) || cmd=$(read_python) || cmd=$(read_raw) ;;
esac

deny() {
  printf 'Blocked by scripts/claude-guard.sh: %s\nSee "Live board safety" in CLAUDE.md. The live taskboard on port 3010 must not be touched.\n' "$1" >&2
  exit 2
}

# The lexer prints the command on one line, with \036 between statements
# (; && || newline) and \037 between simple commands (| & and subshells).
# Separators inside quotes stay text. A command substitution, $(...) or
# backticks, leaves the word __SUBST__ in the command around it, so that
# command keeps all its words, and its contents follow as separate commands at
# the end of the same statement. Arithmetic, $((...)) and ((...)), is text. A
# heredoc body is dropped unless the command it feeds is a shell.
read -r -d '' LEXER <<'AWK'
function out(s) {
  if (lv == 1) printf "%s", s; else buf[lv] = buf[lv] s
  if (length(cur) < 256) cur = cur s
}
function mark(m) {
  if (m == S && pend[lv] != "") { s = pend[lv]; pend[lv] = ""; out(s) }
  out(m); cur = ""
}
function open_sub(kind) {
  out("__SUBST__")
  saved[lv] = cur; lv++; buf[lv] = ""; pend[lv] = ""; cur = ""; ws = 1
  sp++; st[sp] = kind
}
function close_sub(   s) {
  while (sp > 1 && st[sp] != "X" && st[sp] != "B") sp--
  s = buf[lv] pend[lv]; lv--; pend[lv] = pend[lv] C s; cur = saved[lv]
  sp--
}
function has_backtick(   k) {
  for (k = sp; k > 1; k--) if (st[k] == "B") return 1
  return 0
}
function feeds_shell(   parts, np, k, w) {
  np = split(cur, parts, /[ \t]+/)
  for (k = 1; k <= np; k++) {
    w = parts[k]
    gsub(/["']/, "", w)
    if (w == "" || w ~ /^[A-Za-z_][A-Za-z0-9_]*=/ || w ~ /^-/ || w ~ /^[0-9]/) continue
    if (w ~ /^(env|exec|nohup|time|sudo|nice|xargs|timeout|caffeinate)$/) continue
    sub(/.*\//, "", w)
    return w ~ /^(bash|sh|zsh|dash|fish)$/
  }
  return 0
}
{ src = (NR == 1) ? $0 : src "\n" $0 }
END {
  S = "\036"; C = "\037"
  n = split(src, ch, "")
  # Modes: N normal, P subshell (, X $( substitution, B backtick substitution,
  # D double quotes, A arithmetic. lv is the substitution nesting level.
  sp = 1; st[1] = "N"; lv = 1
  nh = 0; ws = 1
  i = 1
  while (i <= n) {
    c = ch[i]; top = st[sp]
    if (top == "A") {
      if (c == "(") { ad[sp]++; out(c); i++; continue }
      if (c == ")") {
        if (ad[sp] > 0) { ad[sp]--; out(c); i++; continue }
        if (ch[i + 1] == ")") { out("))"); i += 2 } else { out(c); i++ }
        sp--; continue
      }
      out(c == "\n" ? " " : c); i++; continue
    }
    if (c == "$" && ch[i + 1] == "(" && ch[i + 2] == "(") { out("$(("); sp++; st[sp] = "A"; ad[sp] = 0; i += 3; continue }
    if (c == "$" && ch[i + 1] == "(") { open_sub("X"); i += 2; continue }
    if (c == "`") { if (has_backtick()) close_sub(); else open_sub("B"); i++; continue }
    if (top == "D") {
      if (c == "\\") { out(c ch[i + 1]); i += 2; continue }
      if (c == "\"") { out(c); sp--; i++; continue }
      out(c == "\n" ? " " : c); i++; continue
    }
    if (c == "\\") {
      if (ch[i + 1] == "\n") { i += 2; continue }
      out(c ch[i + 1]); i += 2; ws = 0; continue
    }
    if (c == "'") {
      out(c); i++
      while (i <= n && ch[i] != "'") { out(ch[i] == "\n" ? " " : ch[i]); i++ }
      out("'"); i++; ws = 0; continue
    }
    if (c == "\"") { out(c); sp++; st[sp] = "D"; i++; ws = 0; continue }
    if (c == "#" && ws) { while (i <= n && ch[i] != "\n") i++; continue }
    if (c == "\n") {
      mark(S); i++; ws = 1
      for (k = 1; k <= nh; k++) {
        while (i <= n) {
          line = ""
          while (i <= n && ch[i] != "\n") { line = line ch[i]; i++ }
          i++
          if (tabs[k]) sub(/^\t+/, "", line)
          if (line == delim[k]) break
        }
      }
      nh = 0
      continue
    }
    if (c == "<" && ch[i + 1] == "<") {
      if (ch[i + 2] == "<") { out("<<<"); i += 3; ws = 1; continue }
      j = i + 2; t = 0
      if (ch[j] == "-") { t = 1; j++ }
      while (j <= n && (ch[j] == " " || ch[j] == "\t")) j++
      d = ""
      while (j <= n && ch[j] !~ /[ \t\n;&|<>()]/) { d = d ch[j]; j++ }
      gsub(/["'\\]/, "", d)
      if (d != "" && !feeds_shell()) { nh++; delim[nh] = d; tabs[nh] = t }
      out(" "); i = j; ws = 1; continue
    }
    if (c == "(" && ch[i + 1] == "(") { out("(("); sp++; st[sp] = "A"; ad[sp] = 0; i += 2; continue }
    if (c == "(") { mark(C); sp++; st[sp] = "P"; i++; ws = 1; continue }
    if (c == ")") {
      if (top == "X") { close_sub(); i++; continue }
      mark(C)
      if (top == "P") sp--
      i++; ws = 1; continue
    }
    if ((c == "&" && ch[i + 1] == "&") || (c == "|" && ch[i + 1] == "|")) { mark(S); i += 2; ws = 1; continue }
    if (c == ";") { mark(S); i++; ws = 1; continue }
    if (c == "&" && (ch[i + 1] == ">" || ch[i - 1] == ">" || ch[i - 1] == "<")) { out(c); i++; continue }
    if (c == "|" || c == "&") { mark(C); i++; ws = 1; continue }
    out(c); ws = (c == " " || c == "\t"); i++
  }
  while (lv > 1) close_sub()
  mark(S)
  printf "\n"
}
AWK

# lex <string>: sets LEXED. Without awk, split naively, ignoring quotes and
# heredocs, and check the command twice: once cut at every ( ) and backtick,
# once not, so that neither view hides what the other would deny.
lex() {
  if [ "${CLAUDE_GUARD_LEXER:-}" != bash ] && LEXED=$(printf '%s' "$1" | awk "$LEXER" 2>/dev/null); then
    return
  fi
  local s=$1 coarse
  s=${s//">&"/">"}
  s=${s//"<&"/"<"}
  s=${s//"&>"/">"}
  s=${s//"&&"/$S}
  s=${s//"||"/$S}
  s=${s//";"/$S}
  s=${s//$nl/$S}
  s=${s//"|"/$C}
  s=${s//"&"/$C}
  coarse=$s
  s=${s//"("/$C}
  s=${s//")"/$C}
  s=${s//"\`"/$C}
  LEXED=$s$S$coarse
}

# unquote_word <word>: sets UNQUOTED to the word without quote characters and
# with the ways of writing $HOME expanded.
unquote_word() {
  local w=$1
  w=${w//"$q"/}
  w=${w//"$dq"/}
  case "$w" in
    "~/"*) w="$HOME/${w#"~/"}" ;;
    '$HOME/'*) w="$HOME/${w#'$HOME/'}" ;;
    '${HOME}/'*) w="$HOME/${w#'${HOME}/'}" ;;
  esac
  UNQUOTED=$w
}

# words_of <command>: sets RAW to its words as written, and WORDS to its
# unquoted words with leading variable assignments and wrappers (env, sudo,
# nohup, xargs, ...) removed.
words_of() {
  local IFS=$' \t' w skipping=1
  RAW=($1)
  WORDS=()
  for w in ${RAW[@]+"${RAW[@]}"}; do
    unquote_word "$w"
    w=$UNQUOTED
    if [ -n "$skipping" ]; then
      case "$w" in
        '' | : | env | exec | nohup | time | sudo | nice | xargs | timeout | caffeinate | -* | [0-9]*) continue ;;
      esac
      if [[ $w =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; then continue; fi
      skipping=
    fi
    WORDS+=("$w")
  done
}

TB='*[Tt][Aa][Ss][Kk][Bb][Oo][Aa][Rr][Dd]*'

has_taskboard_word() {
  local w
  for w in "$@"; do
    case "$w" in $TB) return 0 ;; esac
  done
  return 1
}

check_make_install() {
  case "${WORDS[0]##*/}" in make | gmake) ;; *) return ;; esac
  local w
  for w in "${WORDS[@]:1}"; do
    [ "$w" = install ] && deny "make install replaces the live binary and restarts the live server. It is Bilal's action, never an agent's."
  done
}

check_dev_binary() {
  local b0=${WORDS[0]##*/} path i start=
  if [ "$b0" = taskboard ]; then
    path=${WORDS[0]}
    if [ "$path" = taskboard ]; then
      path=$(command -v taskboard 2>/dev/null || true)
    fi
    [ "$path" = "$HOME/.local/bin/taskboard" ] && return
    start=1
  elif [ "$b0" = go ] && [ "${WORDS[1]:-}" = run ]; then
    for ((i = 2; i < ${#WORDS[@]}; i++)); do
      if [[ ${WORDS[i]} =~ (^|/)cmd/taskboard(/|/main\.go)?$ ]]; then
        start=$((i + 1))
        break
      fi
    done
    [ -n "$start" ] || return
  else
    return
  fi

  case "${WORDS[start]:-}" in help | completion) return ;; esac
  for ((i = start; i < ${#WORDS[@]}; i++)); do
    case "${WORDS[i]}" in
      --help | -h | --db=?*) return ;;
    esac
  done
  # --db takes the next word as written, so --db "$(pwd)/x.db" counts too.
  for ((i = 0; i < ${#RAW[@]}; i++)); do
    [ "${RAW[i]}" = --db ] && [ -n "${RAW[i + 1]:-}" ] && return
  done
  deny "this runs a development taskboard build without --db. Pass --db ./.tmp/<name>.db and a port of 3011 or above. Only ~/.local/bin/taskboard may run without --db."
}

# check_kill_by_name: over the simple commands of one statement, in COMMANDS.
# Only a command whose own name is kill/pkill/killall counts as killing.
check_kill_by_name() {
  local line has_kill= looks_up_name= b0 i
  for line in ${COMMANDS[@]+"${COMMANDS[@]}"}; do
    words_of "$line"
    [ ${#WORDS[@]} -gt 0 ] || continue
    b0=${WORDS[0]##*/}
    case "$b0" in
      kill)
        has_kill=1
        ;;
      pkill | killall)
        has_taskboard_word "${WORDS[@]:1}" && deny "$b0 selects processes by name and would stop the live server. Stop only PIDs you started, or those from lsof -t -- <your own db path>."
        ;;
      pgrep | pidof | ps | grep | egrep | fgrep | rg | awk | sed)
        has_taskboard_word "${WORDS[@]:1}" && looks_up_name=1
        ;;
      lsof)
        for ((i = 1; i < ${#WORDS[@]}; i++)); do
          case "${WORDS[i]}" in
            -c) case "${WORDS[i + 1]:-}" in $TB) looks_up_name=1 ;; esac ;;
            -c$TB) looks_up_name=1 ;;
          esac
        done
        ;;
    esac
  done

  if [ -n "$has_kill" ] && [ -n "$looks_up_name" ]; then
    deny "this kills processes found by the name taskboard, which includes the live server. Stop only PIDs you started, or those from lsof -t -- <your own db path>."
  fi
}

HOST_RE='([Ll][Oo][Cc][Aa][Ll][Hh][Oo][Ss][Tt]|127\.0\.0\.1|0\.0\.0\.0|\[::1\]):3010([^0-9]|$)'
METHOD_RE='^(-X|--request=?|--method=?)?([Pp][Oo][Ss][Tt]|[Pp][Uu][Tt]|[Pp][Aa][Tt][Cc][Hh]|[Dd][Ee][Ll][Ee][Tt][Ee])$'
SHORT_DATA_RE='^-[A-Za-z]*[dFT]'
LONG_DATA_RE='^--(data|data-[a-z]+|json|form|form-string|upload-file|post-data|post-file)(=|$)'

# check_live_write <simple command>: only the flags of the command that names
# the 3010 URL count, not those of commands its output is piped into.
check_live_write() {
  local command=$1 w
  case "$command" in *:3010*) ;; *) return ;; esac
  [[ $command =~ $HOST_RE ]] || return
  words_of "$command"
  for w in ${RAW[@]+"${RAW[@]}"}; do
    unquote_word "$w"
    w=$UNQUOTED
    if [[ $w =~ $METHOD_RE ]] || [[ $w =~ $SHORT_DATA_RE ]] || [[ $w =~ $LONG_DATA_RE ]]; then
      deny "this writes to the live taskboard API on port 3010. Point writes at your own dev server on 3011 or above; GET requests to 3010 are fine."
    fi
  done
}

# Checked on the whole command, heredoc bodies and messages included. Quotes and
# backslashes may sit around the space and the slash. A regex rather than
# stripping quotes first: bash 3.2 substitutions and globs crawl on long commands.
QB="[\"'\\\\]*"
DATA_DIR_RE="[Aa][Pp][Pp][Ll][Ii][Cc][Aa][Tt][Ii][Oo][Nn]$QB $QB[Ss][Uu][Pp][Pp][Oo][Rr][Tt]$QB/+$QB[Tt][Aa][Ss][Kk][Bb][Oo][Aa][Rr][Dd]"
check_live_data_dir() {
  if [[ $1 =~ $DATA_DIR_RE ]]; then
    deny "this refers to the live board's data directory (Application Support/taskboard). Use a database under ./.tmp/ with --db."
  fi
}

# check <command string> <depth>: every rule over one command string. Commands
# wrapped in bash -c / sh -c are unquoted and checked again.
check() {
  local input=$1 depth=$2 statement line i
  local -a statements commands
  check_live_data_dir "$input"
  lex "$input"

  local IFS=$S
  statements=($LEXED)
  for statement in ${statements[@]+"${statements[@]}"}; do
    IFS=$C
    commands=($statement)
    IFS=$' \t\n'
    COMMANDS=(${commands[@]+"${commands[@]}"})
    check_kill_by_name

    for line in ${commands[@]+"${commands[@]}"}; do
      check_live_write "$line"
      words_of "$line"
      [ ${#WORDS[@]} -gt 0 ] || continue
      check_make_install
      check_dev_binary
      case "${WORDS[0]##*/}" in
        bash | sh | zsh | dash | fish)
          [ "$depth" -lt 3 ] || continue
          for ((i = 1; i < ${#WORDS[@]}; i++)); do
            if [[ ${WORDS[i]} =~ ^-[a-z]*c$ ]]; then
              check "${WORDS[*]:i+1}" $((depth + 1))
              break
            fi
          done
          ;;
      esac
    done
    IFS=$S
  done
}

check "$cmd" 0
exit 0
