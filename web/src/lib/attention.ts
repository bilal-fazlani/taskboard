// The Dependencies view's attention animation: a ticket an agent holds has to
// be findable at a glance on a graph that fills the screen, so its card
// breathes a ring in that status's colour and its status dot pulses on the
// same cycle. An in-progress ticket breathes blue and one in agent review
// breathes violet, on the same cycle, so which agent holds it reads from
// across the room while both stay obviously active.
//
// The class names are plain CSS, defined together with their keyframes in
// src/index.css (Tailwind has no utility for a two-stop glow, and each pair
// has to share one animation definition to stay in step). Only the graph card
// animates: the board and the tickets table render TicketCard without the
// `graph` prop and must stay still.
import { isActive, isInProgress } from "./status";

/** Breathing ring around the whole card, in the in-progress blue. */
export const ATTENTION_CARD_CLASS = "graph-attention";

/** Status dot pulsing in step with the blue ring. */
export const ATTENTION_DOT_CLASS = "graph-attention-dot";

/** Breathing ring around the whole card, in the agent-review violet. */
export const ATTENTION_REVIEW_CARD_CLASS = "graph-attention-review";

/** Status dot pulsing in step with the violet ring. */
export const ATTENTION_REVIEW_DOT_CLASS = "graph-attention-review-dot";

export type AttentionClasses = { card: string; dot: string };

const NONE: AttentionClasses = { card: "", dot: "" };

/**
 * Class names that make a card demand attention, or empty strings when it
 * should not. Only an active ticket — `in_progress` or `agent_review` — drawn
 * on the graph qualifies; every other status, and every card off the graph,
 * animates nothing.
 */
export function attentionClasses(status: string, onGraph: boolean): AttentionClasses {
  if (!onGraph || !isActive(status)) return NONE;
  return isInProgress(status)
    ? { card: ATTENTION_CARD_CLASS, dot: ATTENTION_DOT_CLASS }
    : { card: ATTENTION_REVIEW_CARD_CLASS, dot: ATTENTION_REVIEW_DOT_CLASS };
}
