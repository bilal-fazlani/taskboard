// Which card the Dependencies graph lights the chains of. The page feeds this
// the pointer and focus events its cards see; the rule itself is here, so it
// can be tested without a DOM.
//
// Two sources can light a card, and they are kept apart:
//
// - The pointer lights the card it is over, and stops lighting it the moment
//   it leaves that card. Moving onto empty canvas, onto an arrow or out of
//   the window all leave the card, so all of them clear it. (Until ACP-44
//   only leaving the whole graph cleared the pointer's pick, which left the
//   highlight lit over empty canvas.)
// - The keyboard lights the card that shows a focus ring, and only while it
//   shows one. A card the pointer merely pressed on is focused too, without a
//   ring, and must not hold the graph dimmed once the pointer has left it.
//   `:focus-visible` is what tells the two apart, so the page reads it off the
//   element and passes it in.
//
// The pointer wins while it is over a card, so hovering with a card focused
// shows the hovered card's chains and leaving that card hands the highlight
// back to the focused one. Opening the ticket editor clears both, since the
// pointer and the focus it came from are about to move.
//
// Every event returns the state unchanged, by identity, when it changes
// nothing, so the page's setState bails out instead of re-rendering the graph
// on each pointermove over the same card.

export interface HighlightState {
  /** The card the pointer is over, if any. */
  readonly pointer: string | null;
  /** The card showing a focus ring, if any. */
  readonly focus: string | null;
}

/** Nothing lit: no card under the pointer and none showing a focus ring. */
export const NO_HIGHLIGHT: HighlightState = { pointer: null, focus: null };

export type HighlightEvent =
  /** pointerenter or pointermove on a card. */
  | { type: "pointerOverCard"; id: string }
  /** pointerleave on a card, whatever the pointer moved onto. */
  | { type: "pointerLeftCard"; id: string }
  /** pointerleave on the graph itself: a safety net for a card that never saw its own leave. */
  | { type: "pointerLeftGraph" }
  /** focus on a card. `focusVisible` is the element's `:focus-visible` at that moment. */
  | { type: "cardFocused"; id: string; focusVisible: boolean }
  /** blur on a card. */
  | { type: "cardBlurred"; id: string }
  /** Focus left the graph altogether. */
  | { type: "focusLeftGraph" }
  /** The ticket editor was opened from a card. */
  | { type: "editorOpened" };

/** The card whose chains are lit, or null. The pointer wins over a focus ring. */
export function highlightedCard(state: HighlightState): string | null {
  return state.pointer ?? state.focus;
}

/** The state after `event`; the same object, by identity, when nothing changed. */
export function nextHighlight(state: HighlightState, event: HighlightEvent): HighlightState {
  switch (event.type) {
    case "pointerOverCard":
      return withPointer(state, event.id);
    case "pointerLeftCard":
      // A leave that arrives after the pointer has already entered another
      // card is stale and must not clear that card.
      return state.pointer === event.id ? withPointer(state, null) : state;
    case "pointerLeftGraph":
      return withPointer(state, null);
    case "cardFocused":
      // Focus without a ring is the pointer pressing on a card: it lights
      // nothing, and drops any ring another card was showing.
      return withFocus(state, event.focusVisible ? event.id : null);
    case "cardBlurred":
      return state.focus === event.id ? withFocus(state, null) : state;
    case "focusLeftGraph":
      return withFocus(state, null);
    case "editorOpened":
      return state.pointer === null && state.focus === null ? state : NO_HIGHLIGHT;
  }
}

/**
 * The state with `id` forgotten, wherever it was held. The page calls this for
 * a card that is no longer on the graph — a live refetch can take the lit card
 * away while the pointer sits still somewhere else — so that the card doesn't
 * light its chains again the moment it comes back.
 */
export function forgetCard(state: HighlightState, id: string): HighlightState {
  const pointer = state.pointer === id ? null : state.pointer;
  const focus = state.focus === id ? null : state.focus;
  return pointer === state.pointer && focus === state.focus ? state : { pointer, focus };
}

function withPointer(state: HighlightState, pointer: string | null): HighlightState {
  return state.pointer === pointer ? state : { pointer, focus: state.focus };
}

function withFocus(state: HighlightState, focus: string | null): HighlightState {
  return state.focus === focus ? state : { pointer: state.pointer, focus };
}
