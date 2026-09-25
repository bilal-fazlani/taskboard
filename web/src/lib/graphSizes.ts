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

/**
 * The known sizes of the cards in `ids` alone, so cards that have left the
 * graph don't pile up. Returns `prev` itself when every known card is still
 * there, so a state update with the result bails out.
 */
export function pruneSizes(prev: ReadonlyMap<string, Size>, ids: ReadonlySet<string>): ReadonlyMap<string, Size> {
  for (const id of prev.keys()) {
    if (!ids.has(id)) return new Map([...prev].filter(([known]) => ids.has(known)));
  }
  return prev;
}

/** The part of a ResizeObserverEntry that entrySize reads. */
export interface MeasuredEntry {
  borderBoxSize?: readonly { inlineSize: number; blockSize: number }[];
  target: Element;
}

/**
 * A card's size from its ResizeObserver entry: the border box, exact to the
 * fraction of a pixel and untouched by the canvas's scale. offsetWidth and
 * offsetHeight round to whole pixels, so they are only the fallback for an
 * entry without a border box. Cards are laid out in horizontal writing
 * mode, where inline is width and block is height.
 */
export function entrySize(entry: MeasuredEntry): Size {
  const box = entry.borderBoxSize?.[0];
  if (box) return { width: box.inlineSize, height: box.blockSize };
  const el = entry.target as HTMLElement;
  return { width: el.offsetWidth, height: el.offsetHeight };
}
