import { describe, expect, it } from "vitest";
import type { Entry } from "../api/client";
import {
  countsPhrase,
  entryExcerpt,
  findingsSummary,
  groupTicketEntries,
  isOpenNote,
  openChallenges,
  reviewRound,
  shortResumeCommand,
  splitRejected,
} from "./entries";

function entry(id: string, type: string, minute: number, extra: Partial<Entry> = {}): Entry {
  return { id, type, text: `${id} text`, createdAt: `2026-09-27T10:${String(minute).padStart(2, "0")}:00Z`, ...extra };
}

describe("groupTicketEntries", () => {
  it("puts each current entry under its type, newest first, with the latest hand-off apart", () => {
    const g = groupTicketEntries([
      entry("h2", "hand_off", 9),
      entry("n1", "note", 8),
      entry("d2", "decision", 7),
      entry("h1", "hand_off", 6),
      entry("r1", "review", 5),
      entry("l1", "learning", 4),
      entry("p1", "proof", 3),
      entry("d1", "decision", 2),
      entry("n0", "note", 1, { handledBy: "a1", handledAt: "2026-09-27T11:00:00Z" }),
      entry("x1", "retro", 0),
    ]);
    expect(g.openNotes.map((t) => t.entry.id)).toEqual(["n1"]);
    expect(g.handledNotes.map((t) => t.entry.id)).toEqual(["n0"]);
    expect(g.handOff?.entry.id).toBe("h2");
    expect(g.earlierHandOffs.map((t) => t.entry.id)).toEqual(["h1"]);
    expect(g.decisions.map((t) => t.entry.id)).toEqual(["d2", "d1"]);
    expect(g.reviews.map((t) => t.entry.id)).toEqual(["r1"]);
    expect(g.learnings.map((t) => t.entry.id)).toEqual(["l1"]);
    expect(g.proofs.map((t) => t.entry.id)).toEqual(["p1"]);
    expect(g.other.map((t) => t.entry.id)).toEqual(["x1"]);
  });

  it("tucks replaced entries under the entry that replaced them, newest first, and never lists them apart", () => {
    const g = groupTicketEntries([
      entry("d3", "decision", 9, { replaces: "d2" }),
      entry("d2", "decision", 5, { replaces: "d1", replacedBy: "d3" }),
      entry("d1", "decision", 1, { replacedBy: "d2" }),
      entry("d0", "decision", 0, { replacedBy: "gone" }),
    ]);
    expect(g.decisions.map((t) => [t.entry.id, t.replaced.map((e) => e.id)])).toEqual([["d3", ["d2", "d1"]]]);
  });
});

describe("notes and challenges", () => {
  it("counts a note open until it is handled or replaced", () => {
    expect(isOpenNote(entry("n", "note", 0))).toBe(true);
    expect(isOpenNote(entry("n", "note", 0, { handledAt: "2026-09-27T11:00:00Z" }))).toBe(false);
    expect(isOpenNote(entry("n", "note", 0, { replacedBy: "n2" }))).toBe(false);
    expect(isOpenNote(entry("d", "decision", 0))).toBe(false);
  });

  it("finds the entries open notes challenge", () => {
    const got = openChallenges([
      entry("n1", "note", 3, { about: "d1" }),
      entry("n2", "note", 2, { about: "d1", handledAt: "2026-09-27T11:00:00Z" }),
      entry("n3", "note", 1),
    ]);
    expect([...got.keys()]).toEqual(["d1"]);
    expect(got.get("d1")?.map((e) => e.id)).toEqual(["n1"]);
  });
});

describe("reviews", () => {
  it("numbers a review from its report's name, or else its place oldest first", () => {
    const r1 = entry("r1", "review", 1);
    const r2 = entry("r2", "review", 2);
    const named = entry("r9", "review", 3, { reportDocument: "Review 4" });
    expect(reviewRound(r1, [named, r2, r1])).toBe(1);
    expect(reviewRound(r2, [named, r2, r1])).toBe(2);
    expect(reviewRound(named, [named, r2, r1])).toBe(4);
  });

  it("counts findings by severity, nits only when there are some", () => {
    expect(findingsSummary({ blocker: 0, major: 1, minor: 2, nit: 0 })).toBe("0 blocker · 1 major · 2 minor");
    expect(findingsSummary({ blocker: 0, major: 0, minor: 0, nit: 3 })).toBe("0 blocker · 0 major · 0 minor · 3 nit");
    expect(findingsSummary(undefined)).toBe("");
  });
});

describe("text helpers", () => {
  it("sets a decision's Rejected part apart", () => {
    expect(splitRejected("Poll every 250 ms. Rejected: update_hook.")).toEqual({
      chosen: "Poll every 250 ms.",
      rejected: "Rejected: update_hook.",
    });
    expect(splitRejected("Just this.")).toEqual({ chosen: "Just this.", rejected: "" });
  });

  it("shortens a UUID in a resume command to eight characters", () => {
    expect(shortResumeCommand("claude --resume 3da2c294-37eb-4757-be78-0513c53efa33")).toBe("claude --resume 3da2c294");
    expect(shortResumeCommand("codex resume abc")).toBe("codex resume abc");
  });

  it("cuts an excerpt at a word limit with an ellipsis", () => {
    expect(entryExcerpt("short")).toBe("short");
    expect(entryExcerpt("a".repeat(70), 60)).toBe(`${"a".repeat(60)}…`);
    expect(entryExcerpt("Poll every 250 ms with a deadline. Rejected: update_hook.", 40)).toBe(
      "Poll every 250 ms with a deadline.…",
    );
  });

  it("words an epic's or project's counts, open notes always", () => {
    expect(countsPhrase({ decisions: 15, learnings: 0, openNotes: 0 })).toBe("15 decisions, 0 open notes");
    expect(countsPhrase({ decisions: 1, learnings: 6, openNotes: 1 })).toBe("1 decision, 6 learnings, 1 open note");
  });
});
