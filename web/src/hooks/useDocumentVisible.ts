import { useSyncExternalStore } from "react";

function subscribe(onChange: () => void): () => void {
  document.addEventListener("visibilitychange", onChange);
  return () => document.removeEventListener("visibilitychange", onChange);
}

const isVisible = () => document.visibilityState === "visible";

/**
 * Whether the document is shown: false in a background tab or a minimised
 * window, where the browser neither paints the page nor measures it.
 */
export function useDocumentVisible(): boolean {
  return useSyncExternalStore(subscribe, isVisible, () => true);
}
