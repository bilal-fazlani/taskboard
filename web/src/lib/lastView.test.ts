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
    expect(readLastView()).toBe("/table");
    rememberView("/");
    expect(readLastView()).toBe("/");
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
