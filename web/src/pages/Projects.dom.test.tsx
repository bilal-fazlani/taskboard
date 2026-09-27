// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Project } from "../api/client";

// The Projects page with the API mocked: the card marker for agent
// instructions, the trash button and its confirm dialog, the form's
// Description / Agent instructions tabs, the entries column, and ?project=.

const mockApi = vi.hoisted(() => ({
  projects: {
    list: vi.fn(),
    get: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  },
  entries: { list: vi.fn(), createNote: vi.fn() },
}));
vi.mock("../api/client", () => ({ api: mockApi }));
vi.mock("../hooks/useLiveRefresh", () => ({ useLiveRefresh: () => {} }));

import { BrowserRouter } from "react-router-dom";
import Projects from "./Projects";

// No icon by default: the API leaves an empty one out of the JSON
// altogether, so a fixture for the ordinary case omits it too.
const project = (prefix: string, extra: Partial<Project> = {}): Project =>
  ({
    id: `p-${prefix}`,
    name: `${prefix} project`,
    prefix,
    description: `What ${prefix} is.`,
    color: "#3b82f6",
    status: "active",
    createdAt: "",
    updatedAt: "",
    ...extra,
  }) as Project;

const WITH = project("ACP", { hasAgentInstructions: true });
const WITHOUT = project("HOME", { hasAgentInstructions: false });

// jsdom has no ResizeObserver; this one reports every observed element when
// a test says the browser has laid the page out.
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

// jsdom lays nothing out, so a card description measures as if its text ran
// in lines of `textWidth` pixels, 7px a character and 23px a line. Clamped
// (line-clamp-4) it shows at most four lines; its scroll height is always
// every line.
const CHAR = 7;
const LINE = 23;
let textWidth = 280;

function descriptionLines(el: HTMLElement) {
  return Math.max(1, Math.ceil(((el.textContent ?? "").length * CHAR) / textWidth));
}

const layout = {
  clientHeight(this: HTMLElement) {
    if (!this.classList.contains("prose-card")) return 0;
    const lines = descriptionLines(this);
    return (this.classList.contains("line-clamp-4") ? Math.min(lines, 4) : lines) * LINE;
  },
  scrollHeight(this: HTMLElement) {
    return this.classList.contains("prose-card") ? descriptionLines(this) * LINE : 0;
  },
};

// Says the browser has laid the page out, at the current textWidth.
function relayout() {
  act(() => {
    for (const observer of FakeResizeObserver.instances) observer.report();
  });
}

function cardOf(name: string) {
  return screen.getByText(name).parentElement!;
}

function showMore(card: HTMLElement) {
  return within(card).queryByRole("button", { name: /Show more/ });
}

function showLess(card: HTMLElement) {
  return within(card).queryByRole("button", { name: /Show less/ });
}

async function renderPage() {
  render(<Projects />, { wrapper: BrowserRouter });
  await screen.findByText("ACP project");
}

// Opens the edit form from a card and waits for the fetched instructions.
async function openEdit(name: string) {
  await act(async () => {
    fireEvent.click(screen.getByText(name));
  });
  return screen.getByRole("form", { name: "Edit Project" });
}

function tab(form: HTMLElement, name: RegExp) {
  return within(form).getByRole("tab", { name });
}

function textarea(form: HTMLElement) {
  return within(form).getByRole("tabpanel").querySelector("textarea")!;
}

beforeEach(() => {
  FakeResizeObserver.instances = [];
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = FakeResizeObserver;
  textWidth = 280;
  for (const [name, get] of Object.entries(layout)) {
    Object.defineProperty(HTMLElement.prototype, name, { configurable: true, get });
  }
  mockApi.projects.list.mockResolvedValue([WITH, WITHOUT]);
  mockApi.projects.get.mockImplementation(async (id: string) =>
    id === WITH.id
      ? { ...WITH, hasAgentInstructions: undefined, agentInstructions: "Run the tests before landing." }
      : { ...WITHOUT, hasAgentInstructions: undefined, agentInstructions: "" },
  );
  mockApi.projects.create.mockResolvedValue(project("NEW"));
  mockApi.projects.update.mockResolvedValue(WITH);
  mockApi.entries.list.mockResolvedValue({ entries: [], total: 0, hasMore: false, agents: {} });
  window.history.replaceState(null, "", "/projects");
});

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
  vi.clearAllMocks();
  delete (globalThis as unknown as { ResizeObserver?: unknown }).ResizeObserver;
  // Uncovers jsdom's own getters on Element.prototype again.
  for (const name of Object.keys(layout)) {
    delete (HTMLElement.prototype as unknown as Record<string, unknown>)[name];
  }
});

