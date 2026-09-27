// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { ActivityPage, ProjectActivityEntry } from "../api/client";
import { FEED_FRESH_CLASS, FEED_PAGE_SIZE } from "../lib/activityFeed";

// The Activity page's feed, with the API mocked: day headings, entries as
// the editor's Activity list draws them, paging back with Show older, and
// live refreshes that keep older pages and make only new entries glow.

const mockApi = vi.hoisted(() => ({ projects: { activity: vi.fn() } }));
vi.mock("../api/client", () => ({ api: mockApi }));
const liveRefresh = vi.hoisted(() => ({ listener: null as null | (() => void) }));
vi.mock("../hooks/useLiveRefresh", () => ({
  useLiveRefresh: (listener: () => void) => {
    liveRefresh.listener = listener;
  },
}));

import ActivityFeed from "./ActivityFeed";

const NOW = new Date(2026, 8, 27, 18, 0);
const at = (day: number, h: number, m: number) => new Date(2026, 8, day, h, m).toISOString();

const entry = (id: string, createdAt: string, extra: Partial<ProjectActivityEntry> = {}): ProjectActivityEntry => ({
  id,
  ticketId: `t-${id}`,
  fromStatus: "in_progress",
  toStatus: "agent_review",
  note: "",
  createdAt,
  ticketKey: `ACP-${id}`,
  ticketTitle: `Ticket ${id}`,
  ...extra,
});

// The feed the fake API pages through, newest first.
let feed: ProjectActivityEntry[] = [];
function serveFeed() {
  mockApi.projects.activity.mockImplementation(
    async (_project: string, params: { before?: string; limit?: number } = {}): Promise<ActivityPage> => {
      const start = params.before ? feed.findIndex((e) => e.id === params.before) + 1 : 0;
      const limit = params.limit ?? 50;
      const entries = feed.slice(start, start + limit);
      const hasMore = start + limit < feed.length;
      return { entries, hasMore, ...(hasMore ? { nextBefore: entries[entries.length - 1].id } : {}) };
    },
  );
}

async function renderFeed(props: { epics?: string[]; onOpen?: (e: ProjectActivityEntry) => void } = {}) {
  await act(async () => {
    render(<ActivityFeed project="ACP" epics={props.epics ?? []} onOpen={props.onOpen ?? (() => {})} now={NOW} />);
  });
}

const keys = () =>
  screen.queryAllByTestId("feed-entry").map((b) => b.querySelector(".font-mono")!.textContent);
const entryFor = (key: string) =>
  screen.getAllByTestId("feed-entry").find((b) => b.querySelector(".font-mono")!.textContent === key)!;

beforeEach(() => {
  liveRefresh.listener = null;
  feed = [];
  serveFeed();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.resetAllMocks();
});

