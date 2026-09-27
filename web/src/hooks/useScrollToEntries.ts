import { useEffect, useRef, type RefObject } from "react";
import { useLocation } from "react-router-dom";

/** The hash the ticket page's context links carry to open a dialog on its entries. */
export const ENTRIES_HASH = "#entries";

/**
 * When the dialog opened from a link ending in #entries, scrolls its
 * `container` so `target` (the entries) starts at the top: once, as soon as
 * `ready` (the entries have loaded, so there is something to scroll to).
 * Only the container scrolls: scrollIntoView could move the dialog's own
 * frame too. Focus moves to the entries with it (the target needs
 * tabIndex={-1}), so it isn't left in a field scrolled out of view, such as
 * the epic's autofocused Name.
 */
export function useScrollToEntries(
  container: RefObject<HTMLElement | null> | undefined,
  target: RefObject<HTMLElement | null>,
  ready: boolean,
) {
  const { hash } = useLocation();
  const wanted = hash === ENTRIES_HASH;
  const done = useRef(false);
  useEffect(() => {
    if (!wanted || !ready || done.current) return;
    const box = container?.current;
    const el = target.current;
    if (!box || !el) return;
    done.current = true;
    // The box's own top padding stays above the entries, as it does above its first field.
    const gap = parseFloat(getComputedStyle(box).paddingTop) || 0;
    box.scrollTo?.({ top: box.scrollTop + el.getBoundingClientRect().top - box.getBoundingClientRect().top - gap });
    el.focus({ preventScroll: true });
  }, [wanted, ready, container, target]);
}
