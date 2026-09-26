// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Project, Ticket } from "../api/client";
import { memoryStorage } from "../test/memoryStorage";

// ACP-53: the Table's Due column must show the stored calendar date, not
// `new Date(ticket.dueDate).toLocaleDateString()`'s local-timezone reading of
// it (anyone west of UTC would see the previous day). The API is mocked.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
}));

vi.mock("../api/client", () => ({ api: mockApi }));

import Tickets from "./Tickets";

const PROJECT: Project = {
  id: "p-ACP",
  name: "ACP",
  prefix: "ACP",
  description: "",
  icon: "",
  color: "",
  status: "active",
  createdAt: "",
  updatedAt: "",
};

const ticket = (number: number, dueDate?: string): Ticket => ({
  id: `t${number}`,
  projectId: PROJECT.id,
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
  ...(dueDate ? { dueDate } : {}),
});

const TICKETS = [ticket(1, "2026-10-01T00:00:00Z"), ticket(2)];

class FakeEventSource {
  addEventListener() {}
  removeEventListener() {}
  close() {}
}

async function mount() {
  window.history.replaceState(null, "", "/table?project=ACP");
  render(
    <BrowserRouter>
      <Tickets />
    </BrowserRouter>,
  );
  for (let i = 0; i < 3; i++) await act(async () => {});
}

const dueCell = (key: string) => {
  const tr = screen.getAllByRole("row").find((r) => within(r).queryAllByRole("cell")[0]?.textContent === key)!;
  return within(tr).getAllByRole("cell")[7];
};

beforeEach(() => {
  vi.stubGlobal("EventSource", FakeEventSource);
  vi.stubGlobal("localStorage", memoryStorage());
  mockApi.tickets.list.mockResolvedValue(TICKETS);
  mockApi.projects.list.mockResolvedValue([PROJECT]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});

describe("Table's Due column, under a negative-offset timezone", () => {
  const originalTz = process.env.TZ;

  beforeEach(() => {
    process.env.TZ = "America/Los_Angeles";
  });

  afterEach(() => {
    if (originalTz === undefined) {
      delete process.env.TZ;
    } else {
      process.env.TZ = originalTz;
    }
  });

  it("shows the stored calendar date instead of shifting a day earlier", async () => {
    await mount();
    expect(dueCell("ACP-1").textContent).toBe("10/1/2026");
    expect(dueCell("ACP-2").textContent).toBe("—");
  });
});
