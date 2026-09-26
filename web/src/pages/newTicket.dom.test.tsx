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
// view, and on the epic the view filters by. The project and epic stay
// changeable, and a project the user picks stays picked when the projects
// reload. The API is mocked.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn(), create: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  board: { get: vi.fn() },
}));

vi.mock("../api/client", () => ({ api: mockApi }));

import Board from "./Board";
import Tickets from "./Tickets";

const project = (prefix: string, name = prefix, status = "active"): Project => ({
  id: `p-${prefix}`,
  name,
  prefix,
  description: "",
  icon: "",
  color: "",
  status,
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

// Each view, with the button a test opens the form from, and every way the
// view has of opening it: Table's New Ticket, and each Kanban column's +.
const views = [
  [
    "Kanban",
    () => <Board />,
    "/kanban",
    () => within(screen.getByRole("heading", { name: STATUS_LABELS.todo }).parentElement!).getByRole("button"),
    () => screen.getAllByRole("button", { name: /^New ticket in / }),
  ],
  [
    "Table",
    () => <Tickets />,
    "/table",
    () => screen.getByRole("button", { name: "New Ticket" }),
    () => [screen.getByRole("button", { name: "New Ticket" })],
  ],
] as const;

const form = () => screen.getByRole("heading", { name: "New Ticket" }).closest("form")!;
const projectSelect = () => within(form()).getByLabelText("Project") as HTMLSelectElement;

beforeEach(() => {
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
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
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Elsewhere" } });
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
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Here" } });
    await act(async () => screen.getByRole("button", { name: "Create Ticket" }).click());
    expect(mockApi.tickets.create).toHaveBeenCalledWith(
      expect.objectContaining({ projectId: "p-LDR", title: "Here" }),
    );
  });
});

describe.each(views)("%s's new-ticket form accessibility", (_name, page, path, newTicketButton) => {
  it("is a dialog with an accessible name, and every field labelled", async () => {
    await mount(page(), `${path}?project=LDR`);
    await act(async () => newTicketButton().click());

    const dialog = screen.getByRole("dialog", { name: "New Ticket" });
    expect(dialog.getAttribute("aria-modal")).toBe("true");

    expect(within(dialog).getByLabelText("Project")).toBeInstanceOf(HTMLSelectElement);
    expect(within(dialog).getByLabelText("Epic")).toBeInstanceOf(HTMLSelectElement);
    expect(within(dialog).getByLabelText("Title")).toBeInstanceOf(HTMLInputElement);
    expect(within(dialog).getByLabelText("Description")).toBeInstanceOf(HTMLTextAreaElement);
    expect(within(dialog).getByLabelText("Priority")).toBeInstanceOf(HTMLSelectElement);
    expect(within(dialog).getByLabelText("Due Date")).toBeInstanceOf(HTMLInputElement);
    expect(within(dialog).getByLabelText("Labels")).toBeInstanceOf(HTMLInputElement);
  });
});

describe.each(views)("%s's ways to open the new-ticket form", (_name, page, path, _button, openers) => {
  const count = path === "/kanban" ? Object.keys(STATUS_LABELS).length : 1;
  // Each one is disabled with the reason as its title, which hovering shows
  // and a screen reader announces as its description; a click opens nothing.
  const expectDisabled = async (reason: string) => {
    const all = openers();
    expect(all).toHaveLength(count);
    for (const opener of all) {
      expect(opener.getAttribute("aria-disabled")).toBe("true");
      expect(opener.getAttribute("title")).toBe(reason);
      await act(async () => opener.click());
    }
    expect(screen.getAllByRole("button", { description: reason })).toHaveLength(count);
    expect(screen.queryByRole("dialog")).toBeNull();
  };

  it("are enabled, with no reason, on an active project", async () => {
    await mount(page(), `${path}?project=LDR`);
    expect(openers()).toHaveLength(count);
    for (const opener of openers()) {
      expect(opener.getAttribute("aria-disabled")).toBeNull();
      expect(opener.getAttribute("title")).toBeNull();
    }
  });

  it("are disabled on an archived project the URL keeps selected, naming it", async () => {
    serves([...PROJECTS, project("OLD", "Old stuff", "archived")]);
    await mount(page(), `${path}?project=OLD`);
    await expectDisabled("Old stuff is archived. Unarchive it to add tickets.");
  });

  it("are disabled when no project is active", async () => {
    serves([project("OLD", "Old stuff", "archived")]);
    await mount(page(), path);
    await expectDisabled("Create a project to add tickets.");
  });

  it("are disabled when there are no projects at all", async () => {
    serves([]);
    await mount(page(), path);
    await expectDisabled("Create a project to add tickets.");
  });

  it("wait for the projects to load, then open the form on the view's project", async () => {
    let arrive!: (projects: Project[]) => void;
    serves(new Promise<Project[]>((resolve) => (arrive = resolve)));
    await mount(page(), `${path}?project=LDR`);
    await expectDisabled("Loading projects…");

    arrive(PROJECTS);
    await settle();
    await act(async () => openers()[0].click());
    expect(projectSelect().value).toBe("p-LDR");
  });
});

