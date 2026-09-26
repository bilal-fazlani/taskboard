// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { Project } from "../api/client";

// The Projects page with the API mocked: the card marker for agent
// instructions, and the form's Description / Agent instructions tabs.

const mockApi = vi.hoisted(() => ({
  projects: { list: vi.fn(), get: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
}));
vi.mock("../api/client", () => ({ api: mockApi }));
vi.mock("../hooks/useLiveRefresh", () => ({ useLiveRefresh: () => {} }));

import Projects from "./Projects";

const project = (prefix: string, extra: Partial<Project> = {}): Project => ({
  id: `p-${prefix}`,
  name: `${prefix} project`,
  prefix,
  description: `What ${prefix} is.`,
  icon: "",
  color: "#3b82f6",
  status: "active",
  createdAt: "",
  updatedAt: "",
  ...extra,
});

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
  render(<Projects />);
  await screen.findByText("ACP project");
}

// Opens the edit form from a card and waits for the fetched instructions.
async function openEdit(name: string) {
  await act(async () => {
    fireEvent.click(screen.getByText(name));
  });
  return screen.getByRole("heading", { name: "Edit Project" }).closest("form")!;
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
});

afterEach(() => {
  cleanup();
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
});

describe("a project card's Show more", () => {
  // At the starting 280px, 40 characters a line: SHORT takes one line, FITS
  // exactly the four the clamp shows, LONG ten.
  const SHORT = project("SHRT", { description: "Tracks the household chores." });
  const FITS = project("FITS", { description: "x".repeat(160) });
  const LONG = project("LONG", { description: "y".repeat(400) });

  async function renderCards() {
    mockApi.projects.list.mockResolvedValue([SHORT, FITS, LONG]);
    render(<Projects />);
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
    const form = screen.getByRole("heading", { name: "New Project" }).closest("form")!;
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
    const form = screen.getByRole("heading", { name: "Edit Project" }).closest("form")!;
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
