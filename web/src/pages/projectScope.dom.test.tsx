// @vitest-environment jsdom
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Board as BoardData, Project, Ticket, TicketRef } from "../api/client";
import { LAST_PROJECT_KEY } from "../lib/defaultProject";
import { memoryStorage } from "../test/memoryStorage";

// Every view shows one project at a time. These run each view under the real
// BrowserRouter, as the app does, with the API mocked: the bar picks the
// project a URL without one gets, and the view shows only that project's
// tickets. Dependencies lays its graph out from them alone.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  board: { get: vi.fn() },
  documents: { search: vi.fn() },
}));

vi.mock("../api/client", () => ({ api: mockApi }));

import Board from "./Board";
import Graph from "./Graph";
import Tickets from "./Tickets";
import { AppRoutes } from "../App";

// jsdom has no ResizeObserver; this one reports every observed element when
// a test says the browser has laid the page out.
class FakeResizeObserver {
  static instances: FakeResizeObserver[] = [];
  private readonly targets = new Set<Element>();
  private readonly callback: ResizeObserverCallback;

  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
    FakeResizeObserver.instances.push(this);
  }

  observe(target: Element) {
    this.targets.add(target);
  }

  unobserve(target: Element) {
    this.targets.delete(target);
  }

  disconnect() {
    this.targets.clear();
  }

  report() {
    const entries = [...this.targets].map((target) => ({ target }) as ResizeObserverEntry);
    if (entries.length > 0) this.callback(entries, this as unknown as ResizeObserver);
  }
}

// jsdom lays nothing out, so every element measures zero. Cards measure as the
// w-64 cards they are and the viewport as a window, so fitting has something
// real to frame.
const MEASURES = {
  offsetWidth: 256,
  offsetHeight: 96,
  clientWidth: 1200,
  clientHeight: 800,
} as const;
const saved = new Map<string, PropertyDescriptor | undefined>();

beforeAll(() => {
  for (const [name, value] of Object.entries(MEASURES)) {
    saved.set(name, Object.getOwnPropertyDescriptor(HTMLElement.prototype, name));
    Object.defineProperty(HTMLElement.prototype, name, { configurable: true, get: () => value });
  }
});

afterAll(() => {
  for (const [name, descriptor] of saved) {
    if (descriptor) Object.defineProperty(HTMLElement.prototype, name, descriptor);
  }
});