describe("the activity feed", () => {
  it("shows entries newest first under their day, as the editor's Activity list draws them", async () => {
    feed = [
      entry("150", at(27, 14, 2), {
        fromStatus: "agent_review",
        toStatus: "done",
        note: "Approved by Bilal. Landed in 39a07fd",
        epic: { id: "e1", name: "Overview" },
      }),
      entry("137", at(27, 13, 40), { fromStatus: "in_progress", toStatus: "agent_review", epic: { id: "e2", name: "Epics" } }),
      entry("181", at(26, 17, 48), { fromStatus: "", toStatus: "todo" }),
    ];
    await renderFeed();

    expect(mockApi.projects.activity).toHaveBeenCalledWith("ACP", { epics: [], before: undefined, limit: FEED_PAGE_SIZE });
    const days = screen.getAllByRole("region");
    expect(days.map((d) => d.getAttribute("aria-label"))).toEqual(["Today", "Yesterday"]);
    expect(within(days[0]).getAllByTestId("feed-entry")).toHaveLength(2);
    expect(keys()).toEqual(["ACP-150", "ACP-137", "ACP-181"]);

    const landed = entryFor("ACP-150");
    expect(landed.textContent).toContain("14:02");
    expect(landed.textContent).toContain("Ticket 150");
    expect(landed.textContent).toContain("Agent Review");
    expect(landed.textContent).toContain("Done");
    expect(landed.textContent).toContain("Overview");
    const note = within(landed).getByTestId("activity-note");
    expect(note.textContent).toBe("Approved by Bilal. Landed in 39a07fd");
    expect(within(note).getByText("39a07fd").className).toContain("font-mono");

    const created = entryFor("ACP-181");
    expect(created.textContent).toContain("Created in");
    expect(created.textContent).toContain("Todo");
    expect(created.textContent).toContain("No epic");
    expect(within(created).queryByTestId("activity-note")).toBeNull();
  });

  it("asks for the epics it was given", async () => {
    await renderFeed({ epics: ["Overview", "none"] });
    expect(mockApi.projects.activity).toHaveBeenCalledWith("ACP", {
      epics: ["Overview", "none"],
      before: undefined,
      limit: FEED_PAGE_SIZE,
    });
    expect(screen.getByText("No activity in these epics")).toBeTruthy();
  });

  it("says so when the project has no activity, or when it can't be read", async () => {
    await renderFeed();
    expect(screen.getByText("No activity yet")).toBeTruthy();
    cleanup();

    mockApi.projects.activity.mockRejectedValue(new Error("down"));
    await renderFeed();
    expect(screen.getByRole("alert").textContent).toBe("Couldn't load the activity");
  });

  it("opens an entry's ticket when it is clicked", async () => {
    feed = [entry("7", at(27, 9, 0))];
    const onOpen = vi.fn();
    await renderFeed({ onOpen });
    fireEvent.click(entryFor("ACP-7"));
    expect(onOpen).toHaveBeenCalledWith(feed[0]);
  });

  it("loads older entries a page at a time", async () => {
    feed = Array.from({ length: FEED_PAGE_SIZE + 3 }, (_, i) => entry(String(1000 - i), at(27, 12, 0)));
    await renderFeed();
    expect(keys()).toHaveLength(FEED_PAGE_SIZE);

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Show older" }));
    });
    expect(mockApi.projects.activity).toHaveBeenLastCalledWith("ACP", {
      epics: [],
      before: String(1000 - FEED_PAGE_SIZE + 1),
      limit: FEED_PAGE_SIZE,
    });
    expect(keys()).toEqual(feed.map((e) => e.ticketKey));
    expect(screen.queryByRole("button", { name: "Show older" })).toBeNull();
  });

  it("says so when an older page can't be read, and tries again from the same place", async () => {
    feed = Array.from({ length: FEED_PAGE_SIZE + 3 }, (_, i) => entry(String(1000 - i), at(27, 12, 0)));
    await renderFeed();
    mockApi.projects.activity.mockRejectedValueOnce(new Error("down"));
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Show older" }));
    });
    expect(screen.getByRole("alert").textContent).toBe("Couldn't load older activity");
    expect(keys()).toHaveLength(FEED_PAGE_SIZE);

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    });
    expect(mockApi.projects.activity).toHaveBeenLastCalledWith("ACP", {
      epics: [],
      before: String(1000 - FEED_PAGE_SIZE + 1),
      limit: FEED_PAGE_SIZE,
    });
    expect(screen.queryByRole("alert")).toBeNull();
    expect(keys()).toEqual(feed.map((e) => e.ticketKey));
  });

  it("brings in new entries on a live refresh, keeping older pages, and only the new ones glow", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    feed = Array.from({ length: FEED_PAGE_SIZE + 2 }, (_, i) => entry(String(1000 - i), at(27, 12, 0)));
    await renderFeed();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Show older" }));
    });
    expect(keys()).toHaveLength(FEED_PAGE_SIZE + 2);

    feed = [entry("2000", at(27, 17, 0), { note: "just now" }), ...feed];
    await act(async () => {
      liveRefresh.listener!();
    });
    expect(keys()).toHaveLength(FEED_PAGE_SIZE + 3);
    expect(keys()[0]).toBe("ACP-2000");
    const glowing = screen.getAllByTestId("feed-entry").filter((b) => b.classList.contains(FEED_FRESH_CLASS));
    expect(glowing.map((b) => b.querySelector(".font-mono")!.textContent)).toEqual(["ACP-2000"]);

    // The glow lasts as long as a changed card's, then goes.
    await act(async () => {
      vi.advanceTimersByTime(2100);
    });
    expect(document.querySelector(`.${FEED_FRESH_CLASS}`)).toBeNull();
  });

  it("makes nothing glow on the first load", async () => {
    feed = [entry("1", at(27, 9, 0)), entry("2", at(27, 8, 0))];
    await renderFeed();
    expect(document.querySelector(`.${FEED_FRESH_CLASS}`)).toBeNull();
  });
});
