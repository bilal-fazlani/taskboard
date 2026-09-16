import { describe, expect, it } from "vitest";
import {
  NO_HIGHLIGHT,
  highlightedCard,
  nextHighlight,
  type HighlightEvent,
  type HighlightState,
} from "./graphHighlight";

/** Replays a sequence of events from nothing lit. */
function replay(events: HighlightEvent[], from: HighlightState = NO_HIGHLIGHT): HighlightState {
  return events.reduce(nextHighlight, from);
}

/** The card lit after a sequence, or null. */
function lit(events: HighlightEvent[], from?: HighlightState): string | null {
  return highlightedCard(replay(events, from));
}

const over = (id: string): HighlightEvent => ({ type: "pointerOverCard", id });
const left = (id: string): HighlightEvent => ({ type: "pointerLeftCard", id });
const leftGraph: HighlightEvent = { type: "pointerLeftGraph" };
// A card reached with Tab shows a focus ring; one the pointer pressed on does not.
const tabbedTo = (id: string): HighlightEvent => ({ type: "cardFocused", id, focusVisible: true });
const pressedOn = (id: string): HighlightEvent => ({ type: "cardFocused", id, focusVisible: false });
const blurred = (id: string): HighlightEvent => ({ type: "cardBlurred", id });

describe("the pointer", () => {
  it("lights the card it is over", () => {
    expect(lit([over("A")])).toBe("A");
  });

  it("clears the highlight when it leaves the card onto empty canvas", () => {
    expect(lit([over("A"), left("A")])).toBeNull();
  });

  it("clears the highlight when it leaves the graph", () => {
    expect(lit([over("A"), leftGraph])).toBeNull();
  });

  it("moves the highlight from card to card without going dark in between", () => {
    // The leave of the old card arrives after the enter of the new one when
    // the cards touch, and it must not undo the new pick.
    expect(lit([over("A"), over("B"), left("A")])).toBe("B");
    expect(lit([over("A"), left("A"), over("B")])).toBe("B");
  });

  it("keeps the same state object while it moves over the card it already lit", () => {
    const after = replay([over("A")]);
    expect(nextHighlight(after, over("A"))).toBe(after);
  });

  it("ignores a leave for a card it is not over", () => {
    const after = replay([over("A")]);
    expect(nextHighlight(after, left("B"))).toBe(after);
    expect(nextHighlight(NO_HIGHLIGHT, leftGraph)).toBe(NO_HIGHLIGHT);
  });
});

describe("the keyboard", () => {
  it("lights a card that shows a focus ring, and only while it shows one", () => {
    expect(lit([tabbedTo("A")])).toBe("A");
    expect(lit([tabbedTo("A"), blurred("A")])).toBeNull();
    expect(lit([tabbedTo("A"), { type: "focusLeftGraph" }])).toBeNull();
  });

  it("follows the focus ring from card to card", () => {
    expect(lit([tabbedTo("A"), blurred("A"), tabbedTo("B")])).toBe("B");
  });

  it("lights nothing for a card focused without a ring", () => {
    expect(lit([pressedOn("A")])).toBeNull();
  });

  it("drops the ring of another card when the pointer presses on one", () => {
    expect(lit([tabbedTo("A"), blurred("A"), pressedOn("B")])).toBeNull();
    // Even without the blur, since focus can only be in one place.
    expect(lit([tabbedTo("A"), pressedOn("B")])).toBeNull();
  });

  it("ignores a blur for a card that isn't the focused one", () => {
    const after = replay([tabbedTo("A")]);
    expect(nextHighlight(after, blurred("B"))).toBe(after);
  });
});

describe("the pointer and a focus ring together", () => {
  it("shows the hovered card while the pointer is over one", () => {
    expect(lit([tabbedTo("A"), over("B")])).toBe("B");
  });

  it("hands the highlight back to the focused card when the pointer leaves", () => {
    expect(lit([tabbedTo("A"), over("B"), left("B")])).toBe("A");
    expect(lit([tabbedTo("A"), over("B"), leftGraph])).toBe("A");
  });

  it("leaves nothing lit after a press on a card and a release elsewhere", () => {
    // pointerdown focuses the card without a ring; the release happens off it,
    // so the card's own leave is what clears the highlight.
    expect(lit([over("A"), pressedOn("A"), left("A")])).toBeNull();
    expect(lit([over("A"), pressedOn("A"), left("A"), leftGraph])).toBeNull();
    // The same press while another card was tabbed to: neither stays lit.
    expect(lit([tabbedTo("A"), over("B"), pressedOn("B"), left("B")])).toBeNull();
  });
});

describe("the ticket editor", () => {
  it("clears the highlight when it opens", () => {
    expect(lit([over("A"), { type: "editorOpened" }])).toBeNull();
    expect(lit([tabbedTo("A"), { type: "editorOpened" }])).toBeNull();
    expect(lit([tabbedTo("A"), over("B"), { type: "editorOpened" }])).toBeNull();
  });

  it("changes nothing when nothing was lit", () => {
    expect(nextHighlight(NO_HIGHLIGHT, { type: "editorOpened" })).toBe(NO_HIGHLIGHT);
  });
});
