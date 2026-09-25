// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi, type Mock, type MockInstance } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import type { BoardNavigation, TraversalResult } from "../lib/historyTraversal";
import { useOverlayHistory, type OverlayHistory } from "./useOverlayHistory";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// useOverlayHistory.dom.test.tsx pins the moves in a browser without the
// Navigation API (jsdom has none). Here window.navigation is a stand-in that
// lists the board's own entries k0..kN, as a browser's would after an HTML
// document added entries inside its frame, and traverses by moving jsdom's
// history, so the router sees the same popstate a real traversal gives it.
let root: Root;
let container: HTMLDivElement;
let overlays: OverlayHistory;

function Harness() {
  const o = useOverlayHistory();
  useEffect(() => {
    overlays = o;
  });
  return null;
}

async function settle() {
  for (let i = 0; i < 30; i++) await new Promise((resolve) => setTimeout(resolve, 5));
}

async function mount(url: string) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <BrowserRouter>
        <Harness />
      </BrowserRouter>,
    );
  });
}

const done = (): TraversalResult => ({ committed: Promise.resolve(), finished: Promise.resolve() });
function refused(name: string): TraversalResult {
  const error = new DOMException("refused", name);
  return { committed: Promise.reject(error), finished: Promise.reject(error) };
}

interface FakeNavigation extends BoardNavigation {
  index: number;
  back: Mock<() => TraversalResult>;
  traverseTo: Mock<(key: string) => TraversalResult>;
}

// `count` board entries, the last of them current.
function installNavigation(count: number): FakeNavigation {
  const nav = {
    index: count - 1,
    get currentEntry() {
      return { index: nav.index };
    },
    entries: () => Array.from({ length: count }, (_, i) => ({ key: `k${i}` })),
    back: vi.fn(() => {
      nav.index -= 1;
      window.history.go(-1);
      return done();
    }),
    traverseTo: vi.fn((key: string) => {
      const to = Number(key.slice(1));
      const steps = to - nav.index;
      nav.index = to;
      window.history.go(steps);
      return done();
    }),
  };
  Object.defineProperty(window, "navigation", { configurable: true, value: nav });
  return nav as FakeNavigation;
}

let go: MockInstance<History["go"]>;

beforeEach(() => {
  window.history.replaceState(null, "", "/");
  go = vi.spyOn(window.history, "go");
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  delete (window as { navigation?: unknown }).navigation;
  vi.restoreAllMocks();
});

const url = () => window.location.pathname + decodeURIComponent(window.location.search);
const params = (s: string) => new URLSearchParams(s);

async function openTicketThenDoc() {
  await mount("/kanban");
  await act(async () => overlays.push(params("ticket=ACP-7")));
  await act(async () => overlays.push(params("ticket=ACP-7&doc=Page.html")));
}

