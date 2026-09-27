import { afterEach, describe, expect, it, vi } from "vitest";
import { FALLBACK_VIEW, LAST_VIEW_KEY, readLastView, rememberView } from "./lastView";
import { memoryStorage } from "../test/memoryStorage";

afterEach(() => vi.unstubAllGlobals());

describe("last ticket view", () => {
  it("falls back to Kanban when none was shown", () => {
    vi.stubGlobal("localStorage", memoryStorage());
    expect(FALLBACK_VIEW).toBe("/kanban");
    expect(readLastView()).toBe("/kanban");
  });

  it("remembers the ticket views and ignores every other page", () => {
    vi.stubGlobal("localStorage", memoryStorage());
    rememberView("/table");
    expect(readLastView()).toBe("/table");
    rememberView("/epics");
    rememberView("/projects");
    rememberView("/now");
    expect(readLastView()).toBe("/table");
    rememberView("/dependencies");
    expect(readLastView()).toBe("/dependencies");
    // / is no view now: it only redirects, to Now or a ticket's view.
    rememberView("/");
    expect(readLastView()).toBe("/dependencies");
  });

  it.each([
    ["/table/", "/table"],
    ["/kanban/", "/kanban"],
    ["/KANBAN", "/kanban"],
    ["/Table", "/table"],
    ["/dependencies/", "/dependencies"],
  ])("remembers %s as %s, matching paths as the sidebar does", (path, view) => {
    const storage = memoryStorage();
    vi.stubGlobal("localStorage", storage);
    rememberView(path);
    expect(storage.getItem(LAST_VIEW_KEY)).toBe(view);
    expect(readLastView()).toBe(view);
  });

  it.each(["/kanban/extra", "/table//", "/dependencies/x"])("ignores %s, which is no view", (path) => {
    vi.stubGlobal("localStorage", memoryStorage());
    rememberView(path);
    expect(readLastView()).toBe("/kanban");
  });

  it("reads a stored / as Dependencies, which was served there before Now became the home page", () => {
    vi.stubGlobal("localStorage", memoryStorage({ [LAST_VIEW_KEY]: "/" }));
    expect(readLastView()).toBe("/dependencies");
  });

  it("falls back on a value that names no ticket view", () => {
    vi.stubGlobal("localStorage", memoryStorage({ [LAST_VIEW_KEY]: "/labels" }));
    expect(readLastView()).toBe("/kanban");
  });

  it("falls back when storage throws, and remembering then does nothing", () => {
    const broken = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    };
    vi.stubGlobal("localStorage", broken);
    expect(() => rememberView("/table")).not.toThrow();
    expect(readLastView()).toBe("/kanban");
  });
});