describe("project cards", () => {
  it("mark only the projects that have agent instructions", async () => {
    await renderPage();
    const marker = screen.getByTestId("project-agent-instructions");
    expect(marker.textContent).toBe("Agent instructions");
    expect(marker.getAttribute("title")).toBe("Has agent instructions");
    expect(screen.getAllByTestId("project-agent-instructions")).toHaveLength(1);
    // The marker sits on the ACP card, not HOME's.
    const acpCard = screen.getByText("ACP project").parentElement!;
    const homeCard = screen.getByText("HOME project").parentElement!;
    expect(within(acpCard).queryByTestId("project-agent-instructions")).not.toBeNull();
    expect(within(homeCard).queryByTestId("project-agent-instructions")).toBeNull();
  });

  it("keep the colour dot from shrinking when the row wraps", async () => {
    await renderPage();
    const row = screen.getByTestId("project-agent-instructions").parentElement!;
    expect(row.className).toContain("flex-wrap");
    const dot = row.querySelector("span.rounded-full")!;
    expect(dot.className).toContain("shrink-0");
  });

  it("shows a project's icon before its name", async () => {
    mockApi.projects.list.mockResolvedValue([project("ICON", { name: "Iconic", icon: "🧭" })]);
    render(<Projects />, { wrapper: BrowserRouter });
    await screen.findByText("Iconic");
    expect(screen.getByTestId("project-icon").textContent).toBe("🧭");
  });

  it("shows only the name, with no icon element, for a project without an icon", async () => {
    await renderPage();
    // Neither fixture in this file's default project() sets an icon.
    expect(screen.queryByTestId("project-icon")).toBeNull();
  });
});

describe("a project card's trash button", () => {
  function trash(name: string) {
    return screen.getByRole("button", { name: `Delete project ${name}` });
  }

  it("is named for its project and titled Delete project", async () => {
    await renderPage();
    expect(trash("ACP project").getAttribute("title")).toBe("Delete project");
    expect(trash("HOME project").getAttribute("title")).toBe("Delete project");
    // Its icon adds nothing to the name.
    expect(trash("ACP project").querySelector("svg")!.getAttribute("aria-hidden")).toBe("true");
  });

  it("rests faint but visible, and is reachable by Tab", async () => {
    await renderPage();
    const button = trash("ACP project");
    expect(button.tagName).toBe("BUTTON");
    expect(button.getAttribute("type")).toBe("button");
    expect(button.tabIndex).toBe(0);
    expect((button as HTMLButtonElement).disabled).toBe(false);
    expect(button.className).toContain("opacity-45");
    expect(button.className).not.toContain("opacity-0");
    button.focus();
    expect(document.activeElement).toBe(button);
  });

  it("turns red with a focus ring on hover or keyboard focus", async () => {
    await renderPage();
    const classes = trash("ACP project").className.split(/\s+/);
    for (const state of ["hover", "focus-visible"]) {
      expect(classes).toContain(`${state}:text-red-400`);
      expect(classes).toContain(`${state}:opacity-100`);
      expect(classes).toContain(`${state}:ring-2`);
      expect(classes).toContain(`${state}:ring-blue-500`);
    }
  });
});

