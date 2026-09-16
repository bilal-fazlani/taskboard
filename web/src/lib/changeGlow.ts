// What changed between two fetches of the Dependencies graph, so the cards the
// fleet just touched can glow for a moment and the person sees where the work
// happened. Pure functions over plain data, with no React and no DOM; the
// two-second timer and the class toggling live in useChangeGlow and the page.
//
// The rule is deliberately narrow:
//
// - A ticket whose `updatedAt` differs from the previous fetch changed.
// - A ticket the previous fetch did not have is new, which includes a ticket
//   that came back from done.
// - A ticket that is gone says nothing. The page stamps only the cards it
//   draws, so a ticket moved to done is simply absent from the next fetch:
//   it leaves the graph without a flash, and nothing else flashes because of
//   it either.
// - The very first fetch changes nothing. There is no previous fetch to
//   compare against, and a page load must not light up the whole graph.
//
// A fetch that returned nothing is not the same as no fetch at all: when the
// graph was empty and tickets then appear, they are new and do glow, so
// `null` (no fetch yet) and an empty map (a fetch with no tickets) are kept
// apart.

/** The part of a ticket change detection reads. A full Ticket satisfies it. */
export interface GlowTicket {
  id: string;
  updatedAt: string;
}

/** Ticket ids to their `updatedAt`, as of one fetch. */
export type FetchStamps = ReadonlyMap<string, string>;

/** How long a changed card glows, in milliseconds. */
export const GLOW_MS = 2000;

/**
 * The class the page puts on a changed card's wrapper. Plain CSS, defined
 * with its keyframes in src/index.css: it draws the glow on a pseudo-element
 * so it layers over the in-progress breathing ring (src/lib/attention.ts) and
 * over the hover chain's Tailwind ring instead of replacing either.
 */
export const CHANGE_GLOW_CLASS = "graph-changed";

/** The change-detection stamps for one fetch's cards. */
export function fetchStamps(tickets: Iterable<GlowTicket>): FetchStamps {
  const stamps = new Map<string, string>();
  for (const ticket of tickets) stamps.set(ticket.id, ticket.updatedAt);
  return stamps;
}

/**
 * The ids in `next` that changed or are new since `previous`, in the order
 * `next` has them. `previous` is null before any fetch, and then nothing has
 * changed yet.
 */
export function changedSince(previous: FetchStamps | null, next: FetchStamps): string[] {
  if (previous === null) return [];
  const changed: string[] = [];
  for (const [id, updatedAt] of next) {
    if (previous.get(id) !== updatedAt) changed.push(id);
  }
  return changed;
}
