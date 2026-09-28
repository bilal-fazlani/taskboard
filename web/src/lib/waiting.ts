// Tickets waiting on the person (needs_user_input): what their cards and
// rows say about the request, how they sort, and where the header's count
// goes. Kept apart from the pages so it can be tested without a DOM.

import type { Ticket, TicketRequest, UserInputType } from "../api/client";
import { ALL_PROJECTS } from "./nowProject";
import { isWaiting } from "./status";

/**
 * The short name of a type of user input, before the prompt on a waiting
 * card and row: "Approve" for an approval, "Question" for a question, and a
 * later type by its own name, capitalised, so a newer server's type still
 * reads.
 */
export function requestKind(type: UserInputType | string): string {
  switch (type) {
    case "approval":
      return "Approve";
    case "question":
      return "Question";
    default: {
      const name = type.replace(/_/g, " ").trim();
      return name ? name[0].toUpperCase() + name.slice(1) : "Request";
    }
  }
}

/** A prompt's first non-blank line, trimmed; a card has room for no more. */
export function firstLine(prompt: string): string {
  return prompt.split(/\r?\n/).find((line) => line.trim() !== "")?.trim() ?? "";
}

/** The part of a ticket that says when it started waiting. A full Ticket satisfies it. */
export interface WaitingTicket {
  updatedAt?: Ticket["updatedAt"];
  openRequest?: Pick<TicketRequest, "createdAt">;
}

/**
 * When a waiting ticket started waiting: its open request's time, or, for one
 * whose request didn't come with it, its last update ("" when it has neither).
 */
export function waitingSince(ticket: WaitingTicket): string {
  return ticket.openRequest?.createdAt ?? ticket.updatedAt ?? "";
}

/** When a ticket started waiting, in milliseconds; a time that can't be read is Infinity, so it sorts last. */
function waitingAt(ticket: WaitingTicket): number {
  const ms = Date.parse(waitingSince(ticket));
  return Number.isNaN(ms) ? Infinity : ms;
}

/**
 * Oldest first, by when each started waiting; a time that can't be read
 * sorts last. Ties answer 0, so a stable sort keeps their order.
 */
export function compareWaiting(a: WaitingTicket, b: WaitingTicket): number {
  const x = waitingAt(a);
  const y = waitingAt(b);
  return x === y ? 0 : x < y ? -1 : 1;
}

/** How many of the tickets wait on the person. */
export function waitingCount(tickets: readonly Pick<Ticket, "status">[]): number {
  return tickets.filter((t) => isWaiting(t.status)).length;
}

/** The id of the Now page's waiting group, which the header's count links to. */
export const WAITING_ANCHOR = "waiting";

/**
 * Where the header's "N waiting on you" goes until the inbox (ACP-156)
 * exists: Now's waiting group, across every project, since the count is.
 */
export const WAITING_LINK = `/now?project=${ALL_PROJECTS}#${WAITING_ANCHOR}`;