describe("deleting a project", () => {
  const trash = (name: string) => screen.getByRole("button", { name: `Delete project ${name}` });

  async function openConfirm(name = "ACP project") {
    await renderPage();
    fireEvent.click(trash(name));
    return screen.getByRole("alertdialog");
  }

  it("asks first, naming the project and what goes with it", async () => {
    const dialog = await openConfirm();
    expect(mockApi.projects.delete).not.toHaveBeenCalled();
    // The card's own click, which opens the edit form, is not triggered.
    expect(screen.queryByRole("form", { name: "Edit Project" })).toBeNull();
    expect(within(dialog).getByRole("heading").textContent).toBe("Delete ACP project?");
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    const labelledBy = document.getElementById(dialog.getAttribute("aria-labelledby")!)!;
    expect(labelledBy.textContent).toBe("Delete ACP project?");
    const describedBy = document.getElementById(dialog.getAttribute("aria-describedby")!)!;
    expect(describedBy.textContent).toBe(
      "ACP and all its tickets, epics and documents disappear from the board, and from the API, MCP and CLI. This can't be undone.",
    );
  });

  it("starts with focus on Cancel", async () => {
    await openConfirm();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));
  });

  it("cancels on Escape without deleting, and gives focus back to the trash button", async () => {
    await openConfirm();
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(mockApi.projects.delete).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(trash("ACP project"));
  });

  it("cancels on a click beside the box, or on Cancel", async () => {
    const dialog = await openConfirm();
    // The backdrop is the dialog's sibling, behind it.
    fireEvent.click(dialog.previousElementSibling!);
    expect(screen.queryByRole("alertdialog")).toBeNull();

    fireEvent.click(trash("ACP project"));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(mockApi.projects.delete).not.toHaveBeenCalled();
    // Neither opened the edit form behind it.
    expect(screen.queryByRole("form", { name: "Edit Project" })).toBeNull();
  });

  it("deletes on confirm, busy meanwhile, then closes and reloads the list", async () => {
    let finish: () => void = () => {};
    mockApi.projects.delete.mockReturnValue(new Promise<void>((r) => (finish = r)));
    const dialog = await openConfirm("HOME project");
    expect(mockApi.projects.list).toHaveBeenCalledTimes(1);

    const del = within(dialog).getByRole("button", { name: "Delete" }) as HTMLButtonElement;
    await act(async () => {
      fireEvent.click(del);
    });
    expect(mockApi.projects.delete).toHaveBeenCalledWith(WITHOUT.id);
    expect(del.disabled).toBe(true);
    expect(dialog.getAttribute("aria-busy")).toBe("true");
    // A second click while busy sends nothing more.
    fireEvent.click(del);
    expect(mockApi.projects.delete).toHaveBeenCalledTimes(1);

    mockApi.projects.list.mockResolvedValue([WITH]);
    await act(async () => finish());
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(mockApi.projects.list).toHaveBeenCalledTimes(2);
    expect(screen.queryByText("HOME project")).toBeNull();
    expect(screen.getByText("ACP project")).toBeTruthy();
  });

  it("stays open with the server's reason when the delete fails", async () => {
    mockApi.projects.delete.mockRejectedValueOnce(new Error('API error 500: {"error":"database is locked"}'));
    const dialog = await openConfirm();
    const del = within(dialog).getByRole("button", { name: "Delete" }) as HTMLButtonElement;
    await act(async () => {
      fireEvent.click(del);
    });
    expect(screen.getByRole("alertdialog")).toBe(dialog);
    const alert = within(dialog).getByRole("alert");
    expect(alert.textContent).toBe("database is locked");
    expect(alert.className).toContain("text-red-400");
    expect(del.disabled).toBe(false);
    expect(dialog.getAttribute("aria-busy")).toBeNull();
    expect(mockApi.projects.list).toHaveBeenCalledTimes(1);

    // Trying again can still succeed, and then the box closes.
    mockApi.projects.delete.mockResolvedValueOnce(undefined);
    await act(async () => {
      fireEvent.click(del);
    });
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(mockApi.projects.list).toHaveBeenCalledTimes(2);
  });

  it("says the project was not deleted when the server gives no reason", async () => {
    mockApi.projects.delete.mockRejectedValueOnce(new Error("Failed to fetch"));
    const dialog = await openConfirm();
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    });
    expect(within(dialog).getByRole("alert").textContent).toBe("The project was not deleted.");
  });
});

