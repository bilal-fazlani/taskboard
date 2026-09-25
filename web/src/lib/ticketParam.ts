// The open ticket, held in the URL as a `ticket` query parameter.
//
// Opening the editor on any view adds `?ticket=<KEY>` to that view's URL, and
// closing it removes only that parameter, so the filters from the shared
// filter bar survive both. Loading a view with the parameter opens the ticket
// on top of it, which is what makes a ticket linkable: the canonical link is
// the home view, http://<host>/?ticket=<KEY>.
//
// These are the pure parts, shared by the three views through
// hooks/useTicketParam.ts.

import { ticketKey } from "./filters";

/** The query parameter naming the open ticket. */
export const TICKET_PARAM = "ticket";

/** What identifies a ticket in the URL: its display key, or failing that its id. */
export interface TicketIdentity {
  id: string;
  number: number;
  projectPrefix: string;
}

/** The ticket reference in the URL, or "" when no ticket is open. */
export function ticketRef(params: URLSearchParams): string {
  return params.get(TICKET_PARAM) ?? "";
}

/**
 * The params with the ticket parameter set to a ticket's display key, and no
 * open document, since a document belongs to the ticket it was opened on.
 * Every other parameter, the filters included, is kept as it was.
 */
export function withTicket(params: URLSearchParams, ticket: TicketIdentity): URLSearchParams {
  const next = new URLSearchParams(params);
  next.set(TICKET_PARAM, ticketRefFor(ticket));
  next.delete("doc");
  return next;
}

/** The params without the ticket parameter; every other parameter is kept. */
export function withoutTicket(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(TICKET_PARAM);
  return next;
}

/**
 * How a ticket names itself in a URL: the display key, which reads far better
 * than an id, or the id when the ticket carries no project prefix.
 */
export function ticketRefFor(ticket: TicketIdentity): string {
  return ticket.projectPrefix ? ticketKey(ticket) : ticket.id;
}

/**
 * The ticket a reference names: a display key, matched ignoring case so a
 * hand-typed `acp-25` works, or an id. A reference that names no loaded ticket
 * finds nothing, and the caller leaves the parameter alone.
 */
export function findTicket<T extends TicketIdentity>(
  tickets: readonly T[],
  ref: string,
): T | undefined {
  const wanted = ref.trim();
  if (!wanted) return undefined;
  const byId = tickets.find((t) => t.id === wanted);
  if (byId) return byId;
  const lower = wanted.toLowerCase();
  return tickets.find((t) => ticketRefFor(t).toLowerCase() === lower);
}

/**
 * A ticket's shareable URL: the home view with the ticket parameter, whatever
 * view it was copied from, so the link always opens somewhere sensible.
 * `origin` is window.location.origin in the app.
 */
export function ticketUrl(origin: string, ticket: TicketIdentity): string {
  return `${origin}/?${TICKET_PARAM}=${encodeURIComponent(ticketRefFor(ticket))}`;
}
