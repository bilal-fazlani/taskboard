// @vitest-environment jsdom
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { BrowserRouter, MemoryRouter, Route, Routes } from "react-router-dom";
import type { DocumentMeta, Epic, EpicList, EpicProgress, Project, Ticket } from "../api/client";
import { LAST_PROJECT_KEY } from "../lib/defaultProject";
import { LAST_VIEW_KEY } from "../lib/lastView";
import { memoryStorage } from "../test/memoryStorage";

// The Epics view under the real BrowserRouter, with the API mocked and the
// live-refresh stream replaced by a function the tests call.

const mockApi = vi.hoisted(() => ({
  projects: { list: vi.fn() },
  tickets: { list: vi.fn() },
  epics: { list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
  documents: {
    list: vi.fn(),
    get: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    create: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
    rawUrl: (id: string, rev: number) => `/api/documents/${id}/raw?rev=${rev}`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

const live = vi.hoisted(() => ({ refresh: [] as (() => void)[] }));
vi.mock("../hooks/useLiveRefresh", () => ({
  useLiveRefresh: (onChange: () => void) => {
    live.refresh.push(onChange);
  },
}));

import Epics from "./Epics";
import Layout from "../components/Layout";

const project = (prefix: string, status = "active"): Project => ({
  id: `p-${prefix}`,
  name: prefix,
  prefix,
  description: "",
  icon: "",
  color: "",
  status,
  createdAt: "",
  updatedAt: "",
});

function progress(counts: Partial<Record<string, number>> = {}, lastActivityAt: string | null = null): EpicProgress {
  const full = { todo: 0, in_progress: 0, agent_review: 0, done: 0, ...counts } as Record<string, number>;
  const total = Object.values(full).reduce((a, b) => a + b, 0);
  return { counts: full, total, complete: total > 0 && full.done === total, lastActivityAt };
}

function epic(name: string, counts: Partial<Record<string, number>> = {}, lastActivityAt: string | null = null, description = ""): Epic {
  return {
    id: `e-${name}`,
    projectId: "p-ACP",
    name,
    description,
    createdAt: "",
    updatedAt: "",
    ...progress(counts, lastActivityAt),
  };
}

const ticket = (prefix: string, updatedAt: string) => ({ projectPrefix: prefix, updatedAt }) as Ticket;

const PROJECTS = [project("ACP"), project("LDR")];

// Graph: busy, one in progress, changed long ago. Store: changed last, idle.
// Search: changed before Store. Ideas: empty. Launch: every ticket done.
const GRAPH = epic("Graph", { todo: 2, in_progress: 1, done: 1 }, "2026-09-01T10:00:00Z", "Dependency graph home page");
const STORE = epic("Store", { todo: 1, done: 3 }, "2026-09-22T10:00:00Z");
const SEARCH = epic("Search", { todo: 4 }, "2026-09-20T10:00:00Z");
const IDEAS = epic("Ideas");
const LAUNCH = epic("Launch", { done: 5 }, "2026-09-23T10:00:00Z");
const LIST: EpicList = { epics: [STORE, IDEAS, LAUNCH, SEARCH, GRAPH], noEpic: progress({ todo: 1, done: 1 }) };

function serves(list: EpicList) {
  mockApi.epics.list.mockResolvedValue(list);
}

async function settle() {
  for (let i = 0; i < 4; i++) await act(async () => {});
}

// Back and Forward land a moment later: the epic modal closes by going back.
async function settleHistory() {
  await act(async () => {
    for (let i = 0; i < 30; i++) await new Promise((resolve) => setTimeout(resolve, 5));
  });
}

async function mount(url = "/epics?project=ACP", { strict = false } = {}) {
  window.history.replaceState(null, "", url);
  const page = (
    <BrowserRouter>
      <Epics />
    </BrowserRouter>
  );
  // StrictMode as main.tsx has it: in development it runs every effect's
  // cleanup once on mount.
  render(strict ? <StrictMode>{page}</StrictMode> : page);
  await settle();
}

const rowNames = (root: HTMLElement = document.body) =>
  within(root)
    .queryAllByTestId("epic-name")
    .map((n) => n.textContent);
const rowOf = (name: string) => screen.getAllByTestId("epic-row").find((r) => within(r).queryByText(name))!;
const params = () => new URLSearchParams(window.location.search);

beforeEach(() => {
  vi.stubGlobal("localStorage", memoryStorage());
  live.refresh = [];
  mockApi.projects.list.mockResolvedValue(PROJECTS);
  mockApi.tickets.list.mockResolvedValue([]);
  mockApi.documents.list.mockResolvedValue([]);
  serves(LIST);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});

describe("Epics project selector", () => {
  it("shows the named project's epics, asking the API by prefix", async () => {
    await mount("/epics?project=acp");
    expect(mockApi.epics.list).toHaveBeenCalledWith("ACP");
    expect((screen.getByLabelText("Project") as HTMLSelectElement).value).toBe("ACP");
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Epics");
  });

  it("picks the project last shown for a URL without one, and has no other filters", async () => {
    globalThis.localStorage.setItem(LAST_PROJECT_KEY, "LDR");
    await mount("/epics");
    expect(params().get("project")).toBe("LDR");
    expect(mockApi.epics.list).toHaveBeenCalledWith("LDR");
    expect(document.querySelectorAll("select")).toHaveLength(1);
    expect(screen.queryByLabelText("Status")).toBeNull();
  });

  it("picks the active project whose tickets changed last", async () => {
    mockApi.tickets.list.mockResolvedValue([ticket("ACP", "2026-09-01T00:00:00Z"), ticket("LDR", "2026-09-02T00:00:00Z")]);
    await mount("/epics");
    expect(params().get("project")).toBe("LDR");
  });

  it("replaces a deleted project, remembers the one shown, and switches on a pick", async () => {
    await mount("/epics?project=GONE");
    expect(params().get("project")).toBe("ACP");
    expect(globalThis.localStorage.getItem(LAST_PROJECT_KEY)).toBe("ACP");
    await act(async () => {
      fireEvent.change(screen.getByLabelText("Project"), { target: { value: "LDR" } });
    });
    await settle();
    expect(params().get("project")).toBe("LDR");
    expect(mockApi.epics.list).toHaveBeenLastCalledWith("LDR");
  });

  it("shows no epics and no pick with no active projects", async () => {
    mockApi.projects.list.mockResolvedValue([project("OLD", "archived")]);
    await mount("/epics");
    expect(params().get("project")).toBeNull();
    expect(mockApi.epics.list).not.toHaveBeenCalled();
    expect(screen.getByText("No active projects")).toBeTruthy();
    expect(screen.queryByText("Loading epics…")).toBeNull();
  });
});

describe("Epics sidebar link", () => {
  it("is bare, with no filters carried, and still lands on the remembered project", async () => {
    globalThis.localStorage.setItem(LAST_PROJECT_KEY, "LDR");
    render(
      <MemoryRouter initialEntries={["/kanban?project=ACP&label=web"]}>
        <Routes>
          <Route element={<Layout />}>
            <Route path="/kanban" element={null} />
            <Route path="/epics" element={<Epics />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );
    await settle();
    const link = screen.getByRole("link", { name: "Epics" });
    // Unlike Dependencies, Kanban and Table, which carry the current filters
    // (see App.test.tsx), the Epics link outside the Views group is plain:
    // no project or other filter from the page it's clicked from.
    expect(link.getAttribute("href")).toBe("/epics");
    await act(async () => {
      fireEvent.click(link);
    });
    await settle();
    // Epics still opens on the remembered project on its own, the same way
    // it does for any URL without one (see "picks the project last shown"
    // below), even though the link that got it there carried no project.
    expect(mockApi.epics.list).toHaveBeenCalledWith("LDR");
  });
});

describe("Epics rows", () => {
  it("shows the name, the active marker, the description, the bar and the count", async () => {
    await mount();
    const row = rowOf("Graph");
    expect(within(row).getByText("1 active")).toBeTruthy();
    expect(within(row).getByText("Dependency graph home page")).toBeTruthy();
    expect(within(row).getByTestId("epic-count").textContent).toBe("1 / 4 done");
    expect(within(row).getByRole("button", { name: "Actions for Graph" })).toBeTruthy();
    // Store has no active ticket, so no marker.
    expect(within(rowOf("Store")).queryByText(/active/)).toBeNull();
  });

  it("counts agent review as active too", async () => {
    serves({ epics: [epic("Review", { agent_review: 2, in_progress: 1 }, "2026-09-01T00:00:00Z")], noEpic: progress() });
    await mount();
    expect(within(rowOf("Review")).getByText("3 active")).toBeTruthy();
  });

  it("splits the bar by status in proportion, in the status colours", async () => {
    await mount();
    const segments = [...within(rowOf("Graph")).getByTestId("epic-bar").children] as HTMLElement[];
    expect(segments.map((s) => [s.dataset.status, s.style.width, s.className])).toEqual([
      ["done", "25%", "bg-green-500"],
      ["in_progress", "25%", "bg-blue-500"],
      ["todo", "50%", "bg-slate-500"],
    ]);
  });

  it("shows an empty epic with 0 tickets and an empty bar", async () => {
    await mount();
    const row = rowOf("Ideas");
    expect(within(row).getByTestId("epic-count").textContent).toBe("0 tickets");
    expect(within(row).getByTestId("epic-bar").children).toHaveLength(0);
  });

  it("offers Edit and Delete in the row menu, a plain disclosure", async () => {
    await mount();
    const toggle = screen.getByRole("button", { name: "Actions for Store" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(within(rowOf("Store")).queryByRole("button", { name: "Edit" })).toBeNull();
    await act(async () => {
      fireEvent.click(toggle);
    });
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const panel = document.getElementById(toggle.getAttribute("aria-controls")!)!;
    expect(within(panel).getAllByRole("button").map((i) => i.textContent)).toEqual(["Edit", "Delete"]);
    // No menu roles, which would promise arrow-key behaviour.
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.queryByRole("menuitem")).toBeNull();
  });

  it("closes the row menu on Escape and puts focus back on the ⋯ button", async () => {
    await mount();
    const toggle = screen.getByRole("button", { name: "Actions for Store" });
    await act(async () => {
      fireEvent.click(toggle);
    });
    const edit = within(rowOf("Store")).getByRole("button", { name: "Edit" });
    edit.focus();
    await act(async () => {
      fireEvent.keyDown(edit, { key: "Escape" });
    });
    expect(within(rowOf("Store")).queryByRole("button", { name: "Edit" })).toBeNull();
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(toggle);
  });

  it("closes the row menu when focus moves outside it, so only one is ever open", async () => {
    await mount();
    const store = screen.getByRole("button", { name: "Actions for Store" });
    const graph = screen.getByRole("button", { name: "Actions for Graph" });
    store.focus();
    await act(async () => {
      fireEvent.click(store);
    });
    // Moving between the panel's own buttons keeps it open.
    const edit = within(rowOf("Store")).getByRole("button", { name: "Edit" });
    await act(async () => {
      fireEvent.blur(store, { relatedTarget: edit });
      edit.focus();
    });
    expect(store.getAttribute("aria-expanded")).toBe("true");
    // Tabbing on to the next row's ⋯ closes it.
    await act(async () => {
      fireEvent.blur(edit, { relatedTarget: graph });
      graph.focus();
    });
    expect(store.getAttribute("aria-expanded")).toBe("false");
    await act(async () => {
      fireEvent.click(graph);
    });
    expect(graph.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getAllByRole("button", { name: "Edit" })).toHaveLength(1);
  });

  it("closes an open row menu when another row's ⋯ is clicked", async () => {
    await mount();
    const store = screen.getByRole("button", { name: "Actions for Store" });
    const graph = screen.getByRole("button", { name: "Actions for Graph" });
    await act(async () => {
      fireEvent.click(store);
    });
    await act(async () => {
      fireEvent.mouseDown(graph);
      fireEvent.click(graph);
    });
    expect(store.getAttribute("aria-expanded")).toBe("false");
    expect(graph.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getAllByRole("button", { name: "Edit" })).toHaveLength(1);
  });
});

describe("Epics order and sections", () => {
  it("lists active epics first, then by latest activity, then empty ones, then No epic", async () => {
    await mount();
    expect(rowNames()).toEqual(["Graph", "Store", "Search", "Ideas", "No epic"]);
  });

  it("breaks ties by name", async () => {
    serves({
      epics: [epic("beta", { todo: 1 }, "2026-09-01T00:00:00Z"), epic("Alpha", { todo: 1 }, "2026-09-01T00:00:00Z"), epic("Zed"), epic("apple")],
      noEpic: progress(),
    });
    await mount();
    expect(rowNames()).toEqual(["Alpha", "beta", "apple", "Zed"]);
  });

  it("folds complete epics into a collapsed Complete section that expands to the same rows", async () => {
    await mount();
    const toggle = screen.getByRole("button", { name: "Complete (1)" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(rowNames()).not.toContain("Launch");
    await act(async () => {
      fireEvent.click(toggle);
    });
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const section = screen.getByRole("list", { name: "Complete epics" });
    expect(rowNames(section)).toEqual(["Launch"]);
    expect(within(section).getByTestId("epic-count").textContent).toBe("5 / 5 done");
    expect(within(section).getByRole("button", { name: "Actions for Launch" })).toBeTruthy();
  });

  it("has no Complete section when no epic is complete", async () => {
    serves({ epics: [GRAPH], noEpic: progress() });
    await mount();
    expect(screen.queryByRole("button", { name: /^Complete/ })).toBeNull();
  });

  it("shows the No epic row only when the project has tickets without an epic, with no menu", async () => {
    await mount();
    const row = rowOf("No epic");
    expect(within(row).getByTestId("epic-count").textContent).toBe("1 / 2 done");
    expect(within(row).queryByRole("button")).toBeNull();
    cleanup();

    serves({ ...LIST, noEpic: progress() });
    await mount();
    expect(rowNames()).not.toContain("No epic");
  });

  it("says so when the project has no epics and no tickets", async () => {
    serves({ epics: [], noEpic: progress() });
    await mount();
    expect(screen.getByText("No epics yet.")).toBeTruthy();
  });
});

describe("Epics row click", () => {
  const hrefOf = (name: string) => rowOf(name).querySelector("a")!.getAttribute("href");

  it("opens Kanban by default with only the project and the epic", async () => {
    await mount("/epics?project=ACP&status=todo&label=web");
    expect(hrefOf("Graph")).toBe("/kanban?project=ACP&epic=Graph");
    expect(hrefOf("No epic")).toBe("/kanban?project=ACP&epic=none");
    await act(async () => {
      fireEvent.click(rowOf("Graph").querySelector("a")!);
    });
    expect(window.location.pathname).toBe("/kanban");
    expect(window.location.search).toBe("?project=ACP&epic=Graph");
  });

  it.each([
    ["/", "/?project=ACP&epic=Graph"],
    ["/table", "/table?project=ACP&epic=Graph"],
    ["/kanban", "/kanban?project=ACP&epic=Graph"],
    ["/projects", "/kanban?project=ACP&epic=Graph"],
  ])("opens the last-used view %s", async (last, href) => {
    globalThis.localStorage.setItem(LAST_VIEW_KEY, last);
    await mount();
    expect(hrefOf("Graph")).toBe(href);
  });

  it.each([
    ["/table", "/table"],
    ["/", "/"],
    ["/epics", "/kanban"],
    ["/labels", "/kanban"],
  ])("remembers %s as the last view when it is a ticket view", (path, last) => {
    render(
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route element={<Layout />}>
            <Route path="*" element={null} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );
    expect(globalThis.localStorage.getItem(LAST_VIEW_KEY) ?? "/kanban").toBe(last);
  });

  it("encodes an epic name", async () => {
    serves({ epics: [epic("M1 & M2", { todo: 1 })], noEpic: progress() });
    await mount();
    expect(hrefOf("M1 & M2")).toBe("/kanban?project=ACP&epic=M1+%26+M2");
  });
});

describe("Epics dialog", () => {
  const nameField = () => screen.getByLabelText("Name") as HTMLInputElement;
  const alert = () => screen.queryByRole("alert")?.textContent ?? null;
  const type = (value: string) =>
    act(async () => {
      fireEvent.change(nameField(), { target: { value } });
    });
  const press = (name: string) =>
    act(async () => {
      fireEvent.click(screen.getByRole("button", { name }));
    });
  const openNew = () => press("New epic");
  async function openEdit(name: string) {
    await press(`Actions for ${name}`);
    await act(async () => {
      fireEvent.click(within(rowOf(name)).getByRole("button", { name: "Edit" }));
    });
  }

  it("creates an epic with a name and a description", async () => {
    mockApi.epics.create.mockResolvedValue(epic("Realtime"));
    await mount();
    await openNew();
    expect(screen.getByRole("dialog").textContent).toContain("New epic");
    expect(alert()).toBeNull();
    await type("  Realtime ");
    await act(async () => {
      fireEvent.change(screen.getByLabelText(/Description/), { target: { value: "Live updates" } });
    });
    await press("Create");
    expect(mockApi.epics.create).toHaveBeenCalledWith({ projectId: "ACP", name: "Realtime", description: "Live updates" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mockApi.epics.list).toHaveBeenCalledTimes(2);
  });

  it("checks for a duplicate as you type, trimmed and ignoring case", async () => {
    await mount();
    await openNew();
    await type(" graph ");
    expect(alert()).toBe('This project already has an epic called "Graph".');
    // Complete epics count too.
    await type("LAUNCH");
    expect(alert()).toBe('This project already has an epic called "Launch".');
    await type("Graphs");
    expect(alert()).toBeNull();
  });

  it("reserves none and asks for a name", async () => {
    await mount();
    await openNew();
    await type(" NONE ");
    expect(alert()).toBe('"none" is reserved for tickets without an epic.');
    await type("  ");
    expect(alert()).toBe("Enter a name");
  });

  it("keeps the dialog open and focuses the field when pressed with an error", async () => {
    await mount();
    await openNew();
    const create = screen.getByRole("button", { name: "Create" }) as HTMLButtonElement;
    expect(create.disabled).toBe(false);
    await press("Create");
    expect(alert()).toBe("Enter a name");
    expect(document.activeElement).toBe(nameField());
    await type("store");
    (document.activeElement as HTMLElement).blur();
    await press("Create");
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(document.activeElement).toBe(nameField());
    expect(mockApi.epics.create).not.toHaveBeenCalled();
  });

  it("lets an epic be renamed to itself in other capitals", async () => {
    mockApi.epics.update.mockResolvedValue(epic("GRAPH"));
    await mount();
    await openEdit("Graph");
    expect(screen.getByRole("dialog").textContent).toContain("Edit epic");
    expect(nameField().value).toBe("Graph");
    expect((screen.getByLabelText(/Description/) as HTMLInputElement).value).toBe("Dependency graph home page");
    await type("GRAPH");
    expect(alert()).toBeNull();
    await press("Save");
    expect(mockApi.epics.update).toHaveBeenCalledWith("e-Graph", { name: "GRAPH", description: "Dependency graph home page" });
    await settleHistory();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(params().has("epic")).toBe(false);
  });

  it("still refuses renaming to another epic's name", async () => {
    await mount();
    await openEdit("Graph");
    await type("store");
    expect(alert()).toBe('This project already has an epic called "Store".');
  });

  it("shows a server error in the same place and keeps the input", async () => {
    mockApi.epics.create.mockRejectedValue(
      new Error('API error 400: {"error":"This project already has an epic called \\"Realtime\\"."}'),
    );
    await mount();
    await openNew();
    await type("realtime");
    await press("Create");
    expect(alert()).toBe('This project already has an epic called "Realtime".');
    expect(nameField().value).toBe("realtime");
    expect(document.activeElement).toBe(nameField());
    expect(screen.getByRole("dialog")).toBeTruthy();
    // Typing again hands the message back to the checks as you type.
    await type("realtime2");
    expect(alert()).toBeNull();
  });
});

describe("Epics delete", () => {
  async function openDelete(name: string) {
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: `Actions for ${name}` }));
    });
    await act(async () => {
      fireEvent.click(within(rowOf(name)).getByRole("button", { name: "Delete" }));
    });
    return screen.getByRole("alertdialog");
  }

  it("confirms with the ticket count, then deletes", async () => {
    mockApi.epics.delete.mockResolvedValue(undefined);
    await mount();
    const dialog = await openDelete("Graph");
    expect(within(dialog).getByRole("heading").textContent).toBe('Delete "Graph"?');
    expect(dialog.textContent).toContain("Its 4 tickets stay as they are and move to No epic. This can't be undone.");
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    });
    expect(mockApi.epics.delete).toHaveBeenCalledWith("e-Graph");
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("says an empty epic has no tickets, and Cancel deletes nothing", async () => {
    await mount();
    const dialog = await openDelete("Ideas");
    expect(within(dialog).getByRole("heading").textContent).toBe('Delete "Ideas"?');
    expect(dialog.textContent).toContain("It has no tickets.");
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    });
    expect(mockApi.epics.delete).not.toHaveBeenCalled();
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("warns that the epic's documents are deleted for good, even with no tickets", async () => {
    serves({ ...LIST, epics: LIST.epics.map((e) => (e === IDEAS ? { ...e, documentCount: 2 } : e)) });
    await mount();
    const dialog = await openDelete("Ideas");
    expect(dialog.textContent).toContain("It has no tickets. Its 2 documents are deleted for good. This can't be undone.");
  });

  it("shows the server's message when it refuses the delete, and stays open", async () => {
    mockApi.epics.delete.mockRejectedValue(new Error('API error 404: {"error":"epic not found"}'));
    await mount();
    const dialog = await openDelete("Graph");
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    });
    expect(within(screen.getByRole("alertdialog")).getByRole("alert").textContent).toBe("epic not found");
  });
});

describe("Epics dialog focus", () => {
  const tab = (shiftKey = false) =>
    act(async () => {
      fireEvent.keyDown(document.activeElement!, { key: "Tab", shiftKey });
    });
  const escape = () =>
    act(async () => {
      fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    });
  async function openFromMenu(name: string, action: "Edit" | "Delete") {
    const toggle = screen.getByRole("button", { name: `Actions for ${name}` });
    toggle.focus();
    await act(async () => {
      fireEvent.click(toggle);
    });
    await act(async () => {
      fireEvent.click(within(rowOf(name)).getByRole("button", { name: action }));
    });
    return toggle;
  }

  it("focuses the name field on opening under StrictMode, and gives focus back on close", async () => {
    await mount(undefined, { strict: true });
    const newEpic = screen.getByRole("button", { name: "New epic" });
    newEpic.focus();
    await act(async () => {
      fireEvent.click(newEpic);
    });
    await settle();
    expect(document.activeElement).toBe(screen.getByLabelText("Name"));
    await escape();
    expect(document.activeElement).toBe(newEpic);

    const toggle = await openFromMenu("Store", "Edit");
    await settle();
    expect(document.activeElement).toBe(screen.getByLabelText("Name"));
    await escape();
    await settleHistory();
    expect(document.activeElement).toBe(toggle);

    await openFromMenu("Store", "Delete");
    await settle();
    expect(document.activeElement).toBe(
      within(screen.getByRole("alertdialog")).getByRole("button", { name: "Cancel" }),
    );
  });

  it("keeps Tab inside the New epic dialog and gives focus back to New epic on Escape", async () => {
    await mount();
    const newEpic = screen.getByRole("button", { name: "New epic" });
    newEpic.focus();
    await act(async () => {
      fireEvent.click(newEpic);
    });
    const dialog = screen.getByRole("dialog");
    const buttons = within(dialog).getAllByRole("button");
    const [close, create] = [buttons[0], buttons.at(-1)!];
    expect(close.getAttribute("aria-label")).toBe("Close");
    expect(create.textContent).toBe("Create");
    // Tab from the last control wraps to the first, Shift+Tab back again.
    create.focus();
    await tab();
    expect(document.activeElement).toBe(close);
    await tab(true);
    expect(document.activeElement).toBe(create);
    await escape();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(newEpic);
  });

  it("gives focus back to the row's ⋯ when Edit closes, by Cancel or by saving", async () => {
    mockApi.epics.update.mockResolvedValue(STORE);
    await mount();
    let toggle = await openFromMenu("Store", "Edit");
    expect(screen.getByRole("dialog")).toBeTruthy();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    });
    await settleHistory();
    expect(document.activeElement).toBe(toggle);

    toggle = await openFromMenu("Store", "Edit");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
    });
    await settleHistory();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Actions for Store" }));
  });

  it("keeps Tab inside the delete confirmation, and Escape gives focus back to ⋯", async () => {
    await mount();
    const toggle = await openFromMenu("Graph", "Delete");
    const dialog = screen.getByRole("alertdialog");
    const buttons = within(dialog).getAllByRole("button");
    // Cancel has focus to start with.
    expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "Cancel" }));
    buttons.at(-1)!.focus();
    await tab();
    expect(document.activeElement).toBe(buttons[0]);
    await tab(true);
    expect(document.activeElement).toBe(buttons.at(-1));
    await escape();
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(document.activeElement).toBe(toggle);
  });

  it("gives focus to New epic after a delete, since the row's ⋯ goes with it", async () => {
    mockApi.epics.delete.mockResolvedValue(undefined);
    await mount();
    await openFromMenu("Graph", "Delete");
    serves({ ...LIST, epics: LIST.epics.filter((e) => e !== GRAPH) });
    await act(async () => {
      fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }));
    });
    await settle();
    expect(rowNames()).not.toContain("Graph");
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "New epic" }));
  });
});

