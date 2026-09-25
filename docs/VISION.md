# Taskboard: product vision

Date: 2026-09-09
Status: north star. This document describes where the product is going, not what
is being built next. Individual features get their own design in
`docs/superpowers/specs/` and must be consistent with this document.

## One sentence

Taskboard is the control plane a person uses to direct a fleet of AI agents:
a live dependency graph of the work, where every card that needs a human turns
a colour, and answering it sends the agent on its way.

## The shift

Taskboard today is a kanban board with a CLI and an MCP server. Agents already
create, update and close tickets through those surfaces. That part is right and
stays.

What changes is who the board is for. A kanban board is built for people moving
their own work between columns. The next Taskboard is built for one person
supervising many agents that move the work themselves. The person's job becomes:

- see what is ready, what is blocked, and what is in flight;
- notice, instantly, when an agent is waiting on them;
- answer or approve with the least possible friction;
- keep the fleet honest: who is on what, since when, and is it still alive.

Everything in this document follows from that job.

## Principles

1. **The graph is the home page.** Work is a flow, not a set of columns. The
   primary view is a left-to-right dependency graph: ready work on the left,
   deeply blocked work on the right, arrows showing what unblocks what.
2. **Agents write, the person steers.** Agents own status transitions in the
   normal course of work. The person's inputs are answers, approvals, new tickets
   and edits, not dragging cards around.
3. **Waiting on a human is a first-class state**, never a comment buried in a
   description. If an agent is blocked on the person, the card changes colour
   and the graph draws attention to it.
4. **Taskboard is the mailbox.** Agents are separate processes on different
   vendors' stacks and cannot be pushed to. Every human-to-agent message is
   stored on the ticket and the agent collects it. This is what makes the product
   vendor-neutral and restart-safe.
5. **Everything is live.** A write from any surface (web, CLI, MCP, another
   process entirely) appears in every open browser within a second, without a
   refresh.
6. **Everything is addressable.** A ticket, a filtered view, a pending request:
   each has a URL an agent can print and the person can click.
7. **Quiet by default, loud when it matters.** Many agents produce many
   updates. Routine progress is subtle; a card that needs the person is not.
8. **Nothing enforced that cannot be explained.** Dependencies are
   informational. Cycles are allowed and shown, not rejected. The board never
   refuses an agent's write on workflow grounds.

## Concepts

### Ticket

Unchanged in spirit: a unit of work in a project with a key, title,
description, priority, labels, subtasks, repo, and dependencies. Two additions:

- **agent**: which agent currently holds the ticket, if any.
- **open request**: at most one outstanding question or approval the ticket is
  waiting on. See Request below.

### Status

The status set grows from `todo`, `in_progress`, `done` to:

| Status | Meaning | Who sets it | Where it shows |
|---|---|---|---|
| `todo` | Not started | anyone | graph |
| `in_progress` | An agent is working | agent, when it claims | graph, blue, animated |
| `agent_review` | Written, waiting on the review agent | the review agent, when it takes the ticket | graph, violet, animated |
| `needs_input` | Agent asked a question | agent, via a request | graph, red, attention animation |
| `needs_approval` | Agent wants approval to proceed | agent, via a request | graph, yellow |
| `done` | Finished | agent, or the person | table below the graph |

Blue and violet mean an agent holds the ticket; red and yellow mean it is
waiting on a person. A ticket bouncing between an implementer agent and a
review agent alternates between the first two, and the graph treats them as
one group so the card does not move as it flips.

The graph is status-agnostic by construction: `done` goes to the table,
everything else is a card. Adding a status later must be a data change, not a
layout change. The status list lives in exactly one place per layer.

### Agent

An identity for a process working on tickets.

- id, display name, **provider** (anthropic, openai, google, other), created at,
  **last seen at**.
- Last seen is updated by every MCP or CLI call the agent makes. It is the
  heartbeat.
- Provider drives a small brand mark on the card, secondary to the agent's name.
- An agent may hold several tickets. A ticket has at most one agent.

Agent death is handled by leases, not by special cases. A card whose agent has
not been seen for a short while shows as stale. After a longer while the lease
expires: the ticket returns to `todo`, keeps a note of who abandoned it, and
becomes claimable again. The exact durations are configuration.

### Request

A message from an agent to the person, attached to a ticket, expecting an
answer.

- kind: `input` (a question, optionally with choices) or `approval` (a yes/no
  with a description of what will happen on yes).
- prompt: markdown written by the agent.
- choices: optional list. The answer UI always also offers free text.
- answer, answered at.
- created at.

Opening a request moves the ticket to `needs_input` or `needs_approval`.
Answering it moves the ticket back to `in_progress` and stores the answer.
The agent collects the answer and acts. The request stays on the ticket as
history: every question the fleet asked and every answer the person gave is
auditable later.

For approvals, the prompt must state exactly what will happen on yes (merge
this branch, push these commits, deploy this build). The agent acts only on that
approval, never on an older or different one. The safety of the action itself
lives in the agent; the board's job is to make the approval unambiguous.

## Surfaces

### Home: the graph

The home route. Left to right by dependency depth:

- **Ready** column on the far left: tickets with no unfinished dependencies,
  including tickets with no dependencies at all. In-progress tickets sort to the
  top of this column.
- Each further column is one more unfinished dependency away.
- Arrows run from a blocker to the ticket it unblocks. Dependencies on done
  tickets are not drawn; the card shows a small satisfied-dependency count
  instead.
