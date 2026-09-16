import { useEffect, useRef, useState } from "react";
import { GLOW_MS, changedSince, fetchStamps, type FetchStamps, type GlowTicket } from "../lib/changeGlow";

const NONE: ReadonlySet<string> = new Set();

/**
 * The ids of the cards that should be glowing right now: the ones a live
 * refresh changed or brought in, each for `GLOW_MS` from the fetch that
 * carried it.
 *
 * `tickets` is the cards currently on the graph — null until the first fetch
 * lands — and must keep its identity between refetches that changed nothing,
 * which is what the page's memoised topology gives. Which ids count as
 * changed is decided in src/lib/changeGlow.ts; all this adds is the clock.
 *
 * Every card times out on its own, so a second edit elsewhere does not cut an
 * earlier card's glow short, and a card that changes again while it is
 * glowing gets its two seconds afresh. The returned set has a stable identity
 * while nothing starts or stops glowing, so the graph does not re-render on
 * refetches that changed nothing.
 */
export function useChangeGlow(tickets: readonly GlowTicket[] | null): ReadonlySet<string> {
  const [glowing, setGlowing] = useState<ReadonlySet<string>>(NONE);
  // The previous fetch's stamps, and one timer per glowing card. Refs, not
  // state: neither belongs in the render output, and writing them must not
  // schedule a render of its own.
  const previousRef = useRef<FetchStamps | null>(null);
  const timersRef = useRef(new Map<string, ReturnType<typeof setTimeout>>());

  useEffect(() => {
    if (tickets === null) return;
    const stamps = fetchStamps(tickets);
    const previous = previousRef.current;
    previousRef.current = stamps;
    const changed = changedSince(previous, stamps);
    if (changed.length === 0) return;

    const timers = timersRef.current;
    for (const id of changed) {
      const running = timers.get(id);
      if (running !== undefined) clearTimeout(running);
      timers.set(
        id,
        setTimeout(() => {
          timers.delete(id);
          setGlowing((prev) => {
            if (!prev.has(id)) return prev;
            const next = new Set(prev);
            next.delete(id);
            return next.size === 0 ? NONE : next;
          });
        }, GLOW_MS),
      );
    }
    setGlowing((prev) => {
      if (changed.every((id) => prev.has(id))) return prev;
      const next = new Set(prev);
      for (const id of changed) next.add(id);
      return next;
    });
  }, [tickets]);

  // Timers outlive a fetch, so they are cleared when the page goes away
  // rather than on every change.
  useEffect(() => {
    const timers = timersRef.current;
    return () => {
      for (const timer of timers.values()) clearTimeout(timer);
      timers.clear();
    };
  }, []);

  return glowing;
}
