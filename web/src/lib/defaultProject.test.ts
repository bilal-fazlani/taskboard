import { afterEach, describe, expect, it, vi } from "vitest";
import {
  LAST_PROJECT_KEY,
  awaitingProject,
  defaultProject,
  namedProject,
  readLastProject,
  rememberProject,
} from "./defaultProject";
import { memoryStorage } from "../test/memoryStorage";

const prefixes = ["ACP", "IAGML", "LDR"];

afterEach(() => vi.unstubAllGlobals());

describe("defaultProject", () => {
  it("is the remembered project when it still exists", () => {
    expect(defaultProject(prefixes, "LDR")).toBe("LDR");
  });

  it("spells the remembered project as the list does", () => {
    expect(defaultProject(prefixes, "iagml")).toBe("IAGML");
  });

  it("falls back to the first project with nothing remembered", () => {
    expect(defaultProject(prefixes, null)).toBe("ACP");
    expect(defaultProject(prefixes, "")).toBe("ACP");
  });

  it("falls back to the first project when the remembered one is gone", () => {
    expect(defaultProject(prefixes, "GONE")).toBe("ACP");
  });

  it("is nothing when there are no projects", () => {
    expect(defaultProject([], "ACP")).toBe("");
    expect(defaultProject([], null)).toBe("");
  });
});

describe("namedProject", () => {
  it("finds the project a value names, ignoring case", () => {
    expect(namedProject(prefixes, "acp")).toBe("ACP");
    expect(namedProject(prefixes, "GONE")).toBeNull();
    expect(namedProject(prefixes, "")).toBeNull();
    expect(namedProject(prefixes, null)).toBeNull();
  });
});

describe("the remembered project", () => {
  it("round-trips through localStorage under one key for every view", () => {
    const storage = memoryStorage();
    vi.stubGlobal("localStorage", storage);
    expect(readLastProject()).toBeNull();
    rememberProject("IAGML");
    expect(storage.getItem(LAST_PROJECT_KEY)).toBe("IAGML");
    expect(readLastProject()).toBe("IAGML");
  });

  it("is simply absent when storage throws", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("SecurityError");
      },
      setItem: () => {
        throw new Error("QuotaExceededError");
      },
    });
    expect(readLastProject()).toBeNull();
    expect(() => rememberProject("ACP")).not.toThrow();
  });

  it("is simply absent when there is no storage", () => {
    vi.stubGlobal("localStorage", undefined);
    expect(readLastProject()).toBeNull();
    expect(() => rememberProject("ACP")).not.toThrow();
  });

  it("is absent when merely reading localStorage throws", () => {
    // Some browsers throw from the property itself when site data is blocked.
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      get() {
        throw new Error("SecurityError");
      },
    });
    try {
      expect(readLastProject()).toBeNull();
      expect(() => rememberProject("ACP")).not.toThrow();
    } finally {
      delete (globalThis as { localStorage?: unknown }).localStorage;
    }
  });
});

describe("awaitingProject", () => {
  it("waits while the URL names no project and one will be picked", () => {
    expect(awaitingProject("", null)).toBe(true);
    expect(awaitingProject("", [{}])).toBe(true);
  });

  it("does not wait with no projects at all, so the view shows its empty state", () => {
    expect(awaitingProject("", [])).toBe(false);
  });

  it("does not wait once a project is set", () => {
    expect(awaitingProject("ACP", null)).toBe(false);
    expect(awaitingProject("ACP", [{}])).toBe(false);
  });
});