describe("Table's new-ticket form and the projects loading", () => {
  it("never opens before they arrive, so it never shows 'No projects'", async () => {
    let arrive!: (projects: Project[]) => void;
    serves(new Promise<Project[]>((resolve) => (arrive = resolve)));
    await mount(<Tickets />, "/table?project=LDR");
    await act(async () => screen.getByRole("button", { name: "New Ticket" }).click());
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByText("No projects")).toBeNull();

    arrive(PROJECTS);
    await settle();
    await act(async () => screen.getByRole("button", { name: "New Ticket" }).click());
    expect([...projectSelect().options].map((o) => o.textContent)).toEqual(["ACP", "LDR"]);
    expect(projectSelect().value).toBe("p-LDR");
  });

  it("keeps a project picked after they arrive when they reload", async () => {
    let arrive!: (projects: Project[]) => void;
    serves(new Promise<Project[]>((resolve) => (arrive = resolve)));
    await mount(<Tickets />, "/table?project=LDR");
    arrive(PROJECTS);
    await settle();
    await act(async () => screen.getByRole("button", { name: "New Ticket" }).click());
    fireEvent.change(projectSelect(), { target: { value: "p-ACP" } });
    await liveChange();
    expect(projectSelect().value).toBe("p-ACP");
  });
});

// Each project's epics, answered whether asked by prefix (the filter bar) or
// by id (the form).
const NO_PROGRESS = { counts: {}, total: 0, complete: false, lastActivityAt: null };
const EPICS: Record<string, string[]> = { ACP: ["Graph"], LDR: ["Views", "Billing"] };
function epicsOf(ref: string) {
  const prefix = ref.replace(/^p-/, "");
  return {
    epics: (EPICS[prefix] ?? []).map((name) => ({
      ...NO_PROGRESS,
      id: `e-${prefix}-${name}`,
      projectId: `p-${prefix}`,
      name,
      createdAt: "",
      updatedAt: "",
    })),
    noEpic: NO_PROGRESS,
  };
}
function servesEpics() {
  mockApi.epics.list.mockImplementation(async (ref: string) => epicsOf(ref));
}

describe.each(views)("%s's new-ticket form epic", (_name, page, path, newTicketButton) => {
  const epicSelect = () => within(form()).getByLabelText("Epic") as HTMLSelectElement;
  const create = async (title = "Here") => {
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: title } });
    await act(async () => screen.getByRole("button", { name: "Create Ticket" }).click());
    return mockApi.tickets.create.mock.calls[0][0];
  };

  beforeEach(servesEpics);

  it("offers No epic and the form's project's epics by name", async () => {
    await mount(page(), `${path}?project=LDR`);
    await act(async () => newTicketButton().click());
    await settle();
    expect([...epicSelect().options].map((o) => o.textContent)).toEqual(["No epic", "Billing", "Views"]);
  });

  it("starts on the epic the view filters by, and creates the ticket in it", async () => {
    await mount(page(), `${path}?project=LDR&epic=views`);
    await act(async () => newTicketButton().click());
    await settle();
    expect(epicSelect().value).toBe("e-LDR-Views");
    expect(await create()).toMatchObject({ projectId: "p-LDR", epic: "e-LDR-Views" });
  });

  it.each([
    ["no epic filter", ""],
    ["the filter for tickets without an epic", "&epic=none"],
  ])("starts on no epic with %s, and sends none", async (_what, query) => {
    await mount(page(), `${path}?project=LDR${query}`);
    await act(async () => newTicketButton().click());
    await settle();
    expect(epicSelect().value).toBe("");
    const sent = await create();
    expect(sent.projectId).toBe("p-LDR");
    expect(sent.epic).toBeUndefined();
  });

  it("still lets another epic, or none, be chosen", async () => {
    await mount(page(), `${path}?project=LDR&epic=Views`);
    await act(async () => newTicketButton().click());
    await settle();
    fireEvent.change(epicSelect(), { target: { value: "e-LDR-Billing" } });
    expect((await create()).epic).toBe("e-LDR-Billing");
  });

  it("offers the new project's epics, on no epic, when another project is chosen", async () => {
    await mount(page(), `${path}?project=LDR&epic=Views`);
    await act(async () => newTicketButton().click());
    await settle();
    fireEvent.change(projectSelect(), { target: { value: "p-ACP" } });
    await settle();
    expect([...epicSelect().options].map((o) => o.textContent)).toEqual(["No epic", "Graph"]);
    expect(epicSelect().value).toBe("");
    const sent = await create();
    expect(sent).toMatchObject({ projectId: "p-ACP" });
    expect(sent.epic).toBeUndefined();
  });

  it("resets a picked epic to No epic on a switch of project, never offering the last project's epics", async () => {
    await mount(page(), `${path}?project=LDR`);
    await act(async () => newTicketButton().click());
    await settle();
    fireEvent.change(epicSelect(), { target: { value: "e-LDR-Billing" } });
    expect(epicSelect().value).toBe("e-LDR-Billing");

    // ACP's epics are still on their way when the project changes.
    let arrive!: () => void;
    mockApi.epics.list.mockImplementation((ref: string) =>
      ref === "p-ACP" ? new Promise((resolve) => (arrive = () => resolve(epicsOf(ref)))) : Promise.resolve(epicsOf(ref)),
    );
    fireEvent.change(projectSelect(), { target: { value: "p-ACP" } });
    await settle();
    expect([...epicSelect().options].map((o) => o.textContent)).toEqual(["No epic"]);
    expect(epicSelect().value).toBe("");

    await act(async () => arrive());
    await settle();
    expect([...epicSelect().options].map((o) => o.textContent)).toEqual(["No epic", "Graph"]);
    expect(epicSelect().value).toBe("");
    const sent = await create();
    expect(sent.projectId).toBe("p-ACP");
    expect(sent.epic).toBeUndefined();
  });
});
