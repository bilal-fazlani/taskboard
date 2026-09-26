// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { Project, Ticket } from "../api/client";

// ACP-59: the chain ring (CARD_CHAIN_CLASSES in Graph.tsx) has to paint above
// a card's own breathing glow (graph-attention / graph-attention-review,
// index.css), which sits on TicketCard, the wrapper's child. A box-shadow set
// directly on the wrapper paints *before* that child, so it would be
// overpainted at peak glow — that was the original bug. The fix moves the
// ring onto the wrapper's own `::before` pseudo-element, a positioned box
// that always paints after the wrapper's in-flow children.
//
// A first attempt used `::after` instead, which happened to also fix the
// glow-overpaint bug on its own, but collided with `.graph-changed::after`
// (the 2s live-refresh halo, also a pseudo-element on this same wrapper,
// CHANGE_GLOW_CLASS): only one `::after` rule can apply, so a lit chain card
// lost its ring and drop shadow for the halo's 2 seconds. This file pins the
// fix at the class-name level, in the browser DOM (not by reading Graph.tsx's
// source as text), so a regression to either a bare wrapper box-shadow or to
// `after:` fails a test rather than only showing up in review.

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

function ticket(number: number, deps: number[] = [], status = "in_progress"): Ticket {
  return {
    id: `t${number}`,
    projectId: "p1",
    number,
    title: `Ticket ${number}`,
    description: "",
    status,
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

// ACP-1 -> ACP-2 -> ACP-3 <-> ACP-4 -> ACP-5, every card in_progress (so it
// also glows), giving one hover on ACP-3 all four chain roles at once:
// ACP-3 focus, ACP-1/ACP-2 upstream, ACP-5 downstream, ACP-4 cycle.
const TICKETS = [ticket(1), ticket(2, [1]), ticket(3, [2, 4]), ticket(4, [3]), ticket(5, [4])];

const card = (key: string) => screen.getByRole("button", { name: new RegExp(`^${key} `) });

// Every class token on an element, split on whitespace.
const tokens = (el: HTMLElement) => el.className.split(/\s+/).filter(Boolean);

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
  await act(async () => {
    fireEvent.pointerMove(card("ACP-3"));
  });
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  delete (globalThis as unknown as { ResizeObserver?: unknown }).ResizeObserver;
  delete (globalThis as unknown as { EventSource?: unknown }).EventSource;
});

describe("the chain ring's pseudo-element (ACP-59 review round 1)", () => {
  it.each([
    ["ACP-3", "focus"],
    ["ACP-1", "upstream"],
    ["ACP-2", "upstream"],
    ["ACP-5", "downstream"],
    ["ACP-4", "cycle"],
  ])("puts %s's (%s) ring and shadow only on before:, never as a bare wrapper box-shadow", (key) => {
    const classes = tokens(card(key));
    // The bug this guards: CARD_CHAIN_CLASSES going back to plain `ring-*`
    // and `shadow-*` utilities on the wrapper itself, which paint *before*
    // TicketCard's own breathing glow and get overpainted by it at peak.
    const bare = classes.filter((c) => /^(ring-|shadow-)/.test(c));
    expect(bare).toEqual([]);
    expect(classes.some((c) => c.startsWith("before:ring-"))).toBe(true);
    expect(classes.some((c) => c.startsWith("before:shadow-"))).toBe(true);
  });

  it("never puts the ring on after:, which .graph-changed (the live-refresh halo) owns", () => {
    // The bug the round-1 review caught: an earlier fix used after: for the
    // ring, colliding with .graph-changed::after (CHANGE_GLOW_CLASS) on the
    // same wrapper, so a lit chain card lost its ring for the halo's 2s.
    for (const key of ["ACP-1", "ACP-2", "ACP-3", "ACP-4", "ACP-5"]) {
      const classes = tokens(card(key));
      const onAfter = classes.filter((c) => c.startsWith("after:ring-") || c.startsWith("after:shadow-"));
      expect(onAfter).toEqual([]);
    }
  });
});
