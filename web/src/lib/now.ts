// The Now page's wording and links, kept apart from the page so they can be
// tested without a DOM.

import type { LandedTicket, NowTicket } from "../api/client";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** How often the page redraws its timers, in milliseconds. */
export const TICK_MS = 30_000;

/** How far back the Landed list goes, as the server's window (db.NowLandedWindow). */
export const LANDED_WINDOW_MS = DAY;

/**
 * The landed tickets still inside the window at `now`. The page refetches
 * only when the board changes, so on an idle board this is what drops a row
 * as it turns a day old, on the next tick, with no request. A time that
 * can't be read is kept: the server put it in the window.
 */
export function withinLandedWindow(landed: readonly LandedTicket[], now: number): LandedTicket[] {
  return landed.filter((t) => {
    const at = Date.parse(t.doneAt);
    return Number.isNaN(at) || now - at <= LANDED_WINDOW_MS;
  });
}

/** Whole units of `ms`, never below zero (a clock a little behind the server's). */
function units(ms: number, unit: number): number {
  return Math.max(0, Math.floor(ms / unit));
}

/**
 * How long a ticket has been in its status, as a card shows it: "18m",
 * "1h 12m", "3h 05m", and "2d 4h" from a day on. Under a minute is "0m".
 * An unreadable time shows nothing.
 */
export function runningFor(since: string, now: number): string {
  const start = Date.parse(since);
  if (Number.isNaN(start)) return "";
  const ms = now - start;
  if (ms >= DAY) return `${units(ms, DAY)}d ${units(ms % DAY, HOUR)}h`;
  if (ms >= HOUR) return `${units(ms, HOUR)}h ${String(units(ms % HOUR, MINUTE)).padStart(2, "0")}m`;
  return `${units(ms, MINUTE)}m`;
}

/**
 * How long ago a ticket landed, as the Landed list shows it: "just now",
 * "14 min ago", "21 h ago". The list only holds the last day, so hours are
 * the largest unit.
 */
export function landedAgo(doneAt: string, now: number): string {
  const at = Date.parse(doneAt);
  if (Number.isNaN(at)) return "";
  const ms = now - at;
  if (ms < MINUTE) return "just now";
  if (ms < HOUR) return `${units(ms, MINUTE)} min ago`;
  return `${units(ms, HOUR)} h ago`;
}

/** What a card says about review, and in which colour. */
export interface ReviewNote {
  text: string;
  tone: "round" | "approved";
}

/**
 * The review note on a card, or null when there is nothing to say: a ticket
 * in progress that has been reviewed before is back from its latest round; a
 * ticket in review has its review running, approved and waiting on the
 * person, or sent back with changes.
 */
export function reviewNote(ticket: Pick<NowTicket, "status" | "review" | "reviewRounds">): ReviewNote | null {
  const round = Math.max(1, ticket.reviewRounds);
  if (ticket.status === "in_progress") {
    return ticket.reviewRounds > 0 ? { text: `back from review ${ticket.reviewRounds}`, tone: "round" } : null;
  }
  switch (ticket.review) {
    case "approved":
      return { text: "approved, waiting on you", tone: "approved" };
    case "changes":
      return { text: `review ${round}: changes asked`, tone: "round" };
    case "running":
      return { text: `review ${round} running`, tone: "round" };
    default:
      return null;
  }
}

/**
 * Where a card goes: the ticket's editor, over the Dependencies view of the
 * ticket's own project, which is the ticket's canonical link with its
 * project named.
 */
export function editorLink(ticket: { id: string; key: string; projectPrefix: string }): string {
  const params = new URLSearchParams();
  if (ticket.projectPrefix) params.set("project", ticket.projectPrefix);
  params.set("ticket", ticket.projectPrefix ? ticket.key : ticket.id);
  return `/?${params.toString()}`;
}

/** A sha as the list shows it: git's default short length. */
export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}
