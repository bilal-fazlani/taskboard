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
  board: { get: vi.fn() },
}));

vi.mock("../api/client", () => ({ api: mockApi }));

import Board from "./Board";
import Graph from "./Graph";
import Tickets from "./Tickets";

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
  FakeResizeObserver.instances = [];
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = FakeResizeObserver;
  vi.stubGlobal("localStorage", memoryStorage());
  serves(TICKETS, PROJECTS);
  mockApi.labels.list.mockResolvedValue([]);
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
