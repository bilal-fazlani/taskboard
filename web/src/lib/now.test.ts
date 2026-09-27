import { describe, expect, it } from "vitest";
import { editorLink, landedAgo, reviewNote, runningFor, shortSha, withinLandedWindow } from "./now";

const NOW = Date.parse("2026-09-27T12:00:00Z");
const ago = (ms: number) => new Date(NOW - ms).toISOString();
const MIN = 60_000;
const HOUR = 60 * MIN;

describe("runningFor", () => {
  it("counts minutes, then hours and minutes, then days and hours", () => {
    expect(runningFor(ago(30_000), NOW)).toBe("0m");
    expect(runningFor(ago(18 * MIN), NOW)).toBe("18m");
    expect(runningFor(ago(72 * MIN), NOW)).toBe("1h 12m");
    expect(runningFor(ago(3 * HOUR + 5 * MIN), NOW)).toBe("3h 05m");
    expect(runningFor(ago(52 * HOUR + 10 * MIN), NOW)).toBe("2d 4h");
  });

  it("never goes below zero and shows nothing for an unreadable time", () => {
    expect(runningFor(ago(-5 * MIN), NOW)).toBe("0m");
    expect(runningFor("", NOW)).toBe("");
  });
});

describe("landedAgo", () => {
  it("says just now, then minutes, then hours", () => {
    expect(landedAgo(ago(20_000), NOW)).toBe("just now");
    expect(landedAgo(ago(14 * MIN), NOW)).toBe("14 min ago");
    expect(landedAgo(ago(21 * HOUR + 40 * MIN), NOW)).toBe("21 h ago");
    expect(landedAgo("nope", NOW)).toBe("");
  });
});

describe("withinLandedWindow", () => {
  const row = (key: string, doneAt: string) => ({ id: key, key, title: key, projectPrefix: "ACP", doneAt, commits: [] });

  it("keeps what landed in the last 24 hours and drops what has turned older", () => {
    const rows = [row("A", ago(10 * MIN)), row("B", ago(24 * HOUR)), row("C", ago(24 * HOUR + 1)), row("D", "")];
    expect(withinLandedWindow(rows, NOW).map((r) => r.key)).toEqual(["A", "B", "D"]);
    // Half an hour later, B has turned a day old too.
    expect(withinLandedWindow(rows, NOW + 30 * MIN).map((r) => r.key)).toEqual(["A", "D"]);
  });
});

describe("reviewNote", () => {
  it("says a ticket in progress is back from its latest review, and nothing before its first", () => {
    expect(reviewNote({ status: "in_progress", reviewRounds: 0 })).toBeNull();
    expect(reviewNote({ status: "in_progress", reviewRounds: 2 })).toEqual({ text: "back from review 2", tone: "round" });
  });

  it("tells a running review from an approved one and one asking for changes", () => {
    expect(reviewNote({ status: "agent_review", reviewRounds: 1, review: "running" })).toEqual({
      text: "review 1 running",
      tone: "round",
    });
    expect(reviewNote({ status: "agent_review", reviewRounds: 2, review: "approved" })).toEqual({
      text: "approved, waiting on you",
      tone: "approved",
    });
    expect(reviewNote({ status: "agent_review", reviewRounds: 2, review: "changes" })).toEqual({
      text: "review 2: changes asked",
      tone: "round",
    });
    // A ticket that reached review before the history began counts as round 1.
    expect(reviewNote({ status: "agent_review", reviewRounds: 0, review: "running" })?.text).toBe("review 1 running");
  });
});

describe("editorLink", () => {
  it("is the ticket's canonical link with its own project named", () => {
    expect(editorLink({ id: "01X", key: "LDR-4", projectPrefix: "LDR" })).toBe("/?project=LDR&ticket=LDR-4");
    expect(editorLink({ id: "01X", key: "4", projectPrefix: "" })).toBe("/?ticket=01X");
  });
});

it("shortSha keeps git's short length", () => {
  expect(shortSha("39a07fd1234abcd")).toBe("39a07fd");
  expect(shortSha("abc1234")).toBe("abc1234");
});
