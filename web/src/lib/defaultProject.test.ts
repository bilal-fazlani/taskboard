import { afterEach, describe, expect, it, vi } from "vitest";
import {
  LAST_PROJECT_KEY,
  activeProjects,
  awaitingProject,
  defaultProject,
  isArchived,
  latestActivity,
  namedProject,
  projectLabel,
  readLastProject,
  rememberProject,
} from "./defaultProject";
import { memoryStorage } from "../test/memoryStorage";

const prefixes = ["ACP", "IAGML", "LDR"];

const project = (prefix: string, name: string, status = "active") => ({ prefix, name, status });
const ticket = (projectPrefix: string, updatedAt: string) => ({ projectPrefix, updatedAt });

// Listed newest first, as the API does, with an archived project among them.
const FDM = project("FDM", "Zoo field day");
const ACP = project("ACP", "Taskboard: agent control plane");
const OLD = project("OLD", "Archived one", "archived");
const IAGML = project("IAGML", "iagml site");
const PROJECTS = [FDM, OLD, ACP, IAGML];

const NO_ACTIVITY = new Map<string, number>();

afterEach(() => vi.unstubAllGlobals());

describe("defaultProject", () => {
  // ACP's tickets changed last among the active projects; OLD's changed later
  // still, but it is archived.
  const activity = latestActivity([
    ticket("FDM", "2026-09-20T10:00:00Z"),
    ticket("ACP", "2026-09-22T10:00:00Z"),
    ticket("IAGML", "2026-09-21T10:00:00Z"),
    ticket("OLD", "2026-09-23T10:00:00Z"),
  ]);

  it("is the remembered project when it is still active", () => {
    expect(defaultProject(PROJECTS, "IAGML", activity)).toBe("IAGML");
    expect(defaultProject(PROJECTS, "FDM", NO_ACTIVITY)).toBe("FDM");
  });

  it("spells the remembered project as the list does", () => {
    expect(defaultProject(PROJECTS, "iagml", activity)).toBe("IAGML");
  });

  it("is the active project whose tickets changed last, with nothing remembered", () => {
    expect(defaultProject(PROJECTS, null, activity)).toBe("ACP");
    expect(defaultProject(PROJECTS, "", activity)).toBe("ACP");
  });

  it("passes over a remembered project that has since been archived", () => {
    expect(defaultProject(PROJECTS, "OLD", activity)).toBe("ACP");
    expect(defaultProject(PROJECTS, "old", NO_ACTIVITY)).toBe("IAGML");
  });

  it("passes over a remembered project that has since been deleted", () => {
    expect(defaultProject(PROJECTS, "GONE", activity)).toBe("ACP");
  });

  it("never picks an archived project, however recent its tickets", () => {
    const onlyOld = latestActivity([ticket("OLD", "2026-09-23T10:00:00Z")]);
    expect(defaultProject(PROJECTS, null, onlyOld)).toBe("IAGML");
  });

  it("counts a project's latest ticket, whatever its status or order", () => {
    // FDM's newest ticket is its last one; IAGML's older ones don't matter.
    const mixed = latestActivity([
      ticket("IAGML", "2026-09-21T10:00:00Z"),
      ticket("FDM", "2026-09-01T10:00:00Z"),
      ticket("FDM", "2026-09-22T09:00:00.123456+01:00"),
      ticket("IAGML", "2026-09-02T10:00:00Z"),
    ]);
    expect(defaultProject(PROJECTS, null, mixed)).toBe("FDM");
  });

  it("goes by name, ignoring case, among the equally recent", () => {
    const tie = latestActivity([ticket("FDM", "2026-09-22T10:00:00Z"), ticket("IAGML", "2026-09-22T11:00:00+01:00")]);
    // "iagml site" before "Zoo field day", though "i" is lowercase.
    expect(defaultProject(PROJECTS, null, tie)).toBe("IAGML");
  });

  it("goes by name, ignoring case, when no active project has tickets", () => {
    // "iagml site", "Taskboard: …", "Zoo field day": not FDM, the newest.
    expect(defaultProject(PROJECTS, null, NO_ACTIVITY)).toBe("IAGML");
    expect(defaultProject([project("B", "beta"), project("A", "Alpha"), project("C", "Gamma")], null, NO_ACTIVITY)).toBe("A");
  });

  it("is nothing when every project is archived", () => {
    const archived = [project("OLD", "Old", "archived"), project("OLDER", "Older", "archived")];
    expect(defaultProject(archived, "OLD", latestActivity([ticket("OLD", "2026-09-23T10:00:00Z")]))).toBe("");
    expect(defaultProject(archived, null, NO_ACTIVITY)).toBe("");
  });

  it("is nothing when there are no projects", () => {
    expect(defaultProject([], "ACP", activity)).toBe("");
    expect(defaultProject([], null, NO_ACTIVITY)).toBe("");
  });
});

