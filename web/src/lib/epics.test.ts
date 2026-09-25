import { describe, expect, it } from "vitest";
import type { Epic, EpicProgress } from "../api/client";
import {
  arrangeEpics,
  barSegments,
  deleteMessage,
  epicLink,
  nameError,
  progressText,
  serverMessage,
  showsNoEpic,
} from "./epics";

function progress(counts: Partial<Record<string, number>> = {}, lastActivityAt: string | null = null): EpicProgress {
  const full = { todo: 0, in_progress: 0, agent_review: 0, done: 0, ...counts } as Record<string, number>;
  const total = Object.values(full).reduce((a, b) => a + b, 0);
  return { counts: full, total, complete: total > 0 && full.done === total, lastActivityAt };
}

const epic = (name: string, counts: Partial<Record<string, number>> = {}, at: string | null = null): Epic => ({
  id: `e-${name}`,
  projectId: "p",
  name,
  createdAt: "",
  updatedAt: "",
  ...progress(counts, at),
});

const names = (epics: Epic[]) => epics.map((e) => e.name);

describe("arrangeEpics", () => {
  it("puts epics with an active ticket first, then by latest activity, ties by name, empty ones last", () => {
    const { live } = arrangeEpics([
      epic("Empty B"),
      epic("Idle new", { todo: 1 }, "2026-09-22T00:00:00Z"),
      epic("Review old", { agent_review: 1 }, "2026-09-01T00:00:00Z"),
      epic("empty a"),
      epic("Idle old", { todo: 1, done: 1 }, "2026-09-10T00:00:00Z"),
      epic("Busy new", { in_progress: 1 }, "2026-09-21T00:00:00Z"),
      epic("beta", { todo: 1 }, "2026-09-15T00:00:00Z"),
      epic("Alpha", { todo: 1 }, "2026-09-15T00:00:00Z"),
    ]);
    expect(names(live)).toEqual(["Busy new", "Review old", "Idle new", "Alpha", "beta", "Idle old", "empty a", "Empty B"]);
  });

  it("folds complete epics away, in the same order, and never an empty one", () => {
    const { live, complete } = arrangeEpics([
      epic("Old done", { done: 2 }, "2026-09-01T00:00:00Z"),
      epic("Empty"),
      epic("New done", { done: 1 }, "2026-09-20T00:00:00Z"),
      epic("Open", { todo: 1, done: 1 }, "2026-09-02T00:00:00Z"),
    ]);
    expect(names(live)).toEqual(["Open", "Empty"]);
    expect(names(complete)).toEqual(["New done", "Old done"]);
  });
});

describe("showsNoEpic", () => {
  it("shows only when the project has a ticket without an epic", () => {
    expect(showsNoEpic(progress({ todo: 1 }))).toBe(true);
    expect(showsNoEpic(progress({ done: 3 }))).toBe(true);
    expect(showsNoEpic(progress())).toBe(false);
    expect(showsNoEpic(undefined)).toBe(false);
  });
});

describe("barSegments", () => {
  it("sizes each status in proportion, done first, leaving out empty ones", () => {
    expect(barSegments(progress({ todo: 3, in_progress: 2, agent_review: 1, done: 4 }))).toEqual([
      { status: "done", count: 4, percent: 40 },
      { status: "agent_review", count: 1, percent: 10 },
      { status: "in_progress", count: 2, percent: 20 },
      { status: "todo", count: 3, percent: 30 },
    ]);
    expect(barSegments(progress({ todo: 1, done: 3 }))).toEqual([
      { status: "done", count: 3, percent: 75 },
      { status: "todo", count: 1, percent: 25 },
    ]);
  });

  it("is empty with no tickets", () => {
    expect(barSegments(progress())).toEqual([]);
  });
});

describe("progressText", () => {
  it("reads done over total, or 0 tickets", () => {
    expect(progressText(progress({ todo: 4, done: 3 }))).toBe("3 / 7 done");
    expect(progressText(progress({ todo: 1 }))).toBe("0 / 1 done");
    expect(progressText(progress())).toBe("0 tickets");
  });
});

describe("epicLink", () => {
  it("names only the project and the epic", () => {
    expect(epicLink("/kanban", "ACP", "M1:Graph")).toBe("/kanban?project=ACP&epic=M1%3AGraph");
    expect(epicLink("/", "ACP", "none")).toBe("/?project=ACP&epic=none");
    expect(epicLink("/table", "ACP", "a b&c")).toBe("/table?project=ACP&epic=a+b%26c");
  });
});

describe("nameError", () => {
  const epics = [epic("Graph"), epic("Store")];

  it("asks for a name when it is empty or blank", () => {
    expect(nameError("", epics)).toBe("Enter a name");
    expect(nameError("   ", epics)).toBe("Enter a name");
  });

  it("reserves none in any case", () => {
    expect(nameError(" None ", epics)).toBe('"none" is reserved for tickets without an epic.');
  });

  it("refuses a name the project has, trimmed and ignoring case, quoting it as stored", () => {
    expect(nameError("  gRaPh ", epics)).toBe('This project already has an epic called "Graph".');
    expect(nameError("Graphs", epics)).toBeNull();
  });

  it("lets an epic keep its own name in other capitals, but not take another's", () => {
    expect(nameError("GRAPH", epics, "e-Graph")).toBeNull();
    expect(nameError("store", epics, "e-Graph")).toBe('This project already has an epic called "Store".');
  });
});

describe("deleteMessage", () => {
  it("says what happens to the tickets", () => {
    expect(deleteMessage(4, 0)).toBe("Its 4 tickets stay as they are and move to No epic. This can't be undone.");
    expect(deleteMessage(1, 0)).toBe("Its 1 ticket stays as it is and moves to No epic. This can't be undone.");
    expect(deleteMessage(0, 0)).toBe("It has no tickets.");
  });

  it("says the epic's documents are deleted for good, with or without tickets", () => {
    expect(deleteMessage(4, 2)).toBe(
      "Its 4 tickets stay as they are and move to No epic. Its 2 documents are deleted for good. This can't be undone.",
    );
    expect(deleteMessage(0, 1)).toBe("It has no tickets. Its 1 document is deleted for good. This can't be undone.");
    expect(deleteMessage(0, 3)).toBe("It has no tickets. Its 3 documents are deleted for good. This can't be undone.");
  });
});

describe("serverMessage", () => {
  it("takes the API's error text as worded", () => {
    expect(serverMessage(new Error('API error 400: {"error":"Enter a name"}'), "fallback")).toBe("Enter a name");
    expect(serverMessage(new Error("API error 500: boom"), "fallback")).toBe("boom");
  });

  it("falls back when there is nothing to show", () => {
    expect(serverMessage(new TypeError("Failed to fetch"), "fallback")).toBe("fallback");
    expect(serverMessage(new Error("API error 500: "), "fallback")).toBe("fallback");
    expect(serverMessage(new Error('API error 500: {"x":1}'), "fallback")).toBe("fallback");
  });
});
