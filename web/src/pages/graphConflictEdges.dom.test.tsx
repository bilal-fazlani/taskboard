// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { DependencyKind, Project, Ticket } from "../api/client";

// A dependency that waits only to avoid a conflict is drawn as a dashed edge,
// and the legend that says what the dash means shows only while one is drawn.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
}));

vi.mock("../api/client", () => ({ api: mockApi }));

import Graph from "./Graph";

// jsdom has neither; the observer reports every observed element on request.
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

function ticket(number: number, deps: [number, DependencyKind][] = []): Ticket {
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
    dependsOn: deps.map(([d, kind]) => ({ id: `t${d}`, key: `ACP-${d}`, title: `Ticket ${d}`, status: "todo", kind })),
    blocks: [],
  };
}

async function show(tickets: Ticket[]) {
  mockApi.tickets.list.mockResolvedValue(tickets);
  render(
    <MemoryRouter initialEntries={["/?project=ACP"]}>
      <Graph />
    </MemoryRouter>,
  );
  await act(async () => {});
  await act(async () => {
    for (const observer of FakeResizeObserver.instances) observer.report();
  });
}

const edgePaths = () => [...document.querySelectorAll("svg path[marker-end]")] as SVGPathElement[];
const legend = () => document.querySelector("[data-graph-legend]");

beforeEach(() => {
  FakeResizeObserver.instances = [];
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = FakeResizeObserver;
  (globalThis as unknown as { EventSource: unknown }).EventSource = FakeEventSource;
  mockApi.projects.list.mockResolvedValue([ACP]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  delete (globalThis as unknown as { ResizeObserver?: unknown }).ResizeObserver;
  delete (globalThis as unknown as { EventSource?: unknown }).EventSource;
});

describe("conflict-only edges", () => {
  it("dashes only the edges that wait to avoid a conflict, and shows the legend", async () => {
    await show([ticket(1), ticket(2), ticket(3, [[1, "needs_work"], [2, "conflict_only"]])]);
    const edges = edgePaths();
    expect(edges).toHaveLength(2);
    const dashed = edges.filter((p) => p.getAttribute("stroke-dasharray"));
    expect(dashed).toHaveLength(1);
    expect(dashed[0].getAttribute("data-conflict-only")).toBe("true");
    expect(legend()?.textContent).toContain("conflict only");
    expect(legend()?.textContent).toContain("needs work");
  });

  it("draws solid edges and no legend when every dependency needs work", async () => {
    await show([ticket(1), ticket(2, [[1, "needs_work"]])]);
    expect(edgePaths()).toHaveLength(1);
    expect(edgePaths()[0].getAttribute("stroke-dasharray")).toBeNull();
    expect(legend()).toBeNull();
  });
});
