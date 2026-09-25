// Going back through the board's own history entries with the Navigation API.
//
// An HTML document runs in a frame, and when the page navigates inside it (an
// in-page #anchor, history.pushState) the browser adds the frame's entries to
// the tab's joint session history. history.go(-1) from the board then only
// steps back inside the frame. The Navigation API sees the board's own
// entries alone (a frame's entries never show in the parent's
// navigation.entries()), so its back() and traverseTo() skip over the frame's
// and land on the board's entry below.
//
// TypeScript's DOM library does not describe the API yet, so this names only
// the parts the board uses. Browsers without it (and jsdom) have no
// window.navigation; the callers then keep using history.go.

export interface TraversalResult {
  committed: Promise<unknown>;
  finished: Promise<unknown>;
}

export interface BoardNavigation {
  readonly currentEntry: { readonly index: number } | null;
  entries(): { readonly key: string }[];
  back(): TraversalResult;
  traverseTo(key: string): TraversalResult;
}

/** The window's Navigation API, or null where the browser has none. */
export function boardNavigation(): BoardNavigation | null {
  if (typeof window === "undefined") return null;
  const navigation = (window as { navigation?: BoardNavigation }).navigation;
  return navigation && typeof navigation.back === "function" ? navigation : null;
}

/**
 * The key of the board's entry `steps` entries below the current one, or null
 * when the browser no longer has it: joint session history is capped (about
 * 50 entries in Chrome), and a page adding many entries in its frame pushes
 * the board's older entries out.
 */
export function keyBelow(navigation: BoardNavigation, steps: number): string | null {
  const index = navigation.currentEntry?.index ?? -1;
  if (index < 0 || steps > index) return null;
  return navigation.entries()[index - steps]?.key ?? null;
}

/**
 * Runs `fallback` when the traversal is refused, as back() and traverseTo()
 * are when the entry they need is gone. A traversal aborted by a newer
 * navigation is not refused: the newer one decides where the user ends up.
 */
export function onRefused(result: TraversalResult, fallback: () => void): void {
  // Only the committed promise decides; the finished one rejects alongside
  // it and must not surface as an unhandled rejection.
  result.finished.catch(() => {});
  result.committed.catch((error: unknown) => {
    if (error instanceof DOMException && error.name === "AbortError") return;
    fallback();
  });
}
