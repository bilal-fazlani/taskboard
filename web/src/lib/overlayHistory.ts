// Overlays are what opens on top of a view and lives in its query string: the
// ticket editor (`ticket`), a document (`doc`) and, in the Epics view, the
// epic modal (`epic`). In the ticket views `epic` is the epic filter instead,
// which stays when an overlay closes.
// Each overlay the user opens is its own browser history entry, so Back and
// Forward step through them. How many such entries sit on top of the view the
// overlays opened from is kept in the entry's location state, which the
// browser restores with the entry: Back from depth 2 lands on an entry that
// still says 1. Closing everything goes back that many entries at once, which
// leaves history as it was before the first overlay opened. Entries an HTML
// document adds inside its frame are not counted; lib/historyTraversal is how
// closing skips them.
//
// `fromLink` records whether the entry under the first push already had an
// overlay, which is how an editor opened straight from a link looks. Going
// back to it lands on that linked overlay, so the caller strips it after.

const OVERLAY_PARAMS: readonly string[] = ["ticket", "doc"];
const EPICS_OVERLAY_PARAMS: readonly string[] = [...OVERLAY_PARAMS, "epic"];

/** The query parameters that are overlays on the page at `pathname`. */
export function overlayParams(pathname: string): readonly string[] {
  return pathname === "/epics" ? EPICS_OVERLAY_PARAMS : OVERLAY_PARAMS;
}

export interface OverlayState {
  depth: number;
  fromLink: boolean;
}

export function overlayState(state: unknown): OverlayState {
  const s = (state ?? {}) as { overlayDepth?: unknown; overlayFromLink?: unknown };
  const depth = typeof s.overlayDepth === "number" && Number.isInteger(s.overlayDepth) && s.overlayDepth > 0
    ? s.overlayDepth
    : 0;
  return { depth, fromLink: depth > 0 && s.overlayFromLink === true };
}

/** The location state for an entry pushed on top of one whose state is `current`. */
export function pushState(current: unknown, baseHasOverlay: boolean): Record<string, unknown> {
  const below = overlayState(current);
  const rest = current !== null && typeof current === "object" ? (current as Record<string, unknown>) : {};
  return {
    ...rest,
    overlayDepth: below.depth + 1,
    overlayFromLink: below.depth === 0 ? baseHasOverlay : below.fromLink,
  };
}

/**
 * The location state for an entry whose overlays were all closed in place:
 * nothing counts below it any more.
 */
export function closedState(current: unknown): Record<string, unknown> {
  const rest = current !== null && typeof current === "object" ? { ...(current as Record<string, unknown>) } : {};
  delete rest.overlayDepth;
  delete rest.overlayFromLink;
  return rest;
}

export function hasOverlay(pathname: string, params: URLSearchParams): boolean {
  return overlayParams(pathname).some((key) => params.has(key));
}

export function withoutOverlays(pathname: string, params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  for (const key of overlayParams(pathname)) next.delete(key);
  return next;
}
