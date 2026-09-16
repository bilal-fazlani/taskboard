import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DEBOUNCE_MS, RECONNECT_DELAYS, debounce, reconnectDelay } from "./liveRefresh";

describe("reconnectDelay", () => {
  it("grows with each consecutive failure", () => {
    const delays = RECONNECT_DELAYS.map((_, i) => reconnectDelay(i + 1));
    expect(delays).toEqual([...RECONNECT_DELAYS]);
    for (let i = 1; i < delays.length; i++) expect(delays[i]).toBeGreaterThan(delays[i - 1]);
  });

  it("holds at the longest delay rather than growing without bound", () => {
    const longest = RECONNECT_DELAYS[RECONNECT_DELAYS.length - 1];
    expect(reconnectDelay(RECONNECT_DELAYS.length + 1)).toBe(longest);
    expect(reconnectDelay(1000)).toBe(longest);
  });

  it("treats a first attempt and anything below it as the first retry", () => {
    expect(reconnectDelay(1)).toBe(RECONNECT_DELAYS[0]);
    expect(reconnectDelay(0)).toBe(RECONNECT_DELAYS[0]);
  });
});

describe("debounce", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("runs the callback once the calls stop", () => {
    const fn = vi.fn();
    const d = debounce(fn, DEBOUNCE_MS);
    d.call();
    vi.advanceTimersByTime(DEBOUNCE_MS - 1);
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it("coalesces a burst into a single call", () => {
    const fn = vi.fn();
    const d = debounce(fn, DEBOUNCE_MS);
    for (let i = 0; i < 5; i++) {
      d.call();
      vi.advanceTimersByTime(10);
    }
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(DEBOUNCE_MS);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it("starts a fresh wait after it has run", () => {
    const fn = vi.fn();
    const d = debounce(fn, DEBOUNCE_MS);
    d.call();
    vi.advanceTimersByTime(DEBOUNCE_MS);
    d.call();
    vi.advanceTimersByTime(DEBOUNCE_MS);
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it("drops a scheduled call when cancelled", () => {
    const fn = vi.fn();
    const d = debounce(fn, DEBOUNCE_MS);
    d.call();
    d.cancel();
    vi.advanceTimersByTime(DEBOUNCE_MS * 10);
    expect(fn).not.toHaveBeenCalled();
  });
});
