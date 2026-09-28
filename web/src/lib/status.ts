// Ticket status values, in board column order. This is the single source of
// truth for the status set: the board, the tickets list and its filters,
// and the ticket panel's status control all read from it, so adding a status
// is a data change here, not a hunt through every component. It mirrors
// internal/models/status.go: STATUSES are the statuses a write may set, and
// KNOWN_STATUSES every status a ticket can hold.
//
// Tailwind v4 scans source files for literal class names, so the colour
// classes below must stay as full literal strings — never build them by
// concatenation, or they will be purged from the build.

/** The statuses a write may set: the ticket panel's status control offers these. */
export const STATUSES = ["todo", "in_progress", "agent_review", "done"] as const;

export type Status = (typeof STATUSES)[number];

// The status of a ticket waiting on the person: an agent asked for user input
// (an approval, a question, a later type) and waits on the answer. Only that
// request puts a ticket here and only its answer takes it out, so no control
// offers it; the views show it, in red.
export const NEEDS_USER_INPUT_STATUS = "needs_user_input" as const;

/**
 * Every status a ticket can hold, in board column order: the ones a write may
 * set, with needs_user_input after in_progress. The Kanban columns, the
 * filters and every status label read this.
 */
export const KNOWN_STATUSES = ["todo", "in_progress", NEEDS_USER_INPUT_STATUS, "agent_review", "done"] as const;

export type KnownStatus = (typeof KNOWN_STATUSES)[number];

// The status a new ticket starts in.
export const DEFAULT_STATUS: Status = "todo";

// The status that marks a ticket finished. The upcoming graph work
// (ACP-6/ACP-10) branches on this too, so call isDone() rather than
// comparing against the "done" literal.
export const DONE_STATUS: Status = "done";

// The status of a ticket an agent is writing, and the status of one it has
// handed to a review agent. Both mean an agent is at work on the ticket,
// which the graph animates in blue and violet, so call isActive() for that
// and isInProgress() when only the writing half is meant.
export const IN_PROGRESS_STATUS: Status = "in_progress";
export const AGENT_REVIEW_STATUS: Status = "agent_review";

// The statuses an agent is at work in. A ticket bouncing between an
// implementer and a reviewer stays in this set, so it neither moves rows on
// the graph nor loses its ring as it flips.
export const ACTIVE_STATUSES: readonly Status[] = [IN_PROGRESS_STATUS, AGENT_REVIEW_STATUS];

// The statuses an agent holds the ticket in: the active ones, and waiting on
// the person, since the agent that asked keeps the ticket while it waits.
// The graph keeps all three at the top of Ready, off the shelf.
export const HELD_STATUSES: readonly KnownStatus[] = [IN_PROGRESS_STATUS, NEEDS_USER_INPUT_STATUS, AGENT_REVIEW_STATUS];

export const STATUS_LABELS: Record<KnownStatus, string> = {
  todo: "Todo",
  in_progress: "In Progress",
  needs_user_input: "Waiting on You",
  agent_review: "Agent Review",
  done: "Done",
};

// Board column dot accent color. Blue and violet mean an agent holds the
// ticket, red that it waits on the person; there is no yellow.
export const STATUS_COLORS: Record<KnownStatus, string> = {
  todo: "bg-slate-500",
  in_progress: "bg-blue-500",
  needs_user_input: "bg-red-500",
  agent_review: "bg-violet-500",
  done: "bg-green-500",
};

// Tickets list badge style.
export const STATUS_STYLES: Record<KnownStatus, string> = {
  todo: "bg-slate-500/20 text-slate-400",
  in_progress: "bg-blue-500/20 text-blue-400",
  needs_user_input: "bg-red-500/20 text-red-400",
  agent_review: "bg-violet-500/20 text-violet-400",
  done: "bg-green-500/20 text-green-400",
};

// Narrows a plain value (e.g. Ticket.status from the API, which is typed as
// a bare string) to a known status, for callers that need to index one of
// the maps above without an unchecked cast.
export function isStatus(status: unknown): status is KnownStatus {
  return typeof status === "string" && (KNOWN_STATUSES as readonly string[]).includes(status);
}

// Whether a write may set a status: the status control offers it, and a
// Kanban card can be dropped in its column.
export function isWritableStatus(status: unknown): status is Status {
  return typeof status === "string" && (STATUSES as readonly string[]).includes(status);
}

// Whether a status string denotes a finished ticket.
export function isDone(status: string): boolean {
  return status === DONE_STATUS;
}

// Whether a status string denotes a ticket being worked on.
export function isInProgress(status: string): boolean {
  return status === IN_PROGRESS_STATUS;
}

// Whether a status string denotes a ticket an agent is at work on, in either
// direction of the implement/review bounce.
export function isActive(status: string): boolean {
  return (ACTIVE_STATUSES as readonly string[]).includes(status);
}

// Whether a status string denotes a ticket waiting on the person.
export function isWaiting(status: string): boolean {
  return status === NEEDS_USER_INPUT_STATUS;
}

// Whether a status string denotes a ticket an agent holds: at work on it, or
// waiting on the person's answer.
export function isHeld(status: string): boolean {
  return (HELD_STATUSES as readonly string[]).includes(status);
}