const project = (prefix: string, name = prefix, status = "active"): Project => ({
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

const ref = (t: Ticket): TicketRef => ({
  id: t.id,
  key: `${t.projectPrefix}-${t.number}`,
  title: t.title,
  status: t.status,
});

function ticket(prefix: string, number: number, overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: `${prefix}-${number}-id`,
    projectId: `p-${prefix}`,
    number,
    title: `${prefix} ticket ${number}`,
    description: "",
    status: "todo",
    priority: "medium",
    position: number,
    createdAt: "",
    updatedAt: "",
    projectPrefix: prefix,
    labels: [],
    subtasks: [],
    ...overrides,
  };
}

// Two projects. ACP-2 waits on ACP-1, and ACP-3 waits on LDR-1, a ticket in
// the other project, which blocks it but is never drawn on ACP's graph.
const LDR1 = ticket("LDR", 1, { status: "in_progress" });
const LDR2 = ticket("LDR", 2);
const ACP1 = ticket("ACP", 1, { status: "in_progress", repos: ["acme/api"] });
const ACP2 = ticket("ACP", 2, { priority: "high", dependsOn: [ref(ACP1)], repos: ["acme/web"] });
const ACP3 = ticket("ACP", 3, { dependsOn: [ref(LDR1)] });
const TICKETS = [LDR1, LDR2, ACP1, ACP2, ACP3];
const PROJECTS = [project("ACP"), project("LDR")];

function serves(tickets: Ticket[], projects: Project[]) {
  mockApi.tickets.list.mockResolvedValue(tickets);
  mockApi.projects.list.mockResolvedValue(projects);
  const board: BoardData = {
    projectId: "",
    columns: [
      { status: "todo", tickets: tickets.filter((t) => t.status === "todo") },
      { status: "in_progress", tickets: tickets.filter((t) => t.status === "in_progress") },
      { status: "agent_review", tickets: tickets.filter((t) => t.status === "agent_review") },
      { status: "done", tickets: tickets.filter((t) => t.status === "done") },
    ],
  };
  mockApi.board.get.mockResolvedValue(board);
}

async function mount(page: React.ReactNode, url: string) {
  window.history.replaceState(null, "", url);
  render(<BrowserRouter>{page}</BrowserRouter>);
  // The loads resolve, the bar picks a project, and the view settles on it.
  for (let i = 0; i < 3; i++) await act(async () => {});
}

async function layout() {
  await act(async () => {
    for (const observer of FakeResizeObserver.instances) observer.report();
  });
}

const urlProject = () => new URLSearchParams(window.location.search).get("project");
const shows = (text: string) => screen.queryAllByText(text).length > 0;
const count = () => document.querySelector('[role="search"] [aria-live="polite"]')?.textContent;

beforeEach(() => {
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
  FakeResizeObserver.instances = [];
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = FakeResizeObserver;
  vi.stubGlobal("localStorage", memoryStorage());
  serves(TICKETS, PROJECTS);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.documents.search.mockResolvedValue({ ticketIds: [] });
  mockApi.tickets.get.mockImplementation((id: string) =>
    Promise.resolve(TICKETS.find((t) => t.id === id)),
  );
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
  delete (globalThis as unknown as { ResizeObserver?: unknown }).ResizeObserver;
  window.history.replaceState(null, "", "/");
});

const views = [
  ["Dependencies", () => <Graph />, "/", "No open tickets"],
  ["Kanban", () => <Board />, "/kanban", null],
  ["Table", () => <Tickets />, "/table", "No tickets found"],
] as const;

describe.each(views)("%s without a project in its URL", (_name, page, path, emptyText) => {
  it("shows the project last shown on any view", async () => {
    globalThis.localStorage.setItem(LAST_PROJECT_KEY, "LDR");
    await mount(page(), `${path}?status=todo`);
    expect(urlProject()).toBe("LDR");
    expect(new URLSearchParams(window.location.search).get("status")).toBe("todo");
    expect(shows("LDR ticket 2")).toBe(true);
    expect(shows("ACP ticket 3")).toBe(false);
  });

  it("shows the first project by name when none was shown before and no ticket has a time", async () => {
    await mount(page(), path);
    expect(urlProject()).toBe("ACP");
    expect(shows("ACP ticket 1")).toBe(true);
    expect(shows("LDR ticket 2")).toBe(false);
  });

  it("shows the first project by name when the one last shown is gone", async () => {
    globalThis.localStorage.setItem(LAST_PROJECT_KEY, "GONE");
    await mount(page(), path);
    expect(urlProject()).toBe("ACP");
    expect(shows("ACP ticket 1")).toBe(true);
  });

  it("replaces a deleted project in the URL", async () => {
    globalThis.localStorage.setItem(LAST_PROJECT_KEY, "LDR");
    await mount(page(), `${path}?project=GONE`);
    expect(urlProject()).toBe("LDR");
    expect(shows("LDR ticket 1")).toBe(true);
  });

  it("shows its empty state with no projects at all", async () => {
    serves([], []);
    await mount(page(), path);
    expect(urlProject()).toBeNull();
    expect(shows("Loading graph…") || shows("Loading board…") || shows("Loading tickets…")).toBe(false);
    if (emptyText) expect(shows(emptyText)).toBe(true);
    expect(count()).toBe("0 tickets");
  });

  it("counts only the project's tickets", async () => {
    await mount(page(), `${path}?project=ACP`);
    expect(count()).toBe("3 tickets");
    await act(async () => {
      const status = screen.getByLabelText("Status") as HTMLSelectElement;
      status.value = "todo";
      status.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(count()).toBe("2 of 3 tickets");
  });

  it("finds a ticket by text only its documents hold, and counts it", async () => {
    // The server answers with ACP-2, and with LDR-2, which the view's project
    // leaves out all the same.
    mockApi.documents.search.mockImplementation((q: string) =>
      Promise.resolve({ ticketIds: q === "rollout" ? [ACP2.id, LDR2.id] : [] }),
    );
    await mount(page(), `${path}?project=ACP&q=rollout`);
    // Until the server answers, no ticket's own text matches.
    expect(count()).toBe("0 of 3 tickets");
    // The document search is debounced; let it run.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300));
    });
    expect(mockApi.documents.search).toHaveBeenCalledWith("rollout", "ACP");
    expect(count()).toBe("1 of 3 tickets");
    expect(shows("ACP ticket 2")).toBe(true);
    expect(shows("LDR ticket 2")).toBe(false);
  });

  it("still opens another project's ticket named in the URL", async () => {
    await mount(page(), `${path}?project=ACP&ticket=LDR-2`);
    const dialog = screen.getByRole("dialog");
    expect((dialog.querySelector('[aria-label="Title"]') as HTMLInputElement).value).toBe("LDR ticket 2");
    expect(urlProject()).toBe("ACP");
  });
});

// Archived projects are never picked and not offered, though a link to one
// still opens it; the pick goes to the active project whose tickets changed
// last. OLD is archived and its tickets changed last of all.
const OLD1 = ticket("OLD", 1, { updatedAt: "2026-09-23T09:00:00Z" });
const OLD_PROJECT = project("OLD", "An archived project", "archived");

describe.each(views)("%s and archived projects", (_name, page, path, emptyText) => {
  const options = () => [...(screen.getByLabelText("Project") as HTMLSelectElement).options].map((o) => o.value);

  it("shows the active project whose tickets changed last, whatever their status", async () => {
    // LDR's latest change is a ticket that is done, so not even drawn on the
    // graph; ACP's are older.
    serves(
      [
        { ...ACP1, updatedAt: "2026-09-21T10:00:00Z" },
        { ...LDR1, updatedAt: "2026-09-20T10:00:00Z" },
        ticket("LDR", 4, { status: "done", updatedAt: "2026-09-22T10:00:00+01:00" }),
        OLD1,
      ],
      [OLD_PROJECT, project("ACP"), project("LDR")],
    );
    await mount(page(), path);
    expect(urlProject()).toBe("LDR");
    expect(shows("LDR ticket 1")).toBe(true);
    expect(shows("ACP ticket 1")).toBe(false);
  });

  it("passes over a remembered project that has been archived", async () => {
    globalThis.localStorage.setItem(LAST_PROJECT_KEY, "OLD");
    serves([...TICKETS, OLD1], [OLD_PROJECT, ...PROJECTS]);
    await mount(page(), path);
    expect(urlProject()).toBe("ACP");
    expect(shows("OLD ticket 1")).toBe(false);
  });

  it("leaves archived projects out of the dropdown, which goes by name", async () => {
    serves([...TICKETS, OLD1], [project("LDR", "ledger"), OLD_PROJECT, project("ACP", "Control plane")]);
    await mount(page(), `${path}?project=LDR`);
    expect(options()).toEqual(["ACP", "LDR"]);
  });

  it("keeps an archived project its URL names, as an extra entry", async () => {
    serves([...TICKETS, OLD1], [OLD_PROJECT, ...PROJECTS]);
    await mount(page(), `${path}?project=OLD&ticket=ACP-1`);
    expect(urlProject()).toBe("OLD");
    expect(new URLSearchParams(window.location.search).get("ticket")).toBe("ACP-1");
    expect((screen.getByLabelText("Project") as HTMLSelectElement).value).toBe("OLD");
    expect(options()).toEqual(["ACP", "LDR", "OLD"]);
    expect((screen.getByLabelText("Project") as HTMLSelectElement).selectedOptions[0].textContent).toBe(
      "An archived project (archived)",
    );
    expect(shows("OLD ticket 1")).toBe(true);
  });

  it("shows its empty state when every project is archived", async () => {
    serves([OLD1], [OLD_PROJECT]);
    await mount(page(), path);
    expect(urlProject()).toBeNull();
    expect(shows("Loading graph…") || shows("Loading board…") || shows("Loading tickets…")).toBe(false);
    if (emptyText) expect(shows(emptyText)).toBe(true);
    expect(count()).toBe("0 tickets");
  });

  it("still picks when the tickets fail to load, rather than waiting on them", async () => {
    mockApi.tickets.list.mockRejectedValue(new Error("offline"));
    mockApi.board.get.mockRejectedValue(new Error("offline"));
    await mount(page(), path);
    expect(urlProject()).toBe("ACP");
    expect(shows("Loading graph…") || shows("Loading board…") || shows("Loading tickets…")).toBe(false);
  });
});

describe("Dependencies for one project", () => {
  const canvas = () => document.querySelector<HTMLElement>("[data-graph-canvas]")!;
  const card = (key: string) => screen.queryByRole("button", { name: new RegExp(`^${key} `) });
  const edges = () => canvas().querySelectorAll(":scope > svg > path");

  it("lays out only the project's cards, with no edge to another project", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    expect(card("ACP-1")).toBeTruthy();
    expect(card("ACP-2")).toBeTruthy();
    expect(card("ACP-3")).toBeTruthy();
    expect(card("LDR-1")).toBeNull();
    expect(card("LDR-2")).toBeNull();
    // ACP-1 -> ACP-2 is the only edge. ACP-3's blocker is in LDR: no card,
    // no arrow, just a note on the card that something unseen blocks it.
    expect(edges()).toHaveLength(1);
    expect(card("ACP-3")!.textContent).toContain("1 hidden blocker");
    // Ready holds ACP-1 alone; ACP-2 and ACP-3 are one step on. No holes:
    // the columns count the project's cards only.
    expect(canvas().textContent).toMatch(/Ready1/);
    expect(count()).toBe("3 tickets");
  });

  it("offers only the project's repos", async () => {
    serves([...TICKETS, ticket("LDR", 3, { repos: ["other/repo"] })], PROJECTS);
    await mount(<Graph />, "/?project=ACP");
    const repos = [...(screen.getByLabelText("Repo") as HTMLSelectElement).options].map((o) => o.value);
    expect(repos).toEqual(["", "acme/api", "acme/web"]);
  });

  it("dims the cards the other filters don't match, within the project", async () => {
    await mount(<Graph />, "/?project=ACP&priority=high");
    await layout();
    const dimmed = (key: string) => card(key)!.className.includes("opacity-20");
    expect(dimmed("ACP-2")).toBe(false);
    expect(dimmed("ACP-1")).toBe(true);
    expect(dimmed("ACP-3")).toBe(true);
    // The arrow into the matching card from a dimmed one dims too.
    expect(edges()[0].getAttribute("class")).toContain("opacity-20");
    expect(count()).toBe("1 of 3 tickets");
  });

  it("with only the project set, dims nothing and has nothing to clear", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    for (const key of ["ACP-1", "ACP-2", "ACP-3"]) expect(card(key)!.className).not.toContain("opacity-20");
    expect(shows("Clear filters")).toBe(false);
  });

  it("fits the matching cards on Fit", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    const whole = canvas().style.transform;
    await act(async () => {
      const q = screen.getByLabelText("Search") as HTMLInputElement;
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
      setter.call(q, "ACP-2");
      q.dispatchEvent(new Event("input", { bubbles: true }));
    });
    // Filtering moves nothing by itself.
    expect(canvas().style.transform).toBe(whole);
    await act(async () => screen.getByLabelText("Fit to screen").click());
    // One card framed is closer in than the whole graph.
    const scale = (t: string) => Number(/scale\(([^)]+)\)/.exec(t)![1]);
    expect(scale(canvas().style.transform)).toBeGreaterThan(scale(whole));
  });

  it("re-lays and fits the graph when the project changes", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    const fitted = canvas().style.transform;
    await act(async () => {
      canvas().parentElement!.dispatchEvent(new WheelEvent("wheel", { deltaY: 300, bubbles: true }));
    });
    expect(canvas().style.transform).not.toBe(fitted);

    await act(async () => {
      const select = screen.getByLabelText("Project") as HTMLSelectElement;
      select.value = "LDR";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await act(async () => {});
    expect(urlProject()).toBe("LDR");
    expect(card("ACP-1")).toBeNull();
    expect(card("LDR-1")).toBeTruthy();
    expect(card("LDR-2")).toBeTruthy();
    // Hidden until the new cards are measured and fitted, as on first load.
    expect(canvas().className).toContain("invisible");
    await layout();
    expect(canvas().className).not.toContain("invisible");
    // Fitted afresh: the pan from before is gone, and LDR's two Ready cards
    // frame differently from ACP's two columns.
    expect(canvas().style.transform).not.toBe(fitted);
    const panned = /translate\([^,]+, (-?[\d.]+)px\)/.exec(canvas().style.transform)![1];
    expect(Number(panned)).toBeGreaterThanOrEqual(0);
    expect(count()).toBe("2 tickets");
  });

  it("doesn't glow the other project's cards when switching to it", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    await act(async () => {
      const select = screen.getByLabelText("Project") as HTMLSelectElement;
      select.value = "LDR";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await layout();
    expect(document.querySelectorAll(".graph-changed")).toHaveLength(0);
  });
});

