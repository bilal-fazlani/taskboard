import { describe, expect, it, vi } from "vitest";
import type { ActivityPage, ProjectActivityEntry } from "../api/client";
import {
  FEED_MAX_LIMIT,
  FEED_PAGE_SIZE,
  entryTicket,
  feedClock,
  feedDay,
  groupByDay,
  noteParts,
  readNewest,
  sinceBaseline,
} from "./activityFeed";

const local = (y: number, mo: number, d: number, h = 0, mi = 0) => new Date(y, mo - 1, d, h, mi);

const entry = (id: string, createdAt: string, extra: Partial<ProjectActivityEntry> = {}): ProjectActivityEntry => ({
  id,
  ticketId: `t-${id}`,
  fromStatus: "todo",
  toStatus: "in_progress",
  note: "",
  createdAt,
  ticketKey: "ACP-1",
  ticketTitle: "A ticket",
  ...extra,
});

describe("feedDay", () => {
  const now = local(2026, 9, 27, 14, 30);

  it("says Today and Yesterday, in local time", () => {
    expect(feedDay(local(2026, 9, 27, 0, 1).toISOString(), now)).toBe("Today");
    expect(feedDay(local(2026, 9, 26, 23, 59).toISOString(), now)).toBe("Yesterday");
  });

  it("gives the weekday and date for older days, with the year when it is not this year's", () => {
    expect(feedDay(local(2026, 9, 23, 12).toISOString(), now)).toBe("Wed 23 Sep");
    expect(feedDay(local(2025, 12, 31, 12).toISOString(), now)).toBe("Wed 31 Dec 2025");
  });

  it("finds yesterday across a month boundary", () => {
    expect(feedDay(local(2026, 9, 30, 22).toISOString(), local(2026, 10, 1, 9))).toBe("Yesterday");
  });

  it("gives nothing for a time it cannot read", () => {
    expect(feedDay("not a time", now)).toBe("");
    expect(feedClock("not a time")).toBe("");
  });
});

describe("feedClock", () => {
  it("gives the local time of day", () => {
    expect(feedClock(local(2026, 9, 27, 9, 5).toISOString())).toBe("09:05");
  });
});

describe("groupByDay", () => {
  it("puts each run of entries on the same day under one heading, keeping their order", () => {
    const now = local(2026, 9, 27, 18);
    const days = groupByDay(
      [
        entry("c", local(2026, 9, 27, 14, 2).toISOString()),
        entry("b", local(2026, 9, 27, 9).toISOString()),
        entry("a", local(2026, 9, 26, 17, 48).toISOString()),
        entry("z", local(2026, 9, 20, 8).toISOString()),
      ],
      now,
    );
    expect(days.map((d) => [d.label, d.entries.map((e) => e.id)])).toEqual([
      ["Today", ["c", "b"]],
      ["Yesterday", ["a"]],
      ["Sun 20 Sep", ["z"]],
    ]);
  });
});

describe("noteParts", () => {
  it("marks the commit shas in a note", () => {
    expect(noteParts("Approved. Landed in 39a07fd, 2bbef36")).toEqual([
      { text: "Approved. Landed in ", sha: false },
      { text: "39a07fd", sha: true },
      { text: ", ", sha: false },
      { text: "2bbef36", sha: true },
    ]);
  });

  it("leaves plain numbers, hex-looking words and short runs alone", () => {
    expect(noteParts("1234567 decaded abc123 ACP-155")).toEqual([{ text: "1234567 decaded abc123 ACP-155", sha: false }]);
    expect(noteParts("")).toEqual([]);
  });
});

