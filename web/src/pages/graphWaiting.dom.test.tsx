// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { Now, Project, Ticket, TicketRequest } from "../api/client";
import { ATTENTION_WAITING_CARD_CLASS } from "../lib/attention";
import { memoryStorage } from "../test/memoryStorage";

// Tickets waiting on the person on the Dependencies view: red cards at the
// top of their column, and the header's "N waiting on you", which opens
// Now's waiting group.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  now: { get: vi.fn() },
  // The sidebar's build footer (Layout); it never answers here.
  version: { get: () => new Promise(() => {}) },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

vi.mock("../hooks/useLiveRefresh", () => ({ useLiveRefresh: () => {} }));

import { AppRoutes } from "../App";

// jsdom has none. The observer reports every observed element on request, as
// the browser does after layout.
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

const project = (prefix: string): Project => ({
  id: `p-${prefix}`,
  name: prefix,
  prefix,
  description: "",
  icon: "",
  color: "",
  status: "active",
  createdAt: "",
  updatedAt: "",
});

const request = (ticketId: string, type: TicketRequest["type"], prompt: string, createdAt: string): TicketRequest => ({
  id: `r-${ticketId}`,
  ticketId,
  agentId: "a1",
  type,
  prompt,
  choices: [],
  createdAt,
});

function ticket(prefix: string, number: number, status: string, over: Partial<Ticket> = {}): Ticket {
  return {
    id: `${prefix}-${number}`,
    projectId: `p-${prefix}`,
    number,
    title: `Ticket ${prefix}-${number}`,
    description: "",
    status,
    priority: "medium",
    position: number,
    createdAt: "",
    updatedAt: "2026-09-28T09:00:00Z",
    projectPrefix: prefix,
    repos: [],
    labels: [],
    subtasks: [],
    dependsOn: [],
    blocks: [],
    ...over,
  };
}

// In ACP: an in-progress ticket, a todo, and two waiting ones, ACP-4 asked
// before ACP-3; in LDR, a third waiting ticket, which the count takes in too.
const TICKETS: Ticket[] = [
  ticket("ACP", 1, "in_progress"),
  ticket("ACP", 2, "todo"),
  ticket("ACP", 3, "needs_user_input", {
    openRequest: request("ACP-3", "question", "Round only, or the finding too?", "2026-09-28T10:05:00Z"),
  }),
  ticket("ACP", 4, "needs_user_input", {
    openRequest: request("ACP-4", "approval", "Delete the Decisions document?\nNothing else goes.", "2026-09-28T10:00:00Z"),
  }),
  ticket("LDR", 1, "needs_user_input", {
    openRequest: request("LDR-1", "approval", "Seed the leaderboard?", "2026-09-28T10:10:00Z"),
  }),
];

const NOW: Now = { inProgress: [], waiting: [], inReview: [], landed: [] };

async function mount(tickets: Ticket[]) {
  mockApi.tickets.list.mockResolvedValue(tickets);
  render(
    <MemoryRouter initialEntries={["/dependencies?project=ACP"]}>
      <AppRoutes />
    </MemoryRouter>,
  );
  await act(async () => {});
  await act(async () => {
    for (const observer of FakeResizeObserver.instances) observer.report();
  });
}

const card = (key: string) => screen.getByRole("button", { name: new RegExp(`^${key} `) });
const top = (key: string) => parseFloat(card(key).style.top);

beforeEach(() => {
  FakeResizeObserver.instances = [];
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = FakeResizeObserver;
  vi.stubGlobal("localStorage", memoryStorage());
  mockApi.projects.list.mockResolvedValue([project("ACP"), project("LDR")]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
  mockApi.now.get.mockResolvedValue(NOW);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
  delete (globalThis as unknown as { ResizeObserver?: unknown }).ResizeObserver;
});

describe("the Dependencies view's waiting cards", () => {
  it("draws each waiting card red, with its request, rippling", async () => {
    await mount(TICKETS);
    const waiting = card("ACP-4");
    const face = waiting.firstElementChild as HTMLElement;
    expect(face.dataset.waiting).toBe("true");
    expect(face.className.split(/\s+/)).toContain("border-red-500/80");
    expect(face.className.split(/\s+/)).toContain(ATTENTION_WAITING_CARD_CLASS);
    expect(within(waiting).getByTestId("card-request").textContent).toBe("ApproveDelete the Decisions document?");
    expect(within(card("ACP-3")).getByTestId("card-request").textContent).toBe("QuestionRound only, or the finding too?");
    expect(within(card("ACP-1")).queryByTestId("card-request")).toBeNull();
  });

  it("names a waiting card as waiting on you, with its request's type and first line", async () => {
    await mount(TICKETS);
    expect(card("ACP-4").getAttribute("aria-label")).toBe(
      "ACP-4 Ticket ACP-4, waiting on you: Approve: Delete the Decisions document?",
    );
    expect(card("ACP-3").getAttribute("aria-label")).toBe(
      "ACP-3 Ticket ACP-3, waiting on you: Question: Round only, or the finding too?",
    );
    expect(card("ACP-1").getAttribute("aria-label")).toBe("ACP-1 Ticket ACP-1");
  });

  it("puts the waiting cards at the top of Ready, oldest first, above the one in progress", async () => {
    await mount(TICKETS);
    expect(top("ACP-4")).toBeLessThan(top("ACP-3"));
    expect(top("ACP-3")).toBeLessThan(top("ACP-1"));
  });
});

describe("the header's waiting count", () => {
  it("counts every project's waiting tickets and links to Now's waiting group", async () => {
    await mount(TICKETS);
    const chip = screen.getByTestId("waiting-chip");
    expect(chip.textContent).toBe("3 waiting on you");
    // Its accessible name starts with what it shows (WCAG 2.5.3).
    expect(chip.getAttribute("aria-label")).toBe("3 waiting on you, in every project: open the waiting list");
    expect(chip.getAttribute("aria-label")!.startsWith(chip.textContent!)).toBe(true);
    expect(screen.getByRole("link", { name: /^3 waiting on you/ })).toBe(chip);
    expect(chip.getAttribute("href")).toBe("/now?project=all#waiting");
    expect(chip.closest("header")).toBe(screen.getByRole("heading", { level: 1, name: "Dependencies" }).closest("header"));
  });

  it("opens Now on its waiting group, across every project", async () => {
    await mount(TICKETS);
    await act(async () => {
      fireEvent.click(screen.getByTestId("waiting-chip"));
    });
    await act(async () => {});
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Now");
    expect(mockApi.now.get).toHaveBeenLastCalledWith(undefined);
    const group = screen.getByRole("region", { name: /Waiting on You/ });
    expect(document.activeElement).toBe(group);
  });

  it("counts one the same way", async () => {
    await mount(TICKETS.filter((t) => t.id !== "ACP-3" && t.id !== "LDR-1"));
    const chip = screen.getByTestId("waiting-chip");
    expect(chip.textContent).toBe("1 waiting on you");
    expect(chip.getAttribute("aria-label")).toBe("1 waiting on you, in every project: open the waiting list");
  });

  it("is hidden when nothing waits", async () => {
    await mount(TICKETS.filter((t) => t.status !== "needs_user_input"));
    expect(screen.getByRole("heading", { level: 1, name: "Dependencies" })).toBeTruthy();
    expect(screen.queryByTestId("waiting-chip")).toBeNull();
  });
});
