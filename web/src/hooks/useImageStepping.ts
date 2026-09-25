import { useEffect } from "react";
import type { DocumentMeta } from "../api/client";
import { adjacentImage } from "../lib/documents";

/** The attribute marking the 100% view's scroll region, which pans with ← and →. */
export const IMAGE_SCROLL = "data-image-scroll";

function typingIn(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || target.closest("input, textarea, select, [contenteditable='true']") !== null;
}

/** Whether focus is in the 100% view and it can still scroll the way the key points. */
function pansFurther(target: EventTarget | null, key: string): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const region = target.closest<HTMLElement>(`[${IMAGE_SCROLL}]`);
  if (!region) return false;
  if (key === "ArrowLeft") return region.scrollLeft > 0;
  return region.scrollLeft + region.clientWidth < region.scrollWidth - 1;
}

/**
 * ← and → show the previous and next of the owner's images, stopping at
 * either end. They do nothing while `paused` (a rename field or a confirm
 * is up), while focus is in a field, or with a modifier held. With focus in
 * the 100% view they pan the image first (the browser's own scrolling), and
 * step only once it cannot scroll further that way.
 */
export function useImageStepping(
  current: DocumentMeta | null,
  images: readonly DocumentMeta[],
  onStep: ((doc: DocumentMeta) => void) | undefined,
  paused: boolean,
): void {
  useEffect(() => {
    if (!current || !onStep || paused) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      if (e.defaultPrevented || e.isComposing || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
      if (typingIn(e.target) || pansFurther(e.target, e.key)) return;
      const next = adjacentImage(current, images, e.key === "ArrowRight" ? 1 : -1);
      if (!next) return;
      e.preventDefault();
      onStep(next);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [current, images, onStep, paused]);
}
