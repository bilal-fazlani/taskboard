// Ticket status values, in board column order. This is the single source of
// truth for the status set: the board, the tickets list and its filters,
// and the ticket panel's status control all read from it, so adding a status
// (M4 adds needs_input and needs_approval) is a data change here, not a hunt
// through every component.
//
// Tailwind v4 scans source files for literal class names, so the colour
// classes below must stay as full literal strings — never build them by
// concatenation, or they will be purged from the build.
export const STATUSES = ["todo", "in_progress", "agent_review", "done"] as const;

export type Status = (typeof STATUSES)[number];

// The status a new ticket starts in.
export const DEFAULT_STATUS: Status = "todo";

// The status that marks a ticket finished. The upcoming graph work
// (ACP-6/ACP-10) branches on this too, so call isDone() rather than
// comparing against the "done" literal.
export const DONE_STATUS: Status = "done";

// The status of a ticket an agent is writing, and the status of one it has
// handed to a review agent. Both mean an agent holds the ticket, which is
// what the graph sorts and animates on, so call isActive() for that and
// isInProgress() when only the writing half is meant.
export const IN_PROGRESS_STATUS: Status = "in_progress";
export const AGENT_REVIEW_STATUS: Status = "agent_review";

// The statuses an agent holds. A ticket bouncing between an implementer and a
// reviewer stays in this set, so it neither moves rows on the graph nor loses
// its ring as it flips.
export const ACTIVE_STATUSES: readonly Status[] = [IN_PROGRESS_STATUS, AGENT_REVIEW_STATUS];

export const STATUS_LABELS: Record<Status, string> = {
  todo: "Todo",
  in_progress: "In Progress",
  agent_review: "Agent Review",
  done: "Done",
};

// Board column dot accent color.
export const STATUS_COLORS: Record<Status, string> = {
  todo: "bg-slate-500",
  in_progress: "bg-blue-500",
  agent_review: "bg-violet-500",
  done: "bg-green-500",
};

// Tickets list badge style.
export const STATUS_STYLES: Record<Status, string> = {
  todo: "bg-slate-500/20 text-slate-400",
  in_progress: "bg-blue-500/20 text-blue-400",
  agent_review: "bg-violet-500/20 text-violet-400",
  done: "bg-green-500/20 text-green-400",
};

// Narrows a plain value (e.g. Ticket.status from the API, which is typed as
// a bare string) to a known Status, for callers that need to index one of
// the maps above without an unchecked cast.
export function isStatus(status: unknown): status is Status {
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

// Whether a status string denotes a ticket an agent holds, in either
// direction of the implement/review bounce.
export function isActive(status: string): boolean {
  return (ACTIVE_STATUSES as readonly string[]).includes(status);
}
