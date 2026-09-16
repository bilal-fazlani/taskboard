import { useEffect, useRef } from "react";
import { DEBOUNCE_MS, EVENTS_URL, debounce, reconnectDelay } from "../lib/liveRefresh";

/**
 * Calls `onChange` whenever the database changes, so a page shows an edit made
 * elsewhere — another tab, the CLI, an agent over MCP — without a reload.
 *
 * It listens on the `/api/events` stream from ACP-3, which sends a named
 * `changed` event (so `addEventListener`, not `onmessage`) and replays
 * nothing: on connect it sends only a comment. Anything that changed while the
 * stream was down is therefore invisible, which is why every (re)connect also
 * calls `onChange` — including the first one, which closes the gap between a
 * page's own initial fetch and the stream being live.
 *
 * `onChange` is read from a ref, so a callback that changes identity on every
 * render neither reopens the stream nor needs memoising by the caller. The
 * callback
 * should refetch and leave local UI state (scroll, selection, an open editor)
 * alone; the pages own that part.
 */
export function useLiveRefresh(onChange: () => void): void {
  const callbackRef = useRef(onChange);
  useEffect(() => {
    callbackRef.current = onChange;
  }, [onChange]);

  useEffect(() => {
    // No EventSource in a test environment or during any prerender: the page
    // then simply doesn't live-refresh.
    if (typeof EventSource === "undefined") return;

    let stopped = false;
    let source: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let failures = 0;
    const fire = debounce(() => callbackRef.current(), DEBOUNCE_MS);

    const connect = () => {
      const es = new EventSource(EVENTS_URL);
      source = es;
      es.addEventListener("open", () => {
        failures = 0;
        fire.call();
      });
      es.addEventListener("changed", () => fire.call());
      // EventSource retries by itself, but on a fixed delay of the server's
      // choosing and without a way to back off, so the stream is closed here
      // and reopened on our own schedule instead.
      es.addEventListener("error", () => {
        es.close();
        if (stopped || source !== es) return;
        source = null;
        failures += 1;
        retry = setTimeout(connect, reconnectDelay(failures));
      });
    };
    connect();

    return () => {
      stopped = true;
      fire.cancel();
      if (retry !== undefined) clearTimeout(retry);
      source?.close();
      source = null;
    };
  }, []);
}
