// Escape closes the topmost layer only: a confirm over the document modal,
// the modal over the ticket editor. Layers register here and one capturing
// listener calls the newest. It marks the event handled, so the ticket
// editor's own Escape listener, which skips handled events, leaves it alone.
import { useEffect, useRef } from "react";

const stack: Array<() => void> = [];

function onKeyDown(e: KeyboardEvent) {
  if (e.key !== "Escape" || e.isComposing || stack.length === 0) return;
  e.preventDefault();
  stack[stack.length - 1]();
}

export function pushEscape(handler: () => void): () => void {
  if (stack.length === 0) document.addEventListener("keydown", onKeyDown, true);
  stack.push(handler);
  return () => {
    const i = stack.lastIndexOf(handler);
    if (i >= 0) stack.splice(i, 1);
    if (stack.length === 0) document.removeEventListener("keydown", onKeyDown, true);
  };
}

/** Escape calls `handler` while the component is mounted and on top. */
export function useEscape(handler: () => void): void {
  const ref = useRef(handler);
  useEffect(() => {
    ref.current = handler;
  }, [handler]);
  useEffect(() => pushEscape(() => ref.current()), []);
}
