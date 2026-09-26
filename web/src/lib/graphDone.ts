// The done block on the dependency graph: the most recently done tickets, left
// of Ready, with no arrows. Pure functions over plain data, with no React and
// no DOM. computeGraphTopology picks the block's tickets with recentDone, and
// positionGraph lays them out with the shelf's balancing rule (graphShelf.ts).

import { isDone } from "./status";

/** How many done tickets the block holds at most. */
export const DONE_BLOCK_LIMIT = 50;

/** The part of a ticket the done block reads. */
export interface DoneTicket {
  id: string;
  projectPrefix: string;
  number: number;
  status: string;
  /** When the ticket last moved to done, as the ticket list carries it. */
  doneAt?: string;
  createdAt?: string;
}

// When a ticket was done, in milliseconds: its doneAt, or, for a ticket that
// has none, its createdAt, the earliest it can have been done. Never
// updatedAt, which an edit to a done ticket also moves. A ticket with neither
// sorts last.
function doneTime(ticket: DoneTicket): number {
  for (const value of [ticket.doneAt, ticket.createdAt]) {
    const ms = value ? Date.parse(value) : NaN;
    if (!Number.isNaN(ms)) return ms;
  }
  return -Infinity;
}

// Newest done first; on a tie, the later ticket first (prefix, then number
// descending, then id), so the order never depends on the input's.
function compareDone(a: { ticket: DoneTicket; at: number }, b: { ticket: DoneTicket; at: number }): number {
  if (a.at !== b.at) return b.at - a.at;
  const [x, y] = [a.ticket, b.ticket];
  if (x.projectPrefix !== y.projectPrefix) return x.projectPrefix < y.projectPrefix ? 1 : -1;
  if (x.number !== y.number) return y.number - x.number;
  if (x.id !== y.id) return x.id < y.id ? 1 : -1;
  return 0;
}

/**
 * The done tickets among `tickets`, most recently done first, at most `limit`
 * of them, and how many are done in all. Ids are assumed unique.
 */
export function recentDone<T extends DoneTicket>(
  tickets: readonly T[],
  limit: number = DONE_BLOCK_LIMIT,
): { tickets: T[]; total: number } {
  const done = tickets.filter((t) => isDone(t.status)).map((ticket) => ({ ticket, at: doneTime(ticket) }));
  done.sort(compareDone);
  return { tickets: done.slice(0, Math.max(0, limit)).map((d) => d.ticket), total: done.length };
}
