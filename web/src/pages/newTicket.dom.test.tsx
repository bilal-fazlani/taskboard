// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Board as BoardData, Project, Ticket } from "../api/client";
import { LAST_PROJECT_KEY } from "../lib/defaultProject";
import { DEBOUNCE_MS } from "../lib/liveRefresh";
import { STATUS_LABELS } from "../lib/status";
import { memoryStorage } from "../test/memoryStorage";

// The new-ticket form on each view that offers one starts in the project the
// view shows, so the ticket doesn't land in another project and vanish from
// view. The project stays changeable, and a project the user picks stays
// picked when the projects reload. The API is mocked.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn(), create: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  board: { get: vi.fn() },
}));

vi.mock("../api/client", () => ({ api: mockApi }));

import Board from "./Board";
import Tickets from "./Tickets";

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

const ticket = (prefix: string, number: number): Ticket => ({
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
});

// The live-refresh stream, which a test can make deliver the `changed` event
// the server would send.
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

// ACP is both first listed and first by name, so a form that starts in LDR
// took it from the view rather than falling back to the first project.
const PROJECTS = [project("ACP"), project("LDR")];
const TICKETS = [ticket("ACP", 1), ticket("LDR", 1)];

function serves(projects: Project[] | Promise<Project[]>) {
  mockApi.tickets.list.mockResolvedValue(TICKETS);
  mockApi.projects.list.mockReturnValue(Promise.resolve(projects));
  const board: BoardData = {
    projectId: "",
    columns: [
      { status: "todo", tickets: TICKETS },
      { status: "in_progress", tickets: [] },
      { status: "agent_review", tickets: [] },
      { status: "done", tickets: [] },
    ],
  };
  mockApi.board.get.mockResolvedValue(board);
}

async function settle() {
  for (let i = 0; i < 3; i++) await act(async () => {});
}

/**
 * What the server pushing a change does to a mounted page: it refetches, and
 * the projects come back as new objects, as they do from the API.
 */
async function liveChange() {
  serves(PROJECTS.map((p) => ({ ...p })));
  await act(async () => {
    for (const stream of FakeEventSource.opened) stream.emit("changed");
    await new Promise((resolve) => setTimeout(resolve, DEBOUNCE_MS + 20));
  });
  await settle();
}

async function mount(page: React.ReactNode, url: string) {
  window.history.replaceState(null, "", url);
  render(<BrowserRouter>{page}</BrowserRouter>);
  // The loads resolve, the bar picks a project, and the view settles on it.
  await settle();
}

const views = [
  [
    "Kanban",
    () => <Board />,
    "/kanban",
    () => within(screen.getByRole("heading", { name: STATUS_LABELS.todo }).parentElement!).getByRole("button"),
  ],
  ["Table", () => <Tickets />, "/table", () => screen.getByRole("button", { name: "New Ticket" })],
] as const;

const form = () => screen.getByRole("heading", { name: "New Ticket" }).closest("form")!;
// The form's first select is its project.
const projectSelect = () => form().querySelector("select")!;

beforeEach(() => {
  FakeEventSource.opened = [];
  vi.stubGlobal("EventSource", FakeEventSource);
  vi.stubGlobal("localStorage", memoryStorage());
  serves(PROJECTS);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.tickets.create.mockResolvedValue(TICKETS[0]);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});

describe.each(views)("%s's new-ticket form", (_name, page, path, newTicketButton) => {
  it("starts in the project the view shows", async () => {
    await mount(page(), `${path}?project=LDR`);
    await act(async () => newTicketButton().click());
    expect(projectSelect().value).toBe("p-LDR");
  });

  it("starts in the project picked for a URL without one", async () => {
    globalThis.localStorage.setItem(LAST_PROJECT_KEY, "LDR");
    await mount(page(), path);
    expect(new URLSearchParams(window.location.search).get("project")).toBe("LDR");
    await act(async () => newTicketButton().click());
    expect(projectSelect().value).toBe("p-LDR");
  });

  it("follows the view's project written in another case", async () => {
    await mount(page(), `${path}?project=ldr`);
    await act(async () => newTicketButton().click());
    expect(projectSelect().value).toBe("p-LDR");
  });

  it("still lets another project be chosen", async () => {
    await mount(page(), `${path}?project=LDR`);
    await act(async () => newTicketButton().click());
    fireEvent.change(projectSelect(), { target: { value: "p-ACP" } });
    expect(projectSelect().value).toBe("p-ACP");
    fireEvent.change(screen.getByPlaceholderText("What needs to be done?"), { target: { value: "Elsewhere" } });
    await act(async () => screen.getByRole("button", { name: "Create Ticket" }).click());
    expect(mockApi.tickets.create).toHaveBeenCalledWith(
      expect.objectContaining({ projectId: "p-ACP", title: "Elsewhere" }),
    );
  });

  it("keeps a picked project when the projects reload", async () => {
    await mount(page(), `${path}?project=LDR`);
    await act(async () => newTicketButton().click());
    fireEvent.change(projectSelect(), { target: { value: "p-ACP" } });
    const reloads = mockApi.projects.list.mock.calls.length;
    await liveChange();
    expect(mockApi.projects.list.mock.calls.length).toBeGreaterThan(reloads);
    expect(projectSelect().value).toBe("p-ACP");
  });

  it("creates the ticket in the view's project when left alone", async () => {
    await mount(page(), `${path}?project=LDR`);
    await act(async () => newTicketButton().click());
    fireEvent.change(screen.getByPlaceholderText("What needs to be done?"), { target: { value: "Here" } });
    await act(async () => screen.getByRole("button", { name: "Create Ticket" }).click());
    expect(mockApi.tickets.create).toHaveBeenCalledWith(
      expect.objectContaining({ projectId: "p-LDR", title: "Here" }),
    );
  });
});

describe("Table's new-ticket form opened before the projects load", () => {
  it("starts in the view's project once they arrive", async () => {
    let arrive!: (projects: Project[]) => void;
    serves(new Promise<Project[]>((resolve) => (arrive = resolve)));
    await mount(<Tickets />, "/table?project=LDR");
    await act(async () => screen.getByRole("button", { name: "New Ticket" }).click());
    expect(projectSelect().value).toBe("");
    arrive(PROJECTS);
    await settle();
    expect(projectSelect().value).toBe("p-LDR");
  });

  it("keeps a project picked after they arrive when they reload", async () => {
    let arrive!: (projects: Project[]) => void;
    serves(new Promise<Project[]>((resolve) => (arrive = resolve)));
    await mount(<Tickets />, "/table?project=LDR");
    await act(async () => screen.getByRole("button", { name: "New Ticket" }).click());
    arrive(PROJECTS);
    await settle();
    fireEvent.change(projectSelect(), { target: { value: "p-ACP" } });
    await liveChange();
    expect(projectSelect().value).toBe("p-ACP");
  });
});
