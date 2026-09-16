// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { useLiveRefresh } from "./useLiveRefresh";
import { DEBOUNCE_MS, EVENTS_URL, reconnectDelay } from "../lib/liveRefresh";

// jsdom has no EventSource, and the tests must not reach a server anyway, so
// the hook gets this stand-in. It records what was opened and lets a test
// deliver the events the real stream would.
class FakeEventSource {
  static opened: FakeEventSource[] = [];
  private readonly listeners = new Map<string, Set<() => void>>();
  closed = false;

  readonly url: string;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.opened.push(this);
  }

  addEventListener(type: string, listener: () => void) {
    let set = this.listeners.get(type);
    if (!set) this.listeners.set(type, (set = new Set()));
    set.add(listener);
  }

  removeEventListener(type: string, listener: () => void) {
    this.listeners.get(type)?.delete(listener);
  }

  close() {
    this.closed = true;
  }

  /** Delivers an event to the hook, as the browser would. */
  emit(type: string) {
    for (const listener of [...(this.listeners.get(type) ?? [])]) listener();
  }
}

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Long enough to cover every backoff step.
const RECONNECT_ATTEMPTS = 100;

let root: Root;
let container: HTMLDivElement;

function Probe({ onChange }: { onChange: () => void }) {
  useLiveRefresh(onChange);
  return null;
}

function mount(onChange: () => void) {
  act(() => {
    root.render(<Probe onChange={onChange} />);
  });
}

/** Two pages' worth of the hook, as the views and the filter bar are. */
function mountBoth(first: () => void, second: () => void) {
  act(() => {
    root.render(
      <>
        <Probe onChange={first} />
        <Probe onChange={second} />
      </>,
    );
  });
}

/** The stream the hook has open right now. */
function current(): FakeEventSource {
  const last = FakeEventSource.opened[FakeEventSource.opened.length - 1];
  expect(last).toBeDefined();
  return last;
}

beforeEach(() => {
  vi.useFakeTimers();
  FakeEventSource.opened = [];
  (globalThis as unknown as { EventSource: unknown }).EventSource = FakeEventSource;
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.useRealTimers();
  delete (globalThis as unknown as { EventSource?: unknown }).EventSource;
});

describe("useLiveRefresh", () => {
  it("opens the events stream once", () => {
    mount(vi.fn());
    expect(FakeEventSource.opened).toHaveLength(1);
    expect(current().url).toBe(EVENTS_URL);
  });

  it("refetches when the stream opens, since the server replays nothing", () => {
    const onChange = vi.fn();
    mount(onChange);
    act(() => current().emit("open"));
    expect(onChange).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("refetches on a changed event", () => {
    const onChange = vi.fn();
    mount(onChange);
    const stream = current();
    act(() => stream.emit("open"));
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    onChange.mockClear();

    act(() => stream.emit("changed"));
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("coalesces a burst of changed events into one refetch", () => {
    const onChange = vi.fn();
    mount(onChange);
    const stream = current();
    act(() => stream.emit("open"));
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    onChange.mockClear();

    act(() => {
      for (let i = 0; i < 4; i++) {
        stream.emit("changed");
        vi.advanceTimersByTime(DEBOUNCE_MS / 4);
      }
    });
    expect(onChange).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("reconnects with a growing backoff and refetches once back", () => {
    const onChange = vi.fn();
    mount(onChange);
    const first = current();
    act(() => first.emit("open"));
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    onChange.mockClear();

    act(() => first.emit("error"));
    expect(first.closed).toBe(true);
    // Nothing reopens before the first backoff delay has passed.
    act(() => vi.advanceTimersByTime(reconnectDelay(1) - 1));
    expect(FakeEventSource.opened).toHaveLength(1);
    act(() => vi.advanceTimersByTime(1));
    expect(FakeEventSource.opened).toHaveLength(2);

    // A second failure in a row waits longer.
    const second = current();
    act(() => second.emit("error"));
    act(() => vi.advanceTimersByTime(reconnectDelay(1)));
    expect(FakeEventSource.opened).toHaveLength(2);
    act(() => vi.advanceTimersByTime(reconnectDelay(2) - reconnectDelay(1)));
    expect(FakeEventSource.opened).toHaveLength(3);

    // Reconnecting refetches, because changes made while it was down were
    // never sent.
    const third = current();
    act(() => third.emit("open"));
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    expect(onChange).toHaveBeenCalledTimes(1);

    // And a success puts the backoff back to the shortest delay.
    act(() => third.emit("error"));
    act(() => vi.advanceTimersByTime(reconnectDelay(1)));
    expect(FakeEventSource.opened).toHaveLength(4);
  });

  it("keeps one stream when the callback identity changes", () => {
    mount(vi.fn());
    const stream = current();
    mount(vi.fn());
    expect(FakeEventSource.opened).toHaveLength(1);
    expect(stream.closed).toBe(false);
  });

  it("calls the latest callback", () => {
    const first = vi.fn();
    const second = vi.fn();
    mount(first);
    const stream = current();
    mount(second);
    act(() => stream.emit("changed"));
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  // A browser allows six connections to an origin, and a stream is held open
  // for as long as its page is shown, so several subscribers on one page
  // share the one stream rather than each holding a connection of its own.
  it("opens one stream for several subscribers and calls them all", () => {
    const page = vi.fn();
    const panel = vi.fn();
    mountBoth(page, panel);
    expect(FakeEventSource.opened).toHaveLength(1);

    act(() => current().emit("changed"));
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    expect(page).toHaveBeenCalledTimes(1);
    expect(panel).toHaveBeenCalledTimes(1);
  });

  it("keeps the stream while one subscriber remains, and closes it with the last", () => {
    const page = vi.fn();
    const panel = vi.fn();
    mountBoth(page, panel);
    const stream = current();

    // The second subscriber goes; the stream stays, and still feeds the first.
    act(() => root.render(<Probe onChange={page} />));
    expect(stream.closed).toBe(false);
    act(() => stream.emit("changed"));
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS));
    expect(page).toHaveBeenCalledTimes(1);
    expect(panel).not.toHaveBeenCalled();

    act(() => root.render(null));
    expect(stream.closed).toBe(true);
    expect(FakeEventSource.opened).toHaveLength(1);
  });

  it("opens a stream again for a subscriber arriving after the last one left", () => {
    mount(vi.fn());
    act(() => root.render(null));
    expect(current().closed).toBe(true);
    mount(vi.fn());
    expect(FakeEventSource.opened).toHaveLength(2);
    expect(current().closed).toBe(false);
  });

  it("closes the stream on unmount and stops reconnecting", () => {
    const onChange = vi.fn();
    mount(onChange);
    const stream = current();
    act(() => stream.emit("changed"));
    act(() => root.render(null));

    expect(stream.closed).toBe(true);
    act(() => vi.advanceTimersByTime(DEBOUNCE_MS * 10));
    expect(onChange).not.toHaveBeenCalled();

    act(() => stream.emit("error"));
    act(() => vi.advanceTimersByTime(reconnectDelay(RECONNECT_ATTEMPTS)));
    expect(FakeEventSource.opened).toHaveLength(1);
  });
});
