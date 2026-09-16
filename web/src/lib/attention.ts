// The Dependencies view's attention animation: a ticket an agent is working
// on has to be findable at a glance on a graph that fills the screen, so its
// card breathes a ring in the in-progress colour and its status dot pulses on
// the same cycle.
//
// The two class names are plain CSS, defined together with their keyframes in
// src/index.css (Tailwind has no utility for a two-stop glow, and the pair has
// to share one animation definition to stay in step). Only the graph card
// animates: the board and the tickets table render TicketCard without the
// `graph` prop and must stay still.
import { isInProgress } from "./status";

/** Breathing ring around the whole card. */
export const ATTENTION_CARD_CLASS = "graph-attention";

/** Status dot pulsing in step with the ring. */
export const ATTENTION_DOT_CLASS = "graph-attention-dot";

export type AttentionClasses = { card: string; dot: string };

const NONE: AttentionClasses = { card: "", dot: "" };

/**
 * Class names that make a card demand attention, or empty strings when it
 * should not. Only an `in_progress` ticket drawn on the graph qualifies; every
 * other status, and every card off the graph, animates nothing.
 */
export function attentionClasses(status: string, onGraph: boolean): AttentionClasses {
  if (!onGraph || !isInProgress(status)) return NONE;
  return { card: ATTENTION_CARD_CLASS, dot: ATTENTION_DOT_CLASS };
}