describe("Kanban for one project", () => {
  it("offers only the project's repos", async () => {
    serves([...TICKETS, ticket("LDR", 3, { repos: ["other/repo"] })], PROJECTS);
    await mount(<Board />, "/kanban?project=ACP");
    const repos = [...(screen.getByLabelText("Repo") as HTMLSelectElement).options].map((o) => o.value);
    expect(repos).toEqual(["", "acme/api", "acme/web"]);
  });
});

describe("Table for one project", () => {
  it("offers only the project's repos", async () => {
    serves([...TICKETS, ticket("LDR", 3, { repos: ["other/repo"] })], PROJECTS);
    await mount(<Tickets />, "/table?project=ACP");
    const repos = [...(screen.getByLabelText("Repo") as HTMLSelectElement).options].map((o) => o.value);
    expect(repos).toEqual(["", "acme/api", "acme/web"]);
  });
});

// Dependencies dims the cards the filters don't match by default, or with
// `unmatched=hide` in the URL leaves them off the graph. The project filter
// always hides, whichever the mode.
describe("Dependencies dimming or hiding the cards the filters don't match", () => {
  const canvas = () => document.querySelector<HTMLElement>("[data-graph-canvas]");
  const card = (key: string) => screen.queryByRole("button", { name: new RegExp(`^${key} `) });
  const edges = () => canvas()!.querySelectorAll(":scope > svg > path");
  const dimmed = (key: string) => card(key)!.className.includes("opacity-20");
  const columnCounts = () => [...canvas()!.querySelectorAll("h3 + span")].map((s) => s.textContent);
  const radio = (name: "Dim" | "Hide") => screen.getByRole("radio", { name }) as HTMLInputElement;
  const urlMode = () => new URLSearchParams(window.location.search).get("unmatched");
  const urlHas = (key: string) => new URLSearchParams(window.location.search).has(key);
  const pick = (name: string, value: string) =>
    act(async () => {
      const select = screen.getByLabelText(name) as HTMLSelectElement;
      select.value = value;
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
  const wheel = () =>
    act(async () => {
      canvas()!.parentElement!.dispatchEvent(new WheelEvent("wheel", { deltaY: 300, bubbles: true }));
    });

  it("dims them by default, with Dim picked and nothing in the URL", async () => {
    await mount(<Graph />, "/?project=ACP&priority=high");
    await layout();
    expect(radio("Dim").checked).toBe(true);
    expect(radio("Hide").checked).toBe(false);
    expect(urlHas("unmatched")).toBe(false);
    expect(dimmed("ACP-1")).toBe(true);
    expect(dimmed("ACP-3")).toBe(true);
    expect(dimmed("ACP-2")).toBe(false);
    expect(edges()).toHaveLength(1);
    // Each column counts its matching cards out of all of them.
    expect(columnCounts()).toEqual(["0 of 1", "1 of 2"]);
    expect(count()).toBe("1 of 3 tickets");
  });

  it("in hide mode lays out only the matching cards, drops their edges and counts a hidden blocker", async () => {
    await mount(<Graph />, "/?project=ACP&priority=high&unmatched=hide");
    await layout();
    expect(radio("Hide").checked).toBe(true);
    expect(card("ACP-1")).toBeNull();
    expect(card("ACP-3")).toBeNull();
    expect(dimmed("ACP-2")).toBe(false);
    // ACP-2's blocker, ACP-1, is filtered out: no arrow, but the card still
    // says something unseen blocks it, and it stays right of Ready.
    expect(edges()).toHaveLength(0);
    expect(card("ACP-2")!.textContent).toContain("1 hidden blocker");
    expect(canvas()!.textContent).toMatch(/Ready0/);
    // Every card shown matches, so each column has a plain count.
    expect(columnCounts()).toEqual(["0", "1"]);
    expect(count()).toBe("1 of 3 tickets");
  });

  it("counts cross-project and filtered-out blockers as one", async () => {
    const ACP4 = ticket("ACP", 4, { priority: "high", dependsOn: [ref(ACP1), ref(LDR1)] });
    serves([...TICKETS, ACP4], PROJECTS);
    await mount(<Graph />, "/?project=ACP&priority=high&unmatched=hide");
    await layout();
    expect(card("ACP-4")!.textContent).toContain("2 hidden blockers");
  });

  it("switches between dim and hide live, and writes the parameter only for hide", async () => {
    await mount(<Graph />, "/?project=ACP&priority=high");
    await layout();
    await act(async () => radio("Hide").click());
    await layout();
    expect(urlMode()).toBe("hide");
    expect(radio("Hide").checked).toBe(true);
    expect(card("ACP-1")).toBeNull();
    expect(card("ACP-2")).toBeTruthy();

    await act(async () => radio("Dim").click());
    await layout();
    expect(urlHas("unmatched")).toBe(false);
    expect(radio("Dim").checked).toBe(true);
    expect(dimmed("ACP-1")).toBe(true);
    expect(new URLSearchParams(window.location.search).get("priority")).toBe("high");
  });

  it.each([
    ["dim", ""],
    ["hide", "&unmatched=hide"],
  ])("hides the other projects' tickets in %s mode", async (_mode, param) => {
    await mount(<Graph />, `/?project=ACP&status=todo${param}`);
    await layout();
    expect(card("LDR-1")).toBeNull();
    expect(card("LDR-2")).toBeNull();
    expect(card("ACP-2")).toBeTruthy();
    // ACP-3's blocker is in LDR, hidden by the project in either mode.
    expect(card("ACP-3")!.textContent).toContain("1 hidden blocker");
    expect(count()).toBe("2 of 3 tickets");
  });

  it("keeps hide mode on a reload, and when the filters are cleared", async () => {
    await mount(<Graph />, "/?project=ACP&priority=high&unmatched=hide&ticket=ACP-2");
    await layout();
    expect(radio("Hide").checked).toBe(true);
    expect(card("ACP-1")).toBeNull();
    await act(async () => screen.getByRole("button", { name: /Clear filters/ }).click());
    await layout();
    expect(urlHas("priority")).toBe(false);
    expect(urlMode()).toBe("hide");
    expect(radio("Hide").checked).toBe(true);
    // With no filter left, every card is laid out and none is dimmed.
    for (const key of ["ACP-1", "ACP-2", "ACP-3"]) expect(dimmed(key)).toBe(false);
    expect(count()).toBe("3 tickets");
  });

  it.each(["bogus", "dim", "HIDE", ""])("drops unmatched=%s from the URL and dims", async (value) => {
    await mount(<Graph />, `/?project=ACP&priority=high&unmatched=${value}`);
    await layout();
    expect(urlHas("unmatched")).toBe(false);
    expect(new URLSearchParams(window.location.search).get("priority")).toBe("high");
    expect(radio("Dim").checked).toBe(true);
    expect(dimmed("ACP-1")).toBe(true);
  });

  it("says so when the filters match no open ticket in hide mode", async () => {
    // Done tickets are never on the graph, so status Done matches none.
    await mount(<Graph />, "/?project=ACP&status=done&unmatched=hide");
    expect(canvas()).toBeNull();
    expect(shows("No open tickets match the filters")).toBe(true);
    expect(shows("Switch to Dim or clear filters to see the rest.")).toBe(true);
    expect(shows("No open tickets")).toBe(false);
    // Dim mode still shows every card, dimmed.
    await act(async () => radio("Dim").click());
    await layout();
    expect(shows("No open tickets match the filters")).toBe(false);
    expect(dimmed("ACP-1")).toBe(true);
  });

  it("still says there are no open tickets when the project has none, in hide mode", async () => {
    serves([ticket("ACP", 1, { status: "done" }), LDR1], PROJECTS);
    await mount(<Graph />, "/?project=ACP&status=done&unmatched=hide");
    expect(shows("No open tickets")).toBe(true);
    expect(shows("No open tickets match the filters")).toBe(false);
  });

  it("fits afresh when the mode changes", async () => {
    await mount(<Graph />, "/?project=ACP&priority=high");
    await layout();
    await wheel();
    const panned = canvas()!.style.transform;
    await act(async () => radio("Hide").click());
    await layout();
    expect(canvas()!.className).not.toContain("invisible");
    expect(canvas()!.style.transform).not.toBe(panned);

    await wheel();
    const pannedAgain = canvas()!.style.transform;
    await act(async () => radio("Dim").click());
    await layout();
    expect(canvas()!.style.transform).not.toBe(pannedAgain);
  });

  it("in hide mode fits afresh when the filters change, and in dim mode doesn't", async () => {
    await mount(<Graph />, "/?project=ACP&priority=high&unmatched=hide");
    await layout();
    await wheel();
    const panned = canvas()!.style.transform;
    await pick("Priority", "");
    await layout();
    expect(card("ACP-1")).toBeTruthy();
    expect(canvas()!.style.transform).not.toBe(panned);

    await act(async () => radio("Dim").click());
    await layout();
    await wheel();
    const pannedInDim = canvas()!.style.transform;
    await pick("Priority", "high");
    await layout();
    expect(canvas()!.style.transform).toBe(pannedInDim);
  });
});

// Kanban and Table always hide what the filters don't match. They offer no
// choice and ignore the parameter, but leave it in the URL, so it is still
// set on the way back to Dependencies.
describe.each([
  ["Kanban", () => <Board />, "/kanban"],
  ["Table", () => <Tickets />, "/table"],
] as const)("%s and the unmatched parameter", (_name, page, path) => {
  const params = () => new URLSearchParams(window.location.search);

  it.each(["hide", "bogus"])("ignores unmatched=%s but keeps it, through Clear filters too", async (value) => {
    await mount(page(), `${path}?project=ACP&priority=high&unmatched=${value}`);
    expect(screen.queryByRole("radiogroup")).toBeNull();
    expect(params().get("unmatched")).toBe(value);
    expect(shows("ACP ticket 2")).toBe(true);
    expect(shows("ACP ticket 1")).toBe(false);
    await act(async () => screen.getByRole("button", { name: /Clear filters/ }).click());
    expect(params().has("priority")).toBe(false);
    expect(params().get("unmatched")).toBe(value);
  });
});

describe("Hide mode across the views", () => {
  it("goes to Kanban and back with the filters, and is still picked on Dependencies", async () => {
    await mount(<AppRoutes />, "/?project=ACP&priority=high&unmatched=hide");
    expect((screen.getByRole("radio", { name: "Hide" }) as HTMLInputElement).checked).toBe(true);
    await act(async () => screen.getByRole("link", { name: "Kanban" }).click());
    expect(window.location.pathname).toBe("/kanban");
    expect(window.location.search).toBe("?project=ACP&priority=high&unmatched=hide");
    expect(screen.queryByRole("radiogroup")).toBeNull();
    await act(async () => screen.getByRole("link", { name: "Dependencies" }).click());
    for (let i = 0; i < 3; i++) await act(async () => {});
    expect(window.location.pathname).toBe("/");
    expect(new URLSearchParams(window.location.search).get("unmatched")).toBe("hide");
    expect((screen.getByRole("radio", { name: "Hide" }) as HTMLInputElement).checked).toBe(true);
    expect(screen.queryByRole("button", { name: /^ACP-1 / })).toBeNull();
    expect(screen.queryByRole("button", { name: /^ACP-2 / })).toBeTruthy();
  });
});

// Ready tickets that nothing links leave the Ready column for a grid below
// the graph. ACP-1 is held by an agent, so it stays in the column though
// nothing links it; ACP-2 blocks ACP-3; ACP-4 and ACP-5 link nothing.
describe("Dependencies grid of unlinked Ready tickets", () => {
  const G1 = ticket("ACP", 1, { status: "in_progress" });
  const G2 = ticket("ACP", 2);
  const G3 = ticket("ACP", 3, { dependsOn: [ref(G2)] });
  const G4 = ticket("ACP", 4);
  const G5 = ticket("ACP", 5, { priority: "high" });
  const canvas = () => document.querySelector<HTMLElement>("[data-graph-canvas]")!;
  const card = (key: string) => screen.getByRole("button", { name: new RegExp(`^${key} `) });
  const headings = () =>
    [...canvas().querySelectorAll("h3")].map((h) => `${h.textContent} ${h.nextElementSibling!.textContent}`);
  const at = (key: string) => ({ left: parseFloat(card(key).style.left), top: parseFloat(card(key).style.top) });

  beforeEach(() => serves([G1, G2, G3, G4, G5], [project("ACP")]));

  it("lays them out under the graph, under the first columns, with a header of their own", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    // Ready still counts every Ready ticket, the grid's included.
    expect(headings()).toEqual(["Ready 4", "Blocked · 1 step 1", "Ready · no links 2"]);
    expect(at("ACP-1").left).toBe(at("ACP-2").left);
    expect(at("ACP-4").left).toBe(at("ACP-1").left);
    expect(at("ACP-5").left).toBe(at("ACP-3").left);
    expect(at("ACP-5").top).toBe(at("ACP-4").top);
    const lowest = Math.max(...["ACP-1", "ACP-2", "ACP-3"].map((key) => at(key).top + MEASURES.offsetHeight));
    expect(at("ACP-4").top).toBeGreaterThan(lowest);
    // The held ticket tops Ready.
    expect(at("ACP-1").top).toBeLessThan(at("ACP-2").top);
  });

  it("says where Ready's tickets went when every one is in the grid", async () => {
    // ACP-3 waits on another project's ticket, so it is one step on and
    // links nothing; ACP-4 and ACP-5 are Ready and link nothing either.
    serves([LDR1, ticket("ACP", 3, { dependsOn: [ref(LDR1)] }), G4, G5], [project("ACP"), project("LDR")]);
    await mount(<Graph />, "/?project=ACP");
    await layout();
    expect(headings()).toEqual(["Ready 2", "Blocked · 1 step 1", "Ready · no links 2"]);
    const note = screen.getByText("All Ready tickets are unlinked — see below");
    expect(shows("Nothing ready to start")).toBe(false);
    // In the Ready column, above the grid's header.
    expect(parseFloat(note.style.left)).toBe(at("ACP-4").left);
    const gridHeader = [...canvas().querySelectorAll("h3")].find((h) => h.textContent === "Ready · no links")!.parentElement!;
    expect(parseFloat(note.style.top) + parseFloat(note.style.height)).toBeLessThan(parseFloat(gridHeader.style.top));
  });

  it("keeps the note clear of the grid's header when nothing else is on the graph", async () => {
    serves([G4, G5], [project("ACP")]);
    await mount(<Graph />, "/?project=ACP");
    await layout();
    expect(headings()).toEqual(["Ready 2", "Ready · no links 2"]);
    const note = screen.getByText("All Ready tickets are unlinked — see below");
    const gridHeader = [...canvas().querySelectorAll("h3")].find((h) => h.textContent === "Ready · no links")!.parentElement!;
    expect(parseFloat(note.style.top) + parseFloat(note.style.height)).toBeLessThan(parseFloat(gridHeader.style.top));
  });

  it("has no such note while Ready holds a linked or held card", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    expect(shows("All Ready tickets are unlinked — see below")).toBe(false);
  });

  it("says nothing is ready, rather than pointing at the grid, when Ready is empty", async () => {
    serves([LDR1, ticket("ACP", 3, { dependsOn: [ref(LDR1)] })], [project("ACP"), project("LDR")]);
    await mount(<Graph />, "/?project=ACP");
    await layout();
    expect(shows("Nothing ready to start")).toBe(true);
    expect(shows("All Ready tickets are unlinked — see below")).toBe(false);
  });

  it("reaches the grid last with Tab, after the graph", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    const order = [...canvas().querySelectorAll<HTMLElement>("[data-ticket-id]")].map((el) => el.getAttribute("aria-label")!.split(" ")[0]);
    expect(order).toEqual(["ACP-1", "ACP-2", "ACP-3", "ACP-4", "ACP-5"]);
    for (const key of order) expect(card(key).tabIndex).toBe(0);
  });

  it("dims the grid's cards the filters don't match, and counts its matches", async () => {
    await mount(<Graph />, "/?project=ACP&priority=high");
    await layout();
    expect(card("ACP-4").className).toContain("opacity-20");
    expect(card("ACP-5").className).not.toContain("opacity-20");
    expect(headings()).toEqual(["Ready 1 of 4", "Blocked · 1 step 0 of 1", "Ready · no links 1 of 2"]);
  });

  it("frames a matching card in the grid on Fit", async () => {
    await mount(<Graph />, "/?project=ACP&priority=high");
    await layout();
    const whole = canvas().style.transform;
    await act(async () => screen.getByLabelText("Fit to screen").click());
    const [, x, y, k] = /translate\(([-\d.]+)px, ([-\d.]+)px\) scale\(([\d.]+)\)/.exec(canvas().style.transform)!.map(Number);
    expect(k).toBeGreaterThan(Number(/scale\(([^)]+)\)/.exec(whole)![1]));
    // ACP-5's box, on screen, lies inside the viewport.
    const { left, top } = at("ACP-5");
    expect(x + k * left).toBeGreaterThanOrEqual(0);
    expect(y + k * top).toBeGreaterThanOrEqual(0);
    expect(x + k * (left + MEASURES.offsetWidth)).toBeLessThanOrEqual(MEASURES.clientWidth);
    expect(y + k * (top + MEASURES.offsetHeight)).toBeLessThanOrEqual(MEASURES.clientHeight);
  });
});
