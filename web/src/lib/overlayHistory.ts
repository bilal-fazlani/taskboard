// Overlays are what opens on top of a view and lives in its query string: the
// ticket editor (`ticket`), a document (`doc`) and the epic modal (`epic`).
// Each overlay the user opens is its own browser history entry, so Back and
// Forward step through them. How many such entries sit on top of the view the
// overlays opened from is kept in the entry's location state, which the
// browser restores with the entry: Back from depth 2 lands on an entry that
// still says 1. Closing everything goes back that many entries at once, which
// leaves history as it was before the first overlay opened.
//
// `fromLink` records whether the entry under the first push already had an
// overlay, which is how an editor opened straight from a link looks. Going
// back to it lands on that linked overlay, so the caller strips it after.

export const OVERLAY_PARAMS: readonly string[] = ["ticket", "doc", "epic"];

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

export function hasOverlay(params: URLSearchParams): boolean {
  return OVERLAY_PARAMS.some((key) => params.has(key));
}

export function withoutOverlays(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  for (const key of OVERLAY_PARAMS) next.delete(key);
  return next;
}
