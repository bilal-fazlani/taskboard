import { useEffect } from "react";
import type { DocumentMeta } from "../api/client";

function typingIn(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || target.closest("input, textarea, select, [contenteditable='true']") !== null;
}

/**
 * ← and → show the previous and next of the owner's images, stopping at
 * either end. They do nothing while `paused` (a rename field or a confirm
 * is up), while focus is in a field, or with a modifier held.
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
      if (typingIn(e.target)) return;
      const index = images.findIndex((d) => d.id === current.id);
      if (index < 0) return;
      const next = images[index + (e.key === "ArrowRight" ? 1 : -1)];
      if (!next) return;
      e.preventDefault();
      onStep(next);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [current, images, onStep, paused]);
}