describe("a project's status", () => {
  it("is not shown on its card", async () => {
    await renderPage();
    expect(screen.queryByText(/^active$/i)).toBeNull();
  });

  it("has no select on the edit form, and a save sends none", async () => {
    await renderPage();
    const form = await openEdit("ACP project");
    expect(within(form).queryByRole("combobox")).toBeNull();
    expect(within(form).queryByText("Status")).toBeNull();
    expect(within(form).queryByText("Archived")).toBeNull();
    await act(async () => {
      fireEvent.click(within(form).getByRole("button", { name: "Save Changes" }));
    });
    expect("status" in mockApi.projects.update.mock.calls[0][1]).toBe(false);
  });
});

describe("a project card's Show more", () => {
  // At the starting 280px, 40 characters a line: SHORT takes one line, FITS
  // exactly the four the clamp shows, LONG ten.
  const SHORT = project("SHRT", { description: "Tracks the household chores." });
  const FITS = project("FITS", { description: "x".repeat(160) });
  const LONG = project("LONG", { description: "y".repeat(400) });

  async function renderCards() {
    mockApi.projects.list.mockResolvedValue([SHORT, FITS, LONG]);
    render(<Projects />, { wrapper: BrowserRouter });
    await screen.findByText("LONG project");
    relayout();
  }

  it("shows only on a description the clamp cuts off", async () => {
    await renderCards();
    expect(showMore(cardOf("SHRT project"))).toBeNull();
    expect(showMore(cardOf("FITS project"))).toBeNull();
    expect(showMore(cardOf("LONG project"))).not.toBeNull();
    // Nor is a Show less offered where nothing was shown more.
    expect(showLess(cardOf("SHRT project"))).toBeNull();
  });

  it("keeps Show less once expanded, though nothing is clamped then", async () => {
    await renderCards();
    const card = cardOf("LONG project");
    fireEvent.click(showMore(card)!);
    expect(card.querySelector(".prose-card")!.classList.contains("line-clamp-4")).toBe(false);
    relayout();
    expect(showLess(card)).not.toBeNull();
    expect(showMore(card)).toBeNull();

    // Even when the card widens until the whole description would fit.
    textWidth = 4000;
    relayout();
    expect(showLess(card)).not.toBeNull();

    // Collapsing measures again: at that width nothing is cut off.
    fireEvent.click(showLess(card)!);
    relayout();
    expect(showMore(card)).toBeNull();
    expect(showLess(card)).toBeNull();
  });

  it("follows the card's width", async () => {
    await renderCards();
    const fits = cardOf("FITS project");
    expect(showMore(fits)).toBeNull();

    // Narrower, FITS runs to eight lines and the clamp cuts it off.
    textWidth = 140;
    relayout();
    expect(showMore(fits)).not.toBeNull();
    expect(showMore(cardOf("SHRT project"))).toBeNull();

    // Wider again, it fits and Show more goes.
    textWidth = 280;
    relayout();
    expect(showMore(fits)).toBeNull();
    expect(showMore(cardOf("LONG project"))).not.toBeNull();
  });
});

