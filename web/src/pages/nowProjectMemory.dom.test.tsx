// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Board as BoardData, Project } from "../api/client";
import { LAST_PROJECT_KEY } from "../lib/defaultProject";
import { NOW_PROJECT_KEY } from "../lib/nowProject";
import { memoryStorage } from "../test/memoryStorage";

// Now's remembered project and the views' last project are two memories: a
// pick on Dependencies, Kanban, Table, Epics or Activity moves the views'
// one and leaves Now's alone, and Now's `project=all` never reaches a view.
// Each page runs in the app's routes with the API mocked; Now.dom.test.tsx
// has the other direction.

const mockApi = vi.hoisted(() => ({
  now: { get: vi.fn() },
  tickets: { list: vi.fn(), get: vi.fn() },
  projects: { list: vi.fn(), activity: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  board: { get: vi.fn() },
  documents: { search: vi.fn(), list: vi.fn() },
  // The sidebar's build footer (Layout); it never answers here.
  version: { get: () => new Promise(() => {}) },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import { AppRoutes } from "../App";

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

const EMPTY_BOARD: BoardData = {
  projectId: "",
  columns: ["todo", "in_progress", "agent_review", "done"].map((status) => ({ status, tickets: [] })),
};

// jsdom has no ResizeObserver, which Dependencies lays its canvas out with.
class NoResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

async function settle() {
  for (let i = 0; i < 4; i++) await act(async () => {});
}

const urlProject = () => new URLSearchParams(window.location.search).get("project");

beforeEach(() => {
  vi.stubGlobal("localStorage", memoryStorage({ [NOW_PROJECT_KEY]: "IAGML", [LAST_PROJECT_KEY]: "ACP" }));
  vi.stubGlobal("ResizeObserver", NoResizeObserver);
  mockApi.projects.list.mockResolvedValue([project("ACP"), project("IAGML"), project("LDR")]);
  mockApi.projects.activity.mockResolvedValue({ entries: [], hasMore: false });
  mockApi.tickets.list.mockResolvedValue([]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
  mockApi.board.get.mockResolvedValue(EMPTY_BOARD);
  mockApi.documents.search.mockResolvedValue({ ticketIds: [] });
  mockApi.documents.list.mockResolvedValue([]);
  mockApi.now.get.mockResolvedValue({ inProgress: [], inReview: [], landed: [] });
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});

describe.each([
  ["Dependencies", "/dependencies"],
  ["Kanban", "/kanban"],
  ["Table", "/table"],
  ["Epics", "/epics"],
  ["Activity", "/activity"],
])("a project picked on %s", (_name, path) => {
  it("moves the views' last project and leaves Now's alone", async () => {
    window.history.replaceState(null, "", path);
    render(
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>,
    );
    await settle();
    // The page opens on the views' last project, not on Now's.
    expect(urlProject()).toBe("ACP");

    await act(async () => {
      fireEvent.change(screen.getByLabelText("Project"), { target: { value: "LDR" } });
    });
    await settle();
    expect(urlProject()).toBe("LDR");
    expect(globalThis.localStorage.getItem(LAST_PROJECT_KEY)).toBe("LDR");
    expect(globalThis.localStorage.getItem(NOW_PROJECT_KEY)).toBe("IAGML");
  });
});

describe.each([
  ["Dependencies", "/dependencies"],
  ["Kanban", "/kanban"],
  ["Table", "/table"],
  ["Epics", "/epics"],
  ["Activity", "/activity"],
])("the sidebar's %s link from Now on All projects", (name, path) => {
  it.each([
    ["the views' last project", "LDR", "LDR"],
    ["the first project by name, with none remembered", null, "ACP"],
  ])("opens on %s, and never remembers all", async (_what, last, shown) => {
    const storage = memoryStorage({ [NOW_PROJECT_KEY]: "", ...(last ? { [LAST_PROJECT_KEY]: last } : {}) });
    const writes: string[] = [];
    const setItem = storage.setItem;
    storage.setItem = (key, value) => {
      if (key === LAST_PROJECT_KEY) writes.push(value);
      setItem(key, value);
    };
    vi.stubGlobal("localStorage", storage);
    window.history.replaceState(null, "", "/now?project=all&label=web");
    render(
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>,
    );
    await settle();
    expect(urlProject()).toBe("all");

    const link = screen.getByRole("link", { name });
    expect(new URL(link.getAttribute("href") ?? "", "http://localhost").searchParams.get("project")).toBeNull();
    await act(async () => {
      fireEvent.click(link);
    });
    await settle();
    expect(window.location.pathname).toBe(path);
    expect(urlProject()).toBe(shown);
    expect(writes.map((w) => w.toLowerCase())).not.toContain("all");
    expect(storage.getItem(LAST_PROJECT_KEY)?.toLowerCase()).not.toBe("all");
    expect(storage.getItem(NOW_PROJECT_KEY)).toBe("");
  });
});