- Cycles are drawn, with the back-edge in red.

Card content: key, status, priority, title, labels, agent name and provider
mark when held, relative time since last update.

Card states:

- `in_progress`: a quiet, continuous animation (a pulse on the status dot or a
  slow gradient along the card edge) that says "someone is on this".
- `agent_review`: the same animation in violet, so a ticket in review still
  reads as an agent's, and which agent holds it reads at a glance.
- `needs_approval`: yellow.
- `needs_input`: red, with an attention animation that stops once the card is
  opened.
- stale agent: the in-progress animation turns amber; the last-seen time is
  shown.
- just changed: a brief glow when an update arrives, so the person sees where
  the fleet just acted.

Interactions:

- Pan, scroll to zoom, fit to screen.
- Hover a card: its upstream chain, downstream chain and their edges are
  highlighted; everything else dims.
- Click a card: opens the ticket editor.
- Filter bar at the top: project, status, priority, label, repo, agent, text.
  Filtering dims cards on the graph and removes rows from the table. Filter
  state lives in the URL.

### The done table

Below the graph, on the same page. Recently completed work, newest first, with
key, title, agent, closed time, and labels. Rows open the same editor as cards.
Long histories are paginated; the default shows the recent past.

### The ticket editor

A large centred modal, opened from a card, a table row, or a URL such as
`/?ticket=AUTH-7`. It edits every field, shows dependencies both ways, and
shows the request history. When the ticket has an open request, the request is
the first thing on screen: the prompt, the choices, a free-text box, and for
approvals an explicit Approve and Decline.

### The kanban

Kept, moved to its own route. Still useful for a status-first look at a single
project. It is no longer the home page and receives no new features that the
graph does not also get.

### CLI and MCP

The agent-facing surface. Today's tools remain. New tools and commands for the
agent protocol below. The MCP server is the primary integration point for any
vendor's agent; the CLI exists for shells and scripts.

## The agent protocol

The minimum an agent needs, expressed as intents rather than exact signatures:

1. **Register or identify**: an agent announces its name and provider once and
   thereafter identifies itself on every call. Every call refreshes last seen.
2. **Claim**: take a `todo` ticket, setting agent and moving it to
   `in_progress`.
3. **Progress**: update fields, add subtasks, toggle them, add dependencies.
   Unchanged from today.
4. **Ask**: open an `input` or `approval` request with a prompt and optional
   choices. Returns immediately.
5. **Await**: long-poll for the answer to the ticket's open request, with a
   timeout. Agents that cannot block poll the ticket instead.
6. **Release**: give the ticket back to `todo`, or mark it `done`.

The protocol is deliberately pull-based. Taskboard never calls out to an agent.
This keeps the product working with any agent that can make an HTTP or MCP
call, and means an agent that restarts simply asks again.

## Realtime

The CLI and the MCP server open the SQLite file directly as separate processes,
so the web server cannot observe their writes in-process. Change detection
therefore happens at the database: the server watches SQLite's data version
counter and, when it moves, pushes a change event to every connected browser
over Server-Sent Events. Browsers refetch what they show. Sub-second latency, no
schema change, and every writer is covered.

## Non-goals

- **No multi-user, no auth.** One person, one fleet, one machine or a private
  network. Revisit only if that changes.
- **No workflow enforcement.** The board does not block a status change or a
  claim on dependency grounds.
- **No agent orchestration.** Taskboard does not start, stop, schedule or
  assign agents. Agents pull work; something else runs them. Projects can
  store agent instructions, which the board shows to agents and never acts on.
- **No vendor-specific integration** beyond the brand mark. Every agent uses
  the same protocol.
- **No chat.** A request is a structured question with a structured answer,
  not a conversation thread. If back-and-forth is needed, the agent opens
  another request.

## Roadmap

Each step is a separate design and a separate change. Order reflects value: the
graph is the surface everything lands on, and the request loop is the heart of
the new product.

1. **Graph view as home.** Layout, arrows, hover chain, pan and zoom, fit,
   click to open the existing ticket panel. Kanban moves to its own route.
   Custom SVG, no new dependencies.
2. **Realtime.** Data-version watching, Server-Sent Events, change glow on
   cards. Applies to every page.
3. **Agents and requests.** Agent identity with heartbeat, the two new
   statuses, the request model, the agent protocol in MCP and CLI, the request
   UI in the editor, red and yellow cards.
4. **Editor and table.** The centred modal editor, URL-addressable tickets,
   the done table, the shared filter bar with URL state.
5. **Fleet awareness.** Agent name and provider mark on cards, in-progress
   animation, stale detection, lease expiry, agent filter.
6. **Later.** Bulk operations from the filter bar, request history views,
   per-agent pages, notifications outside the browser.

## Open questions

- Should `needs_approval` be a status, or should a review simply be an open
  `approval` request on an `in_progress` ticket? The table above says status;
  the alternative keeps the status set smaller. Decide in the step 3 design.
- Lease and staleness durations: fixed defaults, per-project, or per-agent?
- Should an agent be able to claim a ticket whose dependencies are unfinished?
  The principles say yes, with a visible warning. Confirm.
- What does the person see when two agents try to claim the same ticket?
  Probably last-write-wins with a note on the card; confirm.
- Whether the done table should show all projects or follow the graph's
  project filter. Probably follow it.