describe("the project form", () => {
  it("creates a project with agent instructions separate from the description", async () => {
    await renderPage();
    fireEvent.click(screen.getByRole("button", { name: /New Project/ }));
    const form = screen.getByRole("form", { name: "New Project" });
    expect(mockApi.projects.get).not.toHaveBeenCalled();

    fireEvent.change(within(form).getByPlaceholderText("My Project"), { target: { value: "Billing" } });
    fireEvent.change(within(form).getByPlaceholderText("PRJ"), { target: { value: "bill" } });
    fireEvent.change(textarea(form), { target: { value: "What billing is." } });
    fireEvent.click(tab(form, /Agent instructions/));
    expect(textarea(form).value).toBe("");
    fireEvent.change(textarea(form), { target: { value: "Run the tests." } });
    // Going back shows the description untouched.
    fireEvent.click(tab(form, /Description/));
    expect(textarea(form).value).toBe("What billing is.");

    await act(async () => {
      fireEvent.click(within(form).getByRole("button", { name: "Create Project" }));
    });
    expect(mockApi.projects.create).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Billing",
        prefix: "BILL",
        description: "What billing is.",
        agentInstructions: "Run the tests.",
      }),
    );
  });

  it("loads an edited project's instructions and saves changes to them", async () => {
    await renderPage();
    const form = await openEdit("ACP project");
    expect(mockApi.projects.get).toHaveBeenCalledWith(WITH.id);

    fireEvent.click(tab(form, /Agent instructions/));
    expect(textarea(form).value).toBe("Run the tests before landing.");
    expect(textarea(form).readOnly).toBe(false);
    fireEvent.change(textarea(form), { target: { value: "Review every ticket." } });

    await act(async () => {
      fireEvent.click(within(form).getByRole("button", { name: "Save Changes" }));
    });
    expect(mockApi.projects.update).toHaveBeenCalledWith(
      WITH.id,
      expect.objectContaining({ description: "What ACP is.", agentInstructions: "Review every ticket." }),
    );
  });

  it("sends the instructions only when they were changed from the loaded ones", async () => {
    await renderPage();
    const save = async (form: HTMLElement) => {
      await act(async () => {
        fireEvent.click(within(form).getByRole("button", { name: "Save Changes" }));
      });
      return mockApi.projects.update.mock.calls.at(-1)![1];
    };

    // A save for something else leaves them out, so an agent's newer edit survives.
    let form = await openEdit("ACP project");
    const colour = [...form.querySelectorAll<HTMLButtonElement>("button[style]")].find(
      (b) => b.style.backgroundColor !== "rgb(59, 130, 246)",
    )!;
    fireEvent.click(colour);
    let sent = await save(form);
    expect(sent.color).not.toBe("#3b82f6");
    expect("agentInstructions" in sent).toBe(false);

    // Whitespace around them is no change: the store trims them anyway.
    form = await openEdit("ACP project");
    fireEvent.click(tab(form, /Agent instructions/));
    fireEvent.change(textarea(form), { target: { value: "  Run the tests before landing.\n\n" } });
    sent = await save(form);
    expect("agentInstructions" in sent).toBe(false);

    // A real change is sent, and so is clearing them.
    form = await openEdit("ACP project");
    fireEvent.click(tab(form, /Agent instructions/));
    fireEvent.change(textarea(form), { target: { value: "Review first." } });
    sent = await save(form);
    expect(sent.agentInstructions).toBe("Review first.");

    form = await openEdit("ACP project");
    fireEvent.click(tab(form, /Agent instructions/));
    fireEvent.change(textarea(form), { target: { value: "" } });
    sent = await save(form);
    expect(sent.agentInstructions).toBe("");
  });

  it("leaves the instructions alone when they could not be loaded", async () => {
    mockApi.projects.get.mockRejectedValue(new Error("offline"));
    await renderPage();
    const form = await openEdit("ACP project");

    fireEvent.click(tab(form, /Agent instructions/));
    expect(textarea(form).readOnly).toBe(true);
    expect(within(form).getByRole("alert").textContent).toMatch(/Couldn't load the agent instructions/);

    await act(async () => {
      fireEvent.click(within(form).getByRole("button", { name: "Save Changes" }));
    });
    const sent = mockApi.projects.update.mock.calls[0][1];
    expect(sent.description).toBe("What ACP is.");
    expect("agentInstructions" in sent).toBe(false);
  });

  it("keeps the instructions read-only until they arrive", async () => {
    let resolve: (p: Project) => void = () => {};
    mockApi.projects.get.mockReturnValue(new Promise<Project>((r) => (resolve = r)));
    await renderPage();
    fireEvent.click(screen.getByText("ACP project"));
    const form = screen.getByRole("form", { name: "Edit Project" });
    fireEvent.click(tab(form, /Agent instructions/));
    expect(textarea(form).readOnly).toBe(true);
    expect(textarea(form).placeholder).toBe("Loading agent instructions…");

    await act(async () => resolve({ ...WITH, agentInstructions: "Loaded." }));
    expect(textarea(form).readOnly).toBe(false);
    expect(textarea(form).value).toBe("Loaded.");
  });
});