describe("latestActivity", () => {
  it("keeps each project's latest ticket time, by prefix in lowercase", () => {
    const latest = latestActivity([
      ticket("ACP", "2026-09-22T10:00:00Z"),
      ticket("ACP", "2026-09-23T10:00:00Z"),
      ticket("ACP", "2026-09-21T10:00:00Z"),
      ticket("LDR", "2026-09-20T10:00:00Z"),
    ]);
    expect([...latest]).toEqual([
      ["acp", Date.parse("2026-09-23T10:00:00Z")],
      ["ldr", Date.parse("2026-09-20T10:00:00Z")],
    ]);
  });

  it("skips a time it can't read, and has no entry for a project without tickets", () => {
    const latest = latestActivity([ticket("ACP", ""), ticket("LDR", "not a time")]);
    expect(latest.size).toBe(0);
  });
});

describe("activeProjects", () => {
  it("leaves out archived projects and sorts the rest by name, ignoring case", () => {
    expect(activeProjects(PROJECTS).map((p) => p.prefix)).toEqual(["IAGML", "ACP", "FDM"]);
    expect(activeProjects([project("Z", "zeta"), project("A", "Alpha"), project("B", "beta")]).map((p) => p.prefix)).toEqual([
      "A",
      "B",
      "Z",
    ]);
  });

  it("orders equal names by prefix, and leaves the list it was given alone", () => {
    const given = [project("B", "Same"), project("A", "same")];
    expect(activeProjects(given).map((p) => p.prefix)).toEqual(["A", "B"]);
    expect(given.map((p) => p.prefix)).toEqual(["B", "A"]);
  });

  it("is empty when every project is archived", () => {
    expect(activeProjects([OLD])).toEqual([]);
    expect(isArchived(OLD)).toBe(true);
    expect(isArchived(ACP)).toBe(false);
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
  const active = { status: "active" };
  const archived = { status: "archived" };

  it("waits while the URL names no project and one will be picked", () => {
    expect(awaitingProject("", null)).toBe(true);
    expect(awaitingProject("", [active])).toBe(true);
    expect(awaitingProject("", [archived, active])).toBe(true);
  });

  it("does not wait with no active projects, so the view shows its empty state", () => {
    expect(awaitingProject("", [])).toBe(false);
    expect(awaitingProject("", [archived])).toBe(false);
  });

  it("does not wait once a project is set", () => {
    expect(awaitingProject("ACP", null)).toBe(false);
    expect(awaitingProject("ACP", [active])).toBe(false);
  });
});

describe("projectLabel", () => {
  it("joins icon and name with a space", () => {
    expect(projectLabel({ icon: "🧭", name: "Taskboard", status: "active" })).toBe("🧭 Taskboard");
  });

  it("has no leading space when there is no icon", () => {
    expect(projectLabel({ icon: "", name: "Taskboard", status: "active" })).toBe("Taskboard");
  });

  it("appends (archived) for an archived project, after the icon and name", () => {
    expect(projectLabel({ icon: "🧭", name: "Taskboard", status: "archived" })).toBe("🧭 Taskboard (archived)");
    expect(projectLabel({ icon: "", name: "Taskboard", status: "archived" })).toBe("Taskboard (archived)");
  });
});
