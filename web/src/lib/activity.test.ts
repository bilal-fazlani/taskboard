import { describe, expect, it } from "vitest";
import type { StatusChange } from "../api/client";
import { activityEntries, activityTime } from "./activity";

const local = (y: number, mo: number, d: number, h = 0, mi = 0) => new Date(y, mo - 1, d, h, mi);

describe("activityTime", () => {
  const now = local(2026, 9, 23, 21, 30);

  it("says today and the time, in local time, for today", () => {
    expect(activityTime(local(2026, 9, 23, 20, 12).toISOString(), now)).toBe("today 20:12");
    expect(activityTime(local(2026, 9, 23, 0, 5).toISOString(), now)).toBe("today 00:05");
  });

  it("gives just the date for anything older", () => {
    expect(activityTime(local(2026, 9, 22, 23, 59).toISOString(), now)).toBe("22 Sep");
    expect(activityTime(local(2026, 1, 3, 8).toISOString(), now)).toBe("3 Jan");
  });

  it("adds the year when it is not this year's", () => {
    expect(activityTime(local(2025, 12, 31, 12).toISOString(), now)).toBe("31 Dec 2025");
  });

  it("gives nothing for a time it cannot read", () => {
    expect(activityTime("not a time", now)).toBe("");
  });
});

describe("activityEntries", () => {
  const change = (id: string, createdAt: string): StatusChange => ({
    id,
    ticketId: "t1",
    fromStatus: "todo",
    toStatus: "done",
    note: "",
    createdAt,
  });

  it("makes a status entry per change, newest first, keeping the given order for ties", () => {
    const entries = activityEntries([
      change("a", "2026-09-20T10:00:00Z"),
      change("b", "2026-09-23T10:00:00Z"),
      change("c", "2026-09-20T10:00:00Z"),
    ]);
    expect(entries.map((e) => [e.kind, e.id])).toEqual([
      ["status", "b"],
      ["status", "a"],
      ["status", "c"],
    ]);
    expect(entries[0].change.id).toBe("b");
  });
});