describe("the Description / Agent instructions tabs", () => {
  it("follow the ARIA tablist pattern", async () => {
    await renderPage();
    const form = await openEdit("ACP project");
    const list = within(form).getByRole("tablist", { name: "Project text" });
    const [description, instructions] = within(list).getAllByRole("tab");
    const panel = within(form).getByRole("tabpanel");

    expect(description.textContent).toContain("Description");
    expect(instructions.textContent).toContain("Agent instructions");
    // The Agent instructions tab carries the Bot icon.
    expect(instructions.querySelector("svg.lucide-bot")).not.toBeNull();

    expect(description.getAttribute("aria-selected")).toBe("true");
    expect(instructions.getAttribute("aria-selected")).toBe("false");
    expect(description.tabIndex).toBe(0);
    expect(instructions.tabIndex).toBe(-1);
    expect(description.getAttribute("aria-controls")).toBe(panel.id);
    expect(instructions.getAttribute("aria-controls")).toBe(panel.id);
    expect(panel.getAttribute("aria-labelledby")).toBe(description.id);

    description.focus();
    fireEvent.keyDown(description, { key: "ArrowRight" });
    expect(instructions.getAttribute("aria-selected")).toBe("true");
    expect(description.getAttribute("aria-selected")).toBe("false");
    expect(instructions.tabIndex).toBe(0);
    expect(description.tabIndex).toBe(-1);
    expect(document.activeElement).toBe(instructions);
    expect(panel.getAttribute("aria-labelledby")).toBe(instructions.id);
    expect(textarea(form).value).toBe("Run the tests before landing.");

    // Arrows wrap around; Home and End go to the ends.
    fireEvent.keyDown(instructions, { key: "ArrowRight" });
    expect(description.getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(description);
    fireEvent.keyDown(description, { key: "ArrowLeft" });
    expect(instructions.getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(instructions, { key: "Home" });
    expect(description.getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(description, { key: "End" });
    expect(instructions.getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(instructions);
  });

  it("show the help text of the selected tab", async () => {
    await renderPage();
    const form = await openEdit("ACP project");
    const help = () => {
      const id = textarea(form).getAttribute("aria-describedby")!;
      return document.getElementById(id)!.textContent;
    };
    expect(help()).toBe("What the project is: its goals, scope and context. Shown as the project's summary.");
    fireEvent.click(tab(form, /Agent instructions/));
    expect(help()).toBe("How agents should work on this project's tickets. The board never acts on it.");
    fireEvent.click(tab(form, /Description/));
    expect(help()).toMatch(/^What the project is/);
  });

  it("put a blue dot on a tab whose field has text while the other tab is selected", async () => {
    await renderPage();
    const form = await openEdit("ACP project");
    const dot = (t: "description" | "instructions") => within(form).queryByTestId(`${t}-has-text`);

    // Description selected: Agent instructions has text, so it shows the dot.
    expect(dot("instructions")).not.toBeNull();
    expect(dot("description")).toBeNull();
    expect(tab(form, /Agent instructions/).textContent).toContain("has text");

    // Agent instructions selected: the description has text, so Description shows it.
    fireEvent.click(tab(form, /Agent instructions/));
    expect(dot("instructions")).toBeNull();
    expect(dot("description")).not.toBeNull();

    // Emptying the description takes its dot away.
    fireEvent.click(tab(form, /Description/));
    fireEvent.change(textarea(form), { target: { value: "" } });
    fireEvent.click(tab(form, /Agent instructions/));
    expect(dot("description")).toBeNull();
  });

  it("show no dot for a field that is only whitespace", async () => {
    await renderPage();
    const form = await openEdit("HOME project");
    const dot = (t: "description" | "instructions") => within(form).queryByTestId(`${t}-has-text`);

    fireEvent.click(tab(form, /Agent instructions/));
    fireEvent.change(textarea(form), { target: { value: "  \n\t " } });
    fireEvent.click(tab(form, /Description/));
    expect(dot("instructions")).toBeNull();
    fireEvent.change(textarea(form), { target: { value: "   " } });
    fireEvent.click(tab(form, /Agent instructions/));
    expect(dot("description")).toBeNull();
  });

  it("show no dot for a project without instructions", async () => {
    await renderPage();
    const form = await openEdit("HOME project");
    expect(within(form).queryByTestId("instructions-has-text")).toBeNull();
  });
});

describe("the project's entries on the form", () => {
  it("sit beside the form on a wide screen and under it on a narrow one", async () => {
    mockApi.entries.list.mockResolvedValue({
      entries: [
        { id: "e1", projectId: WITH.id, type: "decision", text: "Run started.", authorName: "bilal", createdAt: "2026-09-26T10:00:00Z" },
      ],
      total: 1,
      hasMore: false,
      agents: {},
    });
    await renderPage();
    const form = await openEdit("ACP project");
    const column = await screen.findByTestId("project-entries-column");
    await within(column).findByText("Run started.");
    expect(mockApi.entries.list.mock.calls[0][0]).toEqual({ projectId: WITH.id });

    // jsdom applies no media queries, so this checks the layout's contract:
    // the entries follow the form in the dialog's body, which stacks them
    // (and scrolls as a whole) until the lg breakpoint, where it becomes two
    // columns that each scroll on their own.
    const body = screen.getByTestId("project-dialog-body");
    expect([...body.children]).toEqual([form, column]);
    expect(body.className).toContain("overflow-y-auto");
    expect(body.className).toContain("lg:grid");
    expect(body.className).toContain("lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]");
    expect(body.className).toContain("lg:overflow-hidden");
    expect(form.className).toContain("lg:overflow-y-auto");
    expect(column.className).toContain("lg:overflow-y-auto");
    expect(column.className).toContain("border-t");
    expect(column.className).toContain("lg:border-l");
    // The note box is not part of the form: leaving a note never saves it.
    expect(within(form).queryByRole("button", { name: "Leave note" })).toBeNull();
    // The journal's free-text author and text box went with it.
    expect(screen.queryByRole("textbox", { name: "Author" })).toBeNull();
    expect(screen.queryByText("Journal")).toBeNull();
  });

  it("are not on the form for a new project", async () => {
    await renderPage();
    fireEvent.click(screen.getByRole("button", { name: /New Project/ }));
    expect(screen.getByRole("form", { name: "New Project" })).toBeTruthy();
    expect(screen.queryByTestId("project-entries-column")).toBeNull();
    expect(screen.getByTestId("project-dialog-body").className).not.toContain("lg:grid");
    expect(mockApi.entries.list).not.toHaveBeenCalled();
  });
});

describe("the project in the URL", () => {
  it("opens the project that ?project= names, by prefix in any case", async () => {
    window.history.replaceState(null, "", "/projects?project=acp");
    await renderPage();
    expect(await screen.findByRole("form", { name: "Edit Project" })).toBeTruthy();
    expect((screen.getByPlaceholderText("My Project") as HTMLInputElement).value).toBe("ACP project");
  });

  it("names the project a card opens, and drops it on close", async () => {
    await renderPage();
    await openEdit("HOME project");
    expect(new URLSearchParams(window.location.search).get("project")).toBe("HOME");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Close" }));
    });
    await waitFor(() => expect(screen.queryByRole("form", { name: "Edit Project" })).toBeNull());
    expect(new URLSearchParams(window.location.search).get("project")).toBeNull();
  });

  it("drops a ?project= that names no project", async () => {
    window.history.replaceState(null, "", "/projects?project=NOPE");
    await renderPage();
    await waitFor(() => expect(window.location.search).toBe(""));
    expect(screen.queryByRole("form", { name: "Edit Project" })).toBeNull();
  });
});