describe("useOverlayHistory with the Navigation API", () => {
  it("closeOne goes back with navigation.back()", async () => {
    await openTicketThenDoc();
    const nav = installNavigation(3);
    await act(async () => {
      overlays.closeOne(params("ticket=ACP-7"));
      await settle();
    });
    expect(nav.back).toHaveBeenCalledTimes(1);
    expect(go).toHaveBeenCalledTimes(1); // the stand-in's own traversal, not the hook's
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(overlays.depth).toBe(1);
  });

  it("closeAll traverses to the entry the overlays opened from", async () => {
    await openTicketThenDoc();
    const nav = installNavigation(3);
    await act(async () => {
      overlays.closeAll();
      await settle();
    });
    expect(nav.traverseTo).toHaveBeenCalledExactlyOnceWith("k0");
    expect(nav.back).not.toHaveBeenCalled();
    expect(url()).toBe("/kanban");
    expect(overlays.depth).toBe(0);
  });

  it("a second close after the document closed returns to the pre-editor entry", async () => {
    await openTicketThenDoc();
    const nav = installNavigation(3);
    await act(async () => {
      overlays.closeOne(params("ticket=ACP-7"));
      await settle();
    });
    await act(async () => {
      overlays.closeAll();
      await settle();
    });
    expect(nav.traverseTo).toHaveBeenCalledExactlyOnceWith("k0");
    expect(nav.index).toBe(0);
    expect(url()).toBe("/kanban");
    expect(overlays.depth).toBe(0);
  });

  it("closeAll from a linked-in overlay still ends on the plain view", async () => {
    await mount("/table?project=ACP&ticket=ACP-7");
    await act(async () => overlays.push(params("project=ACP&ticket=ACP-25")));
    const nav = installNavigation(2);
    await act(async () => {
      overlays.closeAll();
      await settle();
    });
    expect(nav.traverseTo).toHaveBeenCalledExactlyOnceWith("k0");
    expect(url()).toBe("/table?project=ACP");
  });

  it("closeOne replaces the URL when the browser refuses to go back", async () => {
    await openTicketThenDoc();
    const nav = installNavigation(1);
    nav.back.mockImplementation(() => refused("InvalidStateError"));
    const length = window.history.length;
    await act(async () => {
      overlays.closeOne(params("ticket=ACP-7"));
      await settle();
    });
    expect(nav.back).toHaveBeenCalledTimes(1);
    expect(go).not.toHaveBeenCalled();
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(window.history.length).toBe(length);
  });

  it("closeAll replaces the URL when the browser refuses the traversal", async () => {
    await openTicketThenDoc();
    const nav = installNavigation(3);
    nav.traverseTo.mockImplementation(() => refused("InvalidStateError"));
    await act(async () => {
      overlays.closeAll();
      await settle();
    });
    expect(nav.traverseTo).toHaveBeenCalledTimes(1);
    expect(go).not.toHaveBeenCalled();
    expect(url()).toBe("/kanban");
    expect(overlays.depth).toBe(0);
  });

  it("closeAll replaces the URL when the entry below is no longer listed", async () => {
    await openTicketThenDoc();
    const nav = installNavigation(1);
    await act(async () => {
      overlays.closeAll();
      await settle();
    });
    expect(nav.traverseTo).not.toHaveBeenCalled();
    expect(go).not.toHaveBeenCalled();
    expect(url()).toBe("/kanban");
    expect(overlays.depth).toBe(0);

    // Nothing counts below the entry now: the next overlay closes back to it.
    await act(async () => overlays.push(params("ticket=ACP-25")));
    expect(overlays.depth).toBe(1);
  });

  it("leaves the URL alone when a newer navigation aborts the traversal", async () => {
    await openTicketThenDoc();
    const nav = installNavigation(3);
    nav.back.mockImplementation(() => refused("AbortError"));
    await act(async () => {
      overlays.closeOne(params("ticket=ACP-7"));
      await settle();
    });
    expect(url()).toBe("/kanban?ticket=ACP-7&doc=Page.html");
  });

  it("depth 0 still replaces in place without traversing", async () => {
    await mount("/table?ticket=ACP-7&doc=Page.html");
    const nav = installNavigation(1);
    await act(async () => {
      overlays.closeOne(params("ticket=ACP-7"));
      await settle();
    });
    expect(nav.back).not.toHaveBeenCalled();
    expect(url()).toBe("/table?ticket=ACP-7");
  });
});

describe("useOverlayHistory without the Navigation API", () => {
  it("closeOne goes back one entry with history.go", async () => {
    await openTicketThenDoc();
    expect("navigation" in window).toBe(false);
    await act(async () => {
      overlays.closeOne(params("ticket=ACP-7"));
      await settle();
    });
    expect(go).toHaveBeenCalledExactlyOnceWith(-1);
    expect(url()).toBe("/kanban?ticket=ACP-7");
  });

  it("closeAll goes back by the depth with history.go", async () => {
    await openTicketThenDoc();
    await act(async () => {
      overlays.closeAll();
      await settle();
    });
    expect(go).toHaveBeenCalledExactlyOnceWith(-2);
    expect(url()).toBe("/kanban");
  });
});
