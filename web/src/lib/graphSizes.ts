import type { Size } from "./graphLayout";

/**
 * Folds fresh card measurements into the map of known sizes. Returns `prev`
 * itself when nothing changed, so a state update with the result bails out
 * instead of re-rendering, which is what stops measuring from looping.
 */
export function mergeSizes(
  prev: ReadonlyMap<string, Size>,
  measured: Iterable<readonly [id: string, size: Size]>,
): ReadonlyMap<string, Size> {
  let next: Map<string, Size> | null = null;
  for (const [id, size] of measured) {
    const known = (next ?? prev).get(id);
    if (known && known.width === size.width && known.height === size.height) continue;
    next ??= new Map(prev);
    next.set(id, { width: size.width, height: size.height });
  }
  return next ?? prev;
}
