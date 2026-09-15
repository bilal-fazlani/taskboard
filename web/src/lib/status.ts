// Ticket status values, in board column order. This is the single source of
// truth for the status set: the board, the tickets list and its filters,
// and the ticket panel's status control all read from it, so adding a status
// (M3 adds needs_input and needs_review) is a data change here, not a hunt
// through every component.
//
// Tailwind v4 scans source files for literal class names, so the colour
// classes below must stay as full literal strings — never build them by
// concatenation, or they will be purged from the build.
export const STATUSES = ["todo", "in_progress", "done"] as const;

export type Status = (typeof STATUSES)[number];

// The status a new ticket starts in.
export const DEFAULT_STATUS: Status = "todo";

// The status that marks a ticket finished. The upcoming graph work
// (ACP-6/ACP-10) branches on this too, so call isDone() rather than
// comparing against the "done" literal.
export const DONE_STATUS: Status = "done";

// The status of a ticket an agent is working on. The graph layout (ACP-6)
// sorts these to the top of the Ready column, so call isInProgress() rather
// than comparing against the "in_progress" literal.
export const IN_PROGRESS_STATUS: Status = "in_progress";

export const STATUS_LABELS: Record<Status, string> = {
  todo: "Todo",
  in_progress: "In Progress",
  done: "Done",
};

// Board column dot accent color.
export const STATUS_COLORS: Record<Status, string> = {
  todo: "bg-slate-500",
  in_progress: "bg-blue-500",
  done: "bg-green-500",
};

// Tickets list badge style.
export const STATUS_STYLES: Record<Status, string> = {
  todo: "bg-slate-500/20 text-slate-400",
  in_progress: "bg-blue-500/20 text-blue-400",
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