describe("Epics live refresh", () => {
  it("reloads the epics on the shared events stream, keeping an open dialog", async () => {
    await mount();
    expect(live.refresh.length).toBeGreaterThan(0);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "New epic" }));
    });
    await act(async () => {
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Payments" } });
    });
    serves({ ...LIST, epics: [...LIST.epics, epic("Payments", { todo: 1 }, "2026-09-23T12:00:00Z")] });
    const calls = mockApi.epics.list.mock.calls.length;
    await act(async () => {
      live.refresh.at(-1)!();
    });
    await settle();
    expect(mockApi.epics.list.mock.calls.length).toBe(calls + 1);
    expect(rowNames()).toContain("Payments");
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Payments");
    expect(screen.getByRole("alert").textContent).toBe('This project already has an epic called "Payments".');
  });
});

describe("Epics modal", () => {
  const withDocs = (name: string, documentCount: number) =>
    ({ ...LIST, epics: LIST.epics.map((e) => (e.name === name ? { ...e, documentCount } : e)) });
  const modal = () => screen.queryByRole("dialog", { name: "Edit epic" });
  const plan: DocumentMeta = {
    id: "d1", epicId: "e-Graph", name: "Rollout plan", format: "markdown", size: 7, revision: 1, createdAt: "", updatedAt: "",
  };

  it("shows each epic's document count on a paperclip beside the row's link", async () => {
    serves(withDocs("Graph", 2));
    await mount();
    const paperclip = within(rowOf("Graph")).getByRole("button", { name: "Documents of Graph (2)" });
    expect(paperclip.textContent).toBe("2");
    expect(paperclip.closest("a")).toBeNull();
    expect(within(rowOf("Store")).getByRole("button", { name: "Documents of Store (0)" })).toBeTruthy();
    // The No epic row has none.
    const noEpicRow = rowOf("No epic");
    expect(within(noEpicRow).queryByRole("button", { name: /Documents of/ })).toBeNull();
  });

  it("opens the epic modal from the row's paperclip, with the epic in the URL, and Back closes it", async () => {
    serves(withDocs("Graph", 2));
    await mount();
    const paperclip = screen.getByRole("button", { name: "Documents of Graph (2)" });
    paperclip.focus();
    await act(async () => {
      fireEvent.click(paperclip);
    });
    expect(modal()).toBeTruthy();
    expect(params().get("epic")).toBe("Graph");
    expect(mockApi.documents.list).toHaveBeenCalledWith({ epicId: "e-Graph" });

    await act(async () => {
      window.history.back();
    });
    await settleHistory();
    expect(modal()).toBeNull();
    expect(params().has("epic")).toBe(false);
    expect(document.activeElement).toBe(paperclip);

    await act(async () => {
      window.history.forward();
    });
    await settleHistory();
    expect(modal()).toBeTruthy();
  });

  it("opens the epic named in the URL on load, and closing drops it in place", async () => {
    await mount("/epics?project=ACP&epic=graph");
    expect(modal()).toBeTruthy();
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Graph");
    await act(async () => {
      fireEvent.click(within(modal()!).getByRole("button", { name: "Close" }));
    });
    await settleHistory();
    expect(modal()).toBeNull();
    expect(window.location.search).toBe("?project=ACP");
  });

  it("drops an epic the shown project does not have, opening nothing", async () => {
    await mount("/epics?project=ACP&epic=Elsewhere&doc=Plan.md");
    await settleHistory();
    expect(modal()).toBeNull();
    expect(window.location.search).toBe("?project=ACP");
  });

  it("opens an epic's document from a pasted link", async () => {
    mockApi.documents.list.mockResolvedValue([plan]);
    mockApi.documents.get.mockResolvedValue({ ...plan, content: "# Steps" });
    await mount("/epics?project=ACP&epic=Graph&doc=Rollout%20plan.md");
    expect(await screen.findByRole("heading", { name: "Steps" })).toBeTruthy();
    expect(modal()).toBeTruthy();
  });

  it("keeps the epic and its document through two Backs with unsaved text, and Keep editing puts both back", async () => {
    mockApi.documents.list.mockResolvedValue([plan]);
    mockApi.documents.get.mockResolvedValue({ ...plan, content: "# Steps" });
    await mount();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Documents of Graph (0)" }));
    });
    await act(async () => {
      fireEvent.click(await screen.findByRole("button", { name: "Rollout plan.md" }));
    });
    await act(async () => {
      fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    });
    await act(async () => {
      fireEvent.change(screen.getByRole("textbox", { name: "Document content" }), { target: { value: "# Mine" } });
    });
    expect(params().get("doc")).toBe("Rollout plan.md");

    await act(async () => {
      window.history.back();
    });
    await settleHistory();
    await act(async () => {
      window.history.back();
    });
    await settleHistory();
    expect(params().has("epic")).toBe(false);
    expect(modal()).toBeTruthy();
    const ask = screen.getByRole("alertdialog", { name: "Discard your changes?" });
    expect((screen.getByRole("textbox", { name: "Document content" }) as HTMLTextAreaElement).value).toBe("# Mine");

    await act(async () => {
      fireEvent.click(within(ask).getByRole("button", { name: "Keep editing" }));
    });
    await settleHistory();
    expect(params().get("epic")).toBe("Graph");
    expect(params().get("doc")).toBe("Rollout plan.md");
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect((screen.getByRole("textbox", { name: "Document content" }) as HTMLTextAreaElement).value).toBe("# Mine");

    // Two Backs again, and Discard this time: nothing is left open.
    for (let i = 0; i < 2; i++) {
      await act(async () => {
        window.history.back();
      });
      await settleHistory();
    }
    await act(async () => {
      fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Discard" }));
    });
    await settleHistory();
    expect(modal()).toBeNull();
    expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull();
    expect(window.location.search).toBe("?project=ACP");
  });

  it("saves a rename, closes, and the row shows the new name", async () => {
    mockApi.epics.update.mockResolvedValue({ ...GRAPH, name: "Graph view" });
    await mount();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Documents of Graph (0)" }));
    });
    serves({ ...LIST, epics: LIST.epics.map((e) => (e === GRAPH ? { ...e, name: "Graph view" } : e)) });
    await act(async () => {
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Graph view" } });
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
    });
    await settleHistory();
    expect(modal()).toBeNull();
    expect(params().has("epic")).toBe(false);
    expect(rowNames()).toContain("Graph view");
  });
});