describe("readNewest", () => {
  const all = Array.from({ length: 450 }, (_, i) => entry(`e${i}`, new Date(Date.UTC(2026, 8, 27) - i * 1000).toISOString()));
  // A fake API over `all`, newest first, paging like the real one.
  const fakeFetch = () =>
    vi.fn(async (before: string | undefined, limit: number): Promise<ActivityPage> => {
      const start = before ? all.findIndex((e) => e.id === before) + 1 : 0;
      const entries = all.slice(start, start + limit);
      const hasMore = start + limit < all.length;
      return { entries, hasMore, nextBefore: hasMore ? entries[entries.length - 1].id : undefined };
    });

  it("reads a first page's worth when nothing is shown yet", async () => {
    const fetchPage = fakeFetch();
    const page = await readNewest(fetchPage, 0);
    expect(fetchPage).toHaveBeenCalledTimes(1);
    expect(fetchPage).toHaveBeenCalledWith(undefined, FEED_PAGE_SIZE);
    expect(page.entries.map((e) => e.id)).toEqual(all.slice(0, FEED_PAGE_SIZE).map((e) => e.id));
    expect(page.hasMore).toBe(true);
    expect(page.nextBefore).toBe(`e${FEED_PAGE_SIZE - 1}`);
  });

  it("reads as many as are shown, in pages of at most the API's limit", async () => {
    const fetchPage = fakeFetch();
    const page = await readNewest(fetchPage, 300);
    expect(fetchPage.mock.calls).toEqual([
      [undefined, FEED_MAX_LIMIT],
      ["e199", 100],
    ]);
    expect(page.entries).toHaveLength(300);
    expect(new Set(page.entries.map((e) => e.id)).size).toBe(300);
    expect(page.nextBefore).toBe("e299");
  });

  it("reads on past new entries down to the oldest one shown", async () => {
    const fetchPage = fakeFetch();
    // 60 were shown, down to e59; 10 new ones have since pushed it to the 70th place.
    const page = await readNewest(fetchPage, 60, all[69]);
    expect(fetchPage.mock.calls).toEqual([
      [undefined, 60],
      ["e59", FEED_PAGE_SIZE],
    ]);
    expect(page.entries.map((e) => e.id)).toContain("e69");
    expect(page.nextBefore).toBe("e109");
  });

  it("stops at the first entry older than the oldest one shown once that one is gone", async () => {
    const fetchPage = fakeFetch();
    const gone = { id: "deleted", createdAt: all[55].createdAt };
    const page = await readNewest(fetchPage, 50, gone);
    expect(fetchPage).toHaveBeenCalledTimes(2);
    expect(page.entries).toHaveLength(100);
  });

  it("stops at the end of the feed", async () => {
    const page = await readNewest(fakeFetch(), 1000);
    expect(page.entries).toHaveLength(450);
    expect(page.hasMore).toBe(false);
    expect(page.nextBefore).toBeUndefined();
  });
});

describe("sinceBaseline", () => {
  const e = (id: string, minute: number) => entry(id, `2026-09-27T10:${String(minute).padStart(2, "0")}:00Z`);
  const stamps = (list: { id: string }[]) => list.map((x) => x.id);

  it("counts the entries above the first load's newest, and that one", () => {
    const entries = [e("new2", 9), e("new1", 8), e("base", 5), e("old1", 4), e("old2", 3)];
    expect(stamps(sinceBaseline(entries, { id: "base", createdAt: entries[2].createdAt }))).toEqual([
      "new2",
      "new1",
      "base",
    ]);
  });

  it("stamps each entry with when it happened, which never changes", () => {
    expect(sinceBaseline([e("base", 5)], { id: "base", createdAt: e("base", 5).createdAt })).toEqual([
      { id: "base", updatedAt: "2026-09-27T10:05:00Z" },
    ]);
  });

  it("counts only what is newer than the first load's newest once that one is gone", () => {
    const entries = [e("new1", 8), e("old1", 4)];
    expect(stamps(sinceBaseline(entries, { id: "base", createdAt: e("base", 5).createdAt }))).toEqual(["new1"]);
  });

  it("counts every entry when the feed started empty", () => {
    expect(stamps(sinceBaseline([e("a", 2), e("b", 1)], null))).toEqual(["a", "b"]);
  });
});

describe("entryTicket", () => {
  it("names the entry's ticket by id, prefix and number", () => {
    expect(entryTicket({ ticketId: "01X", ticketKey: "MY-APP-12" })).toEqual({
      id: "01X",
      number: 12,
      projectPrefix: "MY-APP",
    });
  });
});
