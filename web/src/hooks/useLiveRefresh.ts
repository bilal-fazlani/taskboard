import { useEffect, useRef, useSyncExternalStore } from "react";
import { DEBOUNCE_MS, EVENTS_URL, debounce, reconnectDelay, type Debounced } from "../lib/liveRefresh";

// One stream for the whole document, shared by every caller of the hook.
//
// A browser allows six connections to an origin over HTTP/1.1, and a page
// holds its stream open for as long as it is shown, so a second subscriber
// must not cost a second connection: at two per page a couple of open tabs
// would leave the API calls nothing to go out on. Everything below the hook
// is therefore module state, opened with the first subscriber and closed with
// the last.
const listeners = new Set<() => void>();
let source: EventSource | null = null;
let retry: ReturnType<typeof setTimeout> | undefined;
let failures = 0;
let fire: Debounced | null = null;

/**
 * Where the shared stream stands: "connecting" until it first opens (and
 * whenever no one is subscribed), "live" while it is open, and "reconnecting"
 * from a drop until it opens again.
 */
export type LiveStatus = "connecting" | "live" | "reconnecting";
let status: LiveStatus = "connecting";
const statusListeners = new Set<() => void>();

function setStatus(next: LiveStatus) {
  if (status === next) return;
  status = next;
  for (const listener of [...statusListeners]) listener();
}

function notify() {
  for (const listener of [...listeners]) listener();
}

function connect() {
  const es = new EventSource(EVENTS_URL);
  source = es;
  es.addEventListener("open", () => {
    failures = 0;
    if (source === es) setStatus("live");
    fire?.call();
  });
  es.addEventListener("changed", () => fire?.call());
  // EventSource retries by itself, but on a fixed delay of the server's
  // choosing and without a way to back off, so the stream is closed here and
  // reopened on our own schedule instead. A stream that is no longer the
  // current one — the last subscriber went, or it was replaced — is left
  // where it is.
  es.addEventListener("error", () => {
    es.close();
    if (source !== es) return;
    source = null;
    failures += 1;
    setStatus("reconnecting");
    retry = setTimeout(connect, reconnectDelay(failures));
  });
}

function start() {
  failures = 0;
  fire = debounce(notify, DEBOUNCE_MS);
  connect();
}

function stop() {
  fire?.cancel();
  fire = null;
  if (retry !== undefined) clearTimeout(retry);
  retry = undefined;
  const open = source;
  source = null;
  open?.close();
  setStatus("connecting");
}

function subscribeStatus(listener: () => void) {
  statusListeners.add(listener);
  return () => {
    statusListeners.delete(listener);
  };
}

/**
 * Where the stream useLiveRefresh shares stands (see LiveStatus), for a page
 * that shows whether it is live. It only watches: the stream is opened by the
 * page's own useLiveRefresh, and without one it stays "connecting".
 */
export function useLiveStatus(): LiveStatus {
  return useSyncExternalStore(subscribeStatus, () => status, () => "connecting");
}

/**
 * Calls `onChange` whenever the database changes, so a page shows an edit made
 * elsewhere — another tab, the CLI, an agent over MCP — without a reload.
 *
 * It listens on the `/api/events` stream from ACP-3, which sends a named
 * `changed` event (so `addEventListener`, not `onmessage`) and replays
 * nothing: on connect it sends only a comment. Anything that changed while the
 * stream was down is therefore invisible, which is why every (re)connect also
 * calls every `onChange` — including the first one, which closes the gap
 * between a page's own initial fetch and the stream being live. A caller that
 * joins a stream already open has no such gap, and hears nothing until
 * something changes.
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

    const listener = () => callbackRef.current();
    listeners.add(listener);
    if (listeners.size === 1) start();

    return () => {
      listeners.delete(listener);
      if (listeners.size === 0) stop();
    };
  }, []);
}
