// The ticket page's requests for user input: what an open request's form
// sends, what its toast says, and how the earlier ones read in their fold.

import type { Agent, TicketRequest } from "../api/client";

/** A request's type as its tag names it; a type this client doesn't know keeps its own name. */
export function requestKindLabel(type: string): string {
  if (type === "question") return "Question";
  if (type === "approval") return "Approve";
  return type.replace(/_/g, " ");
}

/**
 * What a question's form sends: the person's own words when they wrote any,
 * which win over a picked choice, else the choice. "" when there is neither,
 * and the form can't be sent.
 */
export function questionAnswer(choice: string, text: string): string {
  return text.trim() || choice.trim();
}

/** Every request but the open one, newest first: the fold's contents. */
export function earlierRequests(all: readonly TicketRequest[], openId: string | undefined): TicketRequest[] {
  return all
    .filter((r) => r.id !== openId)
    .sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt));
}

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** How long ago a request was made: "just now", "3m ago", "2h ago", "4d ago". */
export function askedAgo(at: string, now: number): string {
  const when = Date.parse(at);
  if (Number.isNaN(when)) return "";
  const ms = Math.max(0, now - when);
  if (ms < MINUTE) return "just now";
  if (ms < HOUR) return `${Math.floor(ms / MINUTE)}m ago`;
  if (ms < DAY) return `${Math.floor(ms / HOUR)}h ago`;
  return `${Math.floor(ms / DAY)}d ago`;
}

/** The agent that collects an answer, as a sentence starts with it. */
export function collectorPhrase(agent: Pick<Agent, "role" | "model"> | undefined): string {
  if (!agent) return "The agent that asked";
  const role = agent.role || "agent";
  return agent.model ? `The ${role} (${agent.model})` : `The ${role}`;
}

/**
 * The answer a request is closed with when the person stops work on its
 * ticket, whatever its type: the server's models.StoppedAnswer, word for
 * word. Neither approved nor declined, and not the person's answer.
 */
export const STOPPED_ANSWER = "Not answered: the person stopped work on this ticket.";

/** The toast after an answer: what was sent, and which agent collects it. */
export function answerToast(type: string, answer: string, agent: Pick<Agent, "role" | "model"> | undefined): string {
  const sent =
    type === "approval" && answer === "approved"
      ? "Approved."
      : type === "approval" && answer === "declined"
        ? "Declined."
        : "Answer sent.";
  return `${sent} ${collectorPhrase(agent)} gets it on its next call.`;
}

/**
 * How an earlier request's answer reads, and its colour. Only "approved" is
 * approved and only "declined" declined; an approval closed any other way,
 * and a request of either type closed by stopping work, is "closed".
 */
export function answerTone(r: TicketRequest): "approved" | "declined" | "answer" | "closed" | "waiting" {
  if (!r.answeredAt) return "waiting";
  if (r.answer === STOPPED_ANSWER) return "closed";
  if (r.type === "approval") {
    if (r.answer === "approved") return "approved";
    if (r.answer === "declined") return "declined";
    return "closed";
  }
  return "answer";
}
