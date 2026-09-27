// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Entry, EntryAgent, Epic, Project } from "../api/client";

// An epic's and a project's entries, in the epic dialog and the project
// dialog, with the API mocked: no test reaches a real server.
const mockApi = vi.hoisted(() => ({
  epics: { update: vi.fn() },
  projects: { list: vi.fn(), get: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
  entries: { list: vi.fn(), createNote: vi.fn() },
  documents: {
    list: vi.fn(),
    get: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    create: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
    rawUrl: (id: string, rev: number) => `/api/documents/${id}/raw?rev=${rev}`,
    imageUrl: (id: string, rev: number) => `/api/documents/${id}/image?rev=${rev}`,
    thumbnailUrl: (id: string, rev: number) => `/api/documents/${id}/thumbnail?rev=${rev}`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

const live = vi.hoisted(() => ({ listeners: new Set<() => void>() }));
vi.mock("../hooks/useLiveRefresh", async () => {
  const { useEffect, useRef } = await import("react");
  return {
    useLiveRefresh: (onChange: () => void) => {
      const ref = useRef(onChange);
      useEffect(() => {
        ref.current = onChange;
      });
      useEffect(() => {
        const listener = () => ref.current();
        live.listeners.add(listener);
        return () => {
          live.listeners.delete(listener);
        };
      }, []);
    },
  };
});
const fireLive = () => act(async () => live.listeners.forEach((l) => l()));

import EpicModal from "./EpicModal";
import Projects from "../pages/Projects";

const agent = (id: string, role: string): EntryAgent => ({
  id,
  sessionId: "s1",
  role,
  model: "opus-5.5",
  provider: "anthropic",
  createdAt: "",
  lastSeenAt: "",
  session: {
    id: "s1",
    vendor: "claude_code",
    vendorSessionId: "3da2c294",
    machine: "bilal-mbp",
    resumeCommand: "claude --resume 3da2c294-37eb-4757-be78-0513c53efa33",
    createdAt: "",
  },
});
const AGENTS = { a0: agent("a0", "orchestrator"), a1: agent("a1", "implementer") };

const at = (minute: number) => `2026-09-27T10:${String(minute).padStart(2, "0")}:00Z`;

// The Agents epic as the mock shows it: an open note, 15 current decisions
// (the newest replaced an older one), 5 learnings and a handled note.
function epicEntries(): Entry[] {
  const decisions: Entry[] = Array.from({ length: 15 }, (_, i) => ({
    id: `d${i + 1}`,
    epicId: "e1",
    type: "decision",
    source: "agent",
    text: `Decision ${i + 1}.`,
    agentId: "a0",
    createdAt: at(20 + i),
  }));
  decisions[14] = { ...decisions[14], text: "A session owns its tickets.", replaces: "d0" };
  const learnings: Entry[] = Array.from({ length: 5 }, (_, i) => ({
    id: `l${i + 1}`,
    epicId: "e1",
    type: "learning",
    text: `Learning ${i + 1}.`,
    agentId: "a1",
    createdAt: at(10 + i),
  }));
  return [
    { id: "n1", epicId: "e1", type: "note", text: "Check the board shows waiting tickets.", authorName: "bilal", createdAt: at(50) },
    ...decisions.reverse(),
    ...learnings.reverse(),
    { id: "d0", epicId: "e1", type: "decision", source: "agent", text: "An agent owns its tickets.", agentId: "a0", replacedBy: "d15", createdAt: at(5) },
    { id: "n0", epicId: "e1", type: "note", text: "Use a temp DB.", authorName: "bilal", handledBy: "a1", handledAt: at(4), createdAt: at(1) },
  ];
}

let entries: Entry[];

const epic = {
  id: "e1", projectId: "p1", name: "Agents", description: "Agents as identities.", createdAt: "", updatedAt: "",
  counts: {}, total: 0, complete: false, lastActivityAt: null, documentCount: 0,
} as Epic;

function renderEpic() {
  return render(
    <BrowserRouter>
      <EpicModal epic={epic} epics={[epic]} projectPrefix="ACP" onClose={vi.fn()} onSaved={vi.fn()} />
    </BrowserRouter>,
  );
}

const dialog = () => screen.getByRole("dialog");
const section = (name: string) => within(dialog()).getByRole("region", { name: new RegExp(`^${name}`) });
const texts = (root: HTMLElement) =>
  within(root)
    .queryAllByTestId("entry")
    .map((el) => el.querySelector("[data-testid=entry-text]")?.textContent);
const entryCard = (id: string) => {
  const card = dialog().querySelector<HTMLElement>(`[data-entry-id="${id}"]`);
  if (!card) throw new Error(`no entry ${id} on screen`);
  return card;
};

beforeEach(() => {
  window.history.replaceState(null, "", "/epics?project=ACP&epic=Agents");
  entries = epicEntries();
  mockApi.documents.list.mockResolvedValue([]);
  mockApi.entries.list.mockImplementation(() =>
    Promise.resolve({ entries, total: entries.length, hasMore: false, agents: AGENTS }),
  );
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  window.history.replaceState(null, "", "/");
});

describe("the epic dialog's entries", () => {
  it("sit between the description and Documents, grouped by type: notes, decisions, learnings", async () => {
    renderEpic();
    await waitFor(() => entryCard("d15"));
    expect(mockApi.entries.list).toHaveBeenCalledWith({ epicId: "e1" }, expect.objectContaining({ includeReplaced: true }));
    const headings = within(dialog())
      .getAllByRole("heading", { level: 3 })
      .map((h) => h.childNodes[0]?.textContent);
    expect(headings).toEqual(["Notes", "Decisions", "Learnings", "Documents"]);
    // After the description field in the dialog's order.
    const description = within(dialog()).getByLabelText(/Description/);
    expect(description.compareDocumentPosition(section("Notes")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    expect(texts(section("Notes"))).toEqual(["Check the board shows waiting tickets."]);
    expect(within(section("Notes")).getByText("1 open")).toBeTruthy();
    expect(within(section("Notes")).getByText(/next agent to start a ticket in this epic/)).toBeTruthy();
    expect(within(section("Decisions")).getByText("15 current · 1 replaced")).toBeTruthy();
    expect(within(section("Learnings")).getByText("5")).toBeTruthy();
    // A handled note is folded away and names who handled it.
    expect(screen.queryByText("Use a temp DB.")).toBeNull();
    fireEvent.click(within(section("Notes")).getByRole("button", { name: /1 handled/ }));
    expect(within(entryCard("n0")).getByText(/handled by/)).toBeTruthy();
  });

  it("shows the 3 newest of each type, then pages the older ones", async () => {
    renderEpic();
    await waitFor(() => entryCard("d15"));
    expect(texts(section("Decisions"))).toEqual(["A session owns its tickets.", "Decision 14.", "Decision 13."]);
    expect(texts(section("Learnings"))).toEqual(["Learning 5.", "Learning 4.", "Learning 3."]);

    const more = within(section("Decisions")).getByRole("button", { name: "Show 12 older decisions" });
    fireEvent.click(more);
    expect(texts(section("Decisions"))).toHaveLength(13);
    fireEvent.click(within(section("Decisions")).getByRole("button", { name: "Show 2 older decisions" }));
    expect(texts(section("Decisions"))).toHaveLength(15);
    expect(within(section("Decisions")).queryByRole("button", { name: /older/ })).toBeNull();

    fireEvent.click(within(section("Learnings")).getByRole("button", { name: "Show 2 older learnings" }));
    expect(texts(section("Learnings"))).toEqual(["Learning 5.", "Learning 4.", "Learning 3.", "Learning 2.", "Learning 1."]);
  });

  it("tucks a replaced entry under the one that replaced it, struck through when opened", async () => {
    renderEpic();
    const card = await waitFor(() => entryCard("d15"));
    expect(screen.queryByText("An agent owns its tickets.")).toBeNull();
    fireEvent.click(within(card).getByRole("button", { name: /Replaces 1 earlier entry/ }));
    const old = within(card).getByTestId("replaced-entry");
    expect(within(old).getByText("An agent owns its tickets.").className).toContain("line-through");
    expect(card.textContent).toContain("orchestrator · opus-5.5");
    expect(within(card).getByTestId("session-chip").textContent).toContain("claude --resume 3da2c294");
  });

  it("leaves a note on the epic from the box under Notes", async () => {
    const note: Entry = { id: "n2", epicId: "e1", type: "note", text: "Aim for 100 ms.", authorName: "bilal", createdAt: at(59) };
    mockApi.entries.createNote.mockImplementation(() => {
      entries = [note, ...entries];
      return Promise.resolve(note);
    });
    renderEpic();
    await waitFor(() => entryCard("n1"));
    const box = within(section("Notes")).getByRole("textbox", { name: "Note" });
    expect(box.getAttribute("placeholder")).toContain("agents working in this epic");
    fireEvent.change(box, { target: { value: " Aim for 100 ms. " } });
    fireEvent.click(within(section("Notes")).getByRole("button", { name: "Leave note" }));
    await waitFor(() => expect(texts(section("Notes"))).toContain("Aim for 100 ms."));
    expect(mockApi.entries.createNote).toHaveBeenCalledWith({ epicId: "e1" }, { type: "note", text: "Aim for 100 ms." });
    expect(within(section("Notes")).getByText("2 open")).toBeTruthy();
  });

  it("challenges a decision: the box points at it, the note carries it, and the entry shows it", async () => {
    const challenge: Entry = {
      id: "n3", epicId: "e1", type: "note", text: "Sessions can't hold tickets.", authorName: "bilal", about: "d15", createdAt: at(58),
    };
    mockApi.entries.createNote.mockImplementation(() => {
      entries = [challenge, ...entries];
      return Promise.resolve(challenge);
    });
    renderEpic();
    const decision = await waitFor(() => entryCard("d15"));
    expect(decision.dataset.challenged).toBeUndefined();
    fireEvent.click(within(decision).getByRole("button", { name: /^Challenge decision/ }));
    const box = within(section("Notes")).getByRole("textbox", { name: "Note" });
    expect(document.activeElement).toBe(box);
    expect(screen.getByTestId("note-about").textContent).toContain("A session owns its tickets.");
    fireEvent.change(box, { target: { value: "Sessions can't hold tickets." } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() =>
      expect(mockApi.entries.createNote).toHaveBeenCalledWith(
        { epicId: "e1" },
        { type: "note", text: "Sessions can't hold tickets.", about: "d15" },
      ),
    );
    await waitFor(() => expect(entryCard("d15").dataset.challenged).toBe("true"));
    expect(within(entryCard("n3")).getByText(/Challenges: “A session owns its tickets.”/)).toBeTruthy();
    expect(screen.queryByTestId("note-about")).toBeNull();
  });

  it("opens scrolled to the entries from a link ending in #entries", async () => {
    // jsdom lays nothing out and doesn't scroll: the entries sit 300px below
    // the top of the dialog's scrolling body, and scrollTo keeps what is set.
    const scrolled = new WeakMap<Element, number>();
    Object.defineProperty(HTMLElement.prototype, "scrollTop", {
      configurable: true,
      get(this: HTMLElement) {
        return scrolled.get(this) ?? 0;
      },
    });
    Object.defineProperty(HTMLElement.prototype, "scrollTo", {
      configurable: true,
      value(this: HTMLElement, options: ScrollToOptions) {
        scrolled.set(this, options.top ?? 0);
      },
    });
    const rect = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockImplementation(function (this: HTMLElement) {
        const top = this.dataset.testid === "level-entries" ? 400 : 100;
        return { top } as DOMRect;
      });
    try {
      window.history.replaceState(null, "", "/epics?project=ACP&epic=Agents#entries");
      renderEpic();
      await waitFor(() => entryCard("d15"));
      // Once the entries have loaded, so there is something to scroll to.
      const body = screen.getByTestId("level-entries").parentElement!;
      expect(body.scrollTop).toBe(300);
      // Focus goes with it, not left in the Name field scrolled out of view.
      const root = screen.getByRole("region", { name: "Epic entries" });
      expect(document.activeElement).toBe(root);
      expect(root.tabIndex).toBe(-1);
      // Only once: a live refresh leaves the scroll and focus where the person put them.
      body.scrollTo({ top: 50 });
      const box = within(section("Notes")).getByRole("textbox", { name: "Note" });
      box.focus();
      await fireLive();
      expect(body.scrollTop).toBe(50);
      expect(document.activeElement).toBe(box);
    } finally {
      rect.mockRestore();
      delete (HTMLElement.prototype as unknown as Record<string, unknown>).scrollTop;
      delete (HTMLElement.prototype as unknown as Record<string, unknown>).scrollTo;
    }
  });

  it("stays at the top when opened without #entries", async () => {
    renderEpic();
    await waitFor(() => entryCard("d15"));
    const body = screen.getByTestId("level-entries").parentElement!;
    expect(body.scrollTop).toBe(0);
    // The Name field keeps its autofocus.
    expect(document.activeElement).toBe(within(dialog()).getByLabelText("Name"));
  });

  it("updates live: an agent's new entry shows up on a change event", async () => {
    renderEpic();
    await waitFor(() => entryCard("l5"));
    entries = [
      { id: "l6", epicId: "e1", type: "learning", text: "Seed agents through the store.", agentId: "a1", createdAt: at(59) },
      ...entries,
    ];
    await fireLive();
    await waitFor(() => expect(texts(section("Learnings"))[0]).toBe("Seed agents through the store."));
    expect(within(section("Learnings")).getByText("6")).toBeTruthy();
  });
});

describe("the project dialog's entries", () => {
  const project: Project = {
    id: "p1", name: "Agent control plane", prefix: "ACP", description: "", icon: "", color: "#3b82f6",
    status: "active", createdAt: "", updatedAt: "",
  };

  beforeEach(() => {
    mockApi.projects.list.mockResolvedValue([project]);
    mockApi.projects.get.mockResolvedValue({ ...project, agentInstructions: "" });
    entries = [
      { id: "pd2", projectId: "p1", type: "decision", source: "person", text: "The roadmap is ordered by the vision.", agentId: "a0", createdAt: at(30) },
      { id: "pd1", projectId: "p1", type: "decision", source: "agent", text: "Chats are a log, the board is state. Rejected: a transcript archive.", agentId: "a0", createdAt: at(29) },
      ...Array.from({ length: 4 }, (_, i): Entry => ({
        id: `pl${i}`, projectId: "p1", type: "learning", text: `Project learning ${i}.`, agentId: "a1", createdAt: at(10 + i),
      })).reverse(),
    ];
  });

  function renderProjects(search: string) {
    window.history.replaceState(null, "", `/projects${search}`);
    return render(<Projects />, { wrapper: BrowserRouter });
  }

  it("open from ?project=, in place of the Journal, grouped and paged", async () => {
    renderProjects("?project=ACP#entries");
    const column = await screen.findByTestId("project-entries-column");
    await within(column).findByText("The roadmap is ordered by the vision.");
    expect(mockApi.entries.list).toHaveBeenCalledWith({ projectId: "p1" }, expect.objectContaining({ includeReplaced: true }));
    const headings = within(column)
      .getAllByRole("heading", { level: 3 })
      .map((h) => h.childNodes[0]?.textContent);
    expect(headings).toEqual(["Notes", "Decisions", "Learnings"]);
    const decisions = within(column).getByRole("region", { name: /^Decisions/ });
    expect(within(decisions).getByText("from you")).toBeTruthy();
    expect(within(decisions).getByText("Rejected: a transcript archive.")).toBeTruthy();
    const learnings = within(column).getByRole("region", { name: /^Learnings/ });
    expect(texts(learnings)).toEqual(["Project learning 3.", "Project learning 2.", "Project learning 1."]);
    fireEvent.click(within(learnings).getByRole("button", { name: "Show 1 older learning" }));
    expect(texts(learnings)).toHaveLength(4);
    expect(screen.queryByText("Journal")).toBeNull();
  });

  it("update live: a new project decision shows up on a change event", async () => {
    renderProjects("?project=ACP");
    const column = await screen.findByTestId("project-entries-column");
    await within(column).findByText("The roadmap is ordered by the vision.");
    entries = [
      { id: "pd3", projectId: "p1", type: "decision", source: "agent", text: "Adapters are optional.", agentId: "a0", createdAt: at(45) },
      ...entries,
    ];
    await fireLive();
    const decisions = within(column).getByRole("region", { name: /^Decisions/ });
    await waitFor(() => expect(texts(decisions)[0]).toBe("Adapters are optional."));
    expect(within(decisions).getByText("3 current")).toBeTruthy();
  });

  const nameField = () =>
    within(screen.getByRole("dialog", { name: "Edit Project" })).getByPlaceholderText("My Project") as HTMLInputElement;

  it("stay open, with what was typed, when a live refresh can't read the list", async () => {
    renderProjects("?project=ACP");
    await screen.findByTestId("project-entries-column");
    fireEvent.change(nameField(), { target: { value: "Half-typed name" } });

    mockApi.projects.list.mockRejectedValue(new Error("API error 500: boom"));
    await fireLive();
    await act(async () => {});
    expect(nameField().value).toBe("Half-typed name");
    expect(new URLSearchParams(window.location.search).get("project")).toBe("ACP");

    // A read that succeeds and still has it changes nothing either.
    mockApi.projects.list.mockResolvedValue([project]);
    await fireLive();
    expect(nameField().value).toBe("Half-typed name");

    // Only a read that succeeds without it closes the dialog.
    mockApi.projects.list.mockResolvedValue([]);
    await fireLive();
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit Project" })).toBeNull());
    expect(new URLSearchParams(window.location.search).get("project")).toBeNull();
  });

  it("follow the open project by id when its prefix is renamed elsewhere", async () => {
    renderProjects("?project=ACP");
    await screen.findByTestId("project-entries-column");
    fireEvent.change(nameField(), { target: { value: "Half-typed name" } });

    mockApi.projects.list.mockResolvedValue([{ ...project, prefix: "ACX" }]);
    await fireLive();
    await waitFor(() => expect(new URLSearchParams(window.location.search).get("project")).toBe("ACX"));
    expect(nameField().value).toBe("Half-typed name");
  });

  it("take a note on the project, and a challenge of its learning", async () => {
    mockApi.entries.createNote.mockResolvedValue({ id: "pn1", projectId: "p1", type: "note", text: "x", createdAt: at(40) });
    renderProjects("?project=ACP");
    const column = await screen.findByTestId("project-entries-column");
    await within(column).findByText("Project learning 3.");
    const box = within(column).getByRole("textbox", { name: "Note" });
    expect(box.getAttribute("placeholder")).toBe("Leave a note for any agent working in this project.");
    fireEvent.click(within(column).getByRole("button", { name: /^Challenge learning: “Project learning 3/ }));
    fireEvent.change(box, { target: { value: "Not any more." } });
    fireEvent.click(within(column).getByRole("button", { name: "Leave note" }));
    await waitFor(() =>
      expect(mockApi.entries.createNote).toHaveBeenCalledWith(
        { projectId: "p1" },
        { type: "note", text: "Not any more.", about: "pl3" },
      ),
    );
  });
});
