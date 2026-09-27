import { afterEach, describe, expect, it, vi } from "vitest";
import { LAST_PROJECT_KEY } from "./defaultProject";
import { NOW_PROJECT_KEY, readNowProject, rememberNowProject, restoredNowProject } from "./nowProject";
import { memoryStorage } from "../test/memoryStorage";

afterEach(() => vi.unstubAllGlobals());

const project = (prefix: string, name: string) => ({ prefix, name });
const PROJECTS = [project("LDR", "Leaderboard"), project("ACP", "Control plane")];

describe("Now's remembered project", () => {
  it("is every project when nothing was picked", () => {
    vi.stubGlobal("localStorage", memoryStorage());
    expect(readNowProject()).toBe("");
  });

  it("round-trips a project and every project under its own key, apart from the views' last project", () => {
    const storage = memoryStorage({ [LAST_PROJECT_KEY]: "LDR" });
    vi.stubGlobal("localStorage", storage);
    expect(NOW_PROJECT_KEY).not.toBe(LAST_PROJECT_KEY);
    rememberNowProject("ACP");
    expect(storage.getItem(NOW_PROJECT_KEY)).toBe("ACP");
    expect(readNowProject()).toBe("ACP");
    rememberNowProject("");
    expect(storage.getItem(NOW_PROJECT_KEY)).toBe("");
    expect(readNowProject()).toBe("");
    expect(storage.getItem(LAST_PROJECT_KEY)).toBe("LDR");
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
    expect(readNowProject()).toBe("");
    expect(() => rememberNowProject("ACP")).not.toThrow();
  });

  it("is simply absent when there is no storage", () => {
    vi.stubGlobal("localStorage", undefined);
    expect(readNowProject()).toBe("");
    expect(() => rememberNowProject("ACP")).not.toThrow();
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
      expect(readNowProject()).toBe("");
      expect(() => rememberNowProject("ACP")).not.toThrow();
    } finally {
      delete (globalThis as { localStorage?: unknown }).localStorage;
    }
  });
});

describe("restoredNowProject", () => {
  it("is the remembered project while it is active, spelled as the list spells it", () => {
    expect(restoredNowProject(PROJECTS, "ACP")).toBe("ACP");
    expect(restoredNowProject(PROJECTS, "ldr")).toBe("LDR");
  });

  it("falls back to every project when the remembered one is deleted", () => {
    expect(restoredNowProject(PROJECTS, "GONE")).toBe("");
  });

  it("is every project with nothing remembered", () => {
    expect(restoredNowProject(PROJECTS, "")).toBe("");
    expect(restoredNowProject([], "ACP")).toBe("");
  });
});
