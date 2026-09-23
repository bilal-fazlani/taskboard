// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { Board as BoardData, Project, Ticket } from "../api/client";
import { DEBOUNCE_MS } from "../lib/liveRefresh";

// A live refresh replaces what a view shows, never where the user is looking
// from: the pan and zoom on Dependencies, the scroll position on Kanban and
// Table, and the view itself all belong to the user, not to the data.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  board: { get: vi.fn() },
}));

vi.mock("../api/client", () => ({ api: mockApi }));

import Board from "./Board";
import Graph from "./Graph";
import Tickets from "./Tickets";

// jsdom has neither of these. The observers report nothing until a test says
// so, and the stream delivers the `changed` event the server would send.
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

  /** Reports every observed element, as the browser does after layout. */
  report() {
    const entries = [...this.targets].map((target) => ({ target }) as ResizeObserverEntry);
    if (entries.length > 0) this.callback(entries, this as unknown as ResizeObserver);
  }
}

class FakeEventSource {
  static opened: FakeEventSource[] = [];
  private readonly listeners = new Map<string, Set<() => void>>();

  constructor() {
    FakeEventSource.opened.push(this);
  }

  addEventListener(type: string, listener: () => void) {
    let set = this.listeners.get(type);
    if (!set) this.listeners.set(type, (set = new Set()));
    set.add(listener);
  }

  removeEventListener(type: string, listener: () => void) {
    this.listeners.get(type)?.delete(listener);
  }

  close() {}

  emit(type: string) {
    for (const listener of [...(this.listeners.get(type) ?? [])]) listener();
  }
}

function makeTicket(number: number, overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: `t${number}`,
    projectId: "p1",
    number,
    title: `Ticket ${number}`,
    description: "",
    status: "todo",
    priority: "medium",
    position: number,
    createdAt: "",
    updatedAt: "",
    projectPrefix: "ACP",
    labels: [],
    subtasks: [],
    ...overrides,
  };
}

const board = (tickets: Ticket[]): BoardData => ({
  projectId: "",
  columns: [
    { status: "todo", tickets },
    { status: "in_progress", tickets: [] },
    { status: "done", tickets: [] },
  ],
});

// Every view shows one project, so the tickets have one to be shown in.
const ACP: Project = {
  id: "p1",
  name: "ACP",
  prefix: "ACP",
  description: "",
  icon: "",
  color: "",
  status: "active",
  createdAt: "",
  updatedAt: "",
};

const before = [makeTicket(1), makeTicket(2)];
const after = [makeTicket(1, { title: "Ticket 1, renamed" }), makeTicket(2), makeTicket(3)];

function serves(tickets: Ticket[]) {
  mockApi.tickets.list.mockResolvedValue(tickets);
  mockApi.board.get.mockResolvedValue(board(tickets));
}

/** Every observer reports, which is what a browser does after a layout. */
async function layout() {
  await act(async () => {
    for (const observer of FakeResizeObserver.instances) observer.report();
  });
}

/** What the server pushing a change does to a mounted page. */
async function liveChange(tickets: Ticket[]) {
  serves(tickets);
  await act(async () => {
    for (const stream of FakeEventSource.opened) stream.emit("changed");
    await new Promise((resolve) => setTimeout(resolve, DEBOUNCE_MS + 20));
  });
}

async function mount(page: React.ReactNode, path = "/") {
  const utils = render(<MemoryRouter initialEntries={[path]}>{page}</MemoryRouter>);
  await act(async () => {});
  return utils;
}

beforeEach(() => {
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
  FakeResizeObserver.instances = [];
  FakeEventSource.opened = [];
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = FakeResizeObserver;
  (globalThis as unknown as { EventSource: unknown }).EventSource = FakeEventSource;
  serves(before);
  mockApi.projects.list.mockResolvedValue([ACP]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.tickets.get.mockImplementation((id: string) => Promise.resolve(makeTicket(1, { id })));
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  delete (globalThis as unknown as { ResizeObserver?: unknown }).ResizeObserver;
  delete (globalThis as unknown as { EventSource?: unknown }).EventSource;
});

describe("Dependencies across a refetch", () => {
  const canvas = () => document.querySelector<HTMLElement>("[data-graph-canvas]")!;
  const viewport = () => canvas().parentElement!;
  const zoomLevel = () => screen.getByLabelText("Zoom level").textContent;

  it("keeps the pan and the zoom", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    const fitted = canvas().style.transform;
    expect(fitted).not.toBe("");

    // Zoom in from the toolbar, then pan with the wheel.
    await act(async () => screen.getByLabelText("Zoom in").click());
    await act(async () => {
      viewport().dispatchEvent(new WheelEvent("wheel", { deltaX: 40, deltaY: 120, bubbles: true }));
    });
    const moved = canvas().style.transform;
    expect(moved).not.toBe(fitted);
    const zoomed = zoomLevel();

    await liveChange(after);
    expect(screen.getByText("Ticket 1, renamed")).toBeTruthy();
    expect(screen.getByText("Ticket 3")).toBeTruthy();
    // The graph under the user's pointer has not moved an inch.
    expect(canvas().style.transform).toBe(moved);
    expect(zoomLevel()).toBe(zoomed);
  });

  it("fits once, and not again when the tickets change", async () => {
    await mount(<Graph />, "/?project=ACP");
    await layout();
    const fitted = canvas().style.transform;
    await act(async () => {
      viewport().dispatchEvent(new WheelEvent("wheel", { deltaY: 200, bubbles: true }));
    });
    const moved = canvas().style.transform;

    await liveChange(after);
    await layout();
    expect(canvas().style.transform).toBe(moved);
    expect(canvas().style.transform).not.toBe(fitted);
  });
});

describe("Table across a refetch", () => {
  it("keeps the scroll position and the same scrolling element", async () => {
    await mount(<Tickets />, "/table?project=ACP");
    const scroller = screen.getByTestId("table-scroll");
    scroller.scrollTop = 240;

    await liveChange(after);

    expect(screen.getByTestId("table-scroll")).toBe(scroller);
    expect(scroller.scrollTop).toBe(240);
    expect(screen.getByText("Ticket 1, renamed")).toBeTruthy();
  });
});

describe("Kanban across a refetch", () => {
  it("keeps the scroll position and the same scrolling element", async () => {
    await mount(<Board />, "/kanban?project=ACP");
    const scroller = screen.getByTestId("board-scroll");
    scroller.scrollLeft = 320;
    scroller.scrollTop = 80;

    await liveChange(after);

    expect(screen.getByTestId("board-scroll")).toBe(scroller);
    expect(scroller.scrollLeft).toBe(320);
    expect(scroller.scrollTop).toBe(80);
    expect(screen.getByText("Ticket 1, renamed")).toBeTruthy();
  });
});

describe("the selected view across a refetch", () => {
  it.each([
    ["Dependencies", <Graph key="g" />, "/"],
    ["Kanban", <Board key="b" />, "/kanban"],
    ["Table", <Tickets key="t" />, "/table"],
  ])("stays on %s with its filters", async (heading, page, path) => {
    await mount(page, `${path}?project=ACP&status=todo&q=ticket`);
    await liveChange(after);
    // Nothing navigated: the view, and the filters in its URL, are the
    // user's, and a change to the data is no reason to move either.
    expect(screen.getByRole("heading", { name: heading })).toBeTruthy();
    expect((screen.getByLabelText("Status") as HTMLSelectElement).value).toBe("todo");
    expect((screen.getByLabelText("Search") as HTMLInputElement).value).toBe("ticket");
  });
});
