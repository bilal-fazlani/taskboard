// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Project, Ticket } from "../api/client";
import { memoryStorage } from "../test/memoryStorage";

// The Table's status badge for a ticket waiting on the person: its label and
// red, as every status shows. The API is mocked.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
}));

vi.mock("../api/client", () => ({ api: mockApi }));
vi.mock("../hooks/useLiveRefresh", () => ({ useLiveRefresh: () => {} }));

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

const ticket = (number: number, status: string): Ticket => ({
  id: `t${number}`,
  projectId: PROJECT.id,
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
  dependsOn: [],
  blocks: [],
});

const rowOf = (key: string) =>
  screen.getAllByRole("row").find((r) => within(r).queryAllByRole("cell")[0]?.textContent === key)!;

beforeEach(() => {
  vi.stubGlobal("localStorage", memoryStorage());
  mockApi.tickets.list.mockResolvedValue([ticket(1, "needs_user_input"), ticket(2, "in_progress")]);
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

describe("the Table's status badge", () => {
  it("labels a ticket waiting on the person Waiting on You, in red", async () => {
    window.history.replaceState(null, "", "/table?project=ACP");
    render(
      <BrowserRouter>
        <Tickets />
      </BrowserRouter>,
    );
    for (let i = 0; i < 3; i++) await act(async () => {});
    const badge = within(rowOf("ACP-1")).getByText("Waiting on You");
    expect(badge.className).toMatch(/\bbg-red-500\/20\b/);
    expect(badge.className).toMatch(/\btext-red-400\b/);
    // Shown as written, not capitalised to "Waiting On You".
    expect(badge.className).not.toMatch(/\bcapitalize\b/);
    expect(within(rowOf("ACP-2")).getByText("In Progress").className).toMatch(/\bbg-blue-500\/20\b/);
  });
});
