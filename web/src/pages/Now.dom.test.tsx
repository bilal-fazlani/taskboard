// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { LandedTicket, Now, NowTicket, Project } from "../api/client";

// The Now page inside the app's routes and layout, with the API mocked and
// the live-refresh stream replaced by a function the tests call.

const mockApi = vi.hoisted(() => ({
  now: { get: vi.fn() },
  projects: { list: vi.fn() },
  // The sidebar's build footer (Layout); it never answers here.
  version: { get: () => new Promise(() => {}) },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

const live = vi.hoisted(() => ({ refresh: [] as (() => void)[] }));
vi.mock("../hooks/useLiveRefresh", () => ({
  useLiveRefresh: (onChange: () => void) => {
    live.refresh.push(onChange);
  },
}));

import { AppRoutes } from "../App";

const NOW = Date.parse("2026-09-27T12:00:00Z");
const ago = (minutes: number) => new Date(NOW - minutes * 60_000).toISOString();

const project = (prefix: string, name: string, status = "active"): Project => ({
  id: `p-${prefix}`,
  name,
  prefix,
  description: "",
  icon: "",
  color: "",
  status,
  createdAt: "",
  updatedAt: "",
});

function active(key: string, title: string, over: Partial<NowTicket> = {}): NowTicket {
  return {
    id: `t-${key}`,
    key,
    title,
    status: "in_progress",
    projectPrefix: key.split("-")[0],
    subtasksDone: 0,
    subtasksTotal: 0,
    reviewRounds: 0,
    since: ago(10),
    ...over,
  };
}

function landed(key: string, title: string, minutesAgo: number, shas: string[]): LandedTicket {
  return {
    id: `t-${key}`,
    key,
    title,
    projectPrefix: key.split("-")[0],
    doneAt: ago(minutesAgo),
    commits: shas.map((sha) => ({ sha, repo: "acme/app" })),
  };
}

const BOARD: Now = {
  inProgress: [
    active("ACP-180", "Epics row opens the edit popup", { subtasksDone: 4, subtasksTotal: 6, reviewRounds: 1, since: ago(160) }),
    active("ACP-155", "Project activity feed", { subtasksDone: 2, subtasksTotal: 4, since: ago(72) }),
    active("IAGML-12", "Batch scoring endpoint", { subtasksDone: 1, subtasksTotal: 5, since: ago(18) }),
  ],
  inReview: [
    active("ACP-158", "A Now page", { status: "agent_review", subtasksDone: 4, subtasksTotal: 4, reviewRounds: 1, review: "approved", since: ago(185) }),
    active("ACP-137", "New epic button on archived projects", { status: "agent_review", subtasksDone: 3, subtasksTotal: 3, reviewRounds: 1, review: "running", since: ago(34) }),
  ],
  landed: [
    landed("ACP-150", "Delivery fields on tickets", 14, ["39a07fd"]),
    landed("ACP-80", "New-ticket form's project dropdown", 21 * 60, ["8640bc7aaaa", "2bbef36"]),
    landed("LDR-4", "Leaderboard seed script", 23 * 60, []),
  ],
};

const PROJECTS = [project("LDR", "Leaderboard"), project("ACP", "Control plane"), project("IAGML", "Scoring"), project("OLD", "Old", "archived")];

async function settle() {
  for (let i = 0; i < 4; i++) await act(async () => {});
}

async function mount(url = "/now") {
  window.history.replaceState(null, "", url);
  render(
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>,
  );
  await settle();
}

const group = (name: string) => screen.getByRole("region", { name: new RegExp(name) });
const cards = (name: string) => within(group(name)).queryAllByTestId("now-card");
const cardOf = (title: string) => screen.getByText(title).closest("a") as HTMLAnchorElement;
const params = () => new URLSearchParams(window.location.search);

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(NOW);
  live.refresh = [];
  mockApi.now.get.mockReset().mockResolvedValue(BOARD);
  mockApi.projects.list.mockReset().mockResolvedValue(PROJECTS);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("Now page", () => {
  // It is not the landing page: App.test.tsx still serves Dependencies at /.
  it("sits at the top of the sidebar, above Views", async () => {
    await mount();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Now");
    const nav = screen.getByRole("navigation");
    const link = within(nav).getByRole("link", { name: "Now" });
    expect(link.getAttribute("href")).toBe("/now");
    expect(link.getAttribute("aria-current")).toBe("page");
    const views = within(nav).getByText("Views");
    expect(link.compareDocumentPosition(views) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(within(nav).getAllByRole("link")[0]).toBe(link);
  });

  it("shows the tickets in progress with subtask progress, review round and time running", async () => {
    await mount();
    const inProgress = group("In Progress");
    expect(within(inProgress).getByText("3")).toBeTruthy();
    expect(cards("In Progress").map((c) => within(c).getAllByText(/-\d+$/)[0].textContent)).toEqual([
      "ACP-180",
      "ACP-155",
      "IAGML-12",
    ]);
    const bounced = cardOf("Epics row opens the edit popup");
    expect(within(bounced).getByTestId("subtasks").textContent).toBe("4/6");
    expect(within(bounced).getByRole("progressbar").getAttribute("aria-valuenow")).toBe("4");
    expect(within(bounced).getByTestId("review-note").textContent).toBe("back from review 1");
    expect(within(bounced).getByTestId("running-for").textContent).toBe("2h 40m");
    const fresh = cardOf("Project activity feed");
    expect(within(fresh).getByTestId("subtasks").textContent).toBe("2/4");
    expect(within(fresh).getByTestId("running-for").textContent).toBe("1h 12m");
    expect(within(fresh).queryByTestId("review-note")).toBeNull();
    expect(within(cardOf("Batch scoring endpoint")).getByTestId("running-for").textContent).toBe("18m");
  });

  it("tells a running review from one approved and waiting on the person", async () => {
    await mount();
    expect(cards("Agent Review")).toHaveLength(2);
    const approved = cardOf("A Now page");
    expect(within(approved).getByTestId("review-note").textContent).toBe("approved, waiting on you");
    expect(within(approved).getByTestId("review-note").className).toContain("text-green-400");
    expect(within(approved).getByTestId("running-for").textContent).toBe("3h 05m");
    const running = cardOf("New epic button on archived projects");
    expect(within(running).getByTestId("review-note").textContent).toBe("review 1 running");
    expect(within(running).getByTestId("subtasks").textContent).toBe("3/3");
  });

  it("lists what landed in the last day with its commits", async () => {
    await mount();
    const rows = within(group("Landed")).getAllByTestId("landed-row");
    expect(rows.map((r) => within(r).getAllByText(/-\d+$/)[0].textContent)).toEqual(["ACP-150", "ACP-80", "LDR-4"]);
    expect(within(rows[0]).getAllByTestId("sha").map((s) => s.textContent)).toEqual(["39a07fd"]);
    expect(within(rows[0]).getByTestId("landed-ago").textContent).toBe("14 min ago");
    const two = within(rows[1]).getAllByTestId("sha");
    expect(two.map((s) => s.textContent)).toEqual(["8640bc7", "2bbef36"]);
    expect(two[0].getAttribute("title")).toBe("acme/app@8640bc7aaaa");
    expect(within(rows[1]).getByTestId("landed-ago").textContent).toBe("21 h ago");
    expect(within(rows[2]).queryAllByTestId("sha")).toHaveLength(0);
  });

  it("drops what turns a day old while the board is idle, with no extra request", async () => {
    vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
    vi.setSystemTime(NOW);
    await mount();
    const landedKeys = () =>
      within(group("Landed"))
        .queryAllByTestId("landed-row")
        .map((r) => within(r).getAllByText(/-\d+$/)[0].textContent);
    expect(landedKeys()).toEqual(["ACP-150", "ACP-80", "LDR-4"]);
    const requests = mockApi.now.get.mock.calls.length;

    // An hour and a half on, LDR-4 landed 24.5 h ago and ACP-80 22.5 h ago.
    await act(async () => {
      vi.advanceTimersByTime(90 * 60_000);
    });
    expect(landedKeys()).toEqual(["ACP-150", "ACP-80"]);
    expect(within(group("Landed")).getAllByTestId("landed-ago")[1].textContent).toBe("22 h ago");

    // Two more hours: only ACP-150 is left.
    await act(async () => {
      vi.advanceTimersByTime(2 * 60 * 60_000);
    });
    expect(landedKeys()).toEqual(["ACP-150"]);
    expect(mockApi.now.get.mock.calls.length).toBe(requests);
  });

  it("says so when a group is empty", async () => {
    mockApi.now.get.mockResolvedValue({ inProgress: [], inReview: [], landed: [] });
    await mount();
    expect(within(group("In Progress")).getByText("Nothing in progress")).toBeTruthy();
    expect(within(group("Agent Review")).getByText("Nothing in review")).toBeTruthy();
    expect(within(group("Landed")).getByText("Nothing landed in the last 24 hours")).toBeTruthy();
  });

  it("links each ticket to its editor, over its own project", async () => {
    await mount();
    expect(cardOf("Batch scoring endpoint").getAttribute("href")).toBe("/?project=IAGML&ticket=IAGML-12");
    expect(cardOf("A Now page").getAttribute("href")).toBe("/?project=ACP&ticket=ACP-158");
    expect(cardOf("Leaderboard seed script").getAttribute("href")).toBe("/?project=LDR&ticket=LDR-4");
  });

  it("shows every project by default, with a chip on each card, and narrows to one from the dropdown", async () => {
    await mount();
    expect(mockApi.now.get).toHaveBeenLastCalledWith(undefined);
    const select = screen.getByLabelText("Project") as HTMLSelectElement;
    expect(select.value).toBe("");
    // Active projects by name; archived ones are left out.
    expect([...select.options].map((o) => o.textContent)).toEqual(["All projects", "Control plane", "Leaderboard", "Scoring"]);
    expect(within(cardOf("Batch scoring endpoint")).getByTestId("project-chip").textContent).toBe("IAGML");
    expect(screen.getAllByTestId("project-chip")).toHaveLength(8);

    mockApi.now.get.mockResolvedValue({ ...BOARD, inProgress: [BOARD.inProgress[2]], inReview: [], landed: [] });
    fireEvent.change(select, { target: { value: "IAGML" } });
    await settle();
    expect(params().get("project")).toBe("IAGML");
    expect(mockApi.now.get).toHaveBeenLastCalledWith("IAGML");
    expect(cards("In Progress")).toHaveLength(1);
    expect((screen.getByLabelText("Project") as HTMLSelectElement).value).toBe("IAGML");

    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "" } });
    await settle();
    expect(params().get("project")).toBeNull();
    expect(mockApi.now.get).toHaveBeenLastCalledWith(undefined);
  });

  it("reads the project from the URL", async () => {
    await mount("/now?project=ldr");
    expect(mockApi.now.get).toHaveBeenLastCalledWith("ldr");
    expect((screen.getByLabelText("Project") as HTMLSelectElement).value).toBe("LDR");
  });

  it("updates live as tickets move", async () => {
    await mount();
    expect(cards("In Progress")).toHaveLength(3);
    // ACP-155 goes to review, and ACP-137's review approves it.
    mockApi.now.get.mockResolvedValue({
      ...BOARD,
      inProgress: [BOARD.inProgress[0], BOARD.inProgress[2]],
      inReview: [
        BOARD.inReview[0],
        { ...BOARD.inReview[1], review: "approved" },
        { ...BOARD.inProgress[1], status: "agent_review", reviewRounds: 1, review: "running", since: ago(0) },
      ],
    });
    await act(async () => {
      for (const refresh of live.refresh) refresh();
    });
    await settle();
    expect(cards("In Progress")).toHaveLength(2);
    expect(within(group("Agent Review")).getByText("Project activity feed")).toBeTruthy();
    const moved = cardOf("Project activity feed");
    expect(within(moved).getByTestId("review-note").textContent).toBe("review 1 running");
    expect(within(moved).getByTestId("running-for").textContent).toBe("0m");
    expect(within(cardOf("New epic button on archived projects")).getByTestId("review-note").textContent).toBe(
      "approved, waiting on you",
    );
  });

  it("keeps what it shows when a refetch fails", async () => {
    await mount();
    mockApi.now.get.mockRejectedValue(new Error("down"));
    await act(async () => {
      for (const refresh of live.refresh) refresh();
    });
    await settle();
    expect(cards("In Progress")).toHaveLength(3);
    expect(screen.getByText(/Can't reach the board/)).toBeTruthy();
  });
});
