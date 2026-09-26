// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { Project, Ticket } from "../api/client";

// Hovering a card lights its chains. Cards in a cycle with it are red, like
// the amber upstream ones but for hue, so they also carry a "cycle" pill that
// sighted users and screen readers both get.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
}));

vi.mock("../api/client", () => ({ api: mockApi }));

import Graph from "./Graph";

// jsdom has neither. The observer reports every observed element on request,
// as the browser does after layout; the stream never sends anything.
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

class FakeEventSource {
  addEventListener() {}
  removeEventListener() {}
  close() {}
}

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

function ticket(number: number, deps: number[] = []): Ticket {
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
    repos: [],
    labels: [],
    subtasks: [],
    dependsOn: deps.map((d) => ({ id: `t${d}`, key: `ACP-${d}`, title: `Ticket ${d}`, status: "todo" })),
    blocks: [],
  };
}

// ACP-1 -> ACP-2 -> ACP-3 <-> ACP-4 -> ACP-5.
const TICKETS = [ticket(1), ticket(2, [1]), ticket(3, [2, 4]), ticket(4, [3]), ticket(5, [4])];

const card = (key: string) => screen.getByRole("button", { name: new RegExp(`^${key} `) });
const pills = () => [...document.querySelectorAll("[id^='cycle-pill-']")];

beforeEach(async () => {
  FakeResizeObserver.instances = [];
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = FakeResizeObserver;
  (globalThis as unknown as { EventSource: unknown }).EventSource = FakeEventSource;
  mockApi.tickets.list.mockResolvedValue(TICKETS);
  mockApi.projects.list.mockResolvedValue([ACP]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
  render(
    <MemoryRouter initialEntries={["/?project=ACP"]}>
      <Graph />
    </MemoryRouter>,
  );
  await act(async () => {});
  await act(async () => {
    for (const observer of FakeResizeObserver.instances) observer.report();
  });
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  delete (globalThis as unknown as { ResizeObserver?: unknown }).ResizeObserver;
  delete (globalThis as unknown as { EventSource?: unknown }).EventSource;
});

describe("the cycle pill", () => {
  it("shows on the cards in a cycle with the hovered one, and no other", async () => {
    expect(pills()).toHaveLength(0);
    await act(async () => {
      fireEvent.pointerMove(card("ACP-3"));
    });
    expect(pills()).toHaveLength(1);
    const pill = screen.getByText("cycle");
    expect(card("ACP-4").contains(pill)).toBe(true);
    // Upstream and downstream cards, and the hovered card itself, get none.
    for (const key of ["ACP-1", "ACP-2", "ACP-3", "ACP-5"]) expect(card(key).contains(pill)).toBe(false);
  });

  it("names the card's cycle for screen readers too", async () => {
    await act(async () => {
      fireEvent.pointerMove(card("ACP-4"));
    });
    // The card's own label wins over its contents, so the pill describes it.
    expect(screen.getByRole("button", { name: /^ACP-3 /, description: "cycle" })).toBeTruthy();
    expect(screen.getAllByRole("button", { description: "cycle" })).toHaveLength(1);
    expect(card("ACP-4").getAttribute("aria-describedby")).toBeNull();
    // The icon is decoration; the word is the name.
    expect(screen.getByText("cycle").querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  });

  it("sits outside the card's flow, so the card keeps its measured size", async () => {
    await act(async () => {
      fireEvent.pointerMove(card("ACP-3"));
    });
    const pill = screen.getByText("cycle");
    expect(pill.parentElement).toBe(card("ACP-4"));
    expect(pill.className).toMatch(/\babsolute\b/);
    expect(pill.className).toMatch(/\bpointer-events-none\b/);
  });

  it("goes when the pointer leaves the card", async () => {
    await act(async () => {
      fireEvent.pointerMove(card("ACP-3"));
    });
    expect(pills()).toHaveLength(1);
    await act(async () => {
      fireEvent.pointerLeave(card("ACP-3"));
    });
    expect(pills()).toHaveLength(0);
    expect(card("ACP-4").getAttribute("aria-describedby")).toBeNull();
  });
});
