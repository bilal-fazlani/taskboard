// The timing behind useLiveRefresh, kept apart from the hook so it can be
// tested without a DOM or an EventSource.

/** Where the server streams its change events. */
export const EVENTS_URL = "/api/events";

/**
 * How long `changed` events are coalesced for. The server watches SQLite's
 * data_version on its own interval, so one edit can arrive as a single event
 * while a burst (a rebase of positions, an agent writing several tickets)
 * arrives as several; waiting a moment turns those into one refetch and still
 * lands well inside the "within a second" the ticket asks for.
 */
export const DEBOUNCE_MS = 100;

/**
 * The reconnect backoff, in milliseconds, by consecutive failed attempt. The
 * first retry is quick, because the usual cause is the dev server restarting;
 * repeated failures (the backend stopped) settle at ten seconds rather than
 * hammering it.
 */
export const RECONNECT_DELAYS: readonly number[] = [500, 1000, 2000, 5000, 10000];

/** The delay before attempt number `failures` (1 for the first retry). */
export function reconnectDelay(failures: number): number {
  const index = Math.min(Math.max(Math.trunc(failures), 1), RECONNECT_DELAYS.length) - 1;
  return RECONNECT_DELAYS[index];
}

export interface Debounced {
  /** Schedules the callback, restarting the wait if one was already running. */
  call: () => void;
  /** Drops a scheduled call, if any. */
  cancel: () => void;
}

/**
 * Calls `fn` once, `ms` after the last `call()`. Trailing only: nothing runs
 * until the events stop, which is what coalescing a burst into one refetch
 * means.
 */
export function debounce(fn: () => void, ms: number): Debounced {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const cancel = () => {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  };
  return {
    call() {
      cancel();
      timer = setTimeout(() => {
        timer = undefined;
        fn();
      }, ms);
    },
    cancel,
  };
}
