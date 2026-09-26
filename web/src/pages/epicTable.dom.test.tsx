// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Project, Ticket } from "../api/client";
import { memoryStorage } from "../test/memoryStorage";

// The Table's Epic column, between Title and Labels: the epic's icon and
// name, or a dash, sortable from its header; and the epic filter narrowing its
// rows. The API is mocked.

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

const ticket = (number: number, epic?: string): Ticket => ({
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
  repos: [],
  labels: [],
  subtasks: [],
  dependsOn: [],
  blocks: [],
  ...(epic ? { epic: { id: `e-${epic}`, name: epic } } : {}),
});

const TICKETS = [ticket(1, "Views"), ticket(2), ticket(3, "Agents"), ticket(4, "views")];

class FakeEventSource {
  addEventListener() {}
  removeEventListener() {}
  close() {}
}

async function mount(query = "") {
  window.history.replaceState(null, "", `/table?project=ACP${query}`);
  render(
    <BrowserRouter>
      <Tickets />
    </BrowserRouter>,
  );
  for (let i = 0; i < 3; i++) await act(async () => {});
}

const headers = () => screen.getAllByRole("columnheader").map((th) => th.textContent);
const rowKeys = () =>
  screen
    .getAllByRole("row")
    .slice(1)
    .map((tr) => within(tr).getAllByRole("cell")[0].textContent);
const epicCell = (key: string) => {
  const tr = screen.getAllByRole("row").find((r) => within(r).queryAllByRole("cell")[0]?.textContent === key)!;
  return within(tr).getAllByRole("cell")[2];
};
const sortButton = () => screen.getByRole("button", { name: "Epic" });

beforeEach(() => {
  vi.stubGlobal("EventSource", FakeEventSource);
  vi.stubGlobal("localStorage", memoryStorage());
  mockApi.tickets.list.mockResolvedValue(TICKETS);
  mockApi.projects.list.mockResolvedValue([PROJECT]);
  mockApi.labels.list.mockResolvedValue([]);
  const progress = { counts: {}, total: 0, complete: false, lastActivityAt: null };
  mockApi.epics.list.mockResolvedValue({
    epics: ["Views", "Agents"].map((name) => ({ ...progress, id: `e-${name}`, projectId: PROJECT.id, name, createdAt: "", updatedAt: "" })),
    noEpic: progress,
  });
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});

describe("Table's Epic column", () => {
  it("sits between Title and Labels", async () => {
    await mount();
    const names = headers();
    expect(names.slice(0, 4)).toEqual(["Key", "Title", "Epic", "Labels"]);
  });

  it("shows the epic's icon and name, and a dash without one", async () => {
    await mount();
    const cell = epicCell("ACP-1");
    expect(cell.textContent).toBe("Views");
    expect(cell.querySelector("svg")).not.toBeNull();
    expect(cell.querySelector("[title]")!.getAttribute("title")).toBe("Views");
    expect(epicCell("ACP-2").textContent).toBe("—");
  });

  it("sorts by epic from its header: ascending, descending, then back", async () => {
    await mount();
    const th = sortButton().closest("th")!;
    expect(rowKeys()).toEqual(["ACP-1", "ACP-2", "ACP-3", "ACP-4"]);
    expect(th.getAttribute("aria-sort")).toBe("none");

    fireEvent.click(sortButton());
    expect(rowKeys()).toEqual(["ACP-3", "ACP-1", "ACP-4", "ACP-2"]);
    expect(th.getAttribute("aria-sort")).toBe("ascending");

    fireEvent.click(sortButton());
    expect(rowKeys()).toEqual(["ACP-1", "ACP-4", "ACP-3", "ACP-2"]);
    expect(th.getAttribute("aria-sort")).toBe("descending");

    fireEvent.click(sortButton());
    expect(rowKeys()).toEqual(["ACP-1", "ACP-2", "ACP-3", "ACP-4"]);
    expect(th.getAttribute("aria-sort")).toBe("none");
  });
});

describe("Table filtered by epic", () => {
  it("shows the tickets in the epic the URL names, ignoring case", async () => {
    await mount("&epic=VIEWS");
    expect(rowKeys()).toEqual(["ACP-1", "ACP-4"]);
    expect(screen.getByText("2 of 4 tickets")).toBeTruthy();
  });

  it("shows the tickets without an epic for none", async () => {
    await mount("&epic=none");
    expect(rowKeys()).toEqual(["ACP-2"]);
  });
});
