// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Board as BoardData, DocumentMeta, Now, Project, Ticket } from "../api/client";
import { LAST_VIEW_KEY } from "../lib/lastView";
import { memoryStorage } from "../test/memoryStorage";

// The home path, /, under the app's routes and the real BrowserRouter, with
// the API mocked. It has no page of its own: a ticket's canonical link,
// /?ticket=<KEY>, goes on to the last ticket view and opens the ticket there,
// and anything else goes on to Now. Both replace / in the history.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn(), history: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  board: { get: vi.fn() },
  documents: {
    list: vi.fn(),
    get: vi.fn(),
    search: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
    rawUrl: (id: string) => `/api/documents/${id}/raw`,
  },
  now: { get: vi.fn() },
  // The sidebar's build footer (Layout); it never answers here.
  version: { get: () => new Promise(() => {}) },
}));
vi.mock("../api/client", () => ({ api: mockApi }));
vi.mock("../hooks/useLiveRefresh", () => ({ useLiveRefresh: () => {} }));

import { AppRoutes } from "../App";

// jsdom has no ResizeObserver, which Dependencies watches its viewport with.
class NoResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const project: Project = {
  id: "p-ACP",
  name: "Control plane",
  prefix: "ACP",
  description: "",
  icon: "",
  color: "",
  status: "active",
  createdAt: "",
  updatedAt: "",
};

const ticket: Ticket = {
  id: "ACP-7-id",
  projectId: "p-ACP",
  number: 7,
  title: "Linked ticket",
  description: "",
  status: "todo",
  priority: "medium",
  position: 7,
  createdAt: "",
  updatedAt: "",
  projectPrefix: "ACP",
  repos: [],
  labels: [],
  subtasks: [],
  dependsOn: [],
  blocks: [],
};

const board: BoardData = {
  projectId: "",
  columns: [
    { status: "todo", tickets: [ticket] },
    { status: "in_progress", tickets: [] },
    { status: "agent_review", tickets: [] },
    { status: "done", tickets: [] },
  ],
};

const empty: Now = { inProgress: [], inReview: [], landed: [] };

const spec: DocumentMeta = {
  id: "doc-1",
  ticketId: ticket.id,
  name: "Design spec",
  format: "markdown",
  size: 6,
  revision: 1,
  createdAt: "",
  updatedAt: "",
};

async function mount(url: string) {
  window.history.replaceState(null, "", url);
  const before = window.history.length;
  render(
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>,
  );
  for (let i = 0; i < 4; i++) await act(async () => {});
  return before;
}

const heading = () => screen.getByRole("heading", { level: 1 }).textContent;

beforeEach(() => {
  vi.stubGlobal("localStorage", memoryStorage());
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = NoResizeObserver;
  mockApi.tickets.list.mockResolvedValue([ticket]);
  mockApi.tickets.get.mockResolvedValue(ticket);
  mockApi.tickets.history.mockResolvedValue([]);
  mockApi.projects.list.mockResolvedValue([project]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
  mockApi.board.get.mockResolvedValue(board);
  mockApi.documents.list.mockResolvedValue([]);
  mockApi.documents.search.mockResolvedValue({ ticketIds: [] });
  mockApi.now.get.mockResolvedValue(empty);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
  delete (globalThis as unknown as { ResizeObserver?: unknown }).ResizeObserver;
  window.history.replaceState(null, "", "/");
});

describe("a ticket's link at /", () => {
  it.each([
    ["/table", "/table", "Table"],
    ["/kanban", "/kanban", "Kanban"],
    ["/dependencies", "/dependencies", "Dependencies"],
    // Dependencies, remembered from when it was served at /.
    ["/", "/dependencies", "Dependencies"],
  ])("opens the ticket on the last ticket view, remembered as %s", async (last, path, name) => {
    globalThis.localStorage.setItem(LAST_VIEW_KEY, last);
    const before = await mount("/?ticket=ACP-7&project=ACP&status=todo");
    expect(window.location.pathname).toBe(path);
    expect(window.location.search).toBe("?ticket=ACP-7&project=ACP&status=todo");
    // Replaced, not pushed: Back never comes back to /.
    expect(window.history.length).toBe(before);
    expect(heading()).toBe(name);
    expect(screen.getByRole("dialog", { name: /ACP-7/ })).toBeTruthy();
  });

  it("opens on Kanban when no ticket view was shown yet, and keeps every parameter", async () => {
    mockApi.documents.list.mockResolvedValue([spec]);
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "# Spec" });
    await mount("/?ticket=acp-7&doc=Design%20spec.md&project=ACP");
    expect(window.location.pathname).toBe("/kanban");
    expect(window.location.search).toBe("?ticket=acp-7&doc=Design%20spec.md&project=ACP");
    expect(screen.getByRole("dialog", { name: /ACP-7/ })).toBeTruthy();
  });
});

describe("the home page at /", () => {
  it.each([
    // Now names what it shows: with nothing remembered, every project.
    ["/", "?project=all"],
    ["/?project=ACP", "?project=ACP"],
    ["/?project=ACP&status=todo", "?project=ACP&status=todo"],
  ])("goes on to Now from %s, keeping its query", async (url, search) => {
    globalThis.localStorage.setItem(LAST_VIEW_KEY, "/table");
    const before = await mount(url);
    expect(window.location.pathname).toBe("/now");
    expect(window.location.search).toBe(search);
    expect(window.history.length).toBe(before);
    expect(heading()).toBe("Now");
    // One url per page: the sidebar marks Now as the page shown.
    expect(screen.getByRole("link", { name: "Now" }).getAttribute("aria-current")).toBe("page");
  });
});
