// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { ActivityPage, Project, Ticket } from "../api/client";
import { memoryStorage } from "../test/memoryStorage";

// The Activity page in the app's routes, with the API mocked: its sidebar
// entry under Views, the filter bar's project and epic (and nothing else)
// deciding what the feed asks for, and an entry opening its ticket.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn() },
  projects: { list: vi.fn(), activity: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  // The sidebar's build footer (Layout); it never answers here.
  version: { get: () => new Promise(() => {}) },
}));
vi.mock("../api/client", () => ({ api: mockApi }));
// The editor has its own tests; here it only has to open on the right ticket.
vi.mock("../components/TicketEditor", () => ({
  default: ({ ticket }: { ticket: Ticket }) => <div data-testid="editor">{ticket.title}</div>,
}));

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

const ticket = (prefix: string, number: number, updatedAt: string): Ticket => ({
  id: `${prefix}-${number}-id`,
  projectId: `p-${prefix}`,
  number,
  title: `${prefix} ticket ${number}`,
  description: "",
  status: "in_progress",
  priority: "medium",
  position: number,
  createdAt: updatedAt,
  updatedAt,
  projectPrefix: prefix,
  repos: [],
  labels: [],
  subtasks: [],
  dependsOn: [],
  blocks: [],
});

const PAGE: ActivityPage = {
  entries: [
    {
      id: "c1",
      ticketId: "ACP-1-id",
      fromStatus: "todo",
      toStatus: "in_progress",
      note: "",
      createdAt: "2026-09-27T10:00:00Z",
      ticketKey: "ACP-1",
      ticketTitle: "ACP ticket 1",
      epic: { id: "e1", name: "Graph" },
    },
  ],
  hasMore: false,
};

async function mount(url: string) {
  window.history.replaceState(null, "", url);
  render(
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>,
  );
  // The loads resolve, the bar picks a project, and the feed loads for it.
  for (let i = 0; i < 4; i++) await act(async () => {});
}

beforeEach(() => {
  vi.stubGlobal("localStorage", memoryStorage());
  mockApi.projects.list.mockResolvedValue([project("ACP"), project("LDR")]);
  mockApi.tickets.list.mockResolvedValue([ticket("ACP", 1, "2026-09-20T00:00:00Z"), ticket("LDR", 1, "2026-09-26T00:00:00Z")]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({
    epics: [{ id: "e1", projectId: "p-ACP", name: "Graph" }],
    noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null },
  });
  mockApi.projects.activity.mockResolvedValue(PAGE);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.resetAllMocks();
});

// A stand-in for the events stream, so a test can open it and drop it.
class FakeEventSource {
  static opened: FakeEventSource[] = [];
  private readonly listeners = new Map<string, Set<() => void>>();
  constructor() {
    FakeEventSource.opened.push(this);
  }
  addEventListener(type: string, listener: () => void) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type)!.add(listener);
  }
  close() {}
  emit(type: string) {
    for (const listener of [...(this.listeners.get(type) ?? [])]) listener();
  }
}

describe("the Activity page", () => {
  it("says Live only while the events stream is open", async () => {
    FakeEventSource.opened = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    await mount("/activity?project=ACP");
    const marker = () => screen.getByTestId("live-marker").textContent;
    const stream = () => FakeEventSource.opened[FakeEventSource.opened.length - 1];
    expect(FakeEventSource.opened).toHaveLength(1);
    expect(marker()).toBe("Connecting…");

    await act(async () => stream().emit("open"));
    expect(marker()).toBe("Live");

    await act(async () => stream().emit("error"));
    expect(marker()).toBe("Reconnecting…");
    expect(screen.getByTestId("live-marker").getAttribute("title")).toMatch(/dropped/);
  });

  it("has its own sidebar entry under Views, carrying the filters like the views", async () => {
    await mount("/activity?project=ACP&epic=Graph&status=todo");
    const views = screen.getByRole("group", { name: "Views" });
    const link = within(views).getByRole("link", { name: "Activity" });
    expect(link.getAttribute("aria-current")).toBe("page");
    expect(link.getAttribute("href")).toBe("/activity?project=ACP&epic=Graph&status=todo");
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Activity");
  });

  it("reads the URL's project and epics, and offers no other filter", async () => {
    await mount("/activity?project=acp&epic=Graph&epic=none&status=todo&q=x");
    expect(mockApi.projects.activity).toHaveBeenCalledWith("ACP", {
      epics: ["Graph", "none"],
      before: undefined,
      limit: 50,
    });
    expect(screen.getByRole("group", { name: "Epic" })).toBeTruthy();
    expect(screen.queryByRole("group", { name: "Status" })).toBeNull();
    expect(screen.queryByRole("searchbox")).toBeNull();
    expect(screen.getByText("ACP ticket 1")).toBeTruthy();
  });

  it("follows the filter bar's pick when the URL names no project", async () => {
    await mount("/activity");
    // LDR's tickets changed last, so the bar picks it.
    expect(new URLSearchParams(window.location.search).get("project")).toBe("LDR");
    expect(mockApi.projects.activity).toHaveBeenCalledWith("LDR", { epics: [], before: undefined, limit: 50 });
  });

  it("opens an entry's ticket in the editor", async () => {
    await mount("/activity?project=ACP");
    await act(async () => {
      fireEvent.click(screen.getByTestId("feed-entry"));
    });
    expect(new URLSearchParams(window.location.search).get("ticket")).toBe("ACP-1");
    expect(screen.getByTestId("editor").textContent).toBe("ACP ticket 1");
  });
});
